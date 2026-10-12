package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func (s *Store) Get(key string) (string, bool) { v, ok := s.secrets[key]; return v, ok }
func (s *Store) Set(key, val string)           { s.secrets[key] = val }
func (s *Store) Delete(key string)             { delete(s.secrets, key) }

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
