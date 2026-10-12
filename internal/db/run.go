package db

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type cmdSpec struct {
	Path   string
	Args   []string
	Env    []string // appended to the current environment
	Stdin  io.Reader
	Stdout io.Writer
}

func runCmd(ctx context.Context, s cmdSpec) error {
	cmd := exec.CommandContext(ctx, s.Path, s.Args...)
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("%s: %w", s.Path, err)
		}
		return fmt.Errorf("%s: %w: %s", s.Path, err, msg)
	}
	return nil
}

// lookTool returns the first of the candidates found on PATH.
func lookTool(primary string, alternatives ...string) (string, error) {
	for _, name := range append([]string{primary}, alternatives...) {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	all := strings.Join(append([]string{primary}, alternatives...), " or ")
	return "", fmt.Errorf("required client tool not found on PATH: install %s", all)
}
