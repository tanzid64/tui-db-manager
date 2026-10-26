package db

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunCmdCapturesStderrOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	err := runCmd(context.Background(), cmdSpec{
		Path: "/bin/sh",
		Args: []string{"-c", "echo boom 1>&2; exit 3"},
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing stderr 'boom', got %v", err)
	}
}

func TestRunCmdPassesEnvAndStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	var out bytes.Buffer
	err := runCmd(context.Background(), cmdSpec{
		Path:   "/bin/sh",
		Args:   []string{"-c", "printf %s \"$SECRET\""},
		Env:    []string{"SECRET=p@ss w\"rd$x"},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != `p@ss w"rd$x` {
		t.Fatalf("env value not passed verbatim, got %q", out.String())
	}
}

func TestLookToolMissing(t *testing.T) {
	_, err := lookTool("definitely-not-a-real-tool-xyz")
	if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-tool-xyz") {
		t.Fatalf("expected missing-tool error naming the tool, got %v", err)
	}
}

func TestLookToolSearchesExtraDirs(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "dbk-fake-dump")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := extraToolDirs
	extraToolDirs = func() []string { return []string{filepath.Join(dir, "missing"), dir} }
	defer func() { extraToolDirs = orig }()

	got, err := lookTool("dbk-not-on-path", "dbk-fake-dump")
	if err != nil || got != tool {
		t.Fatalf("lookTool = %q, %v; want %q", got, err, tool)
	}
}
