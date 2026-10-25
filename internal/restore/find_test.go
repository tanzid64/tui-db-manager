package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

// A job that moved from "old" to "new" storage still sees the old backups.
func TestFindBackupsAcrossStorages(t *testing.T) {
	oldDir, newDir := t.TempDir(), t.TempDir()
	ctx := context.Background()
	put := func(dir, key string, mod time.Time) {
		be := storage.NewLocal(dir)
		if err := be.Put(ctx, key, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
		_ = chtimes(dir, key, mod)
	}
	now := time.Now()
	put(oldDir, "shop/shop-1.sql.gz", now.Add(-48*time.Hour))
	put(oldDir, "other/other-1.sql.gz", now) // another job: must be ignored
	put(newDir, "shop/shop-2.sql.gz", now.Add(-time.Hour))

	cfg := &config.Config{
		Jobs: []config.Job{{Name: "shop", Storage: "new"}},
		Storages: []config.StorageConfig{
			{Name: "old", Type: "local", Path: oldDir},
			{Name: "new", Type: "local", Path: newDir},
			{Name: "broken", Type: "s3"},
		},
	}
	found, errs := FindBackups(ctx, cfg, "shop", func(sc config.StorageConfig) (storage.Backend, error) {
		if sc.Type != "local" {
			return nil, errors.New("offline")
		}
		return storage.NewLocal(sc.Path), nil
	})
	if len(found) != 2 {
		t.Fatalf("expected 2 backups, got %+v", found)
	}
	if found[0].Storage != "new" || found[0].Key != "shop/shop-2.sql.gz" {
		t.Fatalf("expected newest first from new storage, got %+v", found[0])
	}
	if found[1].Storage != "old" || found[1].Key != "shop/shop-1.sql.gz" {
		t.Fatalf("expected old-storage backup listed, got %+v", found[1])
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `"broken"`) {
		t.Fatalf("expected one warning for the broken storage, got %v", errs)
	}

	if b, ok := Locate(found, "shop/shop-1.sql.gz", ""); !ok || b.Storage != "old" {
		t.Fatalf("Locate by key should find old storage, got %+v %v", b, ok)
	}
	if b, ok := Locate(found, "", ""); !ok || b.Key != "shop/shop-2.sql.gz" {
		t.Fatalf("Locate latest wrong: %+v", b)
	}
	if b, ok := Locate(found, "", "old"); !ok || b.Key != "shop/shop-1.sql.gz" {
		t.Fatalf("Locate latest in storage wrong: %+v", b)
	}
}

func chtimes(dir, key string, t time.Time) error {
	return os.Chtimes(filepath.Join(dir, filepath.FromSlash(key)), t, t)
}
