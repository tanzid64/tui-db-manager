package db

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

type postgresEngine struct{}

func init() { Register(postgresEngine{}) }

func (postgresEngine) Name() string     { return "postgres" }
func (postgresEngine) DefaultPort() int { return 5432 }

func (postgresEngine) Detect() (string, error) { return lookTool("pg_dump") }

func PgEnv(c Conn) []string {
	env := []string{}
	if c.Password != "" {
		env = append(env, "PGPASSWORD="+c.Password)
	}
	if c.TLS {
		env = append(env, "PGSSLMODE=require")
	}
	return env
}

// pgConnArgs returns host/port/user flags, filling in defaults for blanks.
func pgConnArgs(c Conn) []string {
	c = withDefaults(c, 5432, "postgres")
	return []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--username=%s", c.User),
		"--no-password",
	}
}

func BuildPgDumpArgs(c Conn) []string {
	return append(pgConnArgs(c), "--format=custom", c.Database)
}

func (e postgresEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("psql")
	if err != nil {
		return err
	}
	args := append(pgConnArgs(c), "-c", "SELECT 1", c.Database)
	return runCmd(ctx, cmdSpec{Path: tool, Env: PgEnv(c), Args: args})
}

func (e postgresEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: BuildPgDumpArgs(c), Env: PgEnv(c), Stdout: w})
}

func (e postgresEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := lookTool("pg_restore")
	if err != nil {
		return err
	}
	// --no-owner/--no-acl let a backup restore cleanly as a different user
	// (e.g. moving between servers or restoring on shared hosting).
	args := append(pgConnArgs(c), "--clean", "--if-exists", "--no-owner", "--no-acl",
		fmt.Sprintf("--dbname=%s", c.Database))
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Env: PgEnv(c), Stdin: r})
}

func (e postgresEngine) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	tool, err := lookTool("psql")
	if err != nil {
		return nil, err
	}
	runQuery := func(dbName string) ([]string, error) {
		var out bytes.Buffer
		args := append(pgConnArgs(c),
			"-d", dbName,
			"-t", "-A",
			"-c", "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname;",
		)
		err := runCmd(ctx, cmdSpec{
			Path:   tool,
			Args:   args,
			Env:    PgEnv(c),
			Stdout: &out,
		})
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		var dbs []string
		for _, l := range lines {
			name := strings.TrimSpace(l)
			if name != "" {
				dbs = append(dbs, name)
			}
		}
		return dbs, nil
	}

	dbTarget := c.Database
	if dbTarget == "" {
		dbTarget = "postgres"
	}
	dbs, err := runQuery(dbTarget)
	if err != nil && dbTarget == "postgres" {
		if dbs2, err2 := runQuery("template1"); err2 == nil {
			return dbs2, nil
		}
	}
	return dbs, err
}
