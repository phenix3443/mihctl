package mihomo

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (e *Env) downloadName() (string, error) {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}

	switch e.OS + "_" + arch {
	case "linux_amd64":
		return "mihomo-linux-amd64-compatible-v" + e.MihomoVersion + ".gz", nil
	case "linux_arm64":
		return "mihomo-linux-arm64-v" + e.MihomoVersion + ".gz", nil
	case "darwin_amd64":
		return "mihomo-darwin-amd64-compatible-v" + e.MihomoVersion + ".gz", nil
	case "darwin_arm64":
		return "mihomo-darwin-arm64-v" + e.MihomoVersion + ".gz", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s_%s", e.OS, arch)
	}
}

func (e *Env) downloadMihomo() (string, error) {
	name, err := e.downloadName()
	if err != nil {
		return "", err
	}

	localArchive := filepath.Join(e.RepoRoot, "backup", name)
	if pathExists(localArchive) {
		logInfo("Using local archive: %s", localArchive)
		return localArchive, nil
	}

	url := e.DownloadURL
	if url == "" {
		url = "https://github.com/MetaCubeX/mihomo/releases/download/v" + e.MihomoVersion + "/" + name
	}
	logInfo("Downloading from: %s", url)

	tmpFile := filepath.Join(os.TempDir(), name)
	if err := downloadToFile(url, tmpFile, e.FetchConnectTimeout, e.FetchMaxTime); err != nil {
		return "", fmt.Errorf("download failed. set MIHOMO_DOWNLOAD_URL or place archive in %s/backup: %w", e.RepoRoot, err)
	}
	return tmpFile, nil
}

func downloadToFile(url, dest string, connectTimeout, maxTime time.Duration) error {
	client := &http.Client{
		Timeout: maxTime,
		Transport: &http.Transport{
			ResponseHeaderTimeout: connectTimeout,
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}

	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func (e *Env) stopForBinaryUpdate() {
	if e.OS == "darwin" {
		_ = e.stopBrewServices()
		if e.IsRoot() && e.isDarwinServiceLoaded() {
			_ = e.stopDarwinService()
		}
		return
	}
	if e.isLinuxServiceActive("mihomo") {
		_ = runCommand("", os.Stdout, os.Stderr, "systemctl", "stop", "mihomo")
		logInfo("Stopped running mihomo before binary update")
	}
}

func (e *Env) installBinary(archive string) error {
	e.stopForBinaryUpdate()
	if err := os.MkdirAll(e.InstallDir, 0o755); err != nil {
		return err
	}

	in, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer in.Close()

	reader, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer reader.Close()

	target := filepath.Join(e.InstallDir, "mihomo")
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, reader); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return err
	}

	if strings.HasPrefix(archive, os.TempDir()+string(os.PathSeparator)) {
		_ = os.Remove(archive)
	}
	versionOutput, _ := runCommandOutput("", target, "-v")
	logSuccess("Installed: %s", strings.TrimSpace(versionOutput))
	return nil
}
