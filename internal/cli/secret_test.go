package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretSetLsRm(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) (string, error) {
		root := NewRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"--config", dir}, args...))
		err := root.Execute()
		return out.String(), err
	}

	if _, err := run("secret", "set", "storage/r2/secret_key", "--value", "SUPERSECRET"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := run("secret", "ls")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(out, "storage/r2/secret_key") {
		t.Fatalf("ls should list the key, got %q", out)
	}
	if strings.Contains(out, "SUPERSECRET") {
		t.Fatalf("ls must never print values, got %q", out)
	}
	if _, err := run("secret", "rm", "storage/r2/secret_key"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	out, _ = run("secret", "ls")
	if strings.Contains(out, "storage/r2/secret_key") {
		t.Fatalf("key should be gone after rm, got %q", out)
	}
}

func TestSecretCmdWithLegacyEnc(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secrets.enc"), []byte("dummy-encrypted-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, error) {
		root := NewRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"--config", dir}, args...))
		err := root.Execute()
		return out.String(), err
	}

	if _, err := run("secret", "set", "storage/s3/key", "--value", "val123"); err != nil {
		t.Fatalf("secret set failed with legacy secrets.enc present: %v", err)
	}

	out, err := run("secret", "ls")
	if err != nil {
		t.Fatalf("secret ls failed: %v", err)
	}
	if !strings.Contains(out, "storage/s3/key") {
		t.Fatalf("expected storage/s3/key in secret ls, got %q", out)
	}
}
