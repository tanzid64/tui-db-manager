package config

import (
	"os"
	"path/filepath"
)

// Dir resolves the config directory (creating it 0700) using override if set,
// else the OS user config dir + "/dbk".
func Dir(override string) (string, error) {
	dir := override
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "dbk")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
