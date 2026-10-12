package restore

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type Deps struct {
	Cfg             *config.Config
	Engine          func(name string) (db.Engine, bool)
	Backend         func() storage.Backend
	backend         storage.Backend // test convenience; unused in production path
	ResolvePassword func(config.Job) (string, error)
	Log             *logutil.Logger
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
	if err := eng.Restore(ctx, conn, rc); err != nil {
		return fmt.Errorf("restore: %w", logutil.MaskError(err, pw))
	}
	d.Log.Infof("restore complete")
	return nil
}
