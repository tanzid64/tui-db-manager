package db

import (
	"context"
	"fmt"
	"io"
	"os"
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
	// Restore into a fresh temp file and swap it in, so the .dump does not
	// collide with tables that already exist in the target file.
	tmp := c.Path + ".dbk-restore"
	_ = os.Remove(tmp)
	if err := runCmd(ctx, cmdSpec{Path: tool, Args: []string{"-bail", tmp}, Stdin: r}); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, c.Path)
}

func (e sqliteEngine) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	return nil, nil
}
