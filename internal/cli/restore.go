package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/history"
	"github.com/tanzid64/tui-db-manager/internal/restore"
	"github.com/tanzid64/tui-db-manager/internal/storage"
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
			var v SecretGetter
			if app.store != nil && app.store.IsUnlocked() {
				v = app.store
			} else {
				opened, err := maybeOpenVault(app.dir)
				if err != nil {
					return err
				}
				v = opened
			}
			job, ok := app.cfg.Job(args[0])
			if !ok {
				return errUnknownJob(args[0])
			}
			sc, ok := app.cfg.Storage(job.Storage)
			if !ok {
				return errUnknownStorage(job.Storage)
			}
			be, err := buildBackend(cmd.Context(), sc, v)
			if err != nil {
				return err
			}
			deps := restore.Deps{
				Cfg:             app.cfg,
				Engine:          engineResolver(),
				Backend:         func() storage.Backend { return be },
				ResolvePassword: resolvePassword(v),
				ResolveIdentity: resolveIdentity(v),
				Log:             app.log,
			}
			start := time.Now()
			err = restore.Run(cmd.Context(), deps, args[0], at, to, yes)
			rec := history.RunRecord{
				ID:       fmt.Sprintf("restore-%s-%d", args[0], start.Unix()),
				Job:      args[0],
				Action:   "restore",
				Started:  start,
				Duration: time.Since(start),
				Target:   at,
			}
			if err != nil {
				rec.Status = history.StatusFailed
				rec.Error = err.Error()
			} else {
				rec.Status = history.StatusSuccess
			}
			_ = app.history().Append(rec)
			return err
		},
	}
	cmd.Flags().String("at", "", "backup key to restore (default: latest)")
	cmd.Flags().String("to", "", "restore into a different database")
	return cmd
}
