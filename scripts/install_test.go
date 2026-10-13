package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestInstallScriptsExist(t *testing.T) {
	files := []string{
		"install.sh",
		filepath.Join("scripts", "install.sh"),
		".goreleaser.yaml",
	}

	for _, f := range files {
		fullPath := filepath.Join("..", f)
		info, err := os.Stat(fullPath)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", f, err)
		}
		if info.Size() == 0 {
			t.Fatalf("expected %s to have content", f)
		}
	}
}

func TestGoreleaserConfigValid(t *testing.T) {
	path := filepath.Join("..", ".goreleaser.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}

	var m struct {
		Version int `yaml:"version"`
		Builds  []struct {
			Binary string   `yaml:"binary"`
			Goos   []string `yaml:"goos"`
			Goarch []string `yaml:"goarch"`
		} `yaml:"builds"`
	}

	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal .goreleaser.yaml: %v", err)
	}

	if m.Version != 2 {
		t.Fatalf("expected goreleaser version 2, got %d", m.Version)
	}

	if len(m.Builds) == 0 {
		t.Fatalf("expected at least one build configuration")
	}

	build := m.Builds[0]
	if build.Binary != "dbk" {
		t.Fatalf("expected binary name 'dbk', got %q", build.Binary)
	}

	expectedOS := map[string]bool{"linux": true, "darwin": true}
	for _, o := range build.Goos {
		delete(expectedOS, o)
	}
	if len(expectedOS) > 0 {
		t.Fatalf("missing required OS targets in goreleaser: %v", expectedOS)
	}

	expectedArch := map[string]bool{"amd64": true, "arm64": true}
	for _, a := range build.Goarch {
		delete(expectedArch, a)
	}
	if len(expectedArch) > 0 {
		t.Fatalf("missing required Arch targets in goreleaser: %v", expectedArch)
	}
}

func TestInstallScriptOSArchDetection(t *testing.T) {
	testCases := []struct {
		unameS       string
		unameM       string
		expectedOS   string
		expectedArch string
	}{
		{"Linux", "x86_64", "linux", "amd64"},
		{"Linux", "aarch64", "linux", "arm64"},
		{"Darwin", "arm64", "darwin", "arm64"},
		{"Darwin", "x86_64", "darwin", "amd64"},
	}

	for _, tc := range testCases {
		t.Run(tc.unameS+"_"+tc.unameM, func(t *testing.T) {
			// Script fragment to test OS and Arch detection logic
			script := `
OS="` + tc.unameS + `"
OS=$(echo "$OS" | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux*)  OS="linux" ;;
  darwin*) OS="darwin" ;;
  *) exit 1 ;;
esac

ARCH="` + tc.unameM + `"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) exit 1 ;;
esac

echo "$OS:$ARCH"
`
			cmd := exec.Command("sh", "-c", script)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("script failed: %v, stderr: %s", err, stderr.String())
			}

			out := strings.TrimSpace(stdout.String())
			parts := strings.Split(out, ":")
			if len(parts) != 2 {
				t.Fatalf("unexpected output: %s", out)
			}
			if parts[0] != tc.expectedOS {
				t.Errorf("expected OS %q, got %q", tc.expectedOS, parts[0])
			}
			if parts[1] != tc.expectedArch {
				t.Errorf("expected Arch %q, got %q", tc.expectedArch, parts[1])
			}
		})
	}
}
