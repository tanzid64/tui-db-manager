package secretref

import (
	"fmt"
	"os"
	"strings"
)

// Resolve turns a reference into a secret value.
//   - "env:NAME"   -> value of environment variable NAME
//   - "file:/path" -> trimmed contents of the file
//   - anything else -> looked up as a vault key via lookup
func Resolve(ref string, lookup func(key string) (string, bool)) (string, error) {
	switch {
	case strings.HasPrefix(ref, "env:"):
		name := strings.TrimPrefix(ref, "env:")
		v, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("env var %q not set", name)
		}
		return v, nil
	case strings.HasPrefix(ref, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(ref, "file:"))
		if err != nil {
			return "", fmt.Errorf("read secret file: %w", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	default:
		if lookup == nil {
			return "", fmt.Errorf("no vault available to resolve %q", ref)
		}
		v, ok := lookup(ref)
		if !ok {
			return "", fmt.Errorf("secret %q not found in vault", ref)
		}
		return v, nil
	}
}
