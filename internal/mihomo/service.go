package mihomo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (e *Env) stopBrewServices() error {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return nil
	}
	if e.IsRoot() {
		return runCommand("", os.Stdout, os.Stderr, brew, "services", "stop", "mihomo")
	}
	return runCommand("", os.Stdout, os.Stderr, "brew", "services", "stop", "mihomo")
}

func (e *Env) isDarwinServiceLoaded() bool {
	if !fileExists(e.PlistPath) {
		return false
	}
	return runCommand("", nil, nil, "sudo", e.Launchctl, "list", e.PlistLabel) == nil
}

func (e *Env) isDarwinServiceRunning() bool {
	if !fileExists(e.PlistPath) {
		return false
	}
	output := runOptional("", "sudo", e.Launchctl, "list", e.PlistLabel)
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "\"PID\"") {
			continue
		}
		pidText := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, line)
		if pidText == "" {
			continue
		}
		pid, err := strconv.Atoi(pidText)
		return err == nil && pid > 0
	}
	return false
}

func (e *Env) startDarwinService() error {
	if !fileExists(e.PlistPath) {
		return fmt.Errorf("launchdaemon plist missing. run: sudo mihctl install")
	}
	if e.isDarwinServiceLoaded() {
		logInfo("Service already loaded; restarting")
		_ = runCommand("", os.Stdout, os.Stderr, "sudo", e.Launchctl, "unload", e.PlistPath)
	}
	return runCommand("", os.Stdout, os.Stderr, "sudo", e.Launchctl, "load", "-w", e.PlistPath)
}

func (e *Env) stopDarwinService() error {
	if e.isDarwinServiceLoaded() {
		return runCommand("", os.Stdout, os.Stderr, "sudo", e.Launchctl, "unload", e.PlistPath)
	}
	logInfo("Service not loaded")
	return nil
}

func (e *Env) restartDarwinService() error {
	if err := e.stopDarwinService(); err != nil {
		return err
	}
	return e.startDarwinService()
}

func (e *Env) showRecentDarwinLogs() {
	if fileExists(e.LogPath) {
		fmt.Fprintf(os.Stdout, "Recent logs (%s):\n", e.LogPath)
		_ = runCommand("", os.Stdout, os.Stderr, "tail", "-15", e.LogPath)
	}
}

func (e *Env) isLinuxServiceActive(name string) bool {
	return runCommand("", nil, nil, "systemctl", "is-active", "--quiet", name) == nil
}

func (e *Env) linuxServiceEnabled(name string) string {
	output, err := runCommandOutput("", "systemctl", "is-enabled", name)
	if err != nil {
		return "disabled"
	}
	return strings.TrimSpace(output)
}

func (e *Env) linuxServiceState(name string) (string, string) {
	active, err := runCommandOutput("", "systemctl", "show", "-p", "ActiveState", "--value", name)
	if err != nil {
		active = "unknown"
	}
	sub, err := runCommandOutput("", "systemctl", "show", "-p", "SubState", "--value", name)
	if err != nil {
		sub = "unknown"
	}
	return strings.TrimSpace(active), strings.TrimSpace(sub)
}

func (e *Env) waitForLinuxService(name string) {
	time.Sleep(1 * time.Second)
}

func (e *Env) systemdUnitPath() string {
	return "/etc/systemd/system/mihomo.service"
}

func (e *Env) writeSystemdUnit() error {
	template, err := os.ReadFile(e.TemplateServicePath)
	if err != nil {
		return err
	}
	rendered := string(template)
	rendered = strings.ReplaceAll(rendered, "@INSTALL_DIR@", e.InstallDir)
	rendered = strings.ReplaceAll(rendered, "@CONFIG_DIR@", e.ConfigDir)
	rendered = strings.ReplaceAll(rendered, "@SERVICE_USER@", e.ServiceUser)
	if err := writeFilePrivileged(e.systemdUnitPath(), []byte(rendered), 0o644); err != nil {
		return err
	}
	return runCommand("", os.Stdout, os.Stderr, "systemctl", "daemon-reload")
}

func (e *Env) removeSystemdUnit() error {
	if err := removeFilePrivileged(e.systemdUnitPath()); err != nil {
		return err
	}
	return runCommand("", os.Stdout, os.Stderr, "systemctl", "daemon-reload")
}

func (e *Env) resolveDarwinMihomoBinary() (string, error) {
	if commandExists("brew") {
		output := strings.TrimSpace(runOptional("", "brew", "--prefix", "mihomo"))
		if output != "" {
			candidate := filepath.Join(output, "bin", "mihomo")
			if fileExists(candidate) {
				return candidate, nil
			}
		}
	}
	candidate := filepath.Join(e.InstallDir, "mihomo")
	if fileExists(candidate) {
		return candidate, nil
	}
	if path, err := exec.LookPath("mihomo"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("mihomo binary not found. run: mihctl install")
}

func (e *Env) writeDarwinPlist() error {
	binaryPath, err := e.resolveDarwinMihomoBinary()
	if err != nil {
		return err
	}
	logDir := filepath.Dir(e.LogPath)
	if err := mkdirAllPrivileged(logDir, 0o755); err != nil {
		return err
	}
	_ = e.stopBrewServices()
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>-d</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
  <key>WorkingDirectory</key>
  <string>%s</string>
</dict>
</plist>
`, e.PlistLabel, binaryPath, e.ConfigDir, e.LogPath, e.LogPath, e.ConfigDir)
	return writeFilePrivileged(e.PlistPath, []byte(plist), 0o644)
}

func (e *Env) writeDarwinSudoers() error {
	user := e.SudoUser()
	if user == "" {
		output := strings.TrimSpace(runOptional("", "logname"))
		if output != "" {
			user = output
		}
	}
	if user == "" || user == "root" {
		logWarn("Cannot determine non-root user for sudoers; start/stop will require sudo")
		return nil
	}
	content := fmt.Sprintf("%s ALL=(root) NOPASSWD: %s load -w %s\n%s ALL=(root) NOPASSWD: %s unload %s\n%s ALL=(root) NOPASSWD: %s list %s\n",
		user, e.Launchctl, e.PlistPath,
		user, e.Launchctl, e.PlistPath,
		user, e.Launchctl, e.PlistLabel,
	)
	if err := writeFilePrivileged(e.SudoersPath, []byte(content), 0o440); err != nil {
		return err
	}
	if runCommand("", nil, nil, "sudo", "visudo", "-cf", e.SudoersPath) != nil {
		_ = removeFilePrivileged(e.SudoersPath)
		return fmt.Errorf("sudoers syntax error")
	}
	return nil
}
