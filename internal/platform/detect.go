package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Info struct {
	OS string
}

func Detect() Info {
	return Info{OS: runtime.GOOS}
}

func DefaultConfigDir() string {
	if runtime.GOOS == "darwin" {
		if brew, err := exec.LookPath("brew"); err == nil {
			cmd := exec.Command(brew, "--prefix")
			output, err := cmd.Output()
			if err == nil {
				prefix := strings.TrimSpace(string(output))
				if prefix != "" {
					return filepath.Join(prefix, "etc", "mihomo")
				}
			}
		}
	}
	return filepath.Join(userConfigHome(), "mihomo")
}

func ValidateSupportedOS() error {
	switch runtime.GOOS {
	case "darwin", "linux":
		return nil
	default:
		return fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}
}

func userConfigHome() string {
	if xdg := strings.TrimSpace(getenv("XDG_CONFIG_HOME")); xdg != "" {
		return xdg
	}
	home := strings.TrimSpace(getenv("HOME"))
	if home == "" {
		return ".config"
	}
	return filepath.Join(home, ".config")
}

var getenv = func(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
