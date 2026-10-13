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
	Version  int             `yaml:"version"`
	Jobs     []Job           `yaml:"jobs"`
	Storages []StorageConfig `yaml:"storages"`
}

type Job struct {
	Name      string    `yaml:"name"`
	Engine    string    `yaml:"engine"` // mysql|postgres|mongodb|sqlite
	Conn      Conn      `yaml:"conn"`
	Storage   string    `yaml:"storage"`
	Retention Retention `yaml:"retention"`
	Compress  bool      `yaml:"compress,omitempty"` // gzip-compress before upload
	Encrypt   bool      `yaml:"encrypt,omitempty"`  // age-encrypt before upload
	// EncryptRecipient is the age X25519 public key ("age1..."). Required when Encrypt is true.
	// The corresponding identity (private key) lives in the vault under "job/<name>/age_identity".
	EncryptRecipient string `yaml:"encrypt_recipient,omitempty"`
}

type Conn struct {
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	User     string `yaml:"user,omitempty"`
	Database string `yaml:"database,omitempty"`
	Path     string `yaml:"path,omitempty"` // sqlite file path
	TLS      bool   `yaml:"tls,omitempty"`
	// PasswordRef optionally resolves the password from "env:VAR" or "file:/path"
	// instead of the vault. Empty means "look in the vault".
	PasswordRef string `yaml:"password_ref,omitempty"`
}

type Retention struct {
	KeepLast int `yaml:"keep_last,omitempty"`
}

type StorageConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // local | s3

	// Local backend.
	Path string `yaml:"path,omitempty"`

	// S3-family backend (AWS S3, Cloudflare R2, MinIO, Backblaze B2, Wasabi).
	Bucket         string `yaml:"bucket,omitempty"`
	Region         string `yaml:"region,omitempty"`
	Endpoint       string `yaml:"endpoint,omitempty"` // empty = AWS default
	Prefix         string `yaml:"prefix,omitempty"`   // key prefix within the bucket
	AccessKeyID    string `yaml:"access_key_id,omitempty"`
	ForcePathStyle bool   `yaml:"force_path_style,omitempty"`
	// SecretKeyRef is "env:NAME", "file:/path", or empty (meaning the vault key
	// "storage/<name>/secret_key"). The raw secret key is never stored here.
	SecretKeyRef string `yaml:"secret_key_ref,omitempty"`

	// Google Drive backend (service account).
	FolderID string `yaml:"folder_id,omitempty"` // destination folder (My Drive or Shared Drive)
	// CredentialsRef is "env:NAME", "file:/path", or empty (meaning the vault key
	// "storage/<name>/credentials"). The raw service-account JSON is never stored here.
	CredentialsRef string `yaml:"credentials_ref,omitempty"`
}

func fileName(dir string) string { return filepath.Join(dir, "config.yaml") }

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
