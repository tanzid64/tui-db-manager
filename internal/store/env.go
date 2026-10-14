package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MasterKeyEnvVar is the canonical environment variable name for the dbk master key.
const MasterKeyEnvVar = "DBK_MASTER_KEY"

// ResolveMasterKey looks for the master key in:
// 1. Process environment variable DBK_MASTER_KEY
// 2. configDir/env or configDir/.env file
func ResolveMasterKey(configDir string) string {
	if val := os.Getenv(MasterKeyEnvVar); strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}

	for _, name := range []string{"env", ".env"} {
		p := filepath.Join(configDir, name)
		if data, err := os.ReadFile(p); err == nil {
			if key := extractKeyFromEnv(data); key != "" {
				return key
			}
		}
	}

	return ""
}

// extractKeyFromEnv parses lines like DBK_MASTER_KEY="value" or DBK_MASTER_KEY=value
func extractKeyFromEnv(data []byte) string {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if after, ok := strings.CutPrefix(line, MasterKeyEnvVar+"="); ok {
			val := strings.TrimSpace(after)
			val = strings.Trim(val, `"'`)
			if val != "" {
				return val
			}
		}
		// Also support export DBK_MASTER_KEY="value"
		if after, ok := strings.CutPrefix(line, "export "+MasterKeyEnvVar+"="); ok {
			val := strings.TrimSpace(after)
			val = strings.Trim(val, `"'`)
			if val != "" {
				return val
			}
		}
	}
	return ""
}

// SaveMasterKeyToEnv saves the master key to configDir/env with mode 0600.
func SaveMasterKeyToEnv(configDir, masterKey string) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	target := filepath.Join(configDir, "env")
	content := fmt.Sprintf("# dbk environment configuration\nexport %s=%q\n", MasterKeyEnvVar, masterKey)
	return os.WriteFile(target, []byte(content), 0o600)
}
