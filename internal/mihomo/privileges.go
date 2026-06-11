package mihomo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func needsPrivilegeFallback(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(err.Error()), "permission denied")
}

func mkdirAllPrivileged(path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		if !needsPrivilegeFallback(err) || !commandExists("sudo") {
			return err
		}
		return runCommand("", os.Stdout, os.Stderr, "sudo", "mkdir", "-p", path)
	}
	return nil
}

func writeFilePrivileged(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err == nil {
		return nil
	} else if !needsPrivilegeFallback(err) || !commandExists("sudo") {
		return err
	}

	tmp, err := os.CreateTemp("", "mihomo-write-*")
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
	defer os.Remove(tmpPath)

	modeString := fmt.Sprintf("%#o", mode&0o777)
	return runCommand("", os.Stdout, os.Stderr, "sudo", "install", "-m", modeString, tmpPath, path)
}

func copyFilePrivileged(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeFilePrivileged(dst, data, mode)
}

func removeFilePrivileged(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		if !needsPrivilegeFallback(err) || !commandExists("sudo") {
			return err
		}
		return runCommand("", os.Stdout, os.Stderr, "sudo", "rm", "-f", path)
	}
	return nil
}

func renameFilePrivileged(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		if !needsPrivilegeFallback(err) || !commandExists("sudo") {
			return err
		}
		return runCommand("", os.Stdout, os.Stderr, "sudo", "mv", "-f", src, dst)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func tempPathInDir(dir, pattern string) (string, error) {
	if err := mkdirAllPrivileged(dir, 0o755); err != nil {
		return "", err
	}
	tmpDir := dir
	if !dirWritable(dir) {
		tmpDir = ""
	}
	file, err := os.CreateTemp(tmpDir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func dirWritable(dir string) bool {
	testFile := filepath.Join(dir, ".mihomo-write-test")
	if err := os.WriteFile(testFile, []byte("1"), 0o600); err != nil {
		return false
	}
	_ = os.Remove(testFile)
	return true
}
