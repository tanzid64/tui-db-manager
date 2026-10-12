package db

import (
	"context"
	"io"
)

// Conn is a resolved connection; Password is already pulled from the vault/refs.
type Conn struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	Path     string // sqlite
	TLS      bool
}

type Job struct {
	Name     string
	Database string
	Conn     Conn
}

type Engine interface {
	Name() string
	DefaultPort() int
	Detect() (clientPath string, err error)
	Test(ctx context.Context, c Conn) error
	Dump(ctx context.Context, c Conn, w io.Writer) error
	Restore(ctx context.Context, c Conn, r io.Reader) error
}
