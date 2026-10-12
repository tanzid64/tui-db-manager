package restore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type recordEngine struct{ restoredInto string }

func (e *recordEngine) Name() string                                  { return "fake" }
func (e *recordEngine) DefaultPort() int                              { return 0 }
func (e *recordEngine) Detect() (string, error)                       { return "x", nil }
func (e *recordEngine) Test(context.Context, db.Conn) error           { return nil }
func (e *recordEngine) Dump(context.Context, db.Conn, io.Writer) error { return nil }
func (e *recordEngine) Restore(_ context.Context, c db.Conn, r io.Reader) error {
	io.ReadAll(r)
	e.restoredInto = c.Database
	return nil
}

func TestLatestKey(t *testing.T) {
	objs := []storage.Object{
		{Key: "main/main-20261001-000000.sql", ModTime: time.Unix(1000, 0)},
		{Key: "main/main-20261012-000000.sql", ModTime: time.Unix(2000, 0)},
	}
	got, ok := LatestKey(objs, "main/")
	if !ok || got != "main/main-20261012-000000.sql" {
		t.Fatalf("latest mismatch: %q ok=%v", got, ok)
	}
}

func TestRunRequiresConfirmation(t *testing.T) {
	d := baseDeps(t, &recordEngine{})
	err := Run(context.Background(), d, "main", "main/x.sql", "", false)
	if err == nil {
		t.Fatal("restore must refuse without confirmation")
	}
}

func TestRunHonorsToTarget(t *testing.T) {
	eng := &recordEngine{}
	d := baseDeps(t, eng)
	// seed a backup object
	d.Backend().Put(context.Background(), "main/x.sql", io.NopCloser(stringReader("DATA")), -1)
	if err := Run(context.Background(), d, "main", "main/x.sql", "otherdb", true); err != nil {
		t.Fatalf("run: %v", err)
	}
	if eng.restoredInto != "otherdb" {
		t.Fatalf("expected restore into otherdb, got %q", eng.restoredInto)
	}
}

type stringReader string

func (s stringReader) Read(p []byte) (int, error) { return copy(p, s), io.EOF }

// baseDeps builds Deps around a single in-memory local backend and one job.
func baseDeps(t *testing.T, eng db.Engine) Deps {
	t.Helper()
	be := storage.NewLocal(t.TempDir())
	cfg := &config.Config{
		Version:  1,
		Jobs:     []config.Job{{Name: "main", Engine: "fake", Storage: "local", Conn: config.Conn{Database: "app"}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	return Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return eng, true },
		backend:         be,
		Backend:         func() storage.Backend { return be },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
}
