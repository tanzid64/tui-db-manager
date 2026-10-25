package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/restore"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <job>",
		Short: "List a job's backups in every storage (newest first)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			found, err := findJobBackups(cmd, app, args[0])
			if err != nil {
				return err
			}
			for _, b := range found {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%d\t%s\t%s\n", b.Key, b.Size, b.ModTime.Format("2006-01-02 15:04:05"), b.Storage)
			}
			return nil
		},
	}
}

// findJobBackups lists the job's backups across all storages. Storages that
// can't be read are reported on stderr; it fails only if nothing was found
// and some storage errored.
func findJobBackups(cmd *cobra.Command, app *appCtx, job string) ([]restore.Backup, error) {
	if _, ok := app.cfg.Job(job); !ok {
		return nil, errUnknownJob(job)
	}
	v := app.secrets()
	found, errs := restore.FindBackups(cmd.Context(), app.cfg, job, func(sc config.StorageConfig) (storage.Backend, error) {
		return buildBackend(cmd.Context(), sc, v)
	})
	for _, e := range errs {
		cmd.PrintErrf("warning: could not read %v\n", e)
	}
	if len(found) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return found, nil
}
