package db

import (
	"context"
	"fmt"
	"io"
)

type sqliteEngine struct{}

func init() { Register(sqliteEngine{}) }

func (sqliteEngine) Name() string     { return "sqlite" }
func (sqliteEngine) DefaultPort() int { return 0 }

func (sqliteEngine) Detect() (string, error) { return lookTool("sqlite3") }

func (e sqliteEngine) Test(ctx context.Context, c Conn) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	if c.Path == "" {
		return fmt.Errorf("sqlite: conn.path (database file) is required")
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: []string{c.Path, "SELECT 1;"}})
}

func (e sqliteEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	if c.Path == "" {
		return fmt.Errorf("sqlite: conn.path (database file) is required")
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: []string{c.Path, ".dump"}, Stdout: w})
}

func (e sqliteEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	if c.Path == "" {
		return fmt.Errorf("sqlite: conn.path (database file) is required")
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: []string{c.Path}, Stdin: r})
}

func (e sqliteEngine) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	return nil, nil
}

