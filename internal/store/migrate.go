package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/history"
)

// LegacyFilesExist returns true if any old flat file (config.yaml, secrets.json, history.jsonl) exists.
func LegacyFilesExist(dir string) bool {
	for _, name := range []string{"config.yaml", "secrets.json", "history.jsonl", "secrets.enc"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// MigrateFromFiles imports legacy file data into the SQLite database and renames migrated files to .bak.
func MigrateFromFiles(dir string, d *DB) (bool, error) {
	if !LegacyFilesExist(dir) {
		return false, nil
	}

	migratedAny := false

	// 1. Migrate config.yaml
	cfgPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		cfg, err := config.Load(dir)
		if err == nil && cfg != nil {
			if err := d.SaveConfig(cfg); err != nil {
				return false, fmt.Errorf("migrate config: %w", err)
			}
			_ = os.Rename(cfgPath, cfgPath+".bak")
			migratedAny = true
		}
	}

	// 2. Migrate secrets.json
	secPath := filepath.Join(dir, "secrets.json")
	if data, err := os.ReadFile(secPath); err == nil {
		var secMap map[string]string
		if err := json.Unmarshal(data, &secMap); err == nil {
			for k, v := range secMap {
				if err := d.SetSecret(k, v); err != nil {
					return false, fmt.Errorf("migrate secret %q: %w", k, err)
				}
			}
			_ = os.Rename(secPath, secPath+".bak")
			migratedAny = true
		}
	}

	// 3. Migrate history.jsonl
	histPath := filepath.Join(dir, "history.jsonl")
	if file, err := os.Open(histPath); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var rec history.RunRecord
			if err := json.Unmarshal([]byte(line), &rec); err == nil {
				_ = d.AppendHistory(rec)
			}
		}
		_ = file.Close()
		_ = os.Rename(histPath, histPath+".bak")
		migratedAny = true
	}

	// Rename secrets.enc if present
	encPath := filepath.Join(dir, "secrets.enc")
	if _, err := os.Stat(encPath); err == nil {
		_ = os.Rename(encPath, encPath+".bak")
		migratedAny = true
	}

	return migratedAny, nil
}
