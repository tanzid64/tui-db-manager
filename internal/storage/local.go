package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Local struct{ root string }

func NewLocal(root string) *Local { return &Local{root: root} }

func (l *Local) Name() string { return "local" }

func (l *Local) full(key string) string { return filepath.Join(l.root, filepath.FromSlash(key)) }

func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	dst := l.full(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return os.Open(l.full(key))
}

func (l *Local) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	err := filepath.Walk(l.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(l.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, prefix) {
			out = append(out, Object{Key: key, Size: info.Size(), ModTime: info.ModTime()})
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

func (l *Local) Delete(ctx context.Context, key string) error {
	return os.Remove(l.full(key))
}

func (l *Local) Stat(ctx context.Context, key string) (Object, error) {
	info, err := os.Stat(l.full(key))
	if err != nil {
		return Object{}, err
	}
	return Object{Key: key, Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (l *Local) Test(ctx context.Context) error {
	return os.MkdirAll(l.root, 0o700)
}
