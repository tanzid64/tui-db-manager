package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/store"
	"golang.org/x/term"
)

func newUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Cleanly remove dbk schedules, configuration, database, and optionally the binary",
		Long: `Uninstall dbk cleanly from the system.
By default, this command removes all automated job schedules (crontab, systemd, or Task Scheduler)
and purges the configuration directory and SQLite database (~/.config/dbk).
With --remove-binary, it also removes the dbk executable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			yes, _ := cmd.Flags().GetBool("yes")
			purgeData, _ := cmd.Flags().GetBool("purge-data")
			removeBinary, _ := cmd.Flags().GetBool("remove-binary")
			all, _ := cmd.Flags().GetBool("all")

			if all {
				purgeData = true
				removeBinary = true
			}

			override, _ := cmd.Flags().GetString("config")
			dir, err := config.Dir(override)
			if err != nil {
				return err
			}

			// Interactive confirmation if not --yes
			if !yes {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("confirmation required: use --yes to proceed non-interactively")
				}

				fmt.Println("⚠️  WARNING: You are about to perform a clean uninstall of dbk.")
				fmt.Printf("   Config / Database directory to remove: %s\n", dir)
				if removeBinary {
					exe, _ := os.Executable()
					fmt.Printf("   Binary to remove: %s\n", exe)
				}
				fmt.Print("\nAre you sure you want to proceed? [y/N]: ")

				reader := bufio.NewReader(os.Stdin)
				input, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				input = strings.TrimSpace(strings.ToLower(input))
				if input != "y" && input != "yes" {
					fmt.Println("Uninstall cancelled.")
					return nil
				}
			}

			cmd.Println("==> Removing automated backup schedules...")
			sched := defaultScheduler

			// Load config to discover all scheduled jobs
			var jobNames []string
			if store.Exists(dir) {
				if s, err := store.Open(dir, ""); err == nil {
					if cfg, err := s.LoadConfig(); err == nil && cfg != nil {
						for _, j := range cfg.Jobs {
							jobNames = append(jobNames, j.Name)
						}
					}
					_ = s.Close()
				}
			} else {
				if cfg, err := config.Load(dir); err == nil && cfg != nil {
					for _, j := range cfg.Jobs {
						jobNames = append(jobNames, j.Name)
					}
				}
			}

			// Also scan schedules directory for wrapper scripts to discover any orphaned jobs
			schedulesDir := filepath.Join(dir, "schedules")
			if entries, err := os.ReadDir(schedulesDir); err == nil {
				for _, e := range entries {
					name := e.Name()
					if strings.HasPrefix(name, "dbk-") && strings.HasSuffix(name, ".sh") {
						job := strings.TrimSuffix(strings.TrimPrefix(name, "dbk-"), ".sh")
						found := false
						for _, existing := range jobNames {
							if existing == job {
								found = true
								break
							}
						}
						if !found {
							jobNames = append(jobNames, job)
						}
					}
				}
			}

			for _, job := range jobNames {
				if err := sched.Remove(cmd.Context(), job); err == nil {
					cmd.Printf("  ✔ Removed schedule for %q\n", job)
				}
			}

			// Clean up schedules directory if present
			_ = os.RemoveAll(schedulesDir)

			if purgeData {
				cmd.Printf("==> Purging configuration, database, and secrets from %s...\n", dir)
				if err := os.RemoveAll(dir); err != nil {
					return fmt.Errorf("purge data directory: %w", err)
				}
				cmd.Println("  ✔ Data directory removed.")
			}

			if removeBinary {
				exe, err := os.Executable()
				if err == nil {
					exe, _ = filepath.EvalSymlinks(exe)
					cmd.Printf("==> Removing binary at %s...\n", exe)
					if err := os.Remove(exe); err != nil {
						cmd.Printf("  ⚠️  Could not remove binary directly (%v). You may need to run:\n      sudo rm %s\n", err, exe)
					} else {
						cmd.Println("  ✔ Binary successfully removed.")
					}
				}
			}

			cmd.Println("\n✔ Clean uninstall completed successfully.")
			return nil
		},
	}

	cmd.Flags().BoolP("yes", "y", false, "confirm uninstall without interactive prompt")
	cmd.Flags().Bool("purge-data", true, "purge database, configuration, and secrets directory (~/.config/dbk)")
	cmd.Flags().Bool("remove-binary", false, "remove the dbk executable from the system")
	cmd.Flags().Bool("all", false, "remove schedules, purge all data, and remove the dbk executable")

	return cmd
}
