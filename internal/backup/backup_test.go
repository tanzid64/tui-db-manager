package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

// errEngine's Dump fails with a configurable error after an optional partial write.
type errEngine struct{ dumpErr error }

func (e *errEngine) Name() string                            { return "err" }
func (e *errEngine) DefaultPort() int                        { return 0 }
func (e *errEngine) Detect() (string, error)                 { return "x", nil }
func (e *errEngine) Test(context.Context, db.Conn) error     { return nil }
func (e *errEngine) Dump(_ context.Context, _ db.Conn, w io.Writer) error {
	io.WriteString(w, "partial")
	return e.dumpErr
}
func (e *errEngine) Restore(context.Context, db.Conn, io.Reader) error { return nil }

// failBackend's Put fails immediately without reading the pipe (e.g. mkdir error).
type failBackend struct{ err error }

func (b *failBackend) Name() string                                        { return "fail" }
func (b *failBackend) Put(context.Context, string, io.Reader, int64) error { return b.err }
func (b *failBackend) Get(context.Context, string) (io.ReadCloser, error)  { return nil, b.err }
func (b *failBackend) List(context.Context, string) ([]storage.Object, error) {
	return nil, b.err
}
func (b *failBackend) Delete(context.Context, string) error            { return b.err }
func (b *failBackend) Stat(context.Context, string) (storage.Object, error) {
	return storage.Object{}, b.err
}
func (b *failBackend) Test(context.Context) error { return b.err }

func depsWith(eng db.Engine, be storage.Backend, pw string) Deps {
	cfg := &config.Config{
		Version:  1,
		Jobs:     []config.Job{{Name: "main", Engine: eng.Name(), Storage: "local", Conn: config.Conn{Database: "app"}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	return Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return eng, true },
		Backend:         func(config.StorageConfig) (storage.Backend, error) { return be, nil },
		ResolvePassword: func(config.Job) (string, error) { return pw, nil },
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
}

func TestRunSurfacesDumpErrorDistinctly(t *testing.T) {
	be := storage.NewLocal(t.TempDir())
	d := depsWith(&errEngine{dumpErr: errors.New("mysqldump: access denied")}, be, "")
	_, err := Run(context.Background(), d, "main")
	if err == nil {
		t.Fatal("expected error from failed dump")
	}
	if !strings.Contains(err.Error(), "dump:") || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("dump error should be labeled 'dump:', got %v", err)
	}
	if strings.Contains(err.Error(), "upload:") {
		t.Fatalf("dump failure must not be mislabeled as upload: %v", err)
	}
}

func TestRunMasksSecretInDumpError(t *testing.T) {
	be := storage.NewLocal(t.TempDir())
	d := depsWith(&errEngine{dumpErr: errors.New("login failed with password supers3cret")}, be, "supers3cret")
	_, err := Run(context.Background(), d, "main")
	if err == nil || strings.Contains(err.Error(), "supers3cret") {
		t.Fatalf("password must be scrubbed from the returned error, got %v", err)
	}
}

func TestRunReportsUploadErrorAndDoesNotHang(t *testing.T) {
	// failBackend.Put returns immediately without draining the pipe; Run must
	// return the upload error (not a dump/pipe artifact) and not deadlock.
	d := depsWith(&fakeEngine{}, &failBackend{err: errors.New("mkdir: permission denied")}, "")
	done := make(chan error, 1)
	go func() { _, e := Run(context.Background(), d, "main"); done <- e }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "upload:") {
			t.Fatalf("expected an upload error, got %v", err)
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("expected the backend's cause, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run hung: dump goroutine likely blocked on a full pipe")
	}
}

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

func TestRunAppliesRetention(t *testing.T) {
	be := storage.NewLocal(t.TempDir())
	ctx := context.Background()
	be.Put(ctx, "main/main-20250101-000000.sql", readerOfB("old1"), 4)
	be.Put(ctx, "main/main-20250102-000000.sql", readerOfB("old2"), 4)

	cfg := &config.Config{
		Version: 1,
		Jobs: []config.Job{{Name: "main", Engine: "fake", Storage: "local",
			Conn: config.Conn{Database: "app"}, Retention: config.Retention{KeepLast: 2}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	d := Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return &fakeEngine{}, true },
		Backend:         func(config.StorageConfig) (storage.Backend, error) { return be, nil },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Now:             func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
	if _, err := Run(ctx, d, "main"); err != nil {
		t.Fatalf("run: %v", err)
	}
	objs, _ := be.List(ctx, "main/")
	if len(objs) != 2 {
		t.Fatalf("expected 2 backups after retention, got %d: %+v", len(objs), objs)
	}
}

func readerOfB(s string) io.Reader { return bytes.NewReader([]byte(s)) }

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
