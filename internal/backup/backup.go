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
	"github.com/tanzid64/tui-db-manager/internal/pipeline"
	"github.com/tanzid64/tui-db-manager/internal/retention"
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
	// ResolveIdentity resolves the age identity (private key) for encryption
	// jobs. Only needed when the job has Encrypt: true. The recipient public key
	// lives in config; the identity lives in the vault under
	// "job/<name>/age_identity". Backup itself only needs the recipient, but
	// ResolveIdentity is part of Deps so restore can share the same wiring.
	ResolveIdentity func(config.Job) (string, error)
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

	if job.Encrypt && job.EncryptRecipient == "" {
		return storage.Object{}, fmt.Errorf("job %q has encrypt: true but no encrypt_recipient", jobName)
	}

	key := BuildKey(job.Name, d.Now(), extFor(job.Engine)) + pipeline.KeySuffix(job.Compress, job.Encrypt)
	d.Log.Infof("starting backup of job %q -> %s", job.Name, key)

	pr, pw2 := io.Pipe()
	dumpErrCh := make(chan error, 1)
	go func() {
		// Build the writer chain in reverse pipeline order so the dump
		// goroutine writes to the innermost (compress) and data flows
		// outward through encrypt into the pipe that feeds Put.
		//
		// Chain: dump → [compress →] [encrypt →] pipe writer
		var w io.Writer = pw2
		var closers []io.Closer

		if job.Encrypt {
			ew, encErr := pipeline.EncryptWriter(pw2, job.EncryptRecipient)
			if encErr != nil {
				pw2.CloseWithError(encErr)
				dumpErrCh <- encErr
				return
			}
			w = ew
			closers = append(closers, ew)
		}

		if job.Compress {
			gw := pipeline.CompressWriter(w)
			w = gw
			closers = append(closers, gw)
		}

		dumpErr := eng.Dump(ctx, conn, w)

		// Close pipeline stages in reverse order (compress first, then
		// encrypt) so each flushes into the next before the downstream
		// writer is finalized.
		for i := len(closers) - 1; i >= 0; i-- {
			if cerr := closers[i].Close(); cerr != nil && dumpErr == nil {
				dumpErr = cerr
			}
		}

		// Propagate a dump failure to the reader so Put fails rather than
		// storing a truncated backup.
		pw2.CloseWithError(dumpErr)
		dumpErrCh <- dumpErr
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

	if n := job.Retention.KeepLast; n > 0 {
		deleted, rerr := retention.Apply(ctx, be, job.Name+"/", n)
		if rerr != nil {
			// Retention failure must not fail an otherwise-good backup; log and continue.
			d.Log.Errorf("retention for job %q: %v", job.Name, logutil.MaskError(rerr, pw))
		} else if len(deleted) > 0 {
			d.Log.Infof("retention: pruned %d old backup(s) for job %q", len(deleted), job.Name)
		}
	}
	return obj, nil
}
