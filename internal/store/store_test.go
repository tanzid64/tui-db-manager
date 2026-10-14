package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/history"
)

func TestStoreInitOpenAndSecrets(t *testing.T) {
	dir := t.TempDir()
	pass := "my-master-password-123"

	// 1. Init
	db, err := Init(dir, pass)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer db.Close()

	if !db.IsUnlocked() {
		t.Fatal("expected store to be unlocked after Init")
	}

	// Verify permissions
	info, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}

	// 2. Set Secrets
	if err := db.SetSecret("job/prod/password", "secretpass"); err != nil {
		t.Fatalf("SetSecret failed: %v", err)
	}
	if err := db.SetSecret("storage/s3/key", "s3secret"); err != nil {
		t.Fatalf("SetSecret failed: %v", err)
	}

	// 3. Get Secret
	val, ok := db.GetSecret("job/prod/password")
	if !ok || val != "secretpass" {
		t.Fatalf("expected secretpass, got %q (ok=%v)", val, ok)
	}

	// 4. Close and reopen with wrong password
	_ = db.Close()

	_, err = Open(dir, "wrong-password")
	if err == nil {
		t.Fatal("expected Open with wrong passphrase to fail")
	}

	// 5. Open with correct password
	db2, err := Open(dir, pass)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db2.Close()

	val, ok = db2.GetSecret("job/prod/password")
	if !ok || val != "secretpass" {
		t.Fatalf("expected secretpass after reopen, got %q (ok=%v)", val, ok)
	}

	// 6. Delete Secret
	if err := db2.DeleteSecret("storage/s3/key"); err != nil {
		t.Fatalf("DeleteSecret failed: %v", err)
	}
	_, ok = db2.GetSecret("storage/s3/key")
	if ok {
		t.Fatal("expected deleted secret to be missing")
	}

	// 7. List Secret Keys
	keys, err := db2.ListSecretKeys()
	if err != nil {
		t.Fatalf("ListSecretKeys failed: %v", err)
	}
	if len(keys) != 1 || keys[0] != "job/prod/password" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

func TestStoreConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pass := "cfg-master-key"

	db, err := Init(dir, pass)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer db.Close()

	originalCfg := &config.Config{
		Version: 1,
		Jobs: []config.Job{
			{
				Name:   "postgres-prod",
				Engine: "postgres",
				Conn: config.Conn{
					Host:        "127.0.0.1",
					Port:        5432,
					User:        "pguser",
					Database:    "pgdb",
					TLS:         true,
					PasswordRef: "env:PG_PASS",
				},
				Storage:          "s3-main",
				Retention:        config.Retention{KeepLast: 7},
				Compress:         true,
				Encrypt:          true,
				Schedule:         "0 3 * * *",
				EncryptRecipient: "age1xyz",
				Notifiers:        []string{"tg-alerts"},
			},
		},
		Storages: []config.StorageConfig{
			{
				Name:           "s3-main",
				Type:           "s3",
				Bucket:         "mybucket",
				Region:         "us-east-1",
				Endpoint:       "https://s3.example.com",
				Prefix:         "backups/",
				AccessKeyID:    "AKIA123",
				ForcePathStyle: true,
				SecretKeyRef:   "storage/s3-main/secret_key",
			},
		},
		Notifiers: []config.NotifierConfig{
			{
				Name:        "tg-alerts",
				Type:        "telegram",
				BotTokenRef: "notifier/tg-alerts/bot_token",
				ChatID:      "123456",
			},
		},
	}

	if err := db.SaveConfig(originalCfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := db.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(loaded.Jobs) != 1 || loaded.Jobs[0].Name != "postgres-prod" {
		t.Fatalf("unexpected loaded jobs: %+v", loaded.Jobs)
	}
	if !loaded.Jobs[0].Conn.TLS || !loaded.Jobs[0].Compress || !loaded.Jobs[0].Encrypt {
		t.Fatalf("boolean flags not preserved: %+v", loaded.Jobs[0])
	}
	if len(loaded.Jobs[0].Notifiers) != 1 || loaded.Jobs[0].Notifiers[0] != "tg-alerts" {
		t.Fatalf("notifiers slice not preserved: %v", loaded.Jobs[0].Notifiers)
	}
	if len(loaded.Storages) != 1 || loaded.Storages[0].Name != "s3-main" || !loaded.Storages[0].ForcePathStyle {
		t.Fatalf("storages not preserved: %+v", loaded.Storages)
	}
	if len(loaded.Notifiers) != 1 || loaded.Notifiers[0].ChatID != "123456" {
		t.Fatalf("notifiers not preserved: %+v", loaded.Notifiers)
	}
}

