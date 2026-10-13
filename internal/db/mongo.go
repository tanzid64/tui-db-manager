package db

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

type mongoEngine struct{}

func init() { Register(mongoEngine{}) }

func (mongoEngine) Name() string     { return "mongodb" }
func (mongoEngine) DefaultPort() int { return 27017 }

func (mongoEngine) Detect() (string, error) { return lookTool("mongodump") }

func BuildMongoBaseArgs(c Conn) []string {
	args := []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
	}
	if c.User != "" {
		args = append(args, fmt.Sprintf("--username=%s", c.User))
	}
	if c.Database != "" {
		args = append(args, fmt.Sprintf("--db=%s", c.Database))
	}
	if c.TLS {
		args = append(args, "--tls")
	}
	return args
}

// writeMongoConfig writes a 0600 YAML config holding only the password, which
// mongodump/mongorestore accept via --config, keeping it off the argv/ps list.
func writeMongoConfig(password string) (string, func(), error) {
	f, err := os.CreateTemp("", "dbk-mongo-*.yaml")
	if err != nil {
		return "", func() {}, err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	if _, err := fmt.Fprintf(f, "password: %q\n", password); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func (e mongoEngine) withConfig(c Conn, base []string) ([]string, func(), error) {
	if c.Password == "" {
		return base, func() {}, nil
	}
	path, cleanup, err := writeMongoConfig(c.Password)
	if err != nil {
		return nil, func() {}, err
	}
	return append(base, "--config="+path), cleanup, nil
}

func (e mongoEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("mongosh", "mongo")
	if err != nil {
		return err
	}
	args, cleanup, err := e.withConfig(c, BuildMongoBaseArgs(c))
	if err != nil {
		return err
	}
	defer cleanup()
	return runCmd(ctx, cmdSpec{Path: tool, Args: append(args, "--eval", "db.runCommand({ping:1})")})
}

func (e mongoEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	args, cleanup, err := e.withConfig(c, append(BuildMongoBaseArgs(c), "--archive"))
	if err != nil {
		return err
	}
	defer cleanup()
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Stdout: w})
}

func (e mongoEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := lookTool("mongorestore")
	if err != nil {
		return err
	}
	args, cleanup, err := e.withConfig(c, append(BuildMongoBaseArgs(c), "--archive", "--drop"))
	if err != nil {
		return err
	}
	defer cleanup()
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Stdin: r})
}

func (e mongoEngine) ListDatabases(ctx context.Context, c Conn) ([]string, error) {
	tool, err := lookTool("mongosh", "mongo")
	if err != nil {
		return nil, err
	}
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := c.Port
	if port <= 0 {
		port = 27017
	}
	baseArgs := []string{
		fmt.Sprintf("--host=%s", host),
		fmt.Sprintf("--port=%d", port),
	}
	if c.User != "" {
		baseArgs = append(baseArgs, fmt.Sprintf("--username=%s", c.User))
	}
	if c.TLS {
		baseArgs = append(baseArgs, "--tls")
	}
	args, cleanup, err := e.withConfig(c, baseArgs)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var out bytes.Buffer
	evalScript := "db.adminCommand({listDatabases:1}).databases.forEach(function(d){print(d.name)})"
	args = append(args, "--quiet", "--eval", evalScript)
	err = runCmd(ctx, cmdSpec{
		Path:   tool,
		Args:   args,
		Stdout: &out,
	})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var dbs []string
	for _, l := range lines {
		name := strings.TrimSpace(l)
		if name != "" && name != "admin" && name != "config" && name != "local" {
			dbs = append(dbs, name)
		}
	}
	return dbs, nil
}

