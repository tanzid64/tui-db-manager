package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// fakeDrive is an in-memory driveAPI: file name -> contents (one parent folder).
type fakeDrive struct {
	files  map[string][]byte
	times  map[string]time.Time
	nextID int
	ids    map[string]string // name -> id
}

func newFakeDrive() *fakeDrive {
	return &fakeDrive{files: map[string][]byte{}, times: map[string]time.Time{}, ids: map[string]string{}}
}

func (f *fakeDrive) Create(ctx context.Context, name, parent string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.files[name] = b
	f.times[name] = time.Now()
	f.nextID++
	f.ids[name] = string(rune('A' + f.nextID))
	return nil
}
func (f *fakeDrive) List(ctx context.Context, parent string) ([]driveFile, error) {
	var out []driveFile
	for name, b := range f.files {
		out = append(out, driveFile{ID: f.ids[name], Name: name, Size: int64(len(b)), ModTime: f.times[name]})
	}
	return out, nil
}
func (f *fakeDrive) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	for name, fid := range f.ids {
		if fid == id {
			return io.NopCloser(bytes.NewReader(f.files[name])), nil
		}
	}
	return nil, io.EOF
}
func (f *fakeDrive) Delete(ctx context.Context, id string) error {
	for name, fid := range f.ids {
		if fid == id {
			delete(f.files, name)
			delete(f.ids, name)
			delete(f.times, name)
			return nil
		}
	}
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
