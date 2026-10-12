package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/vault"
	"github.com/spf13/cobra"
)

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "secret", Short: "Manage secrets in the encrypted vault"}
	cmd.AddCommand(newSecretSetCmd(), newSecretRmCmd(), newSecretLsCmd())
	return cmd
}

func openOrCreateVault(cmd *cobra.Command) (string, *vault.Store, error) {
	override, _ := cmd.Flags().GetString("config")
	dir, err := config.Dir(override)
	if err != nil {
		return "", nil, err
	}
	passFile, _ := cmd.Flags().GetString("passphrase-file")
	pass, err := vault.ReadPassphrase(dir, passFile, true)
	if err != nil {
		return "", nil, err
	}
	if vault.Exists(dir) {
		s, err := vault.Open(dir, pass)
		return dir, s, err
	}
	s, err := vault.Create(dir, pass)
	return dir, s, err
}

func newSecretSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key>",
		Short: "Store a secret value under a key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openOrCreateVault(cmd)
			if err != nil {
				return err
			}
			val, _ := cmd.Flags().GetString("value")
			useStdin, _ := cmd.Flags().GetBool("stdin")
			if useStdin {
				b, rerr := io.ReadAll(os.Stdin)
				if rerr != nil {
					return rerr
				}
				val = strings.TrimRight(string(b), "\r\n")
			}
			if val == "" {
				return fmt.Errorf("no value provided: use --value or --stdin")
			}
			s.Set(args[0], val)
			if err := s.Save(); err != nil {
				return err
			}
			cmd.Printf("stored secret %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().String("value", "", "the secret value (prefer --stdin to keep it out of shell history)")
	cmd.Flags().Bool("stdin", false, "read the secret value from stdin")
	return cmd
}

func newSecretRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <key>",
		Short: "Remove a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openOrCreateVault(cmd)
			if err != nil {
				return err
			}
			s.Delete(args[0])
			if err := s.Save(); err != nil {
				return err
			}
			cmd.Printf("removed secret %q\n", args[0])
			return nil
		},
	}
}

func newSecretLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List secret keys (never values)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openOrCreateVault(cmd)
			if err != nil {
				return err
			}
			for _, k := range s.Keys() {
				cmd.Println(k)
			}
			return nil
		},
	}
}
