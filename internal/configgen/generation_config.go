package configgen

import (
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

type ProxyProviderSpec struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	// URLs 按 profile 覆盖 URL。订阅源在 tailnet 内时，k3s 里的 mihomo pod
	// 解析不了 *.ts.net，只能走集群内地址；Mac 上的 profile 仍走 tailnet。
	URLs map[string]string `yaml:"urls"`
	// SnapshotProfiles 列出「由 mihctl 落快照、mihomo 只读本地文件」的 profile。
	// 订阅源只有宿主机到得了时用它：tailnet 的 100.64/10 归 tailscale 的 utun，
	// 而 mihomo 的出站 socket 绑在 auto-detect 出来的默认网卡上，拨不过去。
	// 这些 profile 生成 `type: file`，config sync 顺带把快照拷进运行目录。
	SnapshotProfiles []string `yaml:"snapshot-profiles"`
	// Override 原样透传给 mihomo 的 proxy-provider override。订阅不写 udp: true
	// 时节点默认不支持 UDP，走它的 UDP 会跳过规则静默直连——服务端实测支持 UDP
	// 的订阅要在这里补 udp: true。
	Override map[string]any `yaml:"override"`
	Interval int            `yaml:"interval"`
	Path     string         `yaml:"path"`
}

// IsSnapshot 报告该 profile 是否由 mihctl 落快照而不是让 mihomo 自己拉。
func (s ProxyProviderSpec) IsSnapshot(profile string) bool {
	return slices.Contains(s.SnapshotProfiles, profile)
}

// ResolveURL 返回该 profile 实际该用的订阅地址。
func (s ProxyProviderSpec) ResolveURL(profile string) string {
	if url, ok := s.URLs[profile]; ok && url != "" {
		return url
	}
	return s.URL
}

type ManualProxySpec Config

type TunSpec struct {
	Linux Config `yaml:"linux"`
	MacOS Config `yaml:"macos"`
}

type ProfileSpec struct {
	OS string `yaml:"os"`
}

type RuleProviderSpec struct {
	Type     string `yaml:"type"`
	Behavior string `yaml:"behavior"`
	Format   string `yaml:"format"`
	URL      string `yaml:"url"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"`
}

type TemplateSnifferSpec struct {
	ForceDomain []string `yaml:"force-domain"`
	SkipDomain  []string `yaml:"skip-domain"`
}

type TemplateDNSSpec struct {
	FakeIPFilter []string `yaml:"fake-ip-filter"`
	// NameserverPolicy 追加到模板内置的域名分流之后。集群内域名必须走集群
	// DNS：公网 nameserver 解析不了 *.cluster.local，而 mihomo 拉 provider
	// 时用的正是自己这套 DNS。
	NameserverPolicy map[string][]string `yaml:"nameserver-policy"`
}

type TemplateSpec struct {
	Secret     string              `yaml:"secret"`
	ExternalUI map[string]string   `yaml:"external-ui"`
	Sniffer    TemplateSnifferSpec `yaml:"sniffer"`
	DNS        TemplateDNSSpec     `yaml:"dns"`
}

type ServiceGroupProfileSpec struct {
	Providers       []string            `yaml:"providers"`
	ProviderMatch   map[string][]string `yaml:"provider-match"`
	ProviderExclude map[string][]string `yaml:"provider-exclude"`
}

type ServiceGroupSpec struct {
	Profiles                 map[string]ServiceGroupProfileSpec `yaml:"profiles"`
	Type                     string                             `yaml:"type"`
	URL                      string                             `yaml:"url"`
	Interval                 int                                `yaml:"interval"`
	Tolerance                int                                `yaml:"tolerance"`
	Lazy                     bool                               `yaml:"lazy"`
	Match                    []string                           `yaml:"match"`
	Exclude                  []string                           `yaml:"exclude"`
	MultiplierFilters        map[string]string                  `yaml:"multiplier-filters"`
	SupportedHighMultipliers []string                           `yaml:"supported-high-multipliers"`
}

type OfficialSupportConfigEntry struct {
	SourceURL   string   `yaml:"source-url"`
	Supported   []string `yaml:"supported"`
	Prohibited  []string `yaml:"prohibited"`
	Description string   `yaml:"description"`
}

