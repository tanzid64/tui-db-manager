package cli

import (
	"time"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/backup"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <job>",
		Short: "Run a backup job now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			passFile, _ := cmd.Flags().GetString("passphrase-file")
			v, err := maybeOpenVault(app.dir, passFile, true)
			if err != nil {
				return err
			}
			deps := backup.Deps{
				Cfg:    app.cfg,
				Engine: engineResolver(),
				Backend: func(sc config.StorageConfig) (storage.Backend, error) {
					return buildBackend(cmd.Context(), sc, v)
				},
				ResolvePassword: resolvePassword(v),
				ResolveIdentity: resolveIdentity(v),
				Now:             time.Now,
				Log:             app.log,
			}
			obj, err := backup.Run(cmd.Context(), deps, args[0])
			if err != nil {
				return err
			}
			cmd.Printf("backup written: %s (%d bytes)\n", obj.Key, obj.Size)
			return nil
		},
	}
}
