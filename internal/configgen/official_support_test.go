package configgen

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefaultOfficialSupportStatePathUsesOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "official-support.yaml")
	t.Setenv("MIHOMO_OFFICIAL_SUPPORT_STATE_PATH", override)

	path, err := DefaultOfficialSupportStatePath()
	if err != nil {
		t.Fatal(err)
	}
	if path != override {
		t.Fatalf("DefaultOfficialSupportStatePath() = %q, want %q", path, override)
	}
}

func TestDefaultOfficialSupportStatePathUsesPlatformDefault(t *testing.T) {
	t.Setenv("MIHOMO_OFFICIAL_SUPPORT_STATE_PATH", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state-home"))

	path, err := DefaultOfficialSupportStatePath()
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(home, "state-home", "clash", "official-support.yaml")
	if runtime.GOOS == "darwin" {
		want = filepath.Join(home, "Library", "Application Support", "clash", "official-support.yaml")
	}
	if path != want {
		t.Fatalf("DefaultOfficialSupportStatePath() = %q, want %q", path, want)
	}
}

func TestSaveAndLoadOfficialSupportState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "official-support.yaml")
	state := &OfficialSupportState{
		Services: map[string]OfficialSupportServiceState{
			"openai": {
				Service:     "openai",
				SourceURL:   "https://example.com/openai",
				Supported:   []string{"US", "SG"},
				UpdatedAt:   "2026-06-07T00:00:00Z",
				Description: "seed data",
			},
		},
	}

	if err := SaveOfficialSupportState(path, state); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadOfficialSupportState(path)
	if err != nil {
		t.Fatal(err)
	}
	service, ok := loaded.Services["openai"]
	if !ok {
		t.Fatal("missing openai state")
	}
	if service.SourceURL != "https://example.com/openai" {
		t.Fatalf("source url = %q", service.SourceURL)
	}

	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".official-support-*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("unexpected temp files left behind: %v", matches)
	}
}

func TestLoadOfficialSupportStateMissingReturnsEmpty(t *testing.T) {
	state, err := LoadOfficialSupportState(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Services) != 0 {
		t.Fatalf("services = %#v, want empty", state.Services)
	}
}

func TestBuildOfficialSupportStateFromConfig(t *testing.T) {
	now := time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC)

	cfg := &GenerationConfig{
		OfficialSupport: map[string]OfficialSupportConfigEntry{
			"openai": {
				SourceURL:   "https://example.com/openai",
				Description: "seeded",
				Supported:   []string{" us ", "SG", "us"},
			},
			"bitget": {
				SourceURL:  "https://example.com/bitget",
				Prohibited: []string{" us ", "HK", "us"},
			},
			"gemini": {
				SourceURL: "https://example.com/gemini",
				Supported: []string{"TW"},
			},
		},
	}

	state := BuildOfficialSupportStateFromConfig(cfg, now)
	openai, ok := state.Services["openai"]
	if !ok {
		t.Fatal("missing openai service")
	}
	if openai.UpdatedAt != now.Format(time.RFC3339) {
		t.Fatalf("openai updated-at = %q", openai.UpdatedAt)
	}
	if openai.SourceURL != "https://example.com/openai" {
		t.Fatalf("openai source-url = %q", openai.SourceURL)
	}
	if got, want := strings.Join(openai.Supported, ","), "SG,US"; got != want {
		t.Fatalf("openai supported = %q, want %q", got, want)
	}
	bitget, ok := state.Services["bitget"]
	if !ok {
		t.Fatal("missing bitget service")
	}
	if got, want := strings.Join(bitget.Prohibited, ","), "HK,US"; got != want {
		t.Fatalf("bitget prohibited = %q, want %q", got, want)
	}
	gemini, ok := state.Services["gemini"]
	if !ok {
		t.Fatal("missing gemini service")
	}
	if got, want := strings.Join(gemini.Supported, ","), "TW"; got != want {
		t.Fatalf("gemini supported = %q, want %q", got, want)
	}
}

func TestNormalizeCountryCodes(t *testing.T) {
	got := normalizeCountryCodes([]string{" us ", "SG", "us", "", "sg"})
	want := []string{"SG", "US"}
	if len(got) != len(want) {
		t.Fatalf("normalizeCountryCodes() len = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("normalizeCountryCodes()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestSaveOfficialSupportStateCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state", "official-support.yaml")
	if err := SaveOfficialSupportState(path, &OfficialSupportState{Services: map[string]OfficialSupportServiceState{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
