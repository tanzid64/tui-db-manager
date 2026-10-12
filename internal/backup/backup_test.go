package backup

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type fakeEngine struct{ dumped string }

func (f *fakeEngine) Name() string                            { return "fake" }
func (f *fakeEngine) DefaultPort() int                        { return 0 }
func (f *fakeEngine) Detect() (string, error)                 { return "x", nil }
func (f *fakeEngine) Test(context.Context, db.Conn) error     { return nil }
func (f *fakeEngine) Dump(_ context.Context, _ db.Conn, w io.Writer) error {
	_, err := io.WriteString(w, "DUMPDATA")
	return err
}
func (f *fakeEngine) Restore(context.Context, db.Conn, io.Reader) error { return nil }

func TestBuildKey(t *testing.T) {
	ts := time.Date(2026, 10, 12, 3, 4, 5, 0, time.UTC)
	if got := BuildKey("main", ts, "sql"); got != "main/main-20261012-030405.sql" {
		t.Fatalf("unexpected key: %q", got)
	}
}

func TestRunBackupWritesToBackend(t *testing.T) {
	cfg := &config.Config{
		Version:  1,
		Jobs:     []config.Job{{Name: "main", Engine: "fake", Storage: "local", Conn: config.Conn{Database: "app"}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	be := storage.NewLocal(t.TempDir())
	deps := Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return &fakeEngine{}, true },
		Backend:         func(config.StorageConfig) (storage.Backend, error) { return be, nil },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Now:             func() time.Time { return time.Date(2026, 10, 12, 3, 4, 5, 0, time.UTC) },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
	obj, err := Run(context.Background(), deps, "main")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rc, _ := be.Get(context.Background(), obj.Key)
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, []byte("DUMPDATA")) {
		t.Fatalf("backend content mismatch: %q", got)
	}
}
