package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type Store struct {
	path    string
	key     []byte
	params  KDFParams
	secrets map[string]string
}

func Path(dir string) string { return filepath.Join(dir, "secrets.enc") }

func Exists(dir string) bool {
	_, err := os.Stat(Path(dir))
	return err == nil
}

func Create(dir, passphrase string) (*Store, error) {
	p := DefaultKDFParams()
	return &Store{
		path:    Path(dir),
		key:     DeriveKey(passphrase, p),
		params:  p,
		secrets: map[string]string{},
	}, nil
}

func Open(dir, passphrase string) (*Store, error) {
	blob, err := os.ReadFile(Path(dir))
	if err != nil {
		return nil, err
	}
	// The key needs the salt from the header, so read the header params first.
	// They are not trusted until open() verifies the AEAD tag.
	p, err := peekParams(blob)
	if err != nil {
		return nil, err
	}
	key := DeriveKey(passphrase, p)
	pt, gotP, err := open(key, blob)
	if err != nil {
		return nil, err
	}
	var secrets map[string]string
	if err := json.Unmarshal(pt, &secrets); err != nil {
		return nil, err
	}
	return &Store{path: Path(dir), key: key, params: gotP, secrets: secrets}, nil
}

// Keys returns the secret keys (not values), sorted.
func (s *Store) Keys() []string {
	out := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Store) Get(key string) (string, bool) { v, ok := s.secrets[key]; return v, ok }
func (s *Store) Set(key, val string)           { s.secrets[key] = val }
func (s *Store) Delete(key string)             { delete(s.secrets, key) }

// All returns a copy of all secrets stored in the vault.
func (s *Store) All() map[string]string {
	out := make(map[string]string, len(s.secrets))
	for k, v := range s.secrets {
		out[k] = v
	}
	return out
}

// Merge merges secrets into the store. If overwrite is true, existing keys are overwritten.
func (s *Store) Merge(secrets map[string]string, overwrite bool) (added, updated, skipped int) {
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
	pt, err := json.Marshal(s.secrets)
	if err != nil {
		return err
	}
	blob, err := seal(s.key, pt, s.params)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
