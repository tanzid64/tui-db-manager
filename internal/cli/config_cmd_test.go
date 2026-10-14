package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

func TestConfigExportImportEndToEnd(t *testing.T) {
	srcDir := t.TempDir()
	t.Setenv("DBK_PASSPHRASE", "master-secret-pw")
	t.Setenv("DBK_TRANSFER_PASSPHRASE", "transfer-pw-123")

	// 1. Initialize src vault and config
	v, err := vault.Create(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	v.Set("job/prod/password", "db-pass-999")
	v.Set("storage/s3/secret_key", "s3-key-888")
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Jobs: []config.Job{{
			Name: "prod", Engine: "postgres", Storage: "cloud",
		}},
		Storages: []config.StorageConfig{{
			Name: "cloud", Type: "s3", Bucket: "my-backups",
		}},
		Notifiers: []config.NotifierConfig{{
			Name: "alerts", Type: "webhook", URL: "https://example.com/alerts",
		}},
	}
	if err := config.Save(srcDir, cfg); err != nil {
		t.Fatal(err)
	}

	bundlePath := filepath.Join(t.TempDir(), "export.yaml")

	// 2. Export with --include-secrets
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", srcDir, "config", "export", "--include-secrets", bundlePath})
	if err := root.Execute(); err != nil {
		t.Fatalf("export execute: %v (out: %s)", err, out.String())
	}
	if !strings.Contains(out.String(), "configuration exported") {
		t.Fatalf("unexpected export output: %s", out.String())
	}

	// 3. Import into completely fresh target directory
	destDir := t.TempDir()
	root = NewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", destDir, "--json", "config", "import", bundlePath})
	if err := root.Execute(); err != nil {
		t.Fatalf("import execute: %v (out: %s)", err, out.String())
	}

	var importSummary map[string]int
	if err := json.Unmarshal(out.Bytes(), &importSummary); err != nil {
		t.Fatalf("parse import json: %v (raw: %s)", err, out.String())
	}
	if importSummary["jobs_added"] != 1 || importSummary["storages_added"] != 1 || importSummary["secrets_added"] != 2 {
		t.Fatalf("unexpected import summary: %+v", importSummary)
	}

	// 4. Verify target directory has config and functional decrypted vault
	destCfg, err := config.Load(destDir)
	if err != nil {
		t.Fatalf("load dest config: %v", err)
	}
	if len(destCfg.Jobs) != 1 || destCfg.Jobs[0].Name != "prod" {
		t.Fatalf("dest config mismatch: %+v", destCfg)
	}

	destVault, err := vault.Open(destDir)
	if err != nil {
		t.Fatalf("open dest vault: %v", err)
	}
	val, ok := destVault.Get("job/prod/password")
	if !ok || val != "db-pass-999" {
		t.Fatalf("dest vault password mismatch: %q ok=%v", val, ok)
	}
	val, ok = destVault.Get("storage/s3/secret_key")
	if !ok || val != "s3-key-888" {
		t.Fatalf("dest vault s3 secret mismatch: %q ok=%v", val, ok)
	}
}

func TestConfigExportStructureOnly(t *testing.T) {
	srcDir := t.TempDir()
	cfg := &config.Config{
		Jobs: []config.Job{{Name: "staging", Engine: "mysql"}},
	}
	if err := config.Save(srcDir, cfg); err != nil {
		t.Fatal(err)
	}

	bundlePath := filepath.Join(t.TempDir(), "structure.json")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", srcDir, "config", "export", bundlePath})
	if err := root.Execute(); err != nil {
		t.Fatalf("export execute: %v", err)
	}
	if !strings.Contains(out.String(), "structure only, secrets excluded") {
		t.Fatalf("unexpected output: %s", out.String())
	}

	destDir := t.TempDir()
	root = NewRootCmd()
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", destDir, "config", "import", bundlePath})
	if err := root.Execute(); err != nil {
		t.Fatalf("import execute: %v", err)
	}

	destCfg, err := config.Load(destDir)
	if err != nil {
		t.Fatalf("load dest config: %v", err)
	}
	if len(destCfg.Jobs) != 1 || destCfg.Jobs[0].Name != "staging" {
		t.Fatalf("dest config mismatch: %+v", destCfg)
	}
}
