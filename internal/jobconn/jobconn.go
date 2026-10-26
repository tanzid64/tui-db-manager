// Package jobconn turns a configured job into a ready-to-use db.Conn,
// resolving the SSH tunnel's secrets from the vault or env:/file: refs.
package jobconn

import (
	"fmt"
	"os"
	"strings"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/secretref"
)

// Vault keys for a job's SSH secrets.
func SSHKeyVaultKey(job string) string      { return "job/" + job + "/ssh_key" }
func SSHPasswordVaultKey(job string) string { return "job/" + job + "/ssh_password" }

// Lookup reads a secret from the vault; nil means no vault.
type Lookup func(key string) (string, bool)

// Build returns the db.Conn for job using the already resolved DB password.
func Build(job config.Job, password string, lookup Lookup) (db.Conn, error) {
	c := db.Conn{
		Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
		Password: password, Database: job.Conn.Database, Path: job.Conn.Path, TLS: job.Conn.TLS,
	}
	t, err := ResolveSSH(job, lookup)
	if err != nil {
		return db.Conn{}, err
	}
	c.SSH = t
	return c, nil
}

// ResolveSSH resolves the job's SSH settings, or returns nil if it has none.
func ResolveSSH(job config.Job, lookup Lookup) (*db.SSHTunnel, error) {
	s := job.Conn.SSH
	if s == nil || s.Host == "" {
		return nil, nil
	}
	t := &db.SSHTunnel{Host: s.Host, Port: s.Port, User: s.User, HostKey: s.HostKey}

	switch {
	case s.KeyPath != "":
		b, err := os.ReadFile(expandHome(s.KeyPath))
		if err != nil {
			return nil, fmt.Errorf("ssh key for job %q: %w", job.Name, err)
		}
		t.PrivateKey = b
	case lookup != nil:
		if k, ok := lookup(SSHKeyVaultKey(job.Name)); ok && k != "" {
			t.PrivateKey = []byte(k)
		}
	}

	if s.PasswordRef != "" {
		pw, err := secretref.Resolve(s.PasswordRef, lookup)
		if err != nil {
			return nil, fmt.Errorf("ssh password for job %q: %w", job.Name, err)
		}
		t.Password = pw
	} else if lookup != nil {
		if pw, ok := lookup(SSHPasswordVaultKey(job.Name)); ok {
			t.Password = pw
		}
	}
	return t, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}
