package configgen

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const probeModeNone = "none"
const syntheticProbeProfile = "__probe__"

func groupProbeService(groupSpec ServiceGroupSpec) string {
	service := strings.TrimSpace(groupSpec.Probe)
	if service == "" {
		return probeModeNone
	}
	return service
}

func groupProbeEnabled(groupSpec ServiceGroupSpec) bool {
	return !strings.EqualFold(groupProbeService(groupSpec), probeModeNone)
}

type GroupCandidate struct {
	Provider           string
	SubscriptionDigest string
	Proxy              ParsedProxy
}

func groupStateKey(profile, groupName string) string {
	return profile + ":" + groupName
}

func groupProbeDigest(profile, groupName string, groupSpec ServiceGroupSpec, groupProfile ServiceGroupProfileSpec, probeDigest string) string {
	payload := struct {
		Profile      string                  `json:"profile"`
		Group        string                  `json:"group"`
		GroupSpec    ServiceGroupSpec        `json:"group_spec"`
		GroupProfile ServiceGroupProfileSpec `json:"group_profile"`
		ProbeDigest  string                  `json:"probe_digest"`
	}{
		Profile:      profile,
		Group:        groupName,
		GroupSpec:    groupSpec,
		GroupProfile: groupProfile,
		ProbeDigest:  probeDigest,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("marshal group probe digest payload: %v", err))
	}
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest[:])
}

func resolveTargetGroups(cfg *GenerationConfig, targets []string) ([]string, error) {
	groupOrder := cfg.GroupOrder
	if len(groupOrder) == 0 {
		for groupName := range cfg.ServiceGroups {
			groupOrder = append(groupOrder, groupName)
		}
		sort.Strings(groupOrder)
	}
	if len(targets) == 0 {
		resolved := make([]string, 0, len(groupOrder))
		for _, groupName := range groupOrder {
			groupSpec, ok := cfg.ServiceGroups[groupName]
			if !ok || !groupProbeEnabled(groupSpec) {
				continue
			}
			resolved = append(resolved, groupName)
		}
		return resolved, nil
	}
	seen := map[string]struct{}{}
	resolved := []string{}
	for _, target := range targets {
		if groupSpec, ok := cfg.ServiceGroups[target]; ok {
			if !groupProbeEnabled(groupSpec) {
				return nil, fmt.Errorf("service group %s has probe disabled (probe=none)", target)
			}
			if _, seenOK := seen[target]; !seenOK {
				seen[target] = struct{}{}
				resolved = append(resolved, target)
			}
			continue
		}
		matched := false
		for _, groupName := range groupOrder {
			groupSpec, ok := cfg.ServiceGroups[groupName]
			if !ok || !groupProbeEnabled(groupSpec) || groupProbeService(groupSpec) != target {
				continue
			}
			matched = true
			if _, seenOK := seen[groupName]; seenOK {
				continue
			}
			seen[groupName] = struct{}{}
			resolved = append(resolved, groupName)
		}
		if !matched {
			return nil, fmt.Errorf("unknown probe target %s", target)
		}
	}
	return resolved, nil
}

