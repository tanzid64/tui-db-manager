package storage

import (
	"context"
	"io"
	"time"
)

type Object struct {
	Key     string
	Size    int64
	ModTime time.Time
}

type Backend interface {
	Name() string
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (Object, error)
	Test(ctx context.Context) error
}
