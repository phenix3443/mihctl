package configgen

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProbeState struct {
	Providers map[string]ProviderProbeState `yaml:"providers"`
	Groups    map[string]GroupProbeState    `yaml:"groups,omitempty"`
}

type ProviderProbeState struct {
	Provider           string                    `yaml:"provider"`
	SubscriptionDigest string                    `yaml:"subscription-digest"`
	Nodes              map[string]NodeProbeState `yaml:"nodes"`
}

type NodeProbeState struct {
	NodeName string                       `yaml:"node-name"`
	Services map[string]ServiceProbeState `yaml:"services"`
}

type ServiceProbeState struct {
	OK            bool   `yaml:"ok"`
	LatencyMillis int    `yaml:"latency-millis,omitempty"`
	Reason        string `yaml:"reason,omitempty"`
	Region        string `yaml:"region,omitempty"`
	Error         string `yaml:"error,omitempty"`
	ProbeDigest   string `yaml:"probe-digest"`
	ProbedAt      string `yaml:"probed-at"`
}

type GroupProbeState struct {
	Group           string                    `yaml:"group"`
	Profile         string                    `yaml:"profile"`
	Probe           string                    `yaml:"probe"`
	Digest          string                    `yaml:"digest"`
	ProviderDigests map[string]string         `yaml:"provider-digests"`
	Nodes           map[string]GroupNodeState `yaml:"nodes"`
}

type GroupNodeState struct {
	Provider string            `yaml:"provider"`
	NodeName string            `yaml:"node-name"`
	Service  ServiceProbeState `yaml:"service"`
}

type ProbeLookupReason string

const (
	ProbeLookupOK                      ProbeLookupReason = "ok"
	ProbeLookupMissingProviderState    ProbeLookupReason = "missing-provider-state"
	ProbeLookupStaleSubscriptionDigest ProbeLookupReason = "stale-subscription-digest"
	ProbeLookupMissingNodeState        ProbeLookupReason = "missing-node-state"
	ProbeLookupMissingServiceState     ProbeLookupReason = "missing-service-state"
	ProbeLookupStaleProbeDigest        ProbeLookupReason = "stale-probe-digest"
	ProbeLookupProbeFailed             ProbeLookupReason = "probe-failed"
)

type ProbeLookupResult struct {
	State  ServiceProbeState
	Reason ProbeLookupReason
}

func (s *ProbeState) LookupGroup(profile, groupName, digest string) (GroupProbeState, bool) {
	if s == nil || s.Groups == nil {
		return GroupProbeState{}, false
	}
	groupState, ok := s.Groups[groupStateKey(profile, groupName)]
	if !ok {
		return GroupProbeState{}, false
	}
	if groupState.Digest != digest {
		return GroupProbeState{}, false
	}
	return groupState, true
}

func LoadProbeState(path string) (*ProbeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProbeState{Providers: map[string]ProviderProbeState{}}, nil
		}
		return nil, fmt.Errorf("read probe state %s: %w", path, err)
	}

	var state ProbeState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode probe state %s: %w", path, err)
	}
	if state.Providers == nil {
		state.Providers = map[string]ProviderProbeState{}
	}
	if state.Groups == nil {
		state.Groups = map[string]GroupProbeState{}
	}
	return &state, nil
}

func SaveProbeState(path string, state *ProbeState) error {
	if state == nil {
		return fmt.Errorf("probe state is nil")
	}
	if state.Providers == nil {
		state.Providers = map[string]ProviderProbeState{}
	}
	if state.Groups == nil {
		state.Groups = map[string]GroupProbeState{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".probe-results-*.yaml")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func (s *ProbeState) Lookup(provider, digest, nodeName, service, probeDigest string) (ServiceProbeState, bool) {
	result := s.Diagnose(provider, digest, nodeName, service, probeDigest)
	if result.Reason != ProbeLookupOK {
		return ServiceProbeState{}, false
	}
	return result.State, result.State.OK
}

func (s *ProbeState) Fresh(provider, digest, nodeName, service, probeDigest string) (ServiceProbeState, bool) {
	result := s.Diagnose(provider, digest, nodeName, service, probeDigest)
	if result.Reason != ProbeLookupOK && result.Reason != ProbeLookupProbeFailed {
		return ServiceProbeState{}, false
	}
	return result.State, true
}

func (s *ProbeState) Diagnose(provider, digest, nodeName, service, probeDigest string) ProbeLookupResult {
	if s == nil {
		return ProbeLookupResult{Reason: ProbeLookupMissingProviderState}
	}
	providerState, ok := s.Providers[provider]
	if !ok {
		return ProbeLookupResult{Reason: ProbeLookupMissingProviderState}
	}
	if providerState.SubscriptionDigest != digest {
		return ProbeLookupResult{Reason: ProbeLookupStaleSubscriptionDigest}
	}
	nodeState, ok := providerState.Nodes[nodeName]
	if !ok {
		return ProbeLookupResult{Reason: ProbeLookupMissingNodeState}
	}
	serviceState, ok := nodeState.Services[service]
	if !ok {
		return ProbeLookupResult{Reason: ProbeLookupMissingServiceState}
	}
	if serviceState.ProbeDigest != probeDigest {
		return ProbeLookupResult{
			State:  serviceState,
			Reason: ProbeLookupStaleProbeDigest,
		}
	}
	if !serviceState.OK {
		return ProbeLookupResult{
			State:  serviceState,
			Reason: ProbeLookupProbeFailed,
		}
	}
	return ProbeLookupResult{
		State:  serviceState,
		Reason: ProbeLookupOK,
	}
}

func (s *ProbeState) ProviderServiceFresh(provider string, snapshot ProviderSnapshot, service, probeDigest string) bool {
	if s == nil {
		return false
	}
	providerState, ok := s.Providers[provider]
	if !ok || providerState.SubscriptionDigest != snapshot.Digest {
		return false
	}
	for _, proxy := range snapshot.Proxies {
		nodeState, ok := providerState.Nodes[proxy.Name]
		if !ok {
			return false
		}
		serviceState, ok := nodeState.Services[service]
		if !ok || serviceState.ProbeDigest != probeDigest {
			return false
		}
	}
	return true
}
