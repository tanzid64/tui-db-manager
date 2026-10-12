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
	root.AddCommand(newVersionCmd())
	return root
}
