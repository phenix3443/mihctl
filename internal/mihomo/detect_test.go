package mihomo

import "testing"

func TestParseStandaloneRuntimeLine(t *testing.T) {
	line := "/opt/homebrew/opt/mihomo/bin/mihomo -d /Users/demo/.config/mihomo"
	binaryPath, configDir, ok := parseStandaloneRuntimeLine(line)
	if !ok {
		t.Fatal("expected standalone runtime line to parse")
	}
	if binaryPath != "/opt/homebrew/opt/mihomo/bin/mihomo" {
		t.Fatalf("binaryPath = %q", binaryPath)
	}
	if configDir != "/Users/demo/.config/mihomo" {
		t.Fatalf("configDir = %q", configDir)
	}
}

func TestParseVergeRuntimeLine(t *testing.T) {
	line := "/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo -d /Users/demo/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev -f /Users/demo/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev/profiles/demo.yaml"
	binaryPath, configFile, ok := parseVergeRuntimeLine(line)
	if !ok {
		t.Fatal("expected verge runtime line to parse")
	}
	if binaryPath != "/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo" {
		t.Fatalf("binaryPath = %q", binaryPath)
	}
	if configFile != "/Users/demo/Library/Application Support/io.github.clash-verge-rev.clash-verge-rev/profiles/demo.yaml" {
		t.Fatalf("configFile = %q", configFile)
	}
}

func TestParseLinuxRuntimeLine(t *testing.T) {
	line := "/usr/local/bin/mihomo -f /etc/clash/config.yaml"
	binaryPath, configFile, ok := parseLinuxRuntimeLine(line)
	if !ok {
		t.Fatal("expected linux runtime line to parse")
	}
	if binaryPath != "/usr/local/bin/mihomo" {
		t.Fatalf("binaryPath = %q", binaryPath)
	}
	if configFile != "/etc/clash/config.yaml" {
		t.Fatalf("configFile = %q", configFile)
	}
}
