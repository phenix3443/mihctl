package configgen

import (
	"os"
	"path/filepath"
	"testing"
)

// select 组节点死了不会自己切走。桌面端有人盯着可以手动切，无人值守的集群
// 要自动兜底，所以同一个组要能按 profile 换类型。
func TestServiceGroupTypeCanBeOverriddenPerProfile(t *testing.T) {
	repoRoot := t.TempDir()
	configDir := filepath.Join(repoRoot, "config")
	providersDir := filepath.Join(repoRoot, "providers")
	for _, dir := range []string{filepath.Join(configDir, "local"), filepath.Join(configDir, "k3s"), providersDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	template := `proxies:
{{ toYAML .Proxies | indent 2 }}
proxy-groups:
{{ toYAML .ProxyGroups | indent 2 }}
proxy-providers:
{{ toYAML .ProxyProviders | indent 2 }}
rule-providers: {}
rules: []
`
	values := `
profiles:
  local:
    os: macos
  k3s:
    os: linux
proxy-providers:
  bywave:
    type: http
    url: https://example.com/bywave
    interval: 60
    path: ./providers/bywave.yaml
service-groups:
  github:
    type: select
    url: https://github.com/
    interval: 300
    tolerance: 50
    lazy: true
    profiles:
      local:
        providers: [bywave]
      k3s:
        providers: [bywave]
        type: fallback
`
	bywaveYAML := `proxies:
  - name: bywave-jp
    type: ss
    server: 1.1.1.1
    port: 443
    cipher: aes-128-gcm
    password: secret-a
`
	for path, body := range map[string]string{
		filepath.Join(configDir, "mihomo.yaml.tmpl"): template,
		filepath.Join(configDir, "values.yaml"):      values,
		filepath.Join(providersDir, "bywave.yaml"):   bywaveYAML,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	service := newTestService(repoRoot, configDir)
	if _, err := service.Generate(GenerateOptions{}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		profile, wantType string
		wantURL           any
	}{
		{"local", "select", nil},
		{"k3s", "fallback", "https://github.com/"},
	} {
		generated, err := LoadConfig(profileConfigPath(configDir, tc.profile))
		if err != nil {
			t.Fatal(err)
		}
		groups, ok := generated["proxy-groups"].([]any)
		if !ok || len(groups) != 1 {
			t.Fatalf("%s: proxy-groups = %#v", tc.profile, generated["proxy-groups"])
		}
		group, _ := asMap(groups[0])
		if got := group["type"]; got != tc.wantType {
			t.Errorf("%s: group type = %#v, want %s", tc.profile, got, tc.wantType)
		}
		if got := group["url"]; got != tc.wantURL {
			t.Errorf("%s: group url = %#v, want %#v", tc.profile, got, tc.wantURL)
		}
	}
}
