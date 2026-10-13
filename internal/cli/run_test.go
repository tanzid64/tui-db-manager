package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/history"
)

// Exercises dbk run end-to-end with a sqlite job and local storage.
func TestRunCommandSQLiteEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	cfgDir := t.TempDir()
	srcDB := filepath.Join(t.TempDir(), "app.db")
	backups := filepath.Join(t.TempDir(), "backups")
	seed := exec.Command("sqlite3", srcDB, "CREATE TABLE t(x); INSERT INTO t VALUES(7);")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}

	cfgYAML := "" +
		"version: 1\n" +
		"jobs:\n" +
		"  - name: main\n" +
		"    engine: sqlite\n" +
		"    storage: local\n" +
		"    conn:\n" +
		"      path: " + srcDB + "\n" +
		"storages:\n" +
		"  - name: local\n" +
		"    type: local\n" +
		"    path: " + backups + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBK_PASSPHRASE", "pw")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "run", "main"})
	if err := root.Execute(); err != nil {
		t.Fatalf("run: %v (out=%s)", err, out.String())
	}

	entries, _ := os.ReadDir(filepath.Join(backups, "main"))
	if len(entries) == 0 {
		t.Fatalf("expected a backup file under %s/main", backups)
	}
}

// The scheduler writes `run <job> --quiet --json` into every wrapper, so the
// run command must register a --quiet flag or scheduled runs fail to parse.
func TestRunCommandHasQuietFlag(t *testing.T) {
	cmd := newRunCmd()
	if cmd.Flags().Lookup("quiet") == nil {
		t.Fatal("run command is missing the --quiet flag")
	}
}

// A headless `dbk run` (the path cron uses) must record a durable history
// entry so scheduled backups appear in the audit trail across restarts.
func TestRunCommandRecordsHistory(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	cfgDir := t.TempDir()
	srcDB := filepath.Join(t.TempDir(), "app.db")
	backups := filepath.Join(t.TempDir(), "backups")
	seed := exec.Command("sqlite3", srcDB, "CREATE TABLE t(x); INSERT INTO t VALUES(1);")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}
	cfgYAML := "version: 1\n" +
		"jobs:\n  - name: main\n    engine: sqlite\n    storage: local\n    conn:\n      path: " + srcDB + "\n" +
		"storages:\n  - name: local\n    type: local\n    path: " + backups + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBK_PASSPHRASE", "pw")

	root := NewRootCmd()
	root.SetArgs([]string{"--config", cfgDir, "run", "main", "--quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}

	recs := history.New(filepath.Join(cfgDir, "history.jsonl")).All()
	if len(recs) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(recs))
	}
	if recs[0].Job != "main" || recs[0].Status != history.StatusSuccess {
		t.Fatalf("unexpected history record: %+v", recs[0])
	}
}
