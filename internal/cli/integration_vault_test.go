package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/history"
	"github.com/tanzid64/tui-db-manager/internal/store"
)

func TestVaultSQLiteFullWorkflow(t *testing.T) {
	dir := t.TempDir()
	masterKey := "my-secure-master-key-2026"
	t.Setenv("DBK_MASTER_KEY", masterKey)

	// 1. dbk init: initializes SQLite db and saves key to configDir/env
	root := NewRootCmd()
	root.SetArgs([]string{"init", "--config", dir})
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	if err := root.Execute(); err != nil {
		t.Fatalf("dbk init failed: %v", err)
	}

	// Verify dbk.db exists
	dbPath := store.Path(dir)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected dbk.db to exist: %v", err)
	}

	// Verify env file was created and contains key
	envVal := store.ResolveMasterKey(dir)
	if envVal != masterKey {
		t.Fatalf("expected resolved master key %q, got %q", masterKey, envVal)
	}

	// 2. Open db directly and add a test job to configuration
	s, err := store.Open(dir, masterKey)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	testCfg := &config.Config{
		Version: 1,
		Jobs: []config.Job{
			{
				Name:    "prod-mysql",
				Engine:  "mysql",
				Storage: "local-backup",
			},
		},
		Storages: []config.StorageConfig{
			{
				Name: "local-backup",
				Type: "local",
				Path: filepath.Join(dir, "backups"),
			},
		},
	}
	if err := s.SaveConfig(testCfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// 3. dbk secret set
	setCmd := NewRootCmd()
	setCmd.SetArgs([]string{"secret", "set", "job/prod-mysql/password", "--value", "topsecret999", "--config", dir})
	if err := setCmd.Execute(); err != nil {
		t.Fatalf("dbk secret set failed: %v", err)
	}

	// Verify secret was encrypted into SQLite
	secVal, ok := s.GetSecret("job/prod-mysql/password")
	if !ok || secVal != "topsecret999" {
		t.Fatalf("expected secret to be topsecret999, got %q (ok=%v)", secVal, ok)
	}

	// 4. Record history into SQLite database and verify audit trail comes from database
	rec := history.RunRecord{
		ID:       "run-prod-01",
		Job:      "prod-mysql",
		Action:   "backup",
		Status:   history.StatusSuccess,
		Started:  time.Now(),
		Duration: 3 * time.Second,
		Size:     4096,
		Target:   "prod-mysql/backup.sql.gz",
	}
	if err := s.AppendHistory(rec); err != nil {
		t.Fatalf("AppendHistory failed: %v", err)
	}

	histRecords, err := s.AllHistory()
	if err != nil || len(histRecords) != 1 || histRecords[0].ID != "run-prod-01" {
		t.Fatalf("expected history record from SQLite db, got: %+v (err=%v)", histRecords, err)
	}

	// 5. dbk schedule install: the key is in the env file, so the wrapper must not copy it
	schedCmd := NewRootCmd()
	schedCmd.SetArgs([]string{"schedule", "install", "prod-mysql", "--at", "0 2 * * *", "--config", dir})
	if err := schedCmd.Execute(); err != nil {
		t.Fatalf("dbk schedule install failed: %v", err)
	}

	wrapperScript := filepath.Join(dir, "schedules", "dbk-prod-mysql.sh")
	wrapperContent, err := os.ReadFile(wrapperScript)
	if err != nil {
		t.Fatalf("read wrapper script: %v", err)
	}
	scriptStr := string(wrapperContent)
	if strings.Contains(scriptStr, "DBK_MASTER_KEY") || strings.Contains(scriptStr, masterKey) {
		t.Fatalf("wrapper script should not contain the master key: %s", scriptStr)
	}
	if !strings.Contains(scriptStr, "--config") {
		t.Fatalf("wrapper script must pass --config so the run finds the env file: %s", scriptStr)
	}

	// 6. dbk config export with transfer passphrase
	exportFile := filepath.Join(dir, "bundle.yaml")
	exportCmd := NewRootCmd()
	exportCmd.SetArgs([]string{
		"config", "export", exportFile,
		"--config", dir,
		"--include-secrets",
		"--transfer-passphrase", "transfer-pw-456",
	})
	if err := exportCmd.Execute(); err != nil {
		t.Fatalf("dbk config export failed: %v", err)
	}

	// 7. dbk config import into new destination directory
	destDir := t.TempDir()
	importCmd := NewRootCmd()
	importCmd.SetArgs([]string{
		"config", "import", exportFile,
		"--config", destDir,
		"--transfer-passphrase", "transfer-pw-456",
		"--yes",
	})
	if err := importCmd.Execute(); err != nil {
		t.Fatalf("dbk config import failed: %v", err)
	}

	// Verify imported SQLite database in destDir
	destStore, err := store.Open(destDir, store.ResolveMasterKey(destDir))
	if err != nil {
		t.Fatalf("open imported dest store: %v", err)
	}
	defer destStore.Close()

	importedSecret, ok := destStore.GetSecret("job/prod-mysql/password")
	if !ok || importedSecret != "topsecret999" {
		t.Fatalf("expected imported secret topsecret999, got %q (ok=%v)", importedSecret, ok)
	}

	destCfg, err := destStore.LoadConfig()
	if err != nil || len(destCfg.Jobs) != 1 || destCfg.Jobs[0].Name != "prod-mysql" {
		t.Fatalf("expected imported job prod-mysql, got: %+v", destCfg)
	}
}
