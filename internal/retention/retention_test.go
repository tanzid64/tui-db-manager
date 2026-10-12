package retention

import (
	"context"
	"io"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/storage"
)

func seed(t *testing.T) *storage.Local {
	t.Helper()
	be := storage.NewLocal(t.TempDir())
	ctx := context.Background()
	put := func(key string) {
		if err := be.Put(ctx, key, readerOf("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	put("main/main-20260101-000000.sql")
	put("main/main-20260102-000000.sql")
	put("main/main-20260103-000000.sql")
	put("other/other-20260103-000000.sql")
	return be
}

func TestApplyKeepsNewestNAndIgnoresOtherJobs(t *testing.T) {
	be := seed(t)
	ctx := context.Background()
	deleted, err := Apply(ctx, be, "main/", 2)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "main/main-20260101-000000.sql" {
		t.Fatalf("expected to delete only the oldest main backup, deleted=%v", deleted)
	}
	if objs, _ := be.List(ctx, "other/"); len(objs) != 1 {
		t.Fatalf("retention must not touch other jobs: %+v", objs)
	}
	if objs, _ := be.List(ctx, "main/"); len(objs) != 2 {
		t.Fatalf("expected 2 main backups kept, got %d", len(objs))
	}
}

func TestApplyKeepLastZeroDeletesNothing(t *testing.T) {
	be := seed(t)
	deleted, err := Apply(context.Background(), be, "main/", 0)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("keep_last=0 must delete nothing, deleted=%v", deleted)
	}
}

func readerOf(s string) io.Reader { return &sliceReader{data: []byte(s)} }

type sliceReader struct {
	data []byte
	off  int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