func probeProfileNames(cfg *GenerationConfig) []string {
	if len(cfg.ProfileOrder) > 0 {
		return append([]string(nil), cfg.ProfileOrder...)
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func orderedProviderNames(cfg *GenerationConfig) []string {
	if len(cfg.ProviderOrder) > 0 {
		return append([]string(nil), cfg.ProviderOrder...)
	}
	names := make([]string, 0, len(cfg.ProxyProviders))
	for name := range cfg.ProxyProviders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func groupCandidatesForProfile(profile, groupName string, groupSpec ServiceGroupSpec, cfg *GenerationConfig, catalog *ProviderCatalog, allowedProviders map[string]struct{}) ([]GroupCandidate, ServiceGroupProfileSpec, error) {
	groupProfile, ok := resolveServiceGroupProfile(groupSpec, profile)
	if !ok {
		return nil, ServiceGroupProfileSpec{}, nil
	}
	matchers, err := compileRegexps(groupSpec.Match)
	if err != nil {
		return nil, ServiceGroupProfileSpec{}, fmt.Errorf("compile match regex for %s: %w", groupName, err)
	}
	excluders, err := compileRegexps(groupSpec.Exclude)
	if err != nil {
		return nil, ServiceGroupProfileSpec{}, fmt.Errorf("compile exclude regex for %s: %w", groupName, err)
	}

	candidates := []GroupCandidate{}
	for _, providerName := range orderedProviderNames(cfg) {
		if _, ok := cfg.ProxyProviders[providerName]; !ok {
			continue
		}
		if len(allowedProviders) > 0 {
			if _, ok := allowedProviders[providerName]; !ok {
				continue
			}
		}
		if len(groupProfile.Providers) > 0 && !containsString(groupProfile.Providers, providerName) {
			continue
		}
		snapshot, ok := catalog.Providers[providerName]
		if !ok {
			continue
		}
		providerMatchers, providerExcluders, err := providerRegexps(groupProfile, providerName)
		if err != nil {
			return nil, ServiceGroupProfileSpec{}, fmt.Errorf("compile provider filters for %s/%s: %w", groupName, providerName, err)
		}
		for _, proxy := range snapshot.Proxies {
			if !matchesProxyName(proxy.Name, matchers, excluders) {
				continue
			}
			if !matchesProxyName(proxy.Name, providerMatchers, providerExcluders) {
				continue
			}
			candidates = append(candidates, GroupCandidate{
				Provider:           providerName,
				SubscriptionDigest: snapshot.Digest,
				Proxy:              proxy,
			})
		}
	}
	return candidates, groupProfile, nil
}

func groupCandidatesByProvider(candidates []GroupCandidate) map[string][]GroupCandidate {
	grouped := make(map[string][]GroupCandidate)
	for _, candidate := range candidates {
		grouped[candidate.Provider] = append(grouped[candidate.Provider], candidate)
	}
	return grouped
}

func prepareProbeConfig(cfg *GenerationConfig, scope ProbeScope) *GenerationConfig {
	if cfg == nil || len(scope.Services) == 0 {
		return cfg
	}

	overrideTargets := map[string]struct{}{}
	for _, target := range scope.Services {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, ok := cfg.Probe.Services[target]; ok {
			overrideTargets[target] = struct{}{}
		}
	}
	if len(overrideTargets) == 0 {
		return cfg
	}

	cloned := *cfg
	cloned.ServiceGroups = make(map[string]ServiceGroupSpec, len(cfg.ServiceGroups))
	for name, spec := range cfg.ServiceGroups {
		cloned.ServiceGroups[name] = spec
	}
	cloned.GroupOrder = append([]string(nil), cfg.GroupOrder...)
	cloned.Profiles = make(map[string]ProfileSpec, len(cfg.Profiles)+1)
	for name, spec := range cfg.Profiles {
		cloned.Profiles[name] = spec
	}
	cloned.ProfileOrder = append([]string(nil), cfg.ProfileOrder...)
	if _, ok := cloned.Profiles[syntheticProbeProfile]; !ok {
		cloned.Profiles[syntheticProbeProfile] = ProfileSpec{OS: "linux"}
		cloned.ProfileOrder = append(cloned.ProfileOrder, syntheticProbeProfile)
	}
	if len(scope.Providers) > 0 {
		cloned.ProxyProviders = make(map[string]ProxyProviderSpec, len(cfg.ProxyProviders))
		for name, spec := range cfg.ProxyProviders {
			cloned.ProxyProviders[name] = spec
		}
	}

	for target := range overrideTargets {
		spec := ServiceGroupSpec{
			Probe: target,
			Profiles: map[string]ServiceGroupProfileSpec{
				syntheticProbeProfile: {},
			},
		}
		if len(scope.Providers) > 0 {
			spec.Profiles[syntheticProbeProfile] = ServiceGroupProfileSpec{
				Providers: append([]string(nil), scope.Providers...),
			}
		}
		cloned.ServiceGroups[target] = spec
		if !containsString(cloned.GroupOrder, target) {
			cloned.GroupOrder = append(cloned.GroupOrder, target)
		}
	}

	return &cloned
}
