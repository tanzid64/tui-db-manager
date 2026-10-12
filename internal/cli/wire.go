package cli

import (
	"fmt"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/secretref"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

func buildBackend(sc config.StorageConfig) (storage.Backend, error) {
	switch sc.Type {
	case "local":
		return storage.NewLocal(sc.Path), nil
	default:
		return nil, fmt.Errorf("storage type %q not supported yet", sc.Type)
	}
}

func engineResolver() func(string) (db.Engine, bool) {
	return func(name string) (db.Engine, bool) { return db.Get(name) }
}

// resolvePassword returns a closure that resolves a job's DB password from a
// PasswordRef (env:/file:) or the vault key "job/<name>/password".
func resolvePassword(v *vault.Store) func(config.Job) (string, error) {
	return func(j config.Job) (string, error) {
		ref := j.Conn.PasswordRef
		var lookup func(string) (string, bool)
		if v != nil {
			lookup = v.Get
		}
		if ref == "" {
			// No explicit ref: look up the conventional vault key; empty is allowed
			// (e.g. sqlite or trust auth).
			if v == nil {
				return "", nil
			}
			if val, ok := v.Get("job/" + j.Name + "/password"); ok {
				return val, nil
			}
			return "", nil
		}
		return secretref.Resolve(ref, lookup)
	}
}

// maybeOpenVault opens the vault if it exists, else returns nil (jobs may have
// no secrets, e.g. sqlite). Passphrase comes from env/file/prompt.
func maybeOpenVault(dir, passFile string, interactive bool) (*vault.Store, error) {
	if !vault.Exists(dir) {
		return nil, nil
	}
	pass, err := vault.ReadPassphrase(dir, passFile, interactive)
	if err != nil {
		return nil, err
	}
	return vault.Open(dir, pass)
}

func errUnknownJob(name string) error     { return fmt.Errorf("job %q not found", name) }
func errUnknownStorage(name string) error { return fmt.Errorf("storage %q not found", name) }
