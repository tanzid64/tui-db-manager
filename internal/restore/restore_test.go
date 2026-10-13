package restore

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type recordEngine struct {
	restoredInto string
	restoredPath string
	restoreErr   error
	restoredData []byte
}

func (e *recordEngine) Name() string                                   { return "fake" }
func (e *recordEngine) DefaultPort() int                               { return 0 }
func (e *recordEngine) Detect() (string, error)                        { return "x", nil }
func (e *recordEngine) Test(context.Context, db.Conn) error            { return nil }
func (e *recordEngine) Dump(context.Context, db.Conn, io.Writer) error { return nil }
func (e *recordEngine) Restore(_ context.Context, c db.Conn, r io.Reader) error {
	e.restoredData, _ = io.ReadAll(r)
	e.restoredInto = c.Database
	e.restoredPath = c.Path
	return e.restoreErr
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

func TestRunToTargetOverridesPathForSQLite(t *testing.T) {
	eng := &recordEngine{}
	be := storage.NewLocal(t.TempDir())
	cfg := &config.Config{
		Version:  1,
		Jobs:     []config.Job{{Name: "main", Engine: "sqlite", Storage: "local", Conn: config.Conn{Path: "/data/app.db"}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	d := Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return eng, true },
		Backend:         func() storage.Backend { return be },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
	be.Put(context.Background(), "main/x.sql", io.NopCloser(stringReader("DATA")), -1)
	if err := Run(context.Background(), d, "main", "main/x.sql", "/tmp/restored.db", true); err != nil {
		t.Fatalf("run: %v", err)
	}
	if eng.restoredPath != "/tmp/restored.db" {
		t.Fatalf("for a path-based engine, --to must override Conn.Path, got %q", eng.restoredPath)
	}
	if eng.restoredInto != "" {
		t.Fatalf("Conn.Database should remain empty for sqlite, got %q", eng.restoredInto)
	}
}

func TestRunMasksSecretInRestoreError(t *testing.T) {
	eng := &recordEngine{restoreErr: errors.New("restore failed for password topsecret9")}
	d := baseDeps(t, eng)
	d.ResolvePassword = func(config.Job) (string, error) { return "topsecret9", nil }
	d.Backend().Put(context.Background(), "main/x.sql", io.NopCloser(stringReader("DATA")), -1)
	err := Run(context.Background(), d, "main", "main/x.sql", "", true)
	if err == nil || strings.Contains(err.Error(), "topsecret9") {
		t.Fatalf("password must be scrubbed from restore error, got %v", err)
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

func TestRunRestoreCompressedBackup(t *testing.T) {
	eng := &recordEngine{}
	d := baseDeps(t, eng)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("COMPRESSED_SQL_DATA"))
	gw.Close()
	d.Backend().Put(context.Background(), "main/main-20261012-000000.sql.gz", bytes.NewReader(buf.Bytes()), int64(buf.Len()))

	if err := Run(context.Background(), d, "main", "main/main-20261012-000000.sql.gz", "", true); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if string(eng.restoredData) != "COMPRESSED_SQL_DATA" {
		t.Fatalf("unexpected restored data: got %q, want %q", string(eng.restoredData), "COMPRESSED_SQL_DATA")
	}
}

func TestRunRestoreEncryptedBackup(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	eng := &recordEngine{}
	d := baseDeps(t, eng)
	d.ResolveIdentity = func(config.Job) (string, error) { return id.String(), nil }

	var buf bytes.Buffer
	ew, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	ew.Write([]byte("ENCRYPTED_SQL_DATA"))
	ew.Close()
	d.Backend().Put(context.Background(), "main/main-20261012-000000.sql.age", bytes.NewReader(buf.Bytes()), int64(buf.Len()))

	if err := Run(context.Background(), d, "main", "main/main-20261012-000000.sql.age", "", true); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if string(eng.restoredData) != "ENCRYPTED_SQL_DATA" {
		t.Fatalf("unexpected restored data: got %q, want %q", string(eng.restoredData), "ENCRYPTED_SQL_DATA")
	}
}

func TestRunRestoreCompressedAndEncryptedBackup(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	eng := &recordEngine{}
	d := baseDeps(t, eng)
	d.ResolveIdentity = func(config.Job) (string, error) { return id.String(), nil }

	// Write compressed then encrypted (matches backup order).
	var buf bytes.Buffer
	ew, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(ew)
	gw.Write([]byte("COMPRESSED_AND_ENCRYPTED_SQL_DATA"))
	gw.Close()
	ew.Close()
	d.Backend().Put(context.Background(), "main/main-20261012-000000.sql.gz.age", bytes.NewReader(buf.Bytes()), int64(buf.Len()))

	if err := Run(context.Background(), d, "main", "main/main-20261012-000000.sql.gz.age", "", true); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if string(eng.restoredData) != "COMPRESSED_AND_ENCRYPTED_SQL_DATA" {
		t.Fatalf("unexpected restored data: got %q, want %q", string(eng.restoredData), "COMPRESSED_AND_ENCRYPTED_SQL_DATA")
	}
}

func TestRunRestoreEncryptedBackupMissingIdentityFails(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	eng := &recordEngine{}
	d := baseDeps(t, eng)
	// d.ResolveIdentity is nil by default in baseDeps

	var buf bytes.Buffer
	ew, err := age.Encrypt(&buf, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	ew.Write([]byte("SECRET_DATA"))
	ew.Close()
	d.Backend().Put(context.Background(), "main/main-20261012-000000.sql.age", bytes.NewReader(buf.Bytes()), int64(buf.Len()))

	err = Run(context.Background(), d, "main", "main/main-20261012-000000.sql.age", "", true)
	if err == nil {
		t.Fatal("expected error when restoring encrypted backup without an identity resolver")
	}
}
