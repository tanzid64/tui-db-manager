package store

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tanzid64/tui-db-manager/internal/vault"
)

var ErrLocked = errors.New("vault is locked: master key is required")

// GetSecret returns the plaintext secret value for key.
func (d *DB) GetSecret(key string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.key) == 0 {
		return "", false
	}

	var blob []byte
	err := d.db.QueryRow("SELECT value FROM secrets WHERE key = ?", key).Scan(&blob)
	if err != nil {
		return "", false
	}

	pt, err := decryptValue(d.key, blob)
	if err != nil {
		return "", false
	}

	return string(pt), true
}

// Get is an alias for GetSecret satisfying SecretStore interfaces.
func (d *DB) Get(key string) (string, bool) {
	return d.GetSecret(key)
}

// Set is an alias for SetSecret.
func (d *DB) Set(key, val string) {
	_ = d.SetSecret(key, val)
}

// Delete is an alias for DeleteSecret.
func (d *DB) Delete(key string) {
	_ = d.DeleteSecret(key)
}

// Keys returns all secret keys as a string slice.
func (d *DB) Keys() []string {
	keys, _ := d.ListSecretKeys()
	return keys
}

// All returns a copy of all decrypted secrets.
func (d *DB) All() map[string]string {
	all, _ := d.AllSecrets()
	return all
}

// Export re-encrypts all secrets with transferPassphrase (matching vault.Store).
func (d *DB) Export(transferPassphrase string) ([]byte, error) {
	return d.ExportSecrets(transferPassphrase)
}

// Merge merges secrets into the store (matching vault.Store).
func (d *DB) Merge(secrets map[string]string, overwrite bool) (added, updated, skipped int) {
	a, u, s, _ := d.MergeSecrets(secrets, overwrite)
	return a, u, s
}

// Save is a no-op for SQLite since writes are committed immediately.
func (d *DB) Save() error {
	return nil
}

// SetSecret encrypts and stores a secret value.
func (d *DB) SetSecret(key, val string) error {
	d.mu.RLock()
	if len(d.key) == 0 {
		d.mu.RUnlock()
		return ErrLocked
	}
	keyBytes := d.key
	d.mu.RUnlock()

	blob, err := encryptValue(keyBytes, []byte(val))
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}

	query := `INSERT INTO secrets (key, value, created_at, updated_at)
VALUES (?, ?, datetime('now'), datetime('now'))
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`

	_, err = d.db.Exec(query, key, blob)
	if err != nil {
		return fmt.Errorf("save secret: %w", err)
	}
	return nil
}

// DeleteSecret removes a secret by key.
func (d *DB) DeleteSecret(key string) error {
	_, err := d.db.Exec("DELETE FROM secrets WHERE key = ?", key)
	return err
}

// ListSecretKeys returns a sorted slice of all stored secret keys.
func (d *DB) ListSecretKeys() ([]string, error) {
	rows, err := d.db.Query("SELECT key FROM secrets ORDER BY key ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// AllSecrets returns a decrypted copy of all secrets stored in the database.
func (d *DB) AllSecrets() (map[string]string, error) {
	d.mu.RLock()
	if len(d.key) == 0 {
		d.mu.RUnlock()
		return nil, ErrLocked
	}
	keyBytes := d.key
	d.mu.RUnlock()

	rows, err := d.db.Query("SELECT key, value FROM secrets ORDER BY key ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]string)
	for rows.Next() {
		var k string
		var blob []byte
		if err := rows.Scan(&k, &blob); err != nil {
			return nil, err
		}
		pt, err := decryptValue(keyBytes, blob)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret %q: %w", k, err)
		}
		res[k] = string(pt)
	}
	return res, nil
}

// MergeSecrets merges incoming secrets into the database.
func (d *DB) MergeSecrets(incoming map[string]string, overwrite bool) (added, updated, skipped int, err error) {
	if len(incoming) == 0 {
		return 0, 0, 0, nil
	}

	keys := make([]string, 0, len(incoming))
	for k := range incoming {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := incoming[k]
		_, exists := d.GetSecret(k)
		if exists {
			if overwrite {
				if err := d.SetSecret(k, v); err != nil {
					return added, updated, skipped, err
				}
				updated++
			} else {
				skipped++
			}
		} else {
			if err := d.SetSecret(k, v); err != nil {
				return added, updated, skipped, err
			}
			added++
		}
	}
	return added, updated, skipped, nil
}

// ExportSecrets re-encrypts all stored secrets using a separate transfer passphrase.
// Uses vault.Export logic (XChaCha20-Poly1305 + Argon2id).
func (d *DB) ExportSecrets(transferPassphrase string) ([]byte, error) {
	all, err := d.AllSecrets()
	if err != nil {
		return nil, err
	}

	tempStore, err := vault.Create(d.dir)
	if err != nil {
		return nil, err
	}
	for k, v := range all {
		tempStore.Set(k, v)
	}
	return tempStore.Export(transferPassphrase)
}

// ImportDecryptSecrets decrypts an exported secrets blob with transferPassphrase.
func (d *DB) ImportDecryptSecrets(transferPassphrase string, blob []byte) (map[string]string, error) {
	return vault.ImportDecrypt(transferPassphrase, blob)
}
