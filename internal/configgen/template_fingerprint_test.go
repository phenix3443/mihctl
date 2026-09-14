package configgen

import (
	"path/filepath"
	"runtime"
	"testing"
)

// mihomo 1.19.27 起删除了 global-client-fingerprint，留着它只会在重载时报 error，
// 并让人误以为全局兜底还在生效。指纹必须由订阅写在节点上。
func TestTemplateDoesNotEmitGlobalClientFingerprint(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	templatePath := filepath.Join(filepath.Dir(thisFile), "..", "..", "config", "mihomo.yaml.tmpl")

	rendered, err := RenderTemplate(templatePath, RenderData{ProxyGroupNames: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigString(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if value, exists := cfg["global-client-fingerprint"]; exists {
		t.Fatalf("rendered config still has global-client-fingerprint = %v", value)
	}
}
