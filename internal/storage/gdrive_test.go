package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"
)

// fakeDrive is an in-memory driveAPI that models Drive's real behavior: file
// names are NOT unique — every Create appends a new file with a fresh id.
type fakeDrive struct {
	meta map[string]driveFile // id -> metadata
	data map[string][]byte    // id -> contents
	seq  int
}

func newFakeDrive() *fakeDrive {
	return &fakeDrive{meta: map[string]driveFile{}, data: map[string][]byte{}}
}

func (f *fakeDrive) Create(ctx context.Context, name, parent string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.seq++
	id := fmt.Sprintf("id%d", f.seq)
	f.meta[id] = driveFile{ID: id, Name: name, Size: int64(len(b)), ModTime: time.Now()}
	f.data[id] = b
	return nil
}
func (f *fakeDrive) List(ctx context.Context, parent string) ([]driveFile, error) {
	out := make([]driveFile, 0, len(f.meta))
	for _, m := range f.meta {
		out = append(out, m)
	}
	return out, nil
}
func (f *fakeDrive) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	b, ok := f.data[id]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (f *fakeDrive) Delete(ctx context.Context, id string) error {
	delete(f.meta, id)
	delete(f.data, id)
	return nil
}
func (f *fakeDrive) Ping(ctx context.Context, parent string) error { return nil }

func TestGDrivePutGetListDeleteByKey(t *testing.T) {
	g := NewGDriveWith(newFakeDrive(), "folder123")
	ctx := context.Background()
	if g.Name() != "gdrive" {
		t.Fatalf("name: %q", g.Name())
	}
	data := []byte("dump-bytes")
	if err := g.Put(ctx, "main/main-20261012-030405.sql", bytes.NewReader(data), -1); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := g.Get(ctx, "main/main-20261012-030405.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: %q", got)
	}
	obj, err := g.Stat(ctx, "main/main-20261012-030405.sql")
	if err != nil || obj.Key != "main/main-20261012-030405.sql" || obj.Size != int64(len(data)) {
		t.Fatalf("stat: %+v err %v", obj, err)
	}
	if err := g.Delete(ctx, "main/main-20261012-030405.sql"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := g.Get(ctx, "main/main-20261012-030405.sql"); err == nil {
		t.Fatal("expected error getting a deleted key")
	}
}

func TestGDrivePutReplacesSameKey(t *testing.T) {
	fd := newFakeDrive()
	g := NewGDriveWith(fd, "f")
	ctx := context.Background()
	if err := g.Put(ctx, "main/x.sql", bytes.NewReader([]byte("v1")), -1); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if err := g.Put(ctx, "main/x.sql", bytes.NewReader([]byte("v2-newer")), -1); err != nil {
		t.Fatalf("put v2: %v", err)
	}
	// Exactly one object for the key — no duplicate Drive files.
	objs, _ := g.List(ctx, "main/")
	if len(objs) != 1 {
		t.Fatalf("a repeated key must leave exactly one object, got %d: %+v", len(objs), objs)
	}
	// Get returns the latest content.
	rc, err := g.Get(ctx, "main/x.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "v2-newer" {
		t.Fatalf("expected the latest content, got %q", got)
	}
}

func TestGDriveListIsPrefixScoped(t *testing.T) {
	g := NewGDriveWith(newFakeDrive(), "f")
	ctx := context.Background()
	g.Put(ctx, "main/a.sql", bytes.NewReader([]byte("1")), -1)
	g.Put(ctx, "main/b.sql", bytes.NewReader([]byte("2")), -1)
	g.Put(ctx, "other/c.sql", bytes.NewReader([]byte("3")), -1)
	objs, err := g.List(ctx, "main/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects under main/, got %d: %+v", len(objs), objs)
	}
	for _, o := range objs {
		if o.Key != "main/a.sql" && o.Key != "main/b.sql" {
			t.Fatalf("list leaked a non-main key: %q", o.Key)
		}
	}
}
