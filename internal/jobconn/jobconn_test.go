package jobconn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
)

func vault(m map[string]string) Lookup {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestNoSSH(t *testing.T) {
	c, err := Build(config.Job{Name: "j", Conn: config.Conn{Host: "db", Port: 5432}}, "pw", nil)
	if err != nil || c.SSH != nil || c.Host != "db" || c.Password != "pw" {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestSSHFromVault(t *testing.T) {
	job := config.Job{Name: "prod", Conn: config.Conn{SSH: &config.SSH{
		Host: "bastion", User: "deploy", HostKey: "SHA256:abc",
	}}}
	tun, err := ResolveSSH(job, vault(map[string]string{
		SSHKeyVaultKey("prod"):      "KEY",
		SSHPasswordVaultKey("prod"): "pass",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if tun.Host != "bastion" || tun.User != "deploy" || string(tun.PrivateKey) != "KEY" ||
		tun.Password != "pass" || tun.HostKey != "SHA256:abc" {
		t.Fatalf("unexpected tunnel %+v", tun)
	}
}

func TestSSHKeyFileAndPasswordRef(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "id")
	os.WriteFile(keyFile, []byte("FILEKEY"), 0o600)
	t.Setenv("SSH_PW", "envpass")
	job := config.Job{Name: "j", Conn: config.Conn{SSH: &config.SSH{
		Host: "h", User: "u", KeyPath: keyFile, PasswordRef: "env:SSH_PW",
	}}}
	// The vault key must not win over an explicit key file.
	tun, err := ResolveSSH(job, vault(map[string]string{SSHKeyVaultKey("j"): "VAULTKEY"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(tun.PrivateKey) != "FILEKEY" || tun.Password != "envpass" {
		t.Fatalf("unexpected tunnel %+v", tun)
	}
}

func TestSSHMissingKeyFile(t *testing.T) {
	job := config.Job{Name: "j", Conn: config.Conn{SSH: &config.SSH{Host: "h", User: "u", KeyPath: "/nope/id"}}}
	if _, err := ResolveSSH(job, nil); err == nil {
		t.Fatal("expected error for missing key file")
	}
}
