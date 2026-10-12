package secretref

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnv(t *testing.T) {
	t.Setenv("MYSECRET", "fromenv")
	got, err := Resolve("env:MYSECRET", nil)
	if err != nil || got != "fromenv" {
		t.Fatalf("env: %q %v", got, err)
	}
}

func TestResolveFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s")
	os.WriteFile(p, []byte("fromfile\n"), 0o600)
	got, err := Resolve("file:"+p, nil)
	if err != nil || got != "fromfile" {
		t.Fatalf("file: %q %v", got, err)
	}
}

func TestResolveVaultKey(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == "job/main/password" {
			return "fromvault", true
		}
		return "", false
	}
	got, err := Resolve("job/main/password", lookup)
	if err != nil || got != "fromvault" {
		t.Fatalf("vault: %q %v", got, err)
	}
	if _, err := Resolve("missing/key", lookup); err == nil {
		t.Fatal("expected error for missing vault key")
	}
}
