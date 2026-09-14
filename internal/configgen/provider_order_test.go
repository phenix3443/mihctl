package configgen

import (
	"os"
	"path/filepath"
	"testing"
)

// select 组默认选 use 的第一个节点。use 默认按 proxy-providers 的键顺序排，
// 想让某个组换首选又不牵动其他组，就只能按组保留 profile 里写的顺序。
func TestRuntimeGroupProvidersPreserveProfileOrderWhenRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte(`
profiles:
  k3s:
    os: linux
proxy-providers:
  alpha:
    type: http
    url: https://example.com/alpha
    path: ./providers/alpha.yaml
  beta:
    type: http
    url: https://example.com/beta
    path: ./providers/beta.yaml
  gamma:
    type: http
    url: https://example.com/gamma
    path: ./providers/gamma.yaml
service-groups:
  ordered:
    type: select
    profiles:
      k3s:
        providers: [gamma, alpha]
        preserve-provider-order: true
  default:
    type: select
    profiles:
      k3s:
        providers: [gamma, alpha]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadGenerationConfig(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		group string
		want  []string
	}{
		{"ordered", []string{"gamma", "alpha"}},
		{"default", []string{"alpha", "gamma"}},
	} {
		spec := cfg.ServiceGroups[tc.group]
		profile, ok := resolveServiceGroupProfile(spec, "k3s")
		if !ok {
			t.Fatalf("%s: k3s profile not resolved", tc.group)
		}
		got, _, _, err := buildRuntimeGroupProvidersAndFilters(profile, spec, cfg)
		if err != nil {
			t.Fatal(err)
		}
		assertStringSlice(t, got, tc.want)
	}
}

func TestPreserveProviderOrderRejectsUnknownProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte(`
profiles:
  k3s:
    os: linux
proxy-providers:
  alpha:
    type: http
    url: https://example.com/alpha
    path: ./providers/alpha.yaml
service-groups:
  ordered:
    type: select
    profiles:
      k3s:
        providers: [alpah]
        preserve-provider-order: true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadGenerationConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	spec := cfg.ServiceGroups["ordered"]
	profile, _ := resolveServiceGroupProfile(spec, "k3s")
	if _, _, _, err := buildRuntimeGroupProvidersAndFilters(profile, spec, cfg); err == nil {
		t.Fatal("expected error for unknown provider name under preserve-provider-order, got nil")
	}
}
