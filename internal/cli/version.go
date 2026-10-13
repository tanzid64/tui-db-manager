package cli

import (
	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/buildinfo"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the dbk version",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Printf("dbk %s\n", buildinfo.Version)
			return nil
		},
	}
}
