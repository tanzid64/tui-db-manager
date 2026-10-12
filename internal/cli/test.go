package cli

import (
	"fmt"

	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <job>",
		Short: "Test connectivity for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			job, ok := app.cfg.Job(args[0])
			if !ok {
				return errUnknownJob(args[0])
			}
			eng, ok := db.Get(job.Engine)
			if !ok {
				return fmt.Errorf("unknown engine %q", job.Engine)
			}
			passFile, _ := cmd.Flags().GetString("passphrase-file")
			v, err := maybeOpenVault(app.dir, passFile, true)
			if err != nil {
				return err
			}
			pw, err := resolvePassword(v)(job)
			if err != nil {
				return err
			}
			conn := db.Conn{Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
				Password: pw, Database: job.Conn.Database, Path: job.Conn.Path, TLS: job.Conn.TLS}
			if err := eng.Test(cmd.Context(), conn); err != nil {
				return err
			}
			cmd.Println("connection OK")
			return nil
		},
	}
}
