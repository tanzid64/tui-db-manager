package db

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

type mysqlEngine struct{}

func init() { Register(mysqlEngine{}) }

func (mysqlEngine) Name() string     { return "mysql" }
func (mysqlEngine) DefaultPort() int { return 3306 }

func (mysqlEngine) Detect() (string, error) {
	return lookTool("mysqldump", "mariadb-dump")
}

func MySQLEnv(c Conn) []string {
	if c.Password == "" {
		return nil
	}
	return []string{"MYSQL_PWD=" + c.Password}
}

// mysqlConnArgs returns the host/port/user/TLS flags shared by every MySQL
// client invocation, filling in defaults for empty fields.
func mysqlConnArgs(c Conn) []string {
	c = withDefaults(c, 3306, "root")
	args := []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--user=%s", c.User),
	}
	if c.TLS {
		args = append(args, "--ssl-mode=REQUIRED")
	}
	return args
}

// BuildMySQLDumpArgs builds mysqldump/mariadb-dump arguments.
// --no-tablespaces avoids needing the global PROCESS privilege, which shared
// hosting (cPanel) database users never have.
func BuildMySQLDumpArgs(c Conn) []string {
	args := mysqlConnArgs(c)
	args = append(args,
		"--single-transaction", "--quick", "--no-tablespaces",
		"--routines", "--triggers", "--events",
		c.Database,
	)
	return args
}

func (e mysqlEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("mysql", "mariadb")
	if err != nil {
		return err
	}
	args := append(mysqlConnArgs(c), "--execute=SELECT 1", c.Database)
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Env: MySQLEnv(c)})
}

func (e mysqlEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: BuildMySQLDumpArgs(c), Env: MySQLEnv(c), Stdout: w})
}

func (e mysqlEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := lookTool("mysql", "mariadb")
	if err != nil {
		return err
	}
	args := append(mysqlConnArgs(c), c.Database)
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Env: MySQLEnv(c), Stdin: r})
}

func (e mysqlEngine) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	tool, err := lookTool("mysql", "mariadb")
	if err != nil {
		return nil, err
	}
	args := append(mysqlConnArgs(c), "--batch", "--skip-column-names", "-e", "SHOW DATABASES;")
	var out bytes.Buffer
	err = runCmd(ctx, cmdSpec{
		Path:   tool,
		Args:   args,
		Env:    MySQLEnv(c),
		Stdout: &out,
	})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var dbs []string
	for _, l := range lines {
		dbName := strings.TrimSpace(l)
		if dbName == "" || dbName == "information_schema" || dbName == "performance_schema" || dbName == "sys" {
			continue
		}
		dbs = append(dbs, dbName)
	}
	return dbs, nil
}
