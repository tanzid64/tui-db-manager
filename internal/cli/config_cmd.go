package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/bundle"
	"github.com/tanzid64/tui-db-manager/internal/vault"
	"golang.org/x/term"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration export and import",
	}
	cmd.AddCommand(newConfigExportCmd())
	cmd.AddCommand(newConfigImportCmd())
	return cmd
}

func readTransferPassphrase(cmd *cobra.Command, prompt string) (string, error) {
	if flagVal, _ := cmd.Flags().GetString("transfer-passphrase"); flagVal != "" {
		return flagVal, nil
	}
	if envVal := os.Getenv("DBK_TRANSFER_PASSPHRASE"); envVal != "" {
		return envVal, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("transfer passphrase required: set DBK_TRANSFER_PASSPHRASE or use --transfer-passphrase")
	}
	fmt.Fprint(os.Stderr, prompt+": ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	val := strings.TrimRight(string(b), "\r\n")
	if val == "" {
		return "", fmt.Errorf("transfer passphrase cannot be empty")
	}
	return val, nil
}

func newConfigExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <file>",
		Short: "Export configuration to a portable bundle file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			targetFile := args[0]
			includeSecrets, _ := cmd.Flags().GetBool("include-secrets")

			var v *vault.Store
			var transferPass string
			if includeSecrets {
				passFile := passFileOf(cmd)
				store, err := maybeOpenVault(app.dir, passFile, true)
				if err != nil {
					return err
				}
				if store == nil {
					return fmt.Errorf("cannot export secrets: vault does not exist")
				}
				v = store
				tp, err := readTransferPassphrase(cmd, "Enter transfer passphrase to encrypt secrets")
				if err != nil {
					return err
				}
				transferPass = tp
			}

			b, err := bundle.Export(app.cfg, v, transferPass)
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}

			if err := b.Save(targetFile); err != nil {
				return fmt.Errorf("save export bundle: %w", err)
			}

			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				out, _ := json.Marshal(map[string]any{
					"exported_file":   targetFile,
					"include_secrets": includeSecrets,
					"jobs":            len(b.Config.Jobs),
					"storages":        len(b.Config.Storages),
					"notifiers":       len(b.Config.Notifiers),
				})
				cmd.Println(string(out))
			} else {
				if includeSecrets {
					cmd.Printf("configuration exported to %q (including re-encrypted secrets)\n", targetFile)
				} else {
					cmd.Printf("configuration exported to %q (structure only, secrets excluded)\n", targetFile)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("include-secrets", false, "re-encrypt and include vault secrets using a transfer passphrase")
	cmd.Flags().String("transfer-passphrase", "", "passphrase for encrypting/decrypting export bundle secrets")
	return cmd
}

func newConfigImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import configuration from a portable bundle file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			bundleFile := args[0]
			b, err := bundle.Load(bundleFile)
			if err != nil {
				return fmt.Errorf("load bundle: %w", err)
			}

			overwrite, _ := cmd.Flags().GetBool("yes")

			var v *vault.Store
			var transferPass string

			if len(b.EncryptedSecrets) > 0 {
				tp, err := readTransferPassphrase(cmd, "Enter transfer passphrase to decrypt secrets")
				if err != nil {
					return err
				}
				transferPass = tp

				passFile := passFileOf(cmd)
				if !vault.Exists(app.dir) {
					// Vault doesn't exist yet in target directory; create it.
					// Use DBK_PASSPHRASE, passFile, or prompt.
					pass, err := vault.ReadPassphrase(app.dir, passFile, true)
					if err != nil {
						return fmt.Errorf("initialize local vault: %w", err)
					}
					store, err := vault.Create(app.dir, pass)
					if err != nil {
						return fmt.Errorf("create local vault: %w", err)
					}
					v = store
				} else {
					store, err := maybeOpenVault(app.dir, passFile, true)
					if err != nil {
						return err
					}
					v = store
				}
			}

			res, err := b.Import(app.dir, v, transferPass, overwrite)
			if err != nil {
				return fmt.Errorf("import bundle: %w", err)
			}

			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				out, _ := json.Marshal(map[string]any{
					"jobs_added":        res.ConfigResult.JobsAdded,
					"jobs_updated":      res.ConfigResult.JobsUpdated,
					"jobs_skipped":      res.ConfigResult.JobsSkipped,
					"storages_added":    res.ConfigResult.StoragesAdded,
					"storages_updated":  res.ConfigResult.StoragesUpdated,
					"storages_skipped":  res.ConfigResult.StoragesSkipped,
					"notifiers_added":   res.ConfigResult.NotifiersAdded,
					"notifiers_updated": res.ConfigResult.NotifiersUpdated,
					"notifiers_skipped": res.ConfigResult.NotifiersSkipped,
					"secrets_added":     res.SecretsAdded,
					"secrets_updated":   res.SecretsUpdated,
					"secrets_skipped":   res.SecretsSkipped,
				})
				cmd.Println(string(out))
			} else {
				cmd.Printf("configuration imported from %q:\n", bundleFile)
				cmd.Printf("  jobs: %d added, %d updated, %d skipped\n",
					res.ConfigResult.JobsAdded, res.ConfigResult.JobsUpdated, res.ConfigResult.JobsSkipped)
				cmd.Printf("  storages: %d added, %d updated, %d skipped\n",
					res.ConfigResult.StoragesAdded, res.ConfigResult.StoragesUpdated, res.ConfigResult.StoragesSkipped)
				cmd.Printf("  notifiers: %d added, %d updated, %d skipped\n",
					res.ConfigResult.NotifiersAdded, res.ConfigResult.NotifiersUpdated, res.ConfigResult.NotifiersSkipped)
				if len(b.EncryptedSecrets) > 0 {
					cmd.Printf("  secrets: %d added, %d updated, %d skipped\n",
						res.SecretsAdded, res.SecretsUpdated, res.SecretsSkipped)
				}
			}

			return nil
		},
	}
	cmd.Flags().String("transfer-passphrase", "", "passphrase for decrypting export bundle secrets")
	return cmd
}
