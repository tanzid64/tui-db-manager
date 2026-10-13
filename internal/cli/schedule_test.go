package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/schedule"
)

type mockScheduler struct {
	installed  bool
	expression string
	targetName string
}

func (m *mockScheduler) Name() string { return m.targetName }
func (m *mockScheduler) Install(_ context.Context, _ string, opts schedule.InstallOptions) error {
	m.installed = true
	m.expression = opts.Expression
	return nil
}
func (m *mockScheduler) Remove(_ context.Context, _ string) error {
	m.installed = false
	m.expression = ""
	return nil
}
func (m *mockScheduler) Status(_ context.Context, _ string) (schedule.ScheduleStatus, error) {
	return schedule.ScheduleStatus{
		Installed:  m.installed,
		Expression: m.expression,
		Target:     m.targetName,
	}, nil
}

func TestScheduleCLICommands(t *testing.T) {
	mock := &mockScheduler{targetName: "mock-timer"}
	orig := defaultScheduler
	defaultScheduler = mock
	defer func() { defaultScheduler = orig }()

	cfgDir := t.TempDir()
	cfgYAML := `version: 1
jobs:
  - name: my-db
    engine: sqlite
    storage: local
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. Install schedule
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "schedule", "install", "my-db", "--at", "15 3 * * *"})
	if err := root.Execute(); err != nil {
		t.Fatalf("install execute: %v", err)
	}
	if !mock.installed || mock.expression != "15 3 * * *" {
		t.Fatalf("install mock mismatch: installed=%v expr=%s", mock.installed, mock.expression)
	}
	if !strings.Contains(out.String(), "schedule installed for \"my-db\"") {
		t.Fatalf("unexpected install output: %s", out.String())
	}

	// 2. Status schedule
	root = NewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "schedule", "status", "my-db"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status execute: %v", err)
	}
	if !strings.Contains(out.String(), "schedule for \"my-db\" is active") {
		t.Fatalf("unexpected status output: %s", out.String())
	}

	// 3. Status with --json
	root = NewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "--json", "schedule", "status", "my-db"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status json execute: %v", err)
	}
	var statResp schedule.ScheduleStatus
	if err := json.Unmarshal(out.Bytes(), &statResp); err != nil {
		t.Fatalf("unmarshal json status: %v (raw: %s)", err, out.String())
	}
	if !statResp.Installed || statResp.Expression != "15 3 * * *" {
		t.Fatalf("json status mismatch: %+v", statResp)
	}

	// 4. Remove schedule
	root = NewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "schedule", "remove", "my-db"})
	if err := root.Execute(); err != nil {
		t.Fatalf("remove execute: %v", err)
	}
	if mock.installed {
		t.Fatal("expected mock to be uninstalled")
	}
	if !strings.Contains(out.String(), "schedule removed for \"my-db\"") {
		t.Fatalf("unexpected remove output: %s", out.String())
	}

	// 5. Unknown job error
	root = NewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "schedule", "install", "nonexistent"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for unknown job")
	}
}

// A scheduled job runs unattended with no TTY, so when a vault exists the
// install must refuse unless a non-interactive passphrase source is given.
func TestScheduleInstallRequiresPassphraseWhenVaultExists(t *testing.T) {
	mock := &mockScheduler{targetName: "mock-timer"}
	orig := defaultScheduler
	defaultScheduler = mock
	defer func() { defaultScheduler = orig }()

	t.Setenv("DBK_PASSPHRASE", "")
	os.Unsetenv("DBK_PASSPHRASE")

	cfgDir := t.TempDir()
	cfgYAML := `version: 1
jobs:
  - name: my-db
    engine: sqlite
    storage: local
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	// Presence of a vault file is what triggers the requirement.
	if err := os.WriteFile(filepath.Join(cfgDir, "secrets.enc"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Without a passphrase source: install must fail and not touch the scheduler.
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "schedule", "install", "my-db"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected install to fail when vault exists and no passphrase source is given")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("error should mention passphrase, got: %v", err)
	}
	if mock.installed {
		t.Fatal("scheduler must not be invoked when the passphrase guard fails")
	}

	// With --passphrase-file: install proceeds.
	passFile := filepath.Join(cfgDir, "pass")
	if err := os.WriteFile(passFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	root = NewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "schedule", "install", "my-db", "--passphrase-file", passFile})
	if err := root.Execute(); err != nil {
		t.Fatalf("install with passphrase file should succeed: %v", err)
	}
	if !mock.installed {
		t.Fatal("expected scheduler to be invoked once passphrase source is provided")
	}
}
