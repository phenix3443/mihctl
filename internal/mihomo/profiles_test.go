package mihomo

import "testing"

func TestAutoSyncProfileUsesNamedMacOSTargetProfiles(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "k3s", OS: "linux"},
			{Name: "local", OS: "macos"},
			{Name: "clash-verge", OS: "macos"},
		},
	}

	profile, err := env.autoSyncProfile("standalone")
	if err != nil {
		t.Fatalf("autoSyncProfile(standalone) error = %v", err)
	}
	if profile.Name != "local" {
		t.Fatalf("profile name = %q, want local", profile.Name)
	}

	profile, err = env.autoSyncProfile("verge")
	if err != nil {
		t.Fatalf("autoSyncProfile(verge) error = %v", err)
	}
	if profile.Name != "clash-verge" {
		t.Fatalf("profile name = %q, want clash-verge", profile.Name)
	}
}

func TestProfileByNameReturnsNamedProfile(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "k3s", OS: "linux"},
			{Name: "local", OS: "macos"},
		},
	}

	profile, err := env.profileByName("local")
	if err != nil {
		t.Fatalf("profileByName(local) error = %v", err)
	}
	if profile.Name != "local" {
		t.Fatalf("profile name = %q, want local", profile.Name)
	}
}

func TestAutoSyncProfileFallsBackToOnlyOSProfile(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "linux-main", OS: "linux"},
			{Name: "mac-local", OS: "macos"},
		},
	}

	profile, err := env.autoSyncProfile("linux")
	if err != nil {
		t.Fatalf("autoSyncProfile(linux) error = %v", err)
	}
	if profile.Name != "linux-main" {
		t.Fatalf("profile name = %q, want linux-main", profile.Name)
	}
}

func TestAutoSyncProfileRejectsAmbiguousMacOSProfiles(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "personal", OS: "macos"},
			{Name: "work", OS: "macos"},
		},
	}

	if _, err := env.autoSyncProfile("standalone"); err == nil {
		t.Fatal("expected ambiguous standalone target to fail")
	}
}

func TestAutoSyncProfileUsesSingleShapeMatchedMacOSProfile(t *testing.T) {
	env := &Env{
		RuntimeProfiles: []RuntimeProfile{
			{Name: "primary", OS: "macos"},
			{Name: "work-verge", OS: "macos"},
		},
	}

	profile, err := env.autoSyncProfile("verge")
	if err != nil {
		t.Fatalf("autoSyncProfile(verge) error = %v", err)
	}
	if profile.Name != "work-verge" {
		t.Fatalf("profile name = %q, want work-verge", profile.Name)
	}
}
