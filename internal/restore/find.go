package restore

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

// Backup is a backup file found in one of the configured storages.
type Backup struct {
	Storage string // storage name the file lives in
	storage.Object
}

// BackendFor builds the backend for a storage config.
type BackendFor func(config.StorageConfig) (storage.Backend, error)

// perStorageTimeout bounds how long one slow or unreachable storage can
// delay the whole list.
const perStorageTimeout = 30 * time.Second

// FindBackups lists a job's backups across every configured storage, not
// just the job's current one, so backups made before the job was moved to
// another storage (e.g. local -> S3) can still be restored.
//
// Results are sorted newest first. A storage that cannot be read does not
// hide the others: its error is returned in errs while the remaining
// backups are still listed.
func FindBackups(ctx context.Context, cfg *config.Config, jobName string, backend BackendFor) (found []Backup, errs []error) {
	job, ok := cfg.Job(jobName)
	if !ok {
		return nil, []error{fmt.Errorf("job %q not found", jobName)}
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, sc := range cfg.Storages {
		wg.Add(1)
		go func(sc config.StorageConfig) {
			defer wg.Done()
			objs, err := listOne(ctx, sc, job.Name+"/", backend)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("storage %q: %w", sc.Name, err))
				return
			}
			for _, o := range objs {
				found = append(found, Backup{Storage: sc.Name, Object: o})
			}
		}(sc)
	}
	wg.Wait()

	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.After(b.ModTime)
		}
		if a.Key != b.Key {
			return a.Key > b.Key
		}
		return a.Storage == job.Storage && b.Storage != job.Storage
	})
	return found, errs
}

func listOne(ctx context.Context, sc config.StorageConfig, prefix string, backend BackendFor) ([]storage.Object, error) {
	ctx, cancel := context.WithTimeout(ctx, perStorageTimeout)
	defer cancel()
	be, err := backend(sc)
	if err != nil {
		return nil, err
	}
	return be.List(ctx, prefix)
}

// Locate returns the backup to restore. With a key, it finds which storage
// holds that key (preferring storageName when given); without a key, it picks
// the newest backup, limited to storageName when given.
func Locate(found []Backup, key, storageName string) (Backup, bool) {
	for _, b := range found {
		if storageName != "" && b.Storage != storageName {
			continue
		}
		if key == "" || b.Key == key {
			return b, true
		}
	}
	return Backup{}, false
}
