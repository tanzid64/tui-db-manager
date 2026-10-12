package vault

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPassphrase resolves the vault passphrase from, in order: the
// DBK_PASSPHRASE env var, a passphrase file, or an interactive TTY prompt.
func ReadPassphrase(dir, passFile string, interactive bool) (string, error) {
	if v := os.Getenv("DBK_PASSPHRASE"); v != "" {
		return v, nil
	}
	if passFile != "" {
		b, err := os.ReadFile(passFile)
		if err != nil {
			return "", fmt.Errorf("read passphrase file: %w", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	if !interactive || !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no passphrase available: set DBK_PASSPHRASE or use --passphrase-file")
	}
	fmt.Fprint(os.Stderr, "Vault passphrase: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
