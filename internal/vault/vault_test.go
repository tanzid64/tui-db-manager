package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateSetSaveOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	s.Set("job/main/password", "s3cr3t")
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify secrets.json permissions are 0600
	info, err := os.Stat(filepath.Join(dir, "secrets.json"))
	if err != nil {
		t.Fatalf("stat secrets.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected permissions 0600, got %o", perm)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, ok := reopened.Get("job/main/password")
	if !ok || got != "s3cr3t" {
		t.Fatalf("secret round-trip failed: %q ok=%v", got, ok)
	}
}

func TestExportImportDecrypt(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("job/pg/password", "secret123")
	s.Set("storage/s3/secret_key", "awskey456")

	blob, err := s.Export("transfer-pass-xyz")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Decrypt with wrong transfer passphrase
	_, err = ImportDecrypt("wrong-pass", blob)
	if err == nil {
		t.Fatal("expected error with wrong transfer passphrase")
	}

	// Decrypt with correct transfer passphrase
	decrypted, err := ImportDecrypt("transfer-pass-xyz", blob)
	if err != nil {
		t.Fatalf("import decrypt: %v", err)
	}
	if decrypted["job/pg/password"] != "secret123" || decrypted["storage/s3/secret_key"] != "awskey456" {
		t.Fatalf("decrypted secrets mismatch: %+v", decrypted)
	}
}

func TestStoreMerge(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("k1", "original1")
	s.Set("k2", "original2")

	incoming := map[string]string{
		"k2": "new2",
		"k3": "new3",
	}

	// Without overwrite
	added, updated, skipped := s.Merge(incoming, false)
	if added != 1 || updated != 0 || skipped != 1 {
		t.Fatalf("unexpected merge without overwrite: added=%d updated=%d skipped=%d", added, updated, skipped)
	}
	val, _ := s.Get("k2")
	if val != "original2" {
		t.Fatalf("expected k2 to remain original2, got %s", val)
	}

	// With overwrite
	added, updated, skipped = s.Merge(incoming, true)
	if added != 0 || updated != 2 || skipped != 0 {
		t.Fatalf("unexpected merge with overwrite: added=%d updated=%d skipped=%d", added, updated, skipped)
	}
	val, _ = s.Get("k2")
	if val != "new2" {
		t.Fatalf("expected k2 to be new2, got %s", val)
	}
}

func TestLegacyEncMigration(t *testing.T) {
	dir := t.TempDir()
	// Create legacy encrypted vault
	s, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("storage/r2/secret_key", "supersecret123")
	encBlob, err := s.Export("my-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "secrets.enc")
	if err := os.WriteFile(legacyPath, encBlob, 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. Without DBK_PASSPHRASE: Exists should be false, Open should return ErrNotExist
	t.Setenv("DBK_PASSPHRASE", "")
	if Exists(dir) {
		t.Fatal("expected Exists to be false when secrets.enc exists without DBK_PASSPHRASE")
	}
	if _, err := Open(dir); !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}

	// 2. With wrong DBK_PASSPHRASE: Exists is true, Open should fail migration
	t.Setenv("DBK_PASSPHRASE", "wrong-passphrase")
	if !Exists(dir) {
		t.Fatal("expected Exists to be true when DBK_PASSPHRASE is provided")
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("expected Open to fail with wrong DBK_PASSPHRASE")
	}

	// 3. With correct DBK_PASSPHRASE: Exists is true, Open migrates secrets.enc to secrets.json
	t.Setenv("DBK_PASSPHRASE", "my-passphrase")
	if !Exists(dir) {
		t.Fatal("expected Exists to be true with correct DBK_PASSPHRASE")
	}
	migrated, err := Open(dir)
	if err != nil {
		t.Fatalf("expected Open to succeed, got %v", err)
	}
	if val, ok := migrated.Get("storage/r2/secret_key"); !ok || val != "supersecret123" {
		t.Fatalf("unexpected migrated secret: %q (ok=%v)", val, ok)
	}

	// Verify secrets.json was created on disk
	info, err := os.Stat(filepath.Join(dir, "secrets.json"))
	if err != nil {
		t.Fatalf("stat secrets.json after migration: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600 permissions on secrets.json, got %o", perm)
	}

	// 4. Subsequent Open without DBK_PASSPHRASE should now succeed directly via secrets.json
	t.Setenv("DBK_PASSPHRASE", "")
	if !Exists(dir) {
		t.Fatal("expected Exists to be true once secrets.json exists")
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopening migrated secrets.json failed: %v", err)
	}
	if val, ok := reopened.Get("storage/r2/secret_key"); !ok || val != "supersecret123" {
		t.Fatalf("unexpected secret after reopening: %q (ok=%v)", val, ok)
	}
}

