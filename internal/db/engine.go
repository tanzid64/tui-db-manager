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

// DatabaseLister is an optional interface implemented by engines that can list available databases on a server.
type DatabaseLister interface {
	ListDatabases(ctx context.Context, c Conn) ([]string, error)
}

// withDefaults fills empty connection fields with sensible defaults so a job
// saved with blanks still connects to a local server.
func withDefaults(c Conn, port int, user string) Conn {
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port <= 0 {
		c.Port = port
	}
	if c.User == "" {
		c.User = user
	}
	return c
}
