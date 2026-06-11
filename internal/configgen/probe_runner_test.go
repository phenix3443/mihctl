package configgen

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestRunProbeNoopsWhenNoProbeEnabledGroups(t *testing.T) {
	repoRoot := t.TempDir()
	providersDir := filepath.Join(repoRoot, "providers")
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(providersDir, "good.yaml"), []byte("proxies: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "bad.yaml"), []byte("trojan://unsupported\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &GenerationConfig{
		Probe: ProbeConfig{
			Services: map[string]ProbeServiceSpec{},
		},
		ProxyProviders: map[string]ProxyProviderSpec{
			"good": {Path: "./providers/good.yaml"},
			"bad":  {Path: "./providers/bad.yaml"},
		},
		ServiceGroups: map[string]ServiceGroupSpec{},
	}

	statePath := filepath.Join(t.TempDir(), "probe-results.yaml")
	if err := RunProbe(repoRoot, statePath, cfg, ProbeScope{Providers: []string{"good"}}); err != nil {
		t.Fatal(err)
	}

	state, err := LoadProbeState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Providers["bad"]; ok {
		t.Fatal("unexpected bad provider state")
	}
	if len(state.Providers) != 0 {
		t.Fatalf("providers state = %#v, want empty", state.Providers)
	}
}

func TestResolveTargetGroupsSkipsProbeNoneGroups(t *testing.T) {
	cfg := &GenerationConfig{
		GroupOrder: []string{"stable", "github"},
		ServiceGroups: map[string]ServiceGroupSpec{
			"stable": {Probe: "none"},
			"github": {Probe: "github"},
		},
	}

	got, err := resolveTargetGroups(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"github"}) {
		t.Fatalf("resolveTargetGroups() = %#v, want %#v", got, []string{"github"})
	}
}

func TestResolveTargetGroupsRejectsProbeNoneTarget(t *testing.T) {
	cfg := &GenerationConfig{
		GroupOrder: []string{"stable", "github"},
		ServiceGroups: map[string]ServiceGroupSpec{
			"stable": {Probe: "none"},
			"github": {Probe: "github"},
		},
	}

	_, err := resolveTargetGroups(cfg, []string{"stable"})
	if err == nil || !strings.Contains(err.Error(), "probe disabled") {
		t.Fatalf("resolveTargetGroups(stable) error = %v, want probe disabled", err)
	}
}

func TestResolveTargetGroupsRejectsUnknownTarget(t *testing.T) {
	cfg := &GenerationConfig{
		GroupOrder: []string{"github"},
		ServiceGroups: map[string]ServiceGroupSpec{
			"github": {Probe: "github"},
		},
	}

	_, err := resolveTargetGroups(cfg, []string{"missing"})
	if err == nil || !strings.Contains(err.Error(), "unknown probe target") {
		t.Fatalf("resolveTargetGroups(missing) error = %v, want unknown target", err)
	}
}

func TestBuildMihomoProbeConfigUsesRuntimeConfig(t *testing.T) {
	payload := buildMihomoProbeConfig(ProviderSnapshot{Name: "demo"}, 19090, 17890, defaultProbeRuntimeConfig)

	var decoded map[string]any
	if err := yaml.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["mode"] != "rule" {
		t.Fatalf("mode = %#v, want rule", decoded["mode"])
	}
	if decoded["log-level"] != "silent" {
		t.Fatalf("log-level = %#v, want silent", decoded["log-level"])
	}
	if decoded["allow-lan"] != false {
		t.Fatalf("allow-lan = %#v, want false", decoded["allow-lan"])
	}
	if decoded["external-controller"] != "127.0.0.1:19090" {
		t.Fatalf("external-controller = %#v", decoded["external-controller"])
	}
	if decoded["mixed-port"] != 17890 {
		t.Fatalf("mixed-port = %#v", decoded["mixed-port"])
	}
	dns, ok := decoded["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns = %#v", decoded["dns"])
	}
	if dns["enable"] != false {
		t.Fatalf("dns.enable = %#v, want false", dns["enable"])
	}
	rules, ok := decoded["rules"].([]any)
	if !ok || len(rules) != 1 || rules[0] != "MATCH,__probe__" {
		t.Fatalf("rules = %#v", decoded["rules"])
	}
	groups, ok := decoded["proxy-groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("proxy-groups = %#v", decoded["proxy-groups"])
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("probe group = %#v", groups[0])
	}
	if group["name"] != "__probe__" {
		t.Fatalf("probe group name = %#v", group["name"])
	}
}

