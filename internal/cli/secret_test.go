package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecretSetLsRm(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DBK_PASSPHRASE", "pw")

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
