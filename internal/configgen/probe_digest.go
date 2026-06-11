package configgen

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

func BuildProbeServiceDigests(cfg *GenerationConfig) (map[string]string, error) {
	digests := make(map[string]string, len(cfg.Probe.Services))
	keys := make([]string, 0, len(cfg.Probe.Services))
	for name := range cfg.Probe.Services {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		digests[name] = probeServiceDigest(cfg.Probe.Services[name])
	}
	return digests, nil
}

func probeServiceDigest(spec ProbeServiceSpec) string {
	payload, err := json.Marshal(spec)
	if err != nil {
		panic(fmt.Sprintf("marshal probe service spec: %v", err))
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest[:])
}
