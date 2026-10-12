package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Config{
		Version: 1,
		Jobs: []Job{{
			Name:    "main",
			Engine:  "mysql",
			Conn:    Conn{Host: "127.0.0.1", Port: 3306, User: "root", Database: "app"},
			Storage: "local",
			Retention: Retention{KeepLast: 7},
		}},
		Storages: []StorageConfig{{Name: "local", Type: "local", Path: "/var/backups/dbk"}},
	}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600, got %o", perm)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	j, ok := got.Job("main")
	if !ok || j.Engine != "mysql" || j.Conn.Port != 3306 {
		t.Fatalf("job round-trip mismatch: %+v ok=%v", j, ok)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("expected empty config v1, got %+v", got)
	}
}
