package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

// errPutAbort is injected into the dump writer when the storage backend fails
// before draining the pipe, so the dump goroutine unblocks and we can tell a
// storage failure apart from a genuine dump failure.
var errPutAbort = errors.New("backup aborted: storage write failed")

type Deps struct {
	Cfg             *config.Config
	Engine          func(name string) (db.Engine, bool)
	Backend         func(config.StorageConfig) (storage.Backend, error)
	ResolvePassword func(config.Job) (string, error)
	Now             func() time.Time
	Log             *logutil.Logger
}

func extFor(engine string) string {
	switch engine {
	case "postgres":
		return "pgdump"
	case "mongodb":
		return "archive"
	default:
		return "sql"
	}
}

func BuildKey(job string, t time.Time, ext string) string {
	return fmt.Sprintf("%s/%s-%s.%s", job, job, t.UTC().Format("20060102-150405"), ext)
}

func Run(ctx context.Context, d Deps, jobName string) (storage.Object, error) {
	job, ok := d.Cfg.Job(jobName)
	if !ok {
		return storage.Object{}, fmt.Errorf("job %q not found", jobName)
	}
	eng, ok := d.Engine(job.Engine)
	if !ok {
		return storage.Object{}, fmt.Errorf("unknown engine %q", job.Engine)
	}
	sc, ok := d.Cfg.Storage(job.Storage)
	if !ok {
		return storage.Object{}, fmt.Errorf("storage %q not found", job.Storage)
	}
	be, err := d.Backend(sc)
	if err != nil {
		return storage.Object{}, err
	}
	pw, err := d.ResolvePassword(job)
	if err != nil {
		return storage.Object{}, err
	}
	if pw != "" {
		d.Log.Redact(pw)
	}
	conn := db.Conn{
		Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
		Password: pw, Database: job.Conn.Database, Path: job.Conn.Path, TLS: job.Conn.TLS,
	}

	key := BuildKey(job.Name, d.Now(), extFor(job.Engine))
	d.Log.Infof("starting backup of job %q -> %s", job.Name, key)

	pr, pw2 := io.Pipe()
	dumpErrCh := make(chan error, 1)
	go func() {
		err := eng.Dump(ctx, conn, pw2)
		// Propagate a dump failure to the reader so Put fails rather than
		// storing a truncated backup.
		pw2.CloseWithError(err)
		dumpErrCh <- err
	}()

	putErr := be.Put(ctx, key, pr, -1)
	if putErr != nil {
		// Put failed on its own (e.g. mkdir/write). Unblock the dump goroutine
		// with a distinct sentinel so we don't misreport a storage failure as a
		// dump failure, and so the goroutine (and its subprocess) can exit.
		pr.CloseWithError(errPutAbort)
	}
	dumpErr := <-dumpErrCh

	switch {
	case putErr != nil && errors.Is(dumpErr, errPutAbort):
		// Storage failed first; the dump was aborted as a consequence.
		return storage.Object{}, fmt.Errorf("upload: %w", logutil.MaskError(putErr, pw))
	case dumpErr != nil:
		// The dump itself failed (its error also surfaced to Put as the read error).
		return storage.Object{}, fmt.Errorf("dump: %w", logutil.MaskError(dumpErr, pw))
	case putErr != nil:
		return storage.Object{}, fmt.Errorf("upload: %w", logutil.MaskError(putErr, pw))
	}

	obj, err := be.Stat(ctx, key)
	if err != nil {
		return storage.Object{}, logutil.MaskError(err, pw)
	}
	d.Log.Infof("backup complete: %s (%d bytes)", key, obj.Size)
	return obj, nil
}
