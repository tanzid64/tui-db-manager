package cli

import (
	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the config directory and encrypted vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			override, _ := cmd.Flags().GetString("config")
			dir, err := config.Dir(override)
			if err != nil {
				return err
			}
			// Load preserves any existing config; saving it back never wipes
			// jobs/storages if init is re-run.
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}
			if err := config.Save(dir, cfg); err != nil {
				return err
			}
			if !vault.Exists(dir) {
				passFile, _ := cmd.Flags().GetString("passphrase-file")
				pass, err := vault.ReadPassphrase(dir, passFile, true)
				if err != nil {
					return err
				}
				s, err := vault.Create(dir, pass)
				if err != nil {
					return err
				}
				if err := s.Save(); err != nil {
					return err
				}
			}
			cmd.Printf("initialized dbk config at %s\n", dir)
			return nil
		},
	}
}
