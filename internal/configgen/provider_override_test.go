package configgen

import (
	"os"
	"path/filepath"
	"testing"
)

// 订阅里不写 udp: true 时节点默认不支持 UDP，而服务端其实支持。走这类节点的
// UDP 会被 mihomo 跳过规则、静默直连。override 要原样带进生成的 provider，
// 快照（file）类型也不能丢。
func TestProxyProviderOverrideIsRendered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte(`
profiles:
  k3s:
    os: linux
  local:
    os: macos
provider-order: [withudp, plain]
proxy-providers:
  withudp:
    type: http
    url: https://example.com/withudp
    interval: 3600
    path: ./providers/withudp.yaml
    snapshot-profiles: [local]
    override:
      udp: true
  plain:
    type: http
    url: https://example.com/plain
    interval: 3600
    path: ./providers/plain.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadGenerationConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{"withudp": true, "plain": true}

	for _, profile := range []string{"k3s", "local"} {
		rendered := orderedProxyProviders(cfg, profile, used)
		provider, _ := rendered.Values["withudp"].(map[string]any)
		override, ok := provider["override"].(map[string]any)
		if !ok {
			t.Fatalf("%s: withudp override missing, provider = %#v", profile, provider)
		}
		if override["udp"] != true {
			t.Errorf("%s: override.udp = %#v, want true", profile, override["udp"])
		}
		plain, _ := rendered.Values["plain"].(map[string]any)
		if _, exists := plain["override"]; exists {
			t.Errorf("%s: plain should not carry override, got %#v", profile, plain["override"])
		}
	}
	local, _ := orderedProxyProviders(cfg, "local", used).Values["withudp"].(map[string]any)
	if local["type"] != "file" {
		t.Errorf("local snapshot type = %v, want file", local["type"])
	}
}
