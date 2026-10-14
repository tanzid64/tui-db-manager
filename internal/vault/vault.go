package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type Store struct {
	path    string
	secrets map[string]string
}

func Path(dir string) string { return filepath.Join(dir, "secrets.json") }

func Exists(dir string) bool {
	if _, err := os.Stat(Path(dir)); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.enc")); err == nil {
		return os.Getenv("DBK_PASSPHRASE") != ""
	}
	return false
}

func Create(dir string) (*Store, error) {
	return &Store{
		path:    Path(dir),
		secrets: map[string]string{},
	}, nil
}

func Open(dir string) (*Store, error) {
	p := Path(dir)
	blob, err := os.ReadFile(p)
	if err == nil {
		var secrets map[string]string
		if err := json.Unmarshal(blob, &secrets); err != nil {
			return nil, fmt.Errorf("corrupt secrets file %s: %w", p, err)
		}
		if secrets == nil {
			secrets = map[string]string{}
		}
		return &Store{path: p, secrets: secrets}, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	// Legacy migration: if secrets.enc exists and DBK_PASSPHRASE is provided, migrate it.
	legacyPath := filepath.Join(dir, "secrets.enc")
	if encBlob, encErr := os.ReadFile(legacyPath); encErr == nil {
		if pass := os.Getenv("DBK_PASSPHRASE"); pass != "" {
			if s, migErr := openLegacy(p, encBlob, pass); migErr == nil {
				_ = s.Save()
				return s, nil
			} else {
				return nil, fmt.Errorf("migrate legacy encrypted vault: %w", migErr)
			}
		}
	}

	return nil, err
}

func openLegacy(targetPath string, blob []byte, passphrase string) (*Store, error) {
	p, err := peekParams(blob)
	if err != nil {
		return nil, err
	}
	key := DeriveKey(passphrase, p)
	pt, _, err := open(key, blob)
	if err != nil {
		return nil, err
	}
	var secrets map[string]string
	if err := json.Unmarshal(pt, &secrets); err != nil {
		return nil, err
	}
	if secrets == nil {
		secrets = map[string]string{}
	}
	return &Store{path: targetPath, secrets: secrets}, nil
}

// Keys returns the secret keys (not values), sorted.
func (s *Store) Keys() []string {
	if s == nil || s.secrets == nil {
		return nil
	}
	out := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Store) Get(key string) (string, bool) {
	if s == nil || s.secrets == nil {
		return "", false
	}
	v, ok := s.secrets[key]
	return v, ok
}

func (s *Store) Set(key, val string) {
	if s.secrets == nil {
		s.secrets = map[string]string{}
	}
	s.secrets[key] = val
}

func (s *Store) Delete(key string) {
	if s.secrets != nil {
		delete(s.secrets, key)
	}
}

// All returns a copy of all secrets stored in the vault.
func (s *Store) All() map[string]string {
	if s == nil || s.secrets == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(s.secrets))
	for k, v := range s.secrets {
		out[k] = v
	}
	return out
}

// Merge merges secrets into the store. If overwrite is true, existing keys are overwritten.
func (s *Store) Merge(secrets map[string]string, overwrite bool) (added, updated, skipped int) {
	if s.secrets == nil {
		s.secrets = map[string]string{}
	}
	for k, v := range secrets {
		if _, exists := s.secrets[k]; exists {
			if overwrite {
				s.secrets[k] = v
				updated++
			} else {
				skipped++
			}
		} else {
			s.secrets[k] = v
			added++
		}
	}
	return added, updated, skipped
}

// Export re-encrypts all secrets in the store using a transfer passphrase and default KDF params.
func (s *Store) Export(transferPassphrase string) ([]byte, error) {
	p := DefaultKDFParams()
	key := DeriveKey(transferPassphrase, p)
	pt, err := json.Marshal(s.secrets)
	if err != nil {
		return nil, err
	}
	return seal(key, pt, p)
}

// ImportDecrypt decrypts an exported secret blob using the transfer passphrase.
func ImportDecrypt(transferPassphrase string, blob []byte) (map[string]string, error) {
	p, err := peekParams(blob)
	if err != nil {
		return nil, err
	}
	key := DeriveKey(transferPassphrase, p)
	pt, _, err := open(key, blob)
	if err != nil {
		return nil, err
	}
	var secrets map[string]string
	if err := json.Unmarshal(pt, &secrets); err != nil {
		return nil, err
	}
	return secrets, nil
}

func (s *Store) Save() error {
	pt, err := json.MarshalIndent(s.secrets, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, pt, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
