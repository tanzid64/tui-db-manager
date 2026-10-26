package restore

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/jobconn"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/pipeline"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type Deps struct {
	Cfg             *config.Config
	Engine          func(name string) (db.Engine, bool)
	Backend         func() storage.Backend
	backend         storage.Backend // test convenience; unused in production path
	ResolvePassword func(config.Job) (string, error)
	// ResolveIdentity resolves the age identity (private key) from the vault
	// key "job/<name>/age_identity". Required when restoring an encrypted backup.
	ResolveIdentity func(config.Job) (string, error)
	// ResolveSSH resolves the job's SSH tunnel secrets. nil falls back to
	// key files, env:/file: refs and ssh-agent only (no vault).
	ResolveSSH func(config.Job) (*db.SSHTunnel, error)
	Log        *logutil.Logger
}

func LatestKey(objs []storage.Object, jobPrefix string) (string, bool) {
	var filtered []storage.Object
	for _, o := range objs {
		if strings.HasPrefix(o.Key, jobPrefix) {
			filtered = append(filtered, o)
		}
	}
	if len(filtered) == 0 {
		return "", false
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ModTime.After(filtered[j].ModTime) })
	return filtered[0].Key, true
}

func Run(ctx context.Context, d Deps, jobName, atKey, toDatabase string, confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("restore not confirmed: re-run with --yes (this overwrites the target database)")
	}
	job, ok := d.Cfg.Job(jobName)
	if !ok {
		return fmt.Errorf("job %q not found", jobName)
	}
	eng, ok := d.Engine(job.Engine)
	if !ok {
		return fmt.Errorf("unknown engine %q", job.Engine)
	}
	be := d.Backend()
	if atKey == "" {
		objs, err := be.List(ctx, job.Name+"/")
		if err != nil {
			return err
		}
		k, ok := LatestKey(objs, job.Name+"/")
		if !ok {
			return fmt.Errorf("no backups found for job %q", job.Name)
		}
		atKey = k
	}
	pw, err := d.ResolvePassword(job)
	if err != nil {
		return err
	}
	if pw != "" {
		d.Log.Redact(pw)
	}
	// --to overrides the restore target. For path-based engines (sqlite) the
	// target is the database file path; for server engines it is the database
	// name. Using the wrong field would silently restore into the source.
	target := job.Conn.Database
	path := job.Conn.Path
	if toDatabase != "" {
		if path != "" {
			path = toDatabase
		} else {
			target = toDatabase
		}
	}
	conn := db.Conn{
		Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
		Password: pw, Database: target, Path: path, TLS: job.Conn.TLS,
	}
	if d.ResolveSSH != nil {
		conn.SSH, err = d.ResolveSSH(job)
	} else {
		conn.SSH, err = jobconn.ResolveSSH(job, nil)
	}
	if err != nil {
		return err
	}
	if conn.SSH != nil && conn.SSH.Password != "" {
		d.Log.Redact(conn.SSH.Password)
	}
	dest := target
	if path != "" {
		dest = path
	}
	d.Log.Infof("restoring %s into %q", atKey, dest)
	rc, err := be.Get(ctx, atKey)
	if err != nil {
		return err
	}
	defer rc.Close()

	// Detect compression/encryption from the key suffix and build the
	// reverse pipeline: decrypt → decompress → engine.Restore.
	_, compressed, encrypted := pipeline.ParseKeySuffix(atKey)

	var r io.Reader = rc
	if encrypted {
		if d.ResolveIdentity == nil {
			return fmt.Errorf("backup %q is encrypted but no identity resolver is configured", atKey)
		}
		identity, err := d.ResolveIdentity(job)
		if err != nil {
			return fmt.Errorf("resolve age identity: %w", err)
		}
		d.Log.Redact(identity)
		dr, err := pipeline.DecryptReader(r, identity)
		if err != nil {
			return fmt.Errorf("decrypt: %w", err)
		}
		r = dr
	}
	if compressed {
		gr, err := pipeline.DecompressReader(r)
		if err != nil {
			return fmt.Errorf("decompress: %w", err)
		}
		defer gr.Close()
		r = gr
	}

	if err := eng.Restore(ctx, conn, r); err != nil {
		return fmt.Errorf("restore: %w", logutil.MaskError(err, pw))
	}
	d.Log.Infof("restore complete")
	return nil
}
