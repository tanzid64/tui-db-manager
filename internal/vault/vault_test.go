package vault

import (
	"errors"
	"testing"
)

func TestCreateSetSaveOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, "pw")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	s.Set("job/main/password", "s3cr3t")
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	reopened, err := Open(dir, "pw")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, ok := reopened.Get("job/main/password")
	if !ok || got != "s3cr3t" {
		t.Fatalf("secret round-trip failed: %q ok=%v", got, ok)
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	s, _ := Create(dir, "right")
	_ = s.Save()
	if _, err := Open(dir, "wrong"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("expected ErrBadPassphrase, got %v", err)
	}
}

func TestReadPassphraseFromEnv(t *testing.T) {
	t.Setenv("DBK_PASSPHRASE", "fromenv")
	got, err := ReadPassphrase(t.TempDir(), "", false)
	if err != nil || got != "fromenv" {
		t.Fatalf("env passphrase: got %q err %v", got, err)
	}
}

func TestExportImportDecrypt(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, "master-pw")
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
	s, err := Create(dir, "pw")
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
