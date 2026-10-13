package tui

import "github.com/tanzid64/tui-db-manager/internal/history"

// The run history now lives in the shared internal/history package so the
// headless CLI (and scheduled cron runs) record into the same durable store.
// These aliases keep the TUI screens unchanged.
type (
	RunRecord = history.RunRecord
	RunStatus = history.RunStatus
)

const (
	StatusSuccess = history.StatusSuccess
	StatusFailed  = history.StatusFailed
	StatusRunning = history.StatusRunning
)

// globalHistory is the persistent store the TUI reads and writes. It is
// initialized from the active config directory in NewService.
var globalHistory = history.New("")
