package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/schedule"
	"github.com/tanzid64/tui-db-manager/internal/store"
)

type uninstallMockScheduler struct {
	removedJobs []string
}

func (m *uninstallMockScheduler) Name() string { return "mock-scheduler" }
func (m *uninstallMockScheduler) Install(_ context.Context, _ string, _ schedule.InstallOptions) error {
	return nil
}
func (m *uninstallMockScheduler) Remove(_ context.Context, job string) error {
	m.removedJobs = append(m.removedJobs, job)
	return nil
}
func (m *uninstallMockScheduler) Status(_ context.Context, _ string) (schedule.ScheduleStatus, error) {
	return schedule.ScheduleStatus{}, nil
}

func TestUninstallRequiresYesNonInteractive(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"uninstall"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when running uninstall non-interactively without --yes")
	}
	if !strings.Contains(err.Error(), "confirmation required") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestUninstallCleanPurgeDataAndSchedules(t *testing.T) {
	mock := &uninstallMockScheduler{}
	orig := defaultScheduler
	defaultScheduler = mock
	defer func() { defaultScheduler = orig }()

	cfgDir := t.TempDir()
	envKey := "test-uninstall-secret-key-12345"
	t.Setenv("DBK_MASTER_KEY", envKey)

	// Create a store with a job
	s, err := store.Init(cfgDir, envKey)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	cfg := &config.Config{
		Version: config.CurrentVersion,
		Jobs: []config.Job{
			{Name: "prod-db", Engine: "postgres"},
		},
	}
	if err := s.SaveConfig(cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	_ = s.Close()

	// Add wrapper scripts in schedules/ dir including an orphaned job
	schedDir := filepath.Join(cfgDir, "schedules")
	if err := os.MkdirAll(schedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(schedDir, "dbk-prod-db.sh"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(filepath.Join(schedDir, "dbk-orphaned-job.sh"), []byte("#!/bin/sh\n"), 0o755)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"uninstall", "--config", cfgDir, "--yes", "--purge-data"})

	if err := root.Execute(); err != nil {
		t.Fatalf("uninstall command failed: %v\nOutput: %s", err, out.String())
	}

	outStr := out.String()
	if !strings.Contains(outStr, "Clean uninstall completed successfully") {
		t.Errorf("expected success message, got: %s", outStr)
	}

	// Verify scheduler removed both prod-db and orphaned-job
	hasProd := false
	hasOrphaned := false
	for _, j := range mock.removedJobs {
		if j == "prod-db" {
			hasProd = true
		}
		if j == "orphaned-job" {
			hasOrphaned = true
		}
	}
	if !hasProd {
		t.Errorf("expected prod-db to be removed from scheduler, got: %v", mock.removedJobs)
	}
	if !hasOrphaned {
		t.Errorf("expected orphaned-job to be removed from scheduler, got: %v", mock.removedJobs)
	}

	// Verify cfgDir was purged
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Errorf("expected cfgDir %s to be purged, but it still exists", cfgDir)
	}
}

func TestUninstallLegacyFlatConfig(t *testing.T) {
	mock := &uninstallMockScheduler{}
	orig := defaultScheduler
	defaultScheduler = mock
	defer func() { defaultScheduler = orig }()

	cfgDir := t.TempDir()
	cfgYAML := `version: 1
jobs:
  - name: legacy-job
    engine: mysql
    storage: local
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"uninstall", "--config", cfgDir, "--yes", "--purge-data"})

	if err := root.Execute(); err != nil {
		t.Fatalf("uninstall command failed: %v", err)
	}

	hasLegacy := false
	for _, j := range mock.removedJobs {
		if j == "legacy-job" {
			hasLegacy = true
		}
	}
	if !hasLegacy {
		t.Errorf("expected legacy-job to be removed from scheduler, got: %v", mock.removedJobs)
	}
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Errorf("expected cfgDir %s to be purged", cfgDir)
	}
}
