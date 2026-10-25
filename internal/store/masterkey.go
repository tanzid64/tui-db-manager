package store

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tanzid64/tui-db-manager/internal/vault"
)

// DefaultMasterKey is the fixed key older dbk versions used when none was
// given. It is public, so it protects nothing; installs still using it are
// moved to a random key by UpgradeDefaultKey.
const DefaultMasterKey = "dbk-default"

// GenerateMasterKey returns a new random master key (256 bits, base64url).
func GenerateMasterKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("store: cannot read random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// MasterKeyFromFile returns the key saved in configDir/env (or .env), ignoring
// the DBK_MASTER_KEY environment variable.
func MasterKeyFromFile(configDir string) string {
	for _, name := range []string{"env", ".env"} {
		if data, err := os.ReadFile(filepath.Join(configDir, name)); err == nil {
			if key := extractKeyFromEnv(data); key != "" {
				return key
			}
		}
	}
	return ""
}

// KeyForSchedule returns the master key a schedule wrapper must export, or ""
// when the scheduled run can read it from the env file itself. This keeps the
// key out of wrapper scripts whenever possible.
func KeyForSchedule(configDir, key string) string {
	if key == "" || key == MasterKeyFromFile(configDir) {
		return ""
	}
	return key
}

// UsesDefaultKey reports whether the database is unlocked with the public
// default key.
func (d *DB) UsesDefaultKey() bool {
	return d != nil && d.IsUnlocked() && d.Passphrase() == DefaultMasterKey
}

// Rekey re-encrypts every secret under newPass, with a fresh salt, in one
// transaction. On error nothing changes.
func (d *DB) Rekey(newPass string) error {
	if newPass == "" {
		return errors.New("new master key cannot be empty")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.key) == 0 {
		return ErrLocked
	}

	kdf := vault.DefaultKDFParams()
	newKey := vault.DeriveKey(newPass, kdf)

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Make sure no other process rekeyed the database since we unlocked it.
	var verifyBlob []byte
	if err := tx.QueryRow("SELECT value FROM meta WHERE key = 'verify_token'").Scan(&verifyBlob); err != nil {
		return fmt.Errorf("read verify token: %w", err)
	}
	if pt, err := decryptValue(d.key, verifyBlob); err != nil || string(pt) != verifySentinel {
		return ErrBadPassphrase
	}

	rows, err := tx.Query("SELECT key, value FROM secrets")
	if err != nil {
		return err
	}
	type kv struct {
		k string
		v []byte
	}
	var all []kv
	for rows.Next() {
		var r kv
		if err := rows.Scan(&r.k, &r.v); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range all {
		pt, err := decryptValue(d.key, r.v)
		if err != nil {
			return fmt.Errorf("decrypt secret %q: %w", r.k, err)
		}
		blob, err := encryptValue(newKey, pt)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE secrets SET value = ?, updated_at = datetime('now') WHERE key = ?", blob, r.k); err != nil {
			return err
		}
	}

	newVerify, err := encryptValue(newKey, []byte(verifySentinel))
	if err != nil {
		return err
	}
	meta := map[string][]byte{
		"kdf_salt":     kdf.Salt,
		"kdf_time":     []byte(strconv.FormatUint(uint64(kdf.Time), 10)),
		"kdf_memory":   []byte(strconv.FormatUint(uint64(kdf.Memory), 10)),
		"kdf_threads":  []byte(strconv.FormatUint(uint64(kdf.Threads), 10)),
		"verify_token": newVerify,
	}
	for k, v := range meta {
		if _, err := tx.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", k, v); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	d.key = newKey
	d.passphrase = newPass
	d.kdf = kdf
	return nil
}

// UpgradeDefaultKey moves an install that still uses the public default key
// (saved in configDir/env) to a new random key: it re-encrypts the secrets,
// saves the new key to the env file and drops the old key from schedule
// wrappers so scheduled runs read the env file instead. It returns true if
// the key was changed. A key the user set themselves is never touched.
func UpgradeDefaultKey(configDir string, d *DB) (bool, error) {
	if !d.UsesDefaultKey() || MasterKeyFromFile(configDir) != DefaultMasterKey {
		return false, nil
	}
	if v := strings.TrimSpace(os.Getenv(MasterKeyEnvVar)); v != "" && v != DefaultMasterKey {
		return false, nil
	}

	newKey := GenerateMasterKey()
	envPath := filepath.Join(configDir, "env")
	tmp := envPath + ".new"
	content := fmt.Sprintf("# dbk environment configuration\nexport %s=%q\n", MasterKeyEnvVar, newKey)
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return false, fmt.Errorf("write new key: %w", err)
	}
	if err := d.Rekey(newKey); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, envPath); err != nil {
		// The database already uses the new key; keep it reachable.
		return true, fmt.Errorf("save new key (it is in %s): %w", tmp, err)
	}
	_ = os.Remove(filepath.Join(configDir, ".env"))
	if os.Getenv(MasterKeyEnvVar) != "" {
		_ = os.Setenv(MasterKeyEnvVar, newKey)
	}
	stripWrapperKeys(filepath.Join(configDir, "schedules"))
	return true, nil
}

// stripWrapperKeys removes baked-in DBK_MASTER_KEY lines from schedule
// wrappers written by older versions. The runs then read the env file.
func stripWrapperKeys(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "dbk-*.sh"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		out := lines[:0]
		changed := false
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "export "+MasterKeyEnvVar+"=") {
				changed = true
				continue
			}
			out = append(out, l)
		}
		if changed {
			_ = os.WriteFile(f, []byte(strings.Join(out, "\n")), 0o700)
		}
	}
}
