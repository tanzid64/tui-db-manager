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

func BuildMySQLDumpArgs(c Conn) []string {
	args := []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--user=%s", c.User),
		"--single-transaction", "--quick", "--routines", "--triggers", "--events",
	}
	if c.TLS {
		args = append(args, "--ssl-mode=REQUIRED")
	}
	args = append(args, c.Database)
	return args
}

func (e mysqlEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("mysql", "mariadb")
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{
		Path: tool,
		Args: []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
			fmt.Sprintf("--user=%s", c.User), "--execute=SELECT 1", c.Database},
		Env: MySQLEnv(c),
	})
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
	args := []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--user=%s", c.User), c.Database}
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Env: MySQLEnv(c), Stdin: r})
}

func (e mysqlEngine) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	tool, err := lookTool("mysql", "mariadb")
	if err != nil {
		return nil, err
	}
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := c.Port
	if port <= 0 {
		port = 3306
	}
	user := c.User
	if user == "" {
		user = "root"
	}
	args := []string{
		fmt.Sprintf("--host=%s", host),
		fmt.Sprintf("--port=%d", port),
		fmt.Sprintf("--user=%s", user),
		"--batch", "--skip-column-names",
		"-e", "SHOW DATABASES;",
	}
	if c.TLS {
		args = append(args, "--ssl-mode=REQUIRED")
	}
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
