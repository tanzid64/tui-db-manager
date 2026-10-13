package notify

import (
	"context"
	"time"
)

type EventType string

const (
	EventBackupSucceeded EventType = "backup_succeeded"
	EventBackupFailed    EventType = "backup_failed"
	EventRestoreDone     EventType = "restore_done"
	EventTest            EventType = "test"
)

type Event struct {
	Type    EventType     `json:"type"`
	Job     string        `json:"job"`
	Message string        `json:"message"`
	Size    int64         `json:"size,omitempty"`
	Started time.Time     `json:"started"`
	Elapsed time.Duration `json:"elapsed"`
	Err     string        `json:"err,omitempty"`
}

type Notifier interface {
	Name() string
	Send(ctx context.Context, ev Event) error
	Test(ctx context.Context) error
}
