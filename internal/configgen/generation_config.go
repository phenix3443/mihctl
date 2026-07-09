package configgen

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ProxyProviderSpec struct {
	Type     string `yaml:"type"`
	URL      string `yaml:"url"`
	Interval int    `yaml:"interval"`
	Path     string `yaml:"path"`
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
