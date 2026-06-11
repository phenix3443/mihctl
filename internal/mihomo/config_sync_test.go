package mihomo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncProvidersDecisionForConfigSync(t *testing.T) {
	if syncProvidersWithConfigSync() {
		t.Fatal("config sync should not sync providers")
	}
}

func TestSyncProvidersDecisionForInstall(t *testing.T) {
	if !syncProvidersWithInstall() {
		t.Fatal("install should sync providers")
	}
}

func TestResolveSyncProfileUsesExplicitProfileFirst(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "local", OS: "macos"},
			{Name: "clash-verge", OS: "macos"},
		},
	}

	profile, err := env.resolveDarwinSyncProfile("clash-verge", "verge")
	if err != nil {
		t.Fatalf("resolveDarwinSyncProfile(clash-verge, verge) error = %v", err)
	}
	if profile.Name != "clash-verge" {
		t.Fatalf("profile name = %q, want clash-verge", profile.Name)
	}
}

func TestResolveLinuxSyncProfileUsesAutoSelectionWhenProfileEmpty(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "k3s", OS: "linux"},
		},
	}

	profile, err := env.resolveLinuxSyncProfile("")
	if err != nil {
		t.Fatalf("resolveLinuxSyncProfile(\"\") error = %v", err)
	}
	if profile.Name != "k3s" {
		t.Fatalf("profile name = %q, want k3s", profile.Name)
	}
}

func TestResolveDarwinSyncProfileUsesAutoSelectionWhenProfileEmpty(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "local", OS: "macos"},
			{Name: "clash-verge", OS: "macos"},
		},
	}

	profile, err := env.resolveDarwinSyncProfile("", "verge")
	if err != nil {
		t.Fatalf("resolveDarwinSyncProfile(\"\", verge) error = %v", err)
	}
	if profile.Name != "clash-verge" {
		t.Fatalf("profile name = %q, want clash-verge", profile.Name)
	}
}

func TestValidateDarwinSyncProfileRejectsStandaloneProfileForVergeTarget(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "local", OS: "macos"},
			{Name: "clash-verge", OS: "macos"},
		},
	}

	if err := env.validateDarwinSyncProfile(RuntimeProfile{Name: "local", OS: "macos"}, "verge"); err == nil {
		t.Fatal("expected standalone profile for verge target to fail")
	}
}

func TestValidateDarwinSyncProfileRejectsVergeProfileForStandaloneTarget(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "local", OS: "macos"},
			{Name: "clash-verge", OS: "macos"},
		},
	}

	if err := env.validateDarwinSyncProfile(RuntimeProfile{Name: "clash-verge", OS: "macos"}, "standalone"); err == nil {
		t.Fatal("expected verge profile for standalone target to fail")
	}
}

func TestValidateDarwinSyncProfileAllowsMatchingTarget(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "local", OS: "macos"},
			{Name: "clash-verge", OS: "macos"},
		},
	}

	if err := env.validateDarwinSyncProfile(RuntimeProfile{Name: "local", OS: "macos"}, "standalone"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := env.validateDarwinSyncProfile(RuntimeProfile{Name: "clash-verge", OS: "macos"}, "verge"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDarwinSyncProfileAllowsCustomShapeMatchedProfiles(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "primary", OS: "macos"},
			{Name: "work-verge", OS: "macos"},
		},
	}

	if err := env.validateDarwinSyncProfile(RuntimeProfile{Name: "primary", OS: "macos"}, "standalone"); err != nil {
		t.Fatalf("unexpected standalone error: %v", err)
	}
	if err := env.validateDarwinSyncProfile(RuntimeProfile{Name: "work-verge", OS: "macos"}, "verge"); err != nil {
		t.Fatalf("unexpected verge error: %v", err)
	}
}

func TestCaptureDarwinVergeReloadInfoFallsBackToGeneratedConfig(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.yaml")
	generatedPath := filepath.Join(dir, "generated.yaml")

	if err := os.WriteFile(targetPath, []byte("secret: only-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generatedPath, []byte("external-controller: 0.0.0.0:9090\nsecret: demo-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := captureDarwinVergeReloadInfo(targetPath, generatedPath)
	if err != nil {
		t.Fatalf("captureDarwinVergeReloadInfo error = %v", err)
	}
	if info.BaseURL != "http://127.0.0.1:9090" {
		t.Fatalf("base url = %q, want http://127.0.0.1:9090", info.BaseURL)
	}
	if info.Secret != "demo-secret" {
		t.Fatalf("secret = %q, want demo-secret", info.Secret)
	}
}
