package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/backup"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/history"
	"github.com/tanzid64/tui-db-manager/internal/notify"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <job>",
		Short: "Run a backup job now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
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
			deps := backup.Deps{
				Cfg:    app.cfg,
				Engine: engineResolver(),
				Backend: func(sc config.StorageConfig) (storage.Backend, error) {
					return buildBackend(cmd.Context(), sc, v)
				},
				ResolvePassword: resolvePassword(v),
				ResolveIdentity: resolveIdentity(v),
				Notifier: func(nc config.NotifierConfig) (notify.Notifier, error) {
					return buildNotifier(cmd.Context(), nc, v)
				},
				Now: time.Now,
				Log: app.log,
			}
			start := time.Now()
			obj, err := backup.Run(cmd.Context(), deps, args[0])
			rec := history.RunRecord{
				ID:       fmt.Sprintf("%s-%d", args[0], start.Unix()),
				Job:      args[0],
				Action:   "backup",
				Started:  start,
				Duration: time.Since(start),
			}
			if err != nil {
				rec.Status = history.StatusFailed
				rec.Error = err.Error()
				_ = app.history().Append(rec)
				return err
			}
			rec.Status = history.StatusSuccess
			rec.Size = obj.Size
			rec.Target = obj.Key
			_ = app.history().Append(rec)

			quiet, _ := cmd.Flags().GetBool("quiet")
			if !quiet {
				cmd.Printf("backup written: %s (%d bytes)\n", obj.Key, obj.Size)
			}
			return nil
		},
	}
	cmd.Flags().Bool("quiet", false, "suppress non-essential output (for unattended/scheduled runs)")
	return cmd
}
