// Package history provides a durable, append-only record of backup and
// restore runs shared by the TUI and the headless CLI. Records are persisted
// as JSON Lines so that concurrent writers (an interactive session and a cron
// job) can both append safely.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxRecords is the number of most-recent records retained on disk.
const maxRecords = 100

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

// DBHandler abstracts the database storage for history run records.
type DBHandler interface {
	AppendHistory(rec RunRecord) error
	AllHistory() ([]RunRecord, error)
	LastForJob(job string) (*RunRecord, error)
}

// Store is a run history store backed by SQLite (if db != nil) or a JSON Lines file.
type Store struct {
	mu   sync.Mutex
	path string
	max  int
	db   DBHandler
}

// New returns a history store backed by the JSON Lines file at path.
func New(path string) *Store {
	return &Store{path: path, max: maxRecords}
}

// NewDB returns a history store backed by a SQLite DBHandler.
func NewDB(db DBHandler) *Store {
	return &Store{db: db, max: maxRecords}
}

// Append durably records rec, then prunes if necessary.
func (s *Store) Append(rec RunRecord) error {
	if s.db != nil {
		return s.db.AppendHistory(rec)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	// O_APPEND keeps a single write atomic across processes on POSIX, so an
	// interactive session and a cron job never interleave a record.
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return s.pruneLocked()
}

// All returns every recorded run, newest first. Unreadable or corrupt lines
// are skipped so a torn write never hides the rest of the history.
func (s *Store) All() []RunRecord {
	if s.db != nil {
		recs, err := s.db.AllHistory()
		if err == nil {
			return recs
		}
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return reverse(s.readLocked())
}

// LastForJob returns the most recent backup/run record for jobName, or nil.
func (s *Store) LastForJob(jobName string) *RunRecord {
	if s.db != nil {
		rec, err := s.db.LastForJob(jobName)
		if err == nil {
			return rec
		}
		return nil
	}

	for _, r := range s.All() {
		if r.Job == jobName && (r.Action == "backup" || r.Action == "run") {
			rr := r
			return &rr
		}
	}
	return nil
}

// readLocked returns records in file (oldest-first) order. Caller holds mu.
func (s *Store) readLocked() []RunRecord {
	f, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []RunRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r RunRecord
		if err := json.Unmarshal(line, &r); err != nil {
			continue // skip torn/partial lines
		}
		out = append(out, r)
	}
	return out
}

// pruneLocked rewrites the file to the newest max records when it grows past
// the cap. Caller holds mu.
func (s *Store) pruneLocked() error {
	recs := s.readLocked()
	if len(recs) <= s.max {
		return nil
	}
	recs = recs[len(recs)-s.max:]

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".history-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	w := bufio.NewWriter(tmp)
	for _, r := range recs {
		line, err := json.Marshal(r)
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}

func reverse(in []RunRecord) []RunRecord {
	out := make([]RunRecord, len(in))
	for i, r := range in {
		out[len(in)-1-i] = r
	}
	return out
}
