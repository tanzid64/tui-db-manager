package db

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// onlyTools makes PATH contain just the named fake executables.
func onlyTools(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	origDirs, origPM := extraToolDirs, packageManager
	extraToolDirs = func() []string { return nil }
	packageManager = func() string { return "apt" }
	t.Cleanup(func() { extraToolDirs, packageManager = origDirs, origPM })
}

func TestCheckToolsReportsMissingWithHint(t *testing.T) {
	onlyTools(t, "mysql")
	if err := CheckTools("mysql", ActionTest, ActionRestore); err != nil {
		t.Fatalf("mysql client is present, got %v", err)
	}
	err := CheckTools("mysql", ActionBackup)
	var mt *MissingToolsError
	if !errors.As(err, &mt) {
		t.Fatalf("expected MissingToolsError, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"mysqldump (or mariadb-dump)", "sudo apt install default-mysql-client"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
}

func TestCheckToolsAlternativeIsEnough(t *testing.T) {
	onlyTools(t, "mariadb", "mariadb-dump")
	if err := CheckTools("mysql"); err != nil {
		t.Fatalf("MariaDB tools should satisfy mysql: %v", err)
	}
}

func TestLookToolErrorHasHint(t *testing.T) {
	onlyTools(t)
	_, err := lookTool("pg_dump")
	if err == nil || !strings.Contains(err.Error(), "postgres client tools") ||
		!strings.Contains(err.Error(), "postgresql-client") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTunneledEngineChecksTools(t *testing.T) {
	onlyTools(t)
	e, _ := Get("postgres")
	tc, ok := e.(ToolChecker)
	if !ok || tc.CheckTools(ActionBackup) == nil {
		t.Fatal("engine from Get should report missing pg_dump")
	}
}
