package tui

import (
	"sync"
	"time"
)

type RunStatus string

const (
	StatusSuccess RunStatus = "SUCCESS"
	StatusFailed  RunStatus = "FAILED"
	StatusRunning RunStatus = "RUNNING"
)

type RunRecord struct {
	ID       string        `json:"id"`
	Job      string        `json:"job"`
	Action   string        `json:"action"` // "backup", "restore", "test"
	Status   RunStatus     `json:"status"`
	Started  time.Time     `json:"started"`
	Duration time.Duration `json:"duration"`
	Size     int64         `json:"size,omitempty"`
	Error    string        `json:"error,omitempty"`
	Target   string        `json:"target,omitempty"`
}

type HistoryStore struct {
	mu      sync.RWMutex
	records []RunRecord
}

var globalHistory = &HistoryStore{
	records: make([]RunRecord, 0),
}

func (h *HistoryStore) Add(rec RunRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Prepend to show most recent first, max 100 entries
	h.records = append([]RunRecord{rec}, h.records...)
	if len(h.records) > 100 {
		h.records = h.records[:100]
	}
}

func (h *HistoryStore) All() []RunRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()
	copied := make([]RunRecord, len(h.records))
	copy(copied, h.records)
	return copied
}

func (h *HistoryStore) LastForJob(jobName string) *RunRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, r := range h.records {
		if r.Job == jobName && (r.Action == "backup" || r.Action == "run") {
			return &r
		}
	}
	return nil
}
