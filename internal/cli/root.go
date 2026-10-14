package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/tui"
	"golang.org/x/term"
)

// NewRootCmd builds the dbk command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "dbk",
		Short:         "Secure database backup CLI/TUI",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return cmd.Help()
			}
			cfgDir, _ := cmd.Flags().GetString("config")
			return tui.Run(cfgDir, "")
		},
	}
	root.PersistentFlags().String("config", "", "config directory (default: OS config dir + /dbk)")
	root.PersistentFlags().String("passphrase-file", "", "deprecated: vault password has been removed")
	_ = root.PersistentFlags().MarkHidden("passphrase-file")
	root.PersistentFlags().Bool("yes", false, "assume yes for confirmations")
	root.PersistentFlags().Bool("json", false, "machine-readable output where supported")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newRestoreCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newTestCmd())
	root.AddCommand(newSecretCmd())
	root.AddCommand(newScheduleCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newUninstallCmd())
	return root
}
