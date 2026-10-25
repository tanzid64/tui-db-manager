package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/history"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/store"
	"golang.org/x/term"
)

type appCtx struct {
	dir   string
	cfg   *config.Config
	store *store.DB
	log   *logutil.Logger
}

// history returns the durable run-history store for this config directory,
// shared with the TUI so interactive, headless, and scheduled runs all
// record into the same audit trail. If SQLite store is available, it writes to dbk.db.
func (a *appCtx) history() *history.Store {
	if a.store != nil {
		return history.NewDB(a.store)
	}
	return history.New(filepath.Join(a.dir, "history.jsonl"))
}

func loadApp(cmd *cobra.Command) (*appCtx, error) {
	override, _ := cmd.Flags().GetString("config")
	dir, err := config.Dir(override)
	if err != nil {
		return nil, err
	}

	var db *store.DB
	if store.Exists(dir) {
		key := store.ResolveMasterKey(dir)
		if key == "" && term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(os.Stderr, "Enter Master Key: ")
			if b, err := term.ReadPassword(int(os.Stdin.Fd())); err == nil {
				fmt.Fprintln(os.Stderr)
				key = string(b)
			}
		}

		s, err := store.Open(dir, key)
		if err == nil {
			db = s
		} else if key != "" && errors.Is(err, store.ErrBadPassphrase) {
			return nil, fmt.Errorf("master key authentication failed: %w", err)
		} else {
			// Try opening locked so config can still be read
			if lockedDB, err := store.Open(dir, ""); err == nil {
				db = lockedDB
			}
		}
	}

	var cfg *config.Config
	if db != nil {
		cfg, err = db.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("load config from database: %w", err)
		}
	} else {
		cfg, err = config.Load(dir)
		if err != nil {
			return nil, err
		}
	}

	return &appCtx{
		dir:   dir,
		cfg:   cfg,
		store: db,
		log:   logutil.New(os.Stderr, logutil.LevelInfo),
	}, nil
}

// setJobSchedule records a job's cron expression in the database so the TUI
// and `dbk schedule status` can show it. Errors are ignored: the OS schedule
// is the source of truth for whether the job actually runs.
func (a *appCtx) setJobSchedule(job, expr string) {
	if a.store == nil {
		return
	}
	for i := range a.cfg.Jobs {
		if a.cfg.Jobs[i].Name == job {
			a.cfg.Jobs[i].Schedule = expr
		}
	}
	_ = a.store.SaveConfig(a.cfg)
}
