package bundle

import (
	"path/filepath"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

func TestExportLoadImportWithoutSecrets(t *testing.T) {
	cfg := &config.Config{
		Jobs: []config.Job{{
			Name: "prod", Engine: "postgres", Storage: "local",
		}},
		Storages: []config.StorageConfig{{
			Name: "local", Type: "local", Path: "/data",
		}},
	}

	b, err := Export(cfg, nil, "")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(b.EncryptedSecrets) != 0 {
		t.Fatal("expected no encrypted secrets in structure-only export")
	}

	bundlePath := filepath.Join(t.TempDir(), "backup-bundle.yaml")
	if err := b.Save(bundlePath); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(bundlePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Config.Jobs) != 1 || loaded.Config.Jobs[0].Name != "prod" {
		t.Fatalf("loaded config mismatch: %+v", loaded.Config)
	}

	// Import into target config dir
	targetDir := t.TempDir()
	res, err := loaded.Import(targetDir, nil, "", false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.ConfigResult.JobsAdded != 1 || res.ConfigResult.StoragesAdded != 1 {
		t.Fatalf("unexpected import result: %+v", res)
	}

	// Verify imported config
	importedCfg, err := config.Load(targetDir)
	if err != nil {
		t.Fatalf("load target config: %v", err)
	}
	if len(importedCfg.Jobs) != 1 || importedCfg.Jobs[0].Name != "prod" {
		t.Fatalf("imported job mismatch: %+v", importedCfg)
	}
}

func TestExportLoadImportWithSecrets(t *testing.T) {
	srcVaultDir := t.TempDir()
	srcVault, err := vault.Create(srcVaultDir)
	if err != nil {
		t.Fatal(err)
	}
	srcVault.Set("job/prod/password", "super-secret-123")
	srcVault.Set("storage/s3/secret_key", "aws-secret-456")

	cfg := &config.Config{
		Jobs: []config.Job{{Name: "prod", Engine: "mysql"}},
	}

	transferPassphrase := "transfer-key-999"
	b, err := Export(cfg, srcVault, transferPassphrase)
	if err != nil {
		t.Fatalf("export with secrets: %v", err)
	}
	if len(b.EncryptedSecrets) == 0 {
		t.Fatal("expected encrypted secrets to be populated")
	}

	bundlePath := filepath.Join(t.TempDir(), "bundle.json")
	if err := b.Save(bundlePath); err != nil {
		t.Fatalf("save json: %v", err)
	}

	loaded, err := Load(bundlePath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	targetDir := t.TempDir()
	destVault, err := vault.Create(targetDir)
	if err != nil {
		t.Fatal(err)
	}

	// Import with wrong transfer passphrase should fail
	_, err = loaded.Import(targetDir, destVault, "wrong-transfer-pw", false)
	if err == nil {
		t.Fatal("expected error importing with wrong transfer passphrase")
	}

	// Import with correct transfer passphrase
	res, err := loaded.Import(targetDir, destVault, transferPassphrase, false)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.SecretsAdded != 2 || res.SecretsSkipped != 0 {
		t.Fatalf("unexpected secrets import count: %+v", res)
	}

	// Reopen dest vault and verify secrets
	reopened, err := vault.Open(targetDir)
	if err != nil {
		t.Fatalf("open destination vault: %v", err)
	}
	pw, ok := reopened.Get("job/prod/password")
	if !ok || pw != "super-secret-123" {
		t.Fatalf("imported password mismatch: %q ok=%v", pw, ok)
	}
	s3, ok := reopened.Get("storage/s3/secret_key")
	if !ok || s3 != "aws-secret-456" {
		t.Fatalf("imported s3 secret mismatch: %q ok=%v", s3, ok)
	}
}