func TestRunProbeProbesAllCandidatesWithoutLatencyBaseline(t *testing.T) {
	originalStartMihomoProbeRuntimeFunc := startMihomoProbeRuntimeFunc
	originalRuntimeHTTPRequestViaNodeFunc := runtimeHTTPRequestViaNodeFunc
	t.Cleanup(func() {
		startMihomoProbeRuntimeFunc = originalStartMihomoProbeRuntimeFunc
		runtimeHTTPRequestViaNodeFunc = originalRuntimeHTTPRequestViaNodeFunc
	})

	startMihomoProbeRuntimeFunc = func(snapshot ProviderSnapshot) (*mihomoProbeRuntime, error) {
		return &mihomoProbeRuntime{
			availableNodes: map[string]struct{}{
				"node-fast": {},
			},
		}, nil
	}
	runtimeHTTPRequestViaNodeFunc = func(runtime *mihomoProbeRuntime, nodeName, method, requestURL string, headers map[string]string, body io.Reader, timeout time.Duration) (*http.Response, []byte, error) {
		if nodeName != "node-fast" {
			t.Fatalf("unexpected runtime request for %s", nodeName)
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil, nil
	}

	repoRoot := t.TempDir()
	providersDir := filepath.Join(repoRoot, "providers")
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "demo.yaml"), []byte(`
proxies:
  - name: node-fast
    type: direct
  - name: node-missing
    type: direct
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &GenerationConfig{
		Probe: ProbeConfig{
			Services: map[string]ProbeServiceSpec{
				"github": {URI: "https://github.com/"},
			},
		},
		ProxyProviders: map[string]ProxyProviderSpec{
			"demo": {Path: "./providers/demo.yaml"},
		},
		ServiceGroups: map[string]ServiceGroupSpec{
			"github": {
				Probe: "github",
				Profiles: map[string]ServiceGroupProfileSpec{
					"k3s": {Providers: []string{"demo"}},
				},
			},
		},
		ProfileOrder: []string{"k3s"},
		Profiles: map[string]ProfileSpec{
			"k3s": {OS: "linux"},
		},
	}

	statePath := filepath.Join(t.TempDir(), "probe-results.yaml")
	err := RunProbe(repoRoot, statePath, cfg, ProbeScope{
		Providers: []string{"demo"},
		Services:  []string{"github"},
		Mode:      ProbeModeService,
	})
	if err != nil {
		t.Fatalf("service probe should probe all candidates directly: %v", err)
	}

	updatedState, err := LoadProbeState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	groupState, ok := updatedState.Groups[groupStateKey("k3s", "github")]
	if !ok {
		t.Fatal("missing group probe state")
	}
	if len(groupState.Nodes) != 1 {
		t.Fatalf("group nodes = %d, want 1", len(groupState.Nodes))
	}
	if _, ok := groupState.Nodes["node-fast"]; !ok {
		t.Fatal("expected node-fast to be selected")
	}
	if _, ok := groupState.Nodes["node-missing"]; ok {
		t.Fatal("unexpected node-missing selection")
	}

	summary, err := SummarizeProbeRun(repoRoot, statePath, cfg, ProbeScope{
		Providers: []string{"demo"},
		Services:  []string{"github"},
		Mode:      ProbeModeService,
	})
	if err != nil {
		t.Fatalf("summarize group probe run: %v", err)
	}
	if len(summary.Services) != 1 {
		t.Fatalf("summary services = %d, want 1", len(summary.Services))
	}
	service := summary.Services[0]
	if service.Candidates != 2 || service.Success != 1 || service.Failed != 1 || service.Skipped != 0 {
		t.Fatalf("summary = %#v, want candidates=2 success=1 failed=1 skipped=0", service)
	}
}

func TestRunProbeKeepsAllSuccessfulServiceNodes(t *testing.T) {
	originalStartMihomoProbeRuntimeFunc := startMihomoProbeRuntimeFunc
	originalRuntimeHTTPRequestViaNodeFunc := runtimeHTTPRequestViaNodeFunc
	t.Cleanup(func() {
		startMihomoProbeRuntimeFunc = originalStartMihomoProbeRuntimeFunc
		runtimeHTTPRequestViaNodeFunc = originalRuntimeHTTPRequestViaNodeFunc
	})

	startMihomoProbeRuntimeFunc = func(snapshot ProviderSnapshot) (*mihomoProbeRuntime, error) {
		return &mihomoProbeRuntime{
			availableNodes: map[string]struct{}{
				"node-fast": {},
				"node-mid":  {},
				"node-slow": {},
			},
		}, nil
	}
	runtimeHTTPRequestViaNodeFunc = func(runtime *mihomoProbeRuntime, nodeName, method, requestURL string, headers map[string]string, body io.Reader, timeout time.Duration) (*http.Response, []byte, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil, nil
	}

	repoRoot := t.TempDir()
	providersDir := filepath.Join(repoRoot, "providers")
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "demo.yaml"), []byte(`
proxies:
  - name: node-fast
    type: direct
  - name: node-mid
    type: direct
  - name: node-slow
    type: direct
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &GenerationConfig{
		Probe: ProbeConfig{
			Services: map[string]ProbeServiceSpec{
				"github": {URI: "https://github.com/"},
			},
		},
		ProxyProviders: map[string]ProxyProviderSpec{
			"demo": {Path: "./providers/demo.yaml"},
		},
		ServiceGroups: map[string]ServiceGroupSpec{
			"github": {
				Probe: "github",
				Profiles: map[string]ServiceGroupProfileSpec{
					"k3s": {Providers: []string{"demo"}},
				},
			},
		},
		ProfileOrder: []string{"k3s"},
		Profiles: map[string]ProfileSpec{
			"k3s": {OS: "linux"},
		},
	}

	statePath := filepath.Join(t.TempDir(), "probe-results.yaml")
	if err := RunProbe(repoRoot, statePath, cfg, ProbeScope{
		Providers: []string{"demo"},
		Services:  []string{"github"},
		Mode:      ProbeModeService,
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadProbeState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	githubDigest := probeServiceDigest(cfg.Probe.Services["github"])
	groupState, ok := loaded.LookupGroup("k3s", "github", groupProbeDigest("k3s", "github", cfg.ServiceGroups["github"], cfg.ServiceGroups["github"].Profiles["k3s"], githubDigest))
	if !ok {
		t.Fatal("expected github group state")
	}
	if len(groupState.Nodes) != 3 {
		t.Fatalf("expected all successful nodes in group state, got %#v", groupState.Nodes)
	}
	for _, nodeName := range []string{"node-fast", "node-mid", "node-slow"} {
		if _, ok := groupState.Nodes[nodeName]; !ok {
			t.Fatalf("expected %s in group state, got %#v", nodeName, groupState.Nodes)
		}
	}
}

func TestRunProbeReportsProgressForAllCandidates(t *testing.T) {
	originalStartMihomoProbeRuntimeFunc := startMihomoProbeRuntimeFunc
	originalRuntimeHTTPRequestViaNodeFunc := runtimeHTTPRequestViaNodeFunc
	restoreReporter := SetProbeProgressReporter(nil)
	t.Cleanup(func() {
		startMihomoProbeRuntimeFunc = originalStartMihomoProbeRuntimeFunc
		runtimeHTTPRequestViaNodeFunc = originalRuntimeHTTPRequestViaNodeFunc
		restoreReporter()
	})

	startMihomoProbeRuntimeFunc = func(snapshot ProviderSnapshot) (*mihomoProbeRuntime, error) {
		return &mihomoProbeRuntime{
			availableNodes: map[string]struct{}{
				"node-fast": {},
				"node-mid":  {},
				"node-slow": {},
			},
		}, nil
	}
	runtimeHTTPRequestViaNodeFunc = func(runtime *mihomoProbeRuntime, nodeName, method, requestURL string, headers map[string]string, body io.Reader, timeout time.Duration) (*http.Response, []byte, error) {
		if nodeName == "node-fast" {
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil, nil
		}
		return nil, nil, fmt.Errorf("timeout")
	}

	events := []ProbeProgressEvent{}
	restoreReporter = SetProbeProgressReporter(func(event ProbeProgressEvent) {
		events = append(events, event)
	})

	repoRoot := t.TempDir()
	providersDir := filepath.Join(repoRoot, "providers")
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "demo.yaml"), []byte(`
proxies:
  - name: node-fast
    type: direct
  - name: node-mid
    type: direct
  - name: node-slow
    type: direct
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &GenerationConfig{
		Probe: ProbeConfig{
			Services: map[string]ProbeServiceSpec{
				"github": {URI: "https://github.com/"},
			},
		},
		ProxyProviders: map[string]ProxyProviderSpec{
			"demo": {Path: "./providers/demo.yaml"},
		},
		ServiceGroups: map[string]ServiceGroupSpec{
			"github": {
				Probe: "github",
				Profiles: map[string]ServiceGroupProfileSpec{
					"k3s": {Providers: []string{"demo"}},
				},
			},
		},
		ProfileOrder: []string{"k3s"},
		Profiles: map[string]ProfileSpec{
			"k3s": {OS: "linux"},
		},
	}

	statePath := filepath.Join(t.TempDir(), "probe-results.yaml")
	if err := RunProbe(repoRoot, statePath, cfg, ProbeScope{
		Providers: []string{"demo"},
		Services:  []string{"github"},
		Mode:      ProbeModeService,
	}); err != nil {
		t.Fatal(err)
	}

	if len(events) < 2 {
		t.Fatalf("expected multiple progress events, got %d", len(events))
	}
	last := events[len(events)-1]
	if last.Completed != 3 || last.Total != 3 || last.Success != 1 || last.Failed != 2 || last.Skipped != 0 {
		t.Fatalf("last progress event = %#v", last)
	}
}

func TestSummarizeProbeRunFailsWhenFreshGroupStateMissing(t *testing.T) {
	repoRoot := t.TempDir()
	providersDir := filepath.Join(repoRoot, "providers")
	if err := os.MkdirAll(providersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providersDir, "demo.yaml"), []byte(`
proxies:
  - name: node-fast
    type: direct
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &GenerationConfig{
		Probe: ProbeConfig{
			Services: map[string]ProbeServiceSpec{
				"github": {URI: "https://github.com/"},
			},
		},
		ProxyProviders: map[string]ProxyProviderSpec{
			"demo": {Path: "./providers/demo.yaml"},
		},
		ServiceGroups: map[string]ServiceGroupSpec{
			"github": {
				Probe: "github",
				Profiles: map[string]ServiceGroupProfileSpec{
					"k3s": {Providers: []string{"demo"}},
				},
			},
		},
		ProfileOrder: []string{"k3s"},
		Profiles: map[string]ProfileSpec{
			"k3s": {OS: "linux"},
		},
	}

	statePath := filepath.Join(t.TempDir(), "probe-results.yaml")
	if err := SaveProbeState(statePath, &ProbeState{Providers: map[string]ProviderProbeState{}}); err != nil {
		t.Fatal(err)
	}

	_, err := SummarizeProbeRun(repoRoot, statePath, cfg, ProbeScope{
		Providers: []string{"demo"},
		Services:  []string{"github"},
		Mode:      ProbeModeService,
	})
	if err == nil {
		t.Fatal("expected summarize to fail without fresh group state")
	}
	if got := err.Error(); got != "missing fresh group probe state for github on k3s (probe=github)" {
		t.Fatalf("unexpected error: %s", got)
	}
}
