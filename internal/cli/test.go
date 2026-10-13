package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/db"
)

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <target>",
		Short: "Test connectivity for a job, storage backend, or notifier",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			target := args[0]
			passFile, _ := cmd.Flags().GetString("passphrase-file")
			v, err := maybeOpenVault(app.dir, passFile, true)
			if err != nil {
				return err
			}

			// 1. Check if target is a Job
			if job, ok := app.cfg.Job(target); ok {
				eng, ok := db.Get(job.Engine)
				if !ok {
					return fmt.Errorf("unknown engine %q", job.Engine)
				}
				pw, err := resolvePassword(v)(job)
				if err != nil {
					return err
				}
				conn := db.Conn{
					Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
					Password: pw, Database: job.Conn.Database, Path: job.Conn.Path, TLS: job.Conn.TLS,
				}
				if err := eng.Test(cmd.Context(), conn); err != nil {
					return err
				}
				cmd.Printf("job %q connection OK\n", target)
				return nil
			}

			// 2. Check if target is a Storage
			if sc, ok := app.cfg.Storage(target); ok {
				be, err := buildBackend(cmd.Context(), sc, v)
				if err != nil {
					return err
				}
				if err := be.Test(cmd.Context()); err != nil {
					return err
				}
				cmd.Printf("storage %q connection OK\n", target)
				return nil
			}

			// 3. Check if target is a Notifier
			if nc, ok := app.cfg.Notifier(target); ok {
				n, err := buildNotifier(cmd.Context(), nc, v)
				if err != nil {
					return err
				}
				if err := n.Test(cmd.Context()); err != nil {
					return err
				}
				cmd.Printf("notifier %q test OK\n", target)
				return nil
			}

			return fmt.Errorf("target %q not found in jobs, storages, or notifiers", target)
		},
	}
}
