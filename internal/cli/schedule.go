package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/schedule"
)

var defaultScheduler = schedule.DetectScheduler()

func newScheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage automated backup schedules (systemd timer, cron, Task Scheduler)",
	}
	cmd.AddCommand(newScheduleInstallCmd())
	cmd.AddCommand(newScheduleRemoveCmd())
	cmd.AddCommand(newScheduleStatusCmd())
	return cmd
}

func newScheduleInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <job>",
		Short: "Install a system schedule for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			jobName := args[0]
			job, ok := app.cfg.Job(jobName)
			if !ok {
				return errUnknownJob(jobName)
			}

			at, _ := cmd.Flags().GetString("at")
			if at == "" {
				at = "0 3 * * *"
			}

			passFile, _ := cmd.Flags().GetString("passphrase-file")
			pathOverride, _ := cmd.Flags().GetString("path")

			sched := defaultScheduler
			opts := schedule.InstallOptions{
				Expression:     at,
				ConfigDir:      app.dir,
				PassphraseFile: passFile,
				PathOverride:   pathOverride,
			}

			if err := sched.Install(cmd.Context(), job.Name, opts); err != nil {
				return fmt.Errorf("install schedule: %w", err)
			}

			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				out, _ := json.Marshal(map[string]any{
					"job":        job.Name,
					"installed":  true,
					"expression": at,
					"target":     sched.Name(),
				})
				cmd.Println(string(out))
			} else {
				cmd.Printf("schedule installed for %q (%s, %s)\n", job.Name, at, sched.Name())
			}
			return nil
		},
	}
	cmd.Flags().String("at", "0 3 * * *", "schedule expression (cron format, e.g. '0 3 * * *')")
	cmd.Flags().String("path", "", "override PATH baked into the scheduled job's wrapper (default: current PATH plus standard system dirs)")
	return cmd
}

func newScheduleRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <job>",
		Short: "Remove a system schedule for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			jobName := args[0]
			if _, ok := app.cfg.Job(jobName); !ok {
				return errUnknownJob(jobName)
			}

			sched := defaultScheduler
			if err := sched.Remove(cmd.Context(), jobName); err != nil {
				return fmt.Errorf("remove schedule: %w", err)
			}

			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				out, _ := json.Marshal(map[string]any{
					"job":     jobName,
					"removed": true,
					"target":  sched.Name(),
				})
				cmd.Println(string(out))
			} else {
				cmd.Printf("schedule removed for %q (%s)\n", jobName, sched.Name())
			}
			return nil
		},
	}
}

func newScheduleStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <job>",
		Short: "Show schedule status for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			jobName := args[0]
			if _, ok := app.cfg.Job(jobName); !ok {
				return errUnknownJob(jobName)
			}

			sched := defaultScheduler
			st, err := sched.Status(cmd.Context(), jobName)
			if err != nil {
				return fmt.Errorf("schedule status: %w", err)
			}

			jsonOut, _ := cmd.Flags().GetBool("json")
			if jsonOut {
				out, _ := json.Marshal(st)
				cmd.Println(string(out))
			} else {
				if st.Installed {
					cmd.Printf("schedule for %q is active on %s (expression: %s)\n", jobName, st.Target, st.Expression)
					if st.Detail != "" {
						cmd.Printf("detail: %s\n", st.Detail)
					}
				} else {
					cmd.Printf("no schedule installed for %q on %s\n", jobName, st.Target)
				}
			}
			return nil
		},
	}
}
