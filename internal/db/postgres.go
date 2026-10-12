package db

import (
	"context"
	"fmt"
	"io"
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
