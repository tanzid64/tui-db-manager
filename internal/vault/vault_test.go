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
