package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the dbk command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "dbk",
		Short:         "Secure database backup CLI/TUI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("config", "", "config directory (default: OS config dir + /dbk)")
	root.PersistentFlags().String("passphrase-file", "", "file containing the vault passphrase")
	root.PersistentFlags().Bool("yes", false, "assume yes for confirmations")
	root.PersistentFlags().Bool("json", false, "machine-readable output where supported")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newRestoreCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newTestCmd())
	root.AddCommand(newSecretCmd())
	return root
}
