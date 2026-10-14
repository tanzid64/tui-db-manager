package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/history"
)

const maxHistoryRecords = 100

// AppendHistory inserts a new execution run record and keeps the latest maxHistoryRecords.
func (d *DB) AppendHistory(rec history.RunRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	startedStr := rec.Started.Format(time.RFC3339Nano)
	query := `INSERT INTO history (id, job, action, status, started, duration, size, error, target)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := d.db.Exec(query,
		rec.ID, rec.Job, rec.Action, string(rec.Status), startedStr,
		int64(rec.Duration), rec.Size, rec.Error, rec.Target,
	)
	if err != nil {
		return fmt.Errorf("insert history: %w", err)
	}

	// Prune history to keep only the newest maxHistoryRecords
	pruneQuery := `DELETE FROM history WHERE id NOT IN (
		SELECT id FROM history ORDER BY started DESC LIMIT ?
	)`
	_, _ = d.db.Exec(pruneQuery, maxHistoryRecords)

	return nil
}

// AllHistory returns all history run records newest first.
func (d *DB) AllHistory() ([]history.RunRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	rows, err := d.db.Query(`SELECT
		id, job, action, status, started, duration, size, error, target
	FROM history ORDER BY started DESC`)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var records []history.RunRecord
	for rows.Next() {
		var rec history.RunRecord
		var statusStr, startedStr string
		var durationNanos int64

		err := rows.Scan(
			&rec.ID, &rec.Job, &rec.Action, &statusStr, &startedStr,
			&durationNanos, &rec.Size, &rec.Error, &rec.Target,
		)
		if err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}

		rec.Status = history.RunStatus(statusStr)
		if t, err := time.Parse(time.RFC3339Nano, startedStr); err == nil {
			rec.Started = t
		} else if t, err := time.Parse(time.RFC3339, startedStr); err == nil {
			rec.Started = t
		}
		rec.Duration = time.Duration(durationNanos)
		records = append(records, rec)
	}

	return records, nil
}

// LastForJob returns the most recent run record for a given job, or nil if none found.
func (d *DB) LastForJob(job string) (*history.RunRecord, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	row := d.db.QueryRow(`SELECT
		id, job, action, status, started, duration, size, error, target
	FROM history WHERE job = ? ORDER BY started DESC LIMIT 1`, job)

	var rec history.RunRecord
	var statusStr, startedStr string
	var durationNanos int64

	err := row.Scan(
		&rec.ID, &rec.Job, &rec.Action, &statusStr, &startedStr,
		&durationNanos, &rec.Size, &rec.Error, &rec.Target,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query last history for job: %w", err)
	}

	rec.Status = history.RunStatus(statusStr)
	if t, err := time.Parse(time.RFC3339Nano, startedStr); err == nil {
		rec.Started = t
	} else if t, err := time.Parse(time.RFC3339, startedStr); err == nil {
		rec.Started = t
	}
	rec.Duration = time.Duration(durationNanos)
	return &rec, nil
}
