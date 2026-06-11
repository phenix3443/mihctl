package mihomo

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func logInfo(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[INFO] "+format+"\n", args...)
}

func logSuccess(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[OK]   "+format+"\n", args...)
}

func logWarn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[WARN] "+format+"\n", args...)
}

func logError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[ERR]  "+format+"\n", args...)
}

func logStep(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "==> "+format+"\n", args...)
}

func runCommand(dir string, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = os.Stdin
	return command.Run()
}

func runCommandOutput(dir string, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message != "" {
			return "", fmt.Errorf("%w: %s", err, message)
		}
		return "", err
	}
	return stdout.String(), nil
}

func runOptional(dir string, name string, args ...string) string {
	output, err := runCommandOutput(dir, name, args...)
	if err != nil {
		return ""
	}
	return output
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
