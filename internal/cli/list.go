package cli

import (
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <job>",
		Short: "List stored backups for a job",
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
			sc, ok := app.cfg.Storage(job.Storage)
			if !ok {
				return errUnknownStorage(job.Storage)
			}
			// A missing vault is fine if the storage uses an env:/file: secret ref.
			v, _ := maybeOpenVault(app.dir, passFileOf(cmd), true)
			be, err := buildBackend(cmd.Context(), sc, v)
			if err != nil {
				return err
			}
			objs, err := be.List(cmd.Context(), job.Name+"/")
			if err != nil {
				return err
			}
			for _, o := range objs {
				cmd.Printf("%s\t%d\t%s\n", o.Key, o.Size, o.ModTime.Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
}