type GenerationConfig struct {
	DefaultProfile    string                                `yaml:"default-profile"`
	Profiles          map[string]ProfileSpec                `yaml:"profiles"`
	Template          TemplateSpec                          `yaml:"template"`
	OfficialSupport   map[string]OfficialSupportConfigEntry `yaml:"official-support"`
	ManualProxies     []ManualProxySpec                     `yaml:"manual-proxies"`
	Tun               TunSpec                               `yaml:"tun"`
	ProxyProviders    map[string]ProxyProviderSpec          `yaml:"proxy-providers"`
	RuleProviders     map[string]RuleProviderSpec           `yaml:"rule-providers"`
	ServiceGroups     map[string]ServiceGroupSpec           `yaml:"service-groups"`
	Rules             []string                              `yaml:"rules"`
	ProfileOrder      []string                              `yaml:"-"`
	ProviderOrder     []string                              `yaml:"-"`
	RuleProviderOrder []string                              `yaml:"-"`
	GroupOrder        []string                              `yaml:"-"`
}

func LoadGenerationConfig(path string) (*GenerationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg GenerationConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err == nil && len(root.Content) > 0 {
		if mapping := root.Content[0]; mapping.Kind == yaml.MappingNode {
			for index := 0; index+1 < len(mapping.Content); index += 2 {
				keyNode := mapping.Content[index]
				valueNode := mapping.Content[index+1]
				switch keyNode.Value {
				case "profiles":
					cfg.ProfileOrder = orderedMappingKeys(valueNode)
				case "proxy-providers":
					cfg.ProviderOrder = orderedMappingKeys(valueNode)
				case "rule-providers":
					cfg.RuleProviderOrder = orderedMappingKeys(valueNode)
				case "service-groups":
					cfg.GroupOrder = orderedMappingKeys(valueNode)
				}
			}
		}
	}

	if cfg.Profiles == nil {
		cfg.Profiles = map[string]ProfileSpec{}
	}
	if cfg.OfficialSupport == nil {
		cfg.OfficialSupport = map[string]OfficialSupportConfigEntry{}
	}
	if cfg.Template.Sniffer.ForceDomain == nil {
		cfg.Template.Sniffer.ForceDomain = []string{}
	}
	if cfg.Template.ExternalUI == nil {
		cfg.Template.ExternalUI = map[string]string{}
	}
	if cfg.Template.Sniffer.SkipDomain == nil {
		cfg.Template.Sniffer.SkipDomain = []string{}
	}
	if cfg.Template.DNS.FakeIPFilter == nil {
		cfg.Template.DNS.FakeIPFilter = []string{}
	}
	if cfg.Template.DNS.NameserverPolicy == nil {
		cfg.Template.DNS.NameserverPolicy = map[string][]string{}
	}
	if cfg.ProxyProviders == nil {
		cfg.ProxyProviders = map[string]ProxyProviderSpec{}
	}
	if cfg.ManualProxies == nil {
		cfg.ManualProxies = []ManualProxySpec{}
	}
	if cfg.Tun.Linux == nil {
		cfg.Tun.Linux = Config{}
	}
	if cfg.Tun.MacOS == nil {
		cfg.Tun.MacOS = Config{}
	}
	if cfg.RuleProviders == nil {
		cfg.RuleProviders = map[string]RuleProviderSpec{}
	}
	if cfg.ServiceGroups == nil {
		cfg.ServiceGroups = map[string]ServiceGroupSpec{}
	}
	if cfg.Rules == nil {
		cfg.Rules = []string{}
	}
	// urls 写错 profile 名不会报错，只会静默回退到默认 url——那正是这个功能
	// 要解决的问题，所以这里拦住。
	for name, spec := range cfg.ProxyProviders {
		for profile := range spec.URLs {
			if _, ok := cfg.Profiles[profile]; !ok {
				return nil, fmt.Errorf("proxy-provider %q: urls 里的 %q 不是已定义的 profile", name, profile)
			}
		}
		for _, profile := range spec.SnapshotProfiles {
			if _, ok := cfg.Profiles[profile]; !ok {
				return nil, fmt.Errorf("proxy-provider %q: snapshot-profiles 里的 %q 不是已定义的 profile", name, profile)
			}
		}
	}
	return &cfg, nil
}

func orderedMappingKeys(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(node.Content)/2)
	for index := 0; index+1 < len(node.Content); index += 2 {
		keys = append(keys, node.Content[index].Value)
	}
	return keys
}