func TestStoreHistory(t *testing.T) {
	dir := t.TempDir()
	pass := "hist-pass"

	db, err := Init(dir, pass)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer db.Close()

	rec1 := history.RunRecord{
		ID:       "run-1",
		Job:      "job-a",
		Action:   "backup",
		Status:   history.StatusSuccess,
		Started:  time.Now().Add(-10 * time.Minute),
		Duration: 5 * time.Second,
		Size:     1024,
	}
	rec2 := history.RunRecord{
		ID:       "run-2",
		Job:      "job-a",
		Action:   "backup",
		Status:   history.StatusFailed,
		Started:  time.Now(),
		Duration: 2 * time.Second,
		Error:    "connection refused",
	}

	if err := db.AppendHistory(rec1); err != nil {
		t.Fatalf("AppendHistory 1 failed: %v", err)
	}
	if err := db.AppendHistory(rec2); err != nil {
		t.Fatalf("AppendHistory 2 failed: %v", err)
	}

	all, err := db.AllHistory()
	if err != nil {
		t.Fatalf("AllHistory failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	// Newest first
	if all[0].ID != "run-2" || all[0].Status != history.StatusFailed {
		t.Fatalf("expected newest record first, got %+v", all[0])
	}

	last, err := db.LastForJob("job-a")
	if err != nil {
		t.Fatalf("LastForJob failed: %v", err)
	}
	if last == nil || last.ID != "run-2" {
		t.Fatalf("expected run-2 as last run, got %+v", last)
	}
}

func TestStoreMigrationFromFiles(t *testing.T) {
	dir := t.TempDir()

	// 1. Write legacy config.yaml
	legacyCfg := `version: 1
jobs:
  - name: legacy-job
    engine: mysql
    storage: local-disk
storages:
  - name: local-disk
    type: local
    path: /tmp/backups
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(legacyCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Write legacy secrets.json
	secData, _ := json.Marshal(map[string]string{"job/legacy-job/password": "legacypass"})
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), secData, 0o600); err != nil {
		t.Fatal(err)
	}

	// 3. Write legacy history.jsonl
	histRec := history.RunRecord{
		ID:      "hist-legacy-1",
		Job:     "legacy-job",
		Action:  "backup",
		Status:  history.StatusSuccess,
		Started: time.Now(),
	}
	histLine, _ := json.Marshal(histRec)
	if err := os.WriteFile(filepath.Join(dir, "history.jsonl"), append(histLine, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	// 4. Initialize store and run migration
	db, err := Init(dir, "migration-master-pass")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer db.Close()

	migrated, err := MigrateFromFiles(dir, db)
	if err != nil {
		t.Fatalf("MigrateFromFiles failed: %v", err)
	}
	if !migrated {
		t.Fatal("expected files to be migrated")
	}

	// Verify migrated config
	cfg, err := db.LoadConfig()
	if err != nil || len(cfg.Jobs) != 1 || cfg.Jobs[0].Name != "legacy-job" {
		t.Fatalf("migrated config incorrect: %+v", cfg)
	}

	// Verify migrated secret
	sec, ok := db.GetSecret("job/legacy-job/password")
	if !ok || sec != "legacypass" {
		t.Fatalf("migrated secret incorrect: %q (ok=%v)", sec, ok)
	}

	// Verify migrated history
	all, err := db.AllHistory()
	if err != nil || len(all) != 1 || all[0].ID != "hist-legacy-1" {
		t.Fatalf("migrated history incorrect: %+v", all)
	}

	// Verify legacy files renamed to .bak
	if _, err := os.Stat(filepath.Join(dir, "config.yaml.bak")); err != nil {
		t.Fatalf("expected config.yaml.bak to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.json.bak")); err != nil {
		t.Fatalf("expected secrets.json.bak to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "history.jsonl.bak")); err != nil {
		t.Fatalf("expected history.jsonl.bak to exist: %v", err)
	}
}

func TestEnvFileResolution(t *testing.T) {
	dir := t.TempDir()
	key := "env-secret-master-key-xyz"

	if err := SaveMasterKeyToEnv(dir, key); err != nil {
		t.Fatalf("SaveMasterKeyToEnv failed: %v", err)
	}

	// Verify permissions on env file
	info, err := os.Stat(filepath.Join(dir, "env"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 on env file, got %o", perm)
	}

	// Clear process env var to ensure file lookup works
	t.Setenv(MasterKeyEnvVar, "")
	resolved := ResolveMasterKey(dir)
	if resolved != key {
		t.Fatalf("expected %q, got %q", key, resolved)
	}

	// Process env var should override env file
	t.Setenv(MasterKeyEnvVar, "override-key")
	if got := ResolveMasterKey(dir); got != "override-key" {
		t.Fatalf("expected override-key, got %q", got)
	}
}
