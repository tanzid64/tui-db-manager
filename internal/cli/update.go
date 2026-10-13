package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/buildinfo"
	"github.com/tanzid64/tui-db-manager/internal/update"
)

func newUpdateCmd() *cobra.Command {
	var checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install updates to dbk",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			current := buildinfo.Version
			cmd.Printf("Checking for updates (current version: %s)...\n", current)

			rel, isNewer, err := update.CheckForUpdate(ctx, update.DefaultRepo, current)
			if err != nil {
				return fmt.Errorf("check for updates failed: %w", err)
			}

			if !isNewer {
				cmd.Printf("✔ You are already using the latest version of dbk (%s).\n", rel.TagName)
				return nil
			}

			cmd.Printf("🔔 A new version of dbk is available: %s (current: %s)\n", rel.TagName, current)
			if rel.HTMLURL != "" {
				cmd.Printf("Release notes: %s\n", rel.HTMLURL)
			}

			if checkOnly {
				return nil
			}

			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				cmd.Printf("Do you want to update to %s now? [y/N]: ", rel.TagName)
				var resp string
				_, _ = fmt.Scanln(&resp)
				resp = strings.ToLower(strings.TrimSpace(resp))
				if resp != "y" && resp != "yes" {
					cmd.Println("Update canceled.")
					return nil
				}
			}

			cmd.Printf("Downloading and applying update %s...\n", rel.TagName)
			if err := update.ApplyUpdate(ctx, rel, ""); err != nil {
				return fmt.Errorf("update failed: %w", err)
			}

			cmd.Printf("✔ Successfully updated dbk to %s! Run 'dbk version' to verify.\n", rel.TagName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check if an update is available without installing")
	cmd.Flags().BoolP("yes", "y", false, "automatically accept update prompt without confirmation")
	return cmd
}
