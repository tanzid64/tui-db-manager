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

func BuildPgDumpArgs(c Conn) []string {
	return []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--username=%s", c.User),
		"--no-password", "--format=custom", c.Database,
	}
}

func (e postgresEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("psql")
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{Path: tool, Env: PgEnv(c),
		Args: []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
			fmt.Sprintf("--username=%s", c.User), "--no-password", "-c", "SELECT 1", c.Database}})
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
	args := []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--username=%s", c.User), "--no-password", "--clean", "--if-exists",
		fmt.Sprintf("--dbname=%s", c.Database)}
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Env: PgEnv(c), Stdin: r})
}

func (e postgresEngine) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	tool, err := lookTool("psql")
	if err != nil {
		return nil, err
	}
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := c.Port
	if port <= 0 {
		port = 5432
	}
	user := c.User
	if user == "" {
		user = "postgres"
	}

	runQuery := func(dbName string) ([]string, error) {
		var out bytes.Buffer
		args := []string{
			fmt.Sprintf("--host=%s", host),
			fmt.Sprintf("--port=%d", port),
			fmt.Sprintf("--username=%s", user),
			"--no-password",
			"-d", dbName,
			"-t", "-A",
			"-c", "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname;",
		}
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
