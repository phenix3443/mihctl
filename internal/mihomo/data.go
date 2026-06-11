package mihomo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (e *Env) SetupGeodata() {
	_ = e.setupGeodataInDir(e.ConfigDir)
}

func (e *Env) setupGeodataInDir(destDir string) error {
	if err := mkdirAllPrivileged(destDir, 0o755); err != nil {
		return err
	}
	geositePath := filepath.Join(destDir, "geosite.dat")
	if !fileExists(geositePath) {
		logInfo("Downloading geosite.dat (required for first start without DNS loop)")
		if err := downloadToFile(e.GeoSiteURL, geositePath, e.FetchConnectTimeout, e.FetchMaxTime); err == nil {
			logSuccess("geosite.dat installed")
		} else {
			logWarn("geosite.dat download failed; start may fail until it is present in %s", destDir)
		}
	} else {
		logInfo("geosite.dat already exists")
	}

	geoipPath := filepath.Join(destDir, "geoip.dat")
	if !fileExists(geoipPath) {
		logInfo("Downloading geoip.dat")
		if err := downloadToFile(e.GeoIPURL, geoipPath, e.FetchConnectTimeout, e.FetchMaxTime); err == nil {
			logSuccess("geoip.dat installed")
		} else {
			logWarn("geoip.dat download failed; start may fail until it is present in %s", destDir)
		}
	} else {
		logInfo("geoip.dat already exists")
	}
	return nil
}

func (e *Env) UpdateGeodata() error {
	if e.OS != "darwin" {
		if err := e.RequireRoot("update-geodata"); err != nil {
			return err
		}
	}
	destDir, err := e.detectGeodataDir()
	if err != nil {
		return err
	}
	logStep("Downloading geodata to %s", destDir)
	_ = removeFilePrivileged(filepath.Join(destDir, "geosite.dat"))
	_ = removeFilePrivileged(filepath.Join(destDir, "geoip.dat"))
	_ = removeFilePrivileged(filepath.Join(destDir, "Country.mmdb"))
	if err := e.setupGeodataInDir(destDir); err != nil {
		return err
	}
	if err := e.updateCountryMMDBInDir(destDir); err != nil {
		return err
	}
	logSuccess("Geodata updated. Restart the client to load it.")
	return nil
}

func (e *Env) detectGeodataDir() (string, error) {
	destDir, err := e.detectLiveConfigDir()
	if err != nil {
		if e.OS == "darwin" && dirExists(e.ClashVergeDataDir()) {
			return e.ClashVergeDataDir(), nil
		} else {
			if strings.TrimSpace(e.ConfigDir) == "" {
				return "", fmt.Errorf("could not detect config directory (start mihomo / Verge or set CONFIG_DIR)")
			}
			return e.ConfigDir, nil
		}
	}
	return destDir, nil
}

func (e *Env) updateCountryMMDBInDir(destDir string) error {
	dest := filepath.Join(destDir, "Country.mmdb")
	logStep("Country.mmdb -> %s", dest)
	logInfo("URL: %s", e.MMDBURL)

	tmp, err := os.CreateTemp("", "Country.mmdb.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := downloadToFile(e.MMDBURL, tmpPath, e.FetchConnectTimeout, e.FetchMaxTime); err != nil {
		return fmt.Errorf("download failed")
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		return err
	}
	if info.Size() < 200000 {
		return fmt.Errorf("download too small or invalid (%d bytes)", info.Size())
	}
	if fileExists(dest) {
		backup := dest + ".bak." + timestampNow()
		if err := copyFilePrivileged(dest, backup, 0o644); err == nil {
			logInfo("Backed up: %s", backup)
		}
	}
	if err := renameFilePrivileged(tmpPath, dest); err != nil {
		return err
	}
	logSuccess("Wrote %s (%d bytes).", dest, info.Size())
	return nil
}

func timestampNow() string {
	return strings.TrimSpace(runOptional("", "date", "+%Y%m%d%H%M%S"))
}
