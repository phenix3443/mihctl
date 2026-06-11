package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureAccessInfoFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`mixed-port: 7890
external-controller: 0.0.0.0:9090
secret: demo-secret
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := CaptureAccessInfoFromYAML(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Controller != "0.0.0.0:9090" {
		t.Fatalf("controller = %q, want 0.0.0.0:9090", info.Controller)
	}
	if info.MixedPort != 7890 {
		t.Fatalf("mixed port = %d, want 7890", info.MixedPort)
	}
}
