package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rec(id, job string, status RunStatus, started time.Time) RunRecord {
	return RunRecord{ID: id, Job: job, Action: "backup", Status: status, Started: started}
}

func TestAppendAndAllNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	now := time.Now()

	if err := s.Append(rec("run-1", "job-a", StatusSuccess, now.Add(-10*time.Minute))); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := s.Append(rec("run-2", "job-a", StatusFailed, now)); err != nil {
		t.Fatalf("append: %v", err)
	}

	all := s.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	if all[0].ID != "run-2" {
		t.Fatalf("expected newest record first, got %s", all[0].ID)
	}
}

func TestPersistsAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	if err := New(path).Append(rec("run-1", "job-a", StatusSuccess, time.Now())); err != nil {
		t.Fatalf("append: %v", err)
	}

	// A fresh store (new process) must read the record back from disk.
	all := New(path).All()
	if len(all) != 1 || all[0].ID != "run-1" {
		t.Fatalf("record did not persist across stores: %+v", all)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	base := time.Now()
	for i := 0; i < maxRecords+25; i++ {
		id := "run-" + time.Duration(i).String()
		if err := s.Append(rec(id, "job-a", StatusSuccess, base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	all := s.All()
	if len(all) != maxRecords {
		t.Fatalf("expected %d records after pruning, got %d", maxRecords, len(all))
	}
	// The most recent append must survive; the oldest must be gone.
	if all[0].Started.Before(all[len(all)-1].Started) {
		t.Fatalf("records not newest-first after prune")
	}
}

func TestLastForJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	now := time.Now()
	_ = s.Append(rec("a1", "job-a", StatusSuccess, now.Add(-time.Hour)))
	_ = s.Append(rec("b1", "job-b", StatusSuccess, now.Add(-30*time.Minute)))
	_ = s.Append(rec("a2", "job-a", StatusFailed, now))

	last := s.LastForJob("job-a")
	if last == nil || last.ID != "a2" {
		t.Fatalf("expected newest run for job-a to be a2, got %+v", last)
	}
	if s.LastForJob("job-missing") != nil {
		t.Fatal("expected nil for unknown job")
	}
}

func TestToleratesCorruptLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := New(path)
	_ = s.Append(rec("run-1", "job-a", StatusSuccess, time.Now()))
	// Simulate a torn/partial write appended by a crashing process.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("{not valid json\n")
	_ = f.Close()

	all := s.All()
	if len(all) != 1 || all[0].ID != "run-1" {
		t.Fatalf("corrupt line should be skipped, got %+v", all)
	}
}

func TestConcurrentStoresBothLand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	// Two independent stores (e.g. TUI + cron) appending to the same file.
	if err := New(path).Append(rec("tui-run", "job-a", StatusSuccess, time.Now())); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := New(path).Append(rec("cron-run", "job-a", StatusSuccess, time.Now())); err != nil {
		t.Fatalf("append: %v", err)
	}

	all := New(path).All()
	if len(all) != 2 {
		t.Fatalf("expected both appends to land, got %d: %+v", len(all), all)
	}
}
