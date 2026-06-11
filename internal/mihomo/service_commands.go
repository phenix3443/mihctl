package mihomo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (e *Env) ServiceStart() error {
	if e.OS == "darwin" {
		if err := e.startDarwinService(); err != nil {
			return err
		}
		if e.isDarwinServiceRunning() {
			logSuccess("mihomo started (LaunchDaemon)")
			return nil
		}
		logWarn("LaunchDaemon PID not detected; checking process...")
		if e.macosKernelRunning() {
			logSuccess("mihomo is running (process found)")
			return nil
		}
		e.showRecentDarwinLogs()
		return fmt.Errorf("mihomo failed to start")
	}

	if err := e.RequireRoot("start"); err != nil {
		return err
	}
	_ = runCommand("", os.Stdout, os.Stderr, "systemctl", "enable", "mihomo")
	if err := runCommand("", os.Stdout, os.Stderr, "systemctl", "restart", "mihomo"); err != nil {
		return err
	}
	e.waitForLinuxService("mihomo")
	if e.isLinuxServiceActive("mihomo") {
		logSuccess("mihomo started and enabled")
		return nil
	}
	_ = runCommand("", os.Stdout, os.Stderr, "journalctl", "-u", "mihomo", "-n", "10", "--no-pager")
	return fmt.Errorf("mihomo failed to start")
}

func (e *Env) ServiceStop() error {
	if e.OS == "darwin" {
		if err := e.stopDarwinService(); err != nil {
			return err
		}
		_ = e.stopBrewServices()
		logSuccess("mihomo stopped")
		return nil
	}
	if err := e.RequireRoot("stop"); err != nil {
		return err
	}
	_ = runCommand("", os.Stdout, os.Stderr, "systemctl", "stop", "mihomo")
	logSuccess("mihomo stopped")
	return nil
}

func (e *Env) ServiceRestart() error {
	if e.OS == "darwin" {
		if err := e.restartDarwinService(); err != nil {
			return err
		}
		if e.isDarwinServiceRunning() {
			logSuccess("mihomo restarted")
			return nil
		}
		e.showRecentDarwinLogs()
		return fmt.Errorf("mihomo failed to restart")
	}
	if err := e.RequireRoot("restart"); err != nil {
		return err
	}
	if err := runCommand("", os.Stdout, os.Stderr, "systemctl", "restart", "mihomo"); err != nil {
		return err
	}
	e.waitForLinuxService("mihomo")
	if e.isLinuxServiceActive("mihomo") {
		logSuccess("mihomo restarted")
		return nil
	}
	_ = runCommand("", os.Stdout, os.Stderr, "journalctl", "-u", "mihomo", "-n", "10", "--no-pager")
	return fmt.Errorf("mihomo failed to restart")
}

func (e *Env) ServiceStatus() error {
	if e.OS == "darwin" {
		fmt.Fprintln(os.Stdout, "Mihomo (macOS)")
		fmt.Fprintln(os.Stdout)
		binaryPath, _ := e.resolveDarwinMihomoBinary()
		fmt.Fprintf(os.Stdout, "  Config:  %s %s\n", filepath.Join(e.ConfigDir, "config.yaml"), existsLabel(fileExists(filepath.Join(e.ConfigDir, "config.yaml")), "exists", "MISSING"))
		if binaryPath != "" {
			version := strings.TrimSpace(runOptional("", binaryPath, "-v"))
			fmt.Fprintf(os.Stdout, "  Binary:  %s\n", binaryPath)
			fmt.Fprintf(os.Stdout, "  Version: %s\n", firstLine(version, "unknown"))
		} else {
			fmt.Fprintln(os.Stdout, "  Binary:  not found")
		}
		fmt.Fprintf(os.Stdout, "  UI:      %s %s\n", filepath.Join(e.ConfigDir, "ui"), existsLabel(dirExists(filepath.Join(e.ConfigDir, "ui")), "installed", "not installed"))
		fmt.Fprintf(os.Stdout, "  Log:     %s\n", e.LogPath)
		fmt.Fprintf(os.Stdout, "  Plist:   %s %s\n", e.PlistPath, existsLabel(fileExists(e.PlistPath), "installed", "MISSING - run: sudo mihctl install"))
		fmt.Fprintf(os.Stdout, "  Sudoers: %s %s\n", e.SudoersPath, existsLabel(fileExists(e.SudoersPath), "installed", "MISSING - start/stop will require sudo"))
		fmt.Fprintln(os.Stdout)
		if fileExists(e.PlistPath) {
			if e.isDarwinServiceRunning() {
				logSuccess("mihomo is running (LaunchDaemon)")
				return nil
			}
			if e.isDarwinServiceLoaded() {
				logWarn("mihomo service loaded but not running")
				e.showRecentDarwinLogs()
				return nil
			}
			logError("mihomo service not loaded. Run: mihctl service start")
		}
		return nil
	}

	logStep("Mihomo Service Status")
	if runCommand("", nil, nil, "systemctl", "list-unit-files", "mihomo.service") != nil {
		return fmt.Errorf("mihomo.service not installed")
	}
	active, sub := e.linuxServiceState("mihomo")
	enabled := e.linuxServiceEnabled("mihomo")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Service: mihomo.service")
	fmt.Fprintf(os.Stdout, "  Active:  %s (%s)\n", active, sub)
	fmt.Fprintf(os.Stdout, "  Enabled: %s\n", enabled)
	binaryPath := filepath.Join(e.InstallDir, "mihomo")
	if fileExists(binaryPath) {
		version := strings.TrimSpace(runOptional("", binaryPath, "-v"))
		fmt.Fprintf(os.Stdout, "  Binary:  %s\n", binaryPath)
		fmt.Fprintf(os.Stdout, "  Version: %s\n", firstLine(version, "unknown"))
	} else {
		fmt.Fprintf(os.Stdout, "  Binary:  NOT FOUND at %s\n", binaryPath)
	}
	fmt.Fprintf(os.Stdout, "  Config:  %s %s\n", filepath.Join(e.ConfigDir, "config.yaml"), existsLabel(fileExists(filepath.Join(e.ConfigDir, "config.yaml")), "exists", "MISSING"))
	fmt.Fprintf(os.Stdout, "  UI:      %s %s\n", filepath.Join(e.ConfigDir, "ui"), existsLabel(dirExists(filepath.Join(e.ConfigDir, "ui")), "installed", "not installed"))
	fmt.Fprintln(os.Stdout)
	switch {
	case active == "active":
		logSuccess("mihomo is running")
		return nil
	case active == "activating" || sub == "auto-restart":
		logWarn("mihomo is crash-looping (%s/%s)", active, sub)
		_ = runCommand("", os.Stdout, os.Stderr, "journalctl", "-u", "mihomo", "-n", "10", "--no-pager")
		return fmt.Errorf("mihomo is crash-looping")
	default:
		return fmt.Errorf("mihomo is not running (%s)", active)
	}
}

func existsLabel(ok bool, yes, no string) string {
	if ok {
		return "(" + yes + ")"
	}
	return "(" + no + ")"
}

func firstLine(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	lines := strings.Split(value, "\n")
	return lines[0]
}
