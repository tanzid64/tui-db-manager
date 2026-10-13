package bundle

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/vault"
	"gopkg.in/yaml.v3"
)

const FormatVersion = 1

type Bundle struct {
	Version          int            `yaml:"version" json:"version"`
	CreatedAt        time.Time      `yaml:"created_at" json:"created_at"`
	Config           *config.Config `yaml:"config" json:"config"`
	EncryptedSecrets string         `yaml:"encrypted_secrets,omitempty" json:"encrypted_secrets,omitempty"`
}

type ImportResult struct {
	ConfigResult   config.MergeResult
	SecretsAdded   int
	SecretsUpdated int
	SecretsSkipped int
}

// Export creates a portable bundle from the provided config and optional vault store.
// If transferPassphrase is provided and v is non-nil, all secrets in v are re-encrypted
// with the transferPassphrase and embedded into the bundle as a base64 string.
func Export(cfg *config.Config, v *vault.Store, transferPassphrase string) (*Bundle, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	b := &Bundle{
		Version:   FormatVersion,
		CreatedAt: time.Now().UTC(),
		Config:    cfg,
	}

	if transferPassphrase != "" && v != nil {
		enc, err := v.Export(transferPassphrase)
		if err != nil {
			return nil, fmt.Errorf("re-encrypt secrets for export: %w", err)
		}
		b.EncryptedSecrets = base64.StdEncoding.EncodeToString(enc)
	}

	return b, nil
}

// Save serializes the bundle to the target file. If the file ends with .json,
// JSON is used; otherwise YAML is written with 0600 file permissions.
func (b *Bundle) Save(targetFile string) error {
	var data []byte
	var err error

	if strings.ToLower(filepath.Ext(targetFile)) == ".json" {
		data, err = json.MarshalIndent(b, "", "  ")
	} else {
		data, err = yaml.Marshal(b)
	}
	if err != nil {
		return fmt.Errorf("serialize bundle: %w", err)
	}

	tmp := targetFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, targetFile)
}

// Load reads and parses a bundle file (YAML or JSON).
func Load(targetFile string) (*Bundle, error) {
	raw, err := os.ReadFile(targetFile)
	if err != nil {
		return nil, err
	}

	var b Bundle
	if err := yaml.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("parse bundle: %w", err)
	}

	if b.Config == nil {
		return nil, fmt.Errorf("invalid bundle: missing config section")
	}

	return &b, nil
}

// Import merges the bundle's config and decrypted secrets into the local configuration.
func (b *Bundle) Import(cfgDir string, v *vault.Store, transferPassphrase string, overwrite bool) (*ImportResult, error) {
	localCfg, err := config.Load(cfgDir)
	if err != nil {
		return nil, fmt.Errorf("load local config: %w", err)
	}

	cfgRes := localCfg.Merge(b.Config, overwrite)
	if err := config.Save(cfgDir, localCfg); err != nil {
		return nil, fmt.Errorf("save merged config: %w", err)
	}

	res := &ImportResult{
		ConfigResult: cfgRes,
	}

	if len(b.EncryptedSecrets) > 0 {
		if transferPassphrase == "" {
			return nil, fmt.Errorf("bundle contains encrypted secrets: transfer passphrase is required")
		}
		if v == nil {
			return nil, fmt.Errorf("bundle contains encrypted secrets: local vault is required")
		}

		rawEnc, err := base64.StdEncoding.DecodeString(b.EncryptedSecrets)
		if err != nil {
			return nil, fmt.Errorf("decode encrypted secrets base64: %w", err)
		}

		decrypted, err := vault.ImportDecrypt(transferPassphrase, rawEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt bundle secrets: %w", err)
		}

		added, updated, skipped := v.Merge(decrypted, overwrite)
		if err := v.Save(); err != nil {
			return nil, fmt.Errorf("save merged vault: %w", err)
		}

		res.SecretsAdded = added
		res.SecretsUpdated = updated
		res.SecretsSkipped = skipped
	}

	return res, nil
}
