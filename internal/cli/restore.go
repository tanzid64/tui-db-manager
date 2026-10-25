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
			if !yes {
				return fmt.Errorf("restore not confirmed: re-run with --yes (this overwrites the target database)")
			}
			storageName, _ := cmd.Flags().GetString("storage")
			// Look in every storage so backups made before the job moved to a
			// new storage (e.g. local -> S3) can still be restored.
			found, err := findJobBackups(cmd, app, args[0])
			if err != nil {
				return err
			}
			chosen, ok := restore.Locate(found, at, storageName)
			if !ok {
				switch {
				case at != "":
					return fmt.Errorf("backup %q not found in any storage (run 'dbk list %s')", at, args[0])
				case storageName != "":
					return fmt.Errorf("no backups for job %q in storage %q", args[0], storageName)
				default:
					return fmt.Errorf("no backups found for job %q", args[0])
				}
			}
			at = chosen.Key
			sc, ok := app.cfg.Storage(chosen.Storage)
			if !ok {
				return errUnknownStorage(chosen.Storage)
			}
			be, err := buildBackend(cmd.Context(), sc, v)
			if err != nil {
				return err
			}
			cmd.PrintErrf("restoring %s from storage %q\n", at, chosen.Storage)
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
	cmd.Flags().String("storage", "", "only use backups from this storage (default: search all storages)")
	return cmd
}
