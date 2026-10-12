package cli

import (
	"os"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/spf13/cobra"
)

type appCtx struct {
	dir string
	cfg *config.Config
	log *logutil.Logger
}

func loadApp(cmd *cobra.Command) (*appCtx, error) {
	override, _ := cmd.Flags().GetString("config")
	dir, err := config.Dir(override)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	return &appCtx{dir: dir, cfg: cfg, log: logutil.New(os.Stderr, logutil.LevelInfo)}, nil
}
