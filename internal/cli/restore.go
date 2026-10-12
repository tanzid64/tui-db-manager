package cli

import (
	"github.com/tanzid64/tui-db-manager/internal/restore"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <job>",
		Short: "Restore a backup (requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			at, _ := cmd.Flags().GetString("at")
			to, _ := cmd.Flags().GetString("to")
			yes, _ := cmd.Flags().GetBool("yes")
			passFile, _ := cmd.Flags().GetString("passphrase-file")
			v, err := maybeOpenVault(app.dir, passFile, true)
			if err != nil {
				return err
			}
			job, ok := app.cfg.Job(args[0])
			if !ok {
				return errUnknownJob(args[0])
			}
			sc, ok := app.cfg.Storage(job.Storage)
			if !ok {
				return errUnknownStorage(job.Storage)
			}
			be, err := buildBackend(sc)
			if err != nil {
				return err
			}
			deps := restore.Deps{
				Cfg:             app.cfg,
				Engine:          engineResolver(),
				Backend:         func() storage.Backend { return be },
				ResolvePassword: resolvePassword(v),
				Log:             app.log,
			}
			return restore.Run(cmd.Context(), deps, args[0], at, to, yes)
		},
	}
	cmd.Flags().String("at", "", "backup key to restore (default: latest)")
	cmd.Flags().String("to", "", "restore into a different database")
	return cmd
}
