package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/history"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
)

type appCtx struct {
	dir string
	cfg *config.Config
	log *logutil.Logger
}

// history returns the durable run-history store for this config directory,
// shared with the TUI so interactive, headless, and scheduled runs all
// record into the same audit trail.
func (a *appCtx) history() *history.Store {
	return history.New(filepath.Join(a.dir, "history.jsonl"))
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
