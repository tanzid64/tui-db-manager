package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestLocalPutGetListDelete(t *testing.T) {
	b := NewLocal(t.TempDir())
	ctx := context.Background()
	data := []byte("hello backup")
	if err := b.Put(ctx, "main/2026-10-12.sql", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := b.Get(ctx, "main/2026-10-12.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: %q", got)
	}
	objs, err := b.List(ctx, "main/")
	if err != nil || len(objs) != 1 || objs[0].Key != "main/2026-10-12.sql" {
		t.Fatalf("list mismatch: %+v err %v", objs, err)
	}
	if err := b.Delete(ctx, "main/2026-10-12.sql"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if objs, _ := b.List(ctx, "main/"); len(objs) != 0 {
		t.Fatalf("expected empty after delete, got %+v", objs)
	}
}
