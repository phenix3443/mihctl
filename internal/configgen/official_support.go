package configgen

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type OfficialSupportState struct {
	Services map[string]OfficialSupportServiceState `yaml:"services"`
}

type OfficialSupportServiceState struct {
	Service     string   `yaml:"service"`
	SourceURL   string   `yaml:"source-url"`
	Supported   []string `yaml:"supported"`
	Prohibited  []string `yaml:"prohibited,omitempty"`
	UpdatedAt   string   `yaml:"updated-at"`
	Description string   `yaml:"description,omitempty"`
}

func DefaultOfficialSupportStatePath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("MIHOMO_OFFICIAL_SUPPORT_STATE_PATH")); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "clash", "official-support.yaml"), nil
	}
	stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "clash", "official-support.yaml"), nil
}

func LoadOfficialSupportState(path string) (*OfficialSupportState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &OfficialSupportState{Services: map[string]OfficialSupportServiceState{}}, nil
		}
		return nil, fmt.Errorf("read official support state %s: %w", path, err)
	}

	var state OfficialSupportState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode official support state %s: %w", path, err)
	}
	if state.Services == nil {
		state.Services = map[string]OfficialSupportServiceState{}
	}
	return &state, nil
}

func SaveOfficialSupportState(path string, state *OfficialSupportState) error {
	if state == nil {
		return fmt.Errorf("official support state is nil")
	}
	if state.Services == nil {
		state.Services = map[string]OfficialSupportServiceState{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".official-support-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func BuildOfficialSupportStateFromConfig(cfg *GenerationConfig, now time.Time) *OfficialSupportState {
	state := &OfficialSupportState{Services: map[string]OfficialSupportServiceState{}}
	if cfg == nil {
		return state
	}
	for service, entry := range cfg.OfficialSupport {
		supported := normalizeCountryCodes(entry.Supported)
		prohibited := normalizeCountryCodes(entry.Prohibited)
		state.Services[service] = OfficialSupportServiceState{
			Service:     service,
			SourceURL:   entry.SourceURL,
			Supported:   supported,
			Prohibited:  prohibited,
			UpdatedAt:   now.UTC().Format(time.RFC3339),
			Description: entry.Description,
		}
	}
	return state
}

func normalizeCountryCodes(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		code := strings.ToUpper(strings.TrimSpace(value))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}
