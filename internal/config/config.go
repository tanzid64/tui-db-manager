package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

type Config struct {
	Version   int              `yaml:"version" json:"version"`
	Jobs      []Job            `yaml:"jobs" json:"jobs"`
	Storages  []StorageConfig  `yaml:"storages" json:"storages"`
	Notifiers []NotifierConfig `yaml:"notifiers,omitempty" json:"notifiers,omitempty"`
}

type Job struct {
	Name      string    `yaml:"name" json:"name"`
	Engine    string    `yaml:"engine" json:"engine"` // mysql|postgres|mongodb|sqlite
	Conn      Conn      `yaml:"conn" json:"conn"`
	Storage   string    `yaml:"storage" json:"storage"`
	Retention Retention `yaml:"retention" json:"retention"`
	Compress  bool      `yaml:"compress,omitempty" json:"compress,omitempty"` // gzip-compress before upload
	Encrypt   bool      `yaml:"encrypt,omitempty" json:"encrypt,omitempty"`   // age-encrypt before upload
	// EncryptRecipient is the age X25519 public key ("age1..."). Required when Encrypt is true.
	// The corresponding identity (private key) lives in the vault under "job/<name>/age_identity".
	EncryptRecipient string   `yaml:"encrypt_recipient,omitempty" json:"encrypt_recipient,omitempty"`
	Notifiers        []string `yaml:"notifiers,omitempty" json:"notifiers,omitempty"`
}

type Conn struct {
	Host     string `yaml:"host,omitempty" json:"host,omitempty"`
	Port     int    `yaml:"port,omitempty" json:"port,omitempty"`
	User     string `yaml:"user,omitempty" json:"user,omitempty"`
	Database string `yaml:"database,omitempty" json:"database,omitempty"`
	Path     string `yaml:"path,omitempty" json:"path,omitempty"` // sqlite file path
	TLS      bool   `yaml:"tls,omitempty" json:"tls,omitempty"`
	// PasswordRef optionally resolves the password from "env:VAR" or "file:/path"
	// instead of the vault. Empty means "look in the vault".
	PasswordRef string `yaml:"password_ref,omitempty" json:"password_ref,omitempty"`
}

type Retention struct {
	KeepLast int `yaml:"keep_last,omitempty" json:"keep_last,omitempty"`
}

type StorageConfig struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type" json:"type"` // local | s3

	// Local backend.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// S3-family backend (AWS S3, Cloudflare R2, MinIO, Backblaze B2, Wasabi).
	Bucket         string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Region         string `yaml:"region,omitempty" json:"region,omitempty"`
	Endpoint       string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"` // empty = AWS default
	Prefix         string `yaml:"prefix,omitempty" json:"prefix,omitempty"`     // key prefix within the bucket
	AccessKeyID    string `yaml:"access_key_id,omitempty" json:"access_key_id,omitempty"`
	ForcePathStyle bool   `yaml:"force_path_style,omitempty" json:"force_path_style,omitempty"`
	// SecretKeyRef is "env:NAME", "file:/path", or empty (meaning the vault key
	// "storage/<name>/secret_key"). The raw secret key is never stored here.
	SecretKeyRef string `yaml:"secret_key_ref,omitempty" json:"secret_key_ref,omitempty"`

	// Google Drive backend (service account).
	FolderID string `yaml:"folder_id,omitempty" json:"folder_id,omitempty"` // destination folder (My Drive or Shared Drive)
	// CredentialsRef is "env:NAME", "file:/path", or empty (meaning the vault key
	// "storage/<name>/credentials"). The raw service-account JSON is never stored here.
	CredentialsRef string `yaml:"credentials_ref,omitempty" json:"credentials_ref,omitempty"`
}

type NotifierConfig struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type" json:"type"` // telegram | email | webhook

	// Telegram
	BotTokenRef string `yaml:"bot_token_ref,omitempty" json:"bot_token_ref,omitempty"` // env:/file: or vault key "notifier/<name>/bot_token"
	ChatID      string `yaml:"chat_id,omitempty" json:"chat_id,omitempty"`

	// Email
	Host        string   `yaml:"host,omitempty" json:"host,omitempty"`
	Port        int      `yaml:"port,omitempty" json:"port,omitempty"`
	From        string   `yaml:"from,omitempty" json:"from,omitempty"`
	To          []string `yaml:"to,omitempty" json:"to,omitempty"`
	Username    string   `yaml:"username,omitempty" json:"username,omitempty"`
	PasswordRef string   `yaml:"password_ref,omitempty" json:"password_ref,omitempty"` // env:/file: or vault key "notifier/<name>/password"
	TLS         bool     `yaml:"tls,omitempty" json:"tls,omitempty"`                   // implicit TLS (port 465)

	// Webhook
	URL      string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	TokenRef string            `yaml:"token_ref,omitempty" json:"token_ref,omitempty"` // env:/file: or vault key "notifier/<name>/token"
}

func fileName(dir string) string { return filepath.Join(dir, "config.yaml") }

// Exists reports whether config.yaml exists in dir.
func Exists(dir string) bool {
	_, err := os.Stat(fileName(dir))
	return err == nil
}

func Load(dir string) (*Config, error) {
	b, err := os.ReadFile(fileName(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Version: CurrentVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	return &c, nil
}

func Save(dir string, c *Config) error {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := fileName(dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, fileName(dir))
}

func (c *Config) Job(name string) (Job, bool) {
	for _, j := range c.Jobs {
		if j.Name == name {
			return j, true
		}
	}
	return Job{}, false
}

func (c *Config) Storage(name string) (StorageConfig, bool) {
	for _, s := range c.Storages {
		if s.Name == name {
			return s, true
		}
	}
	return StorageConfig{}, false
}

func (c *Config) Notifier(name string) (NotifierConfig, bool) {
	for _, n := range c.Notifiers {
		if n.Name == name {
			return n, true
		}
	}
	return NotifierConfig{}, false
}

type MergeResult struct {
	JobsAdded        int
	JobsUpdated      int
	JobsSkipped      int
	StoragesAdded    int
	StoragesUpdated  int
	StoragesSkipped  int
	NotifiersAdded   int
	NotifiersUpdated int
	NotifiersSkipped int
}

// Merge merges another config into c.
func (c *Config) Merge(other *Config, overwrite bool) MergeResult {
	var res MergeResult
	if other == nil {
		return res
	}

	for _, oj := range other.Jobs {
		found := false
		for i, j := range c.Jobs {
			if j.Name == oj.Name {
				found = true
				if overwrite {
					c.Jobs[i] = oj
					res.JobsUpdated++
				} else {
					res.JobsSkipped++
				}
				break
			}
		}
		if !found {
			c.Jobs = append(c.Jobs, oj)
			res.JobsAdded++
		}
	}

	for _, os := range other.Storages {
		found := false
		for i, s := range c.Storages {
			if s.Name == os.Name {
				found = true
				if overwrite {
					c.Storages[i] = os
					res.StoragesUpdated++
				} else {
					res.StoragesSkipped++
				}
				break
			}
		}
		if !found {
			c.Storages = append(c.Storages, os)
			res.StoragesAdded++
		}
	}

	for _, on := range other.Notifiers {
		found := false
		for i, n := range c.Notifiers {
			if n.Name == on.Name {
				found = true
				if overwrite {
					c.Notifiers[i] = on
					res.NotifiersUpdated++
				} else {
					res.NotifiersSkipped++
				}
				break
			}
		}
		if !found {
			c.Notifiers = append(c.Notifiers, on)
			res.NotifiersAdded++
		}
	}

	return res
}
