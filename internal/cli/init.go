package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/store"
	"golang.org/x/term"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize dbk SQLite database and secrets vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			override, _ := cmd.Flags().GetString("config")
			dir, err := config.Dir(override)
			if err != nil {
				return err
			}

			if store.Exists(dir) {
				cmd.Printf("dbk database already exists at %s\n", store.Path(dir))
				if db, err := store.Open(dir, ""); err == nil {
					defer db.Close()
					if cfg, err := db.LoadConfig(); err == nil && cfg.EnsureDefaultStorage() {
						if db.SaveConfig(cfg) == nil {
							_ = os.MkdirAll(config.DefaultBackupDir(), 0o700)
							cmd.Printf("default local storage %q -> %s\n", config.DefaultStorageName, config.DefaultBackupDir())
						}
					}
				}
				return nil
			}

			key := store.ResolveMasterKey(dir)
			if key == "" && term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Fprint(os.Stderr, "Enter Master Password or Encryption Key: ")
				b1, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				pass1 := strings.TrimRight(string(b1), "\r\n")
				if pass1 == "" {
					return errors.New("master password cannot be empty")
				}

				fmt.Fprint(os.Stderr, "Confirm Master Password: ")
				b2, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				pass2 := strings.TrimRight(string(b2), "\r\n")
				if pass1 != pass2 {
					return errors.New("passwords do not match")
				}
				key = pass1
			}

			if key == "" {
				key = "dbk-default"
			}

			db, err := store.Init(dir, key)
			if err != nil {
				return fmt.Errorf("init database: %w", err)
			}
			defer db.Close()

			_ = store.SaveMasterKeyToEnv(dir, key)

			// Migrate any existing files if present
			if store.LegacyFilesExist(dir) {
				migrated, err := store.MigrateFromFiles(dir, db)
				if err == nil && migrated {
					cmd.Println("Migrated existing configuration, secrets, and history into SQLite database.")
				}
			}

			// Every install starts with a ready-to-use local storage.
			cfg, err := db.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.EnsureDefaultStorage() {
				if err := db.SaveConfig(cfg); err != nil {
					return fmt.Errorf("save default storage: %w", err)
				}
				_ = os.MkdirAll(config.DefaultBackupDir(), 0o700)
				cmd.Printf("default local storage %q -> %s\n", config.DefaultStorageName, config.DefaultBackupDir())
			}

			cmd.Printf("initialized dbk config at %s\n", dir)
			return nil
		},
	}
	return cmd
}
