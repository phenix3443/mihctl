package mihomo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/phenix3443/mihomo-companion/internal/configgen"
)

func (e *Env) SyncRules() error {
	targetDir, err := e.detectLiveConfigDir()
	if err != nil {
		return fmt.Errorf("could not detect mihomo config directory")
	}
	rulesDir := filepath.Join(targetDir, "ruleset")
	if err := mkdirAllPrivileged(rulesDir, 0o755); err != nil {
		return err
	}
	sourceYAML, err := e.detectLiveConfigFile()
	if err != nil {
		return err
	}
	if !fileExists(sourceYAML) {
		return fmt.Errorf("missing live config: %s", sourceYAML)
	}

	cfg, err := configgen.LoadConfig(sourceYAML)
	if err != nil {
		return err
	}
	rawProviders, ok := asConfigMap(cfg["rule-providers"])
	if !ok || len(rawProviders) == 0 {
		logInfo("No rule-providers in %s", sourceYAML)
		return nil
	}

	keys := make([]string, 0, len(rawProviders))
	for key := range rawProviders {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	logStep("Updating remote rule-providers into %s", rulesDir)
	for _, name := range keys {
		provider, ok := asConfigMap(rawProviders[name])
		if !ok {
			continue
		}
		rawURL, _ := provider["url"].(string)
		rawPath, _ := provider["path"].(string)
		if strings.TrimSpace(rawURL) == "" || strings.TrimSpace(rawPath) == "" {
			continue
		}
		dest := filepath.Join(rulesDir, filepath.Base(rawPath))
		logInfo("Rule provider %s -> %s", name, dest)
		if err := e.fetchRemoteWithFallback(dest, rawURL, "", ""); err != nil {
			if fileExists(dest) {
				logWarn("Fetch failed; kept existing %s", filepath.Base(dest))
				continue
			}
			logError("Fetch failed for %s", name)
			continue
		}
		logSuccess("Updated %s", filepath.Base(dest))
	}

	logSuccess("Remote rule sync complete")
	logStep("Restarting mihomo to apply synced rules")
	if e.OS == "darwin" {
		if e.isDarwinServiceLoaded() {
			if err := e.restartDarwinService(); err != nil {
				return err
			}
			logSuccess("mihomo restarted (LaunchDaemon) after rule sync")
			return nil
		}
		logWarn("Could not determine running mihomo service on macOS; restart manually if needed")
		return nil
	}

	if e.isLinuxServiceActive("mihomo") {
		if err := runCommand("", os.Stdout, os.Stderr, "systemctl", "restart", "mihomo"); err != nil {
			return err
		}
		logSuccess("mihomo restarted via systemd after rule sync")
		return nil
	}

	logWarn("mihomo is not running; start the service to apply synced rules")
	return nil
}
