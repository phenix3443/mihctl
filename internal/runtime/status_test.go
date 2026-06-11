package runtime

import "testing"

func TestLinuxConfigPathUsesConfigDirEnv(t *testing.T) {
	t.Setenv("CONFIG_DIR", "/tmp/demo-clash")
	if got := linuxConfigPath(); got != "/tmp/demo-clash/config.yaml" {
		t.Fatalf("linuxConfigPath() = %q, want /tmp/demo-clash/config.yaml", got)
	}
}
