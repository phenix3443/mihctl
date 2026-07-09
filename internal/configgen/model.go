package configgen

import (
	"path/filepath"
	"strconv"
	"strings"
)

type Config map[string]any

type GenerateOptions struct {
	EnableLinuxTUN bool
	EnableMacOSTUN bool
}

type Paths struct {
	RepoRoot       string
	TemplateRoot   string
	TemplateConfig string
	ValuesConfig   string
}

func (p Paths) OutputForProfile(profile string) string {
	return filepath.Join(p.RepoRoot, "config", profile, "mihomo.yaml")
}

func DeepCopy(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, inner := range typed {
			cloned[key] = DeepCopy(inner)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, inner := range typed {
			cloned[index] = DeepCopy(inner)
		}
		return cloned
	default:
		return typed
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	return DeepCopy(value).(map[string]any)
}

func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	default:
		return false
	}
}
