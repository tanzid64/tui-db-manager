package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultStorageName is the name of the local storage created on first run.
const DefaultStorageName = "local"

// DefaultBackupDir is where the default local storage keeps backups:
// ~/dbk-backups. It sits in the home directory (never inside public_html), so
// it works the same on a VPS and on cPanel shared hosting.
func DefaultBackupDir() string {
	return filepath.Join(homeDir(), "dbk-backups")
}

// DefaultLocalStorage returns the storage created automatically on first run.
func DefaultLocalStorage() StorageConfig {
	return StorageConfig{Name: DefaultStorageName, Type: "local", Path: DefaultBackupDir()}
}

// EnsureDefaultStorage adds the default local storage when no storage exists
// yet. It reports whether the config was changed.
func (c *Config) EnsureDefaultStorage() bool {
	if len(c.Storages) > 0 {
		return false
	}
	c.Storages = append(c.Storages, DefaultLocalStorage())
	return true
}

// ExpandPath expands a leading "~" to the user's home directory and makes the
// path absolute, so local storage paths behave the same under cron.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if p == "~" {
		p = homeDir()
	} else if strings.HasPrefix(p, "~/") {
		p = filepath.Join(homeDir(), p[2:])
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "."
}
