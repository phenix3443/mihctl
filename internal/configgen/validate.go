package configgen

import "fmt"

func Validate(config Config) error {
	requiredKeys := []string{
		"proxy-groups",
		"proxy-providers",
		"rule-providers",
		"rules",
	}

	for _, key := range requiredKeys {
		if _, ok := config[key]; !ok {
			return fmt.Errorf("missing required key %q", key)
		}
	}

	return nil
}
