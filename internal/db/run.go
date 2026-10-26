package db

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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

// extraToolDirs are searched after PATH. On macOS, Homebrew's mysql-client
// and libpq are "keg-only" (never linked onto PATH) and Postgres.app keeps its
// tools inside the app bundle, so a plain PATH lookup misses them.
var extraToolDirs = func() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	for _, prefix := range []string{"/opt/homebrew/opt", "/usr/local/opt"} {
		for _, pattern := range []string{
			"mysql-client", "mysql-client@*", "libpq", "postgresql@*",
			"mariadb", "mariadb@*", "mysql", "mysql@*", "mongodb-database-tools",
		} {
			matches, _ := filepath.Glob(filepath.Join(prefix, pattern, "bin"))
			sort.Sort(sort.Reverse(sort.StringSlice(matches))) // newest version first
			dirs = append(dirs, matches...)
		}
	}
	return append(dirs, "/Applications/Postgres.app/Contents/Versions/latest/bin")
}

// lookTool returns the first of the candidates found on PATH, then in
// extraToolDirs, or a MissingToolsError with an install hint.
func lookTool(primary string, alternatives ...string) (string, error) {
	names := append([]string{primary}, alternatives...)
	if p, err := findTool(names); err == nil {
		return p, nil
	}
	label := primary
	if len(alternatives) > 0 {
		label += " (or " + strings.Join(alternatives, ", ") + ")"
	}
	engine := engineForTool(primary)
	return "", &MissingToolsError{Engine: engine, Tools: []string{label}, Hint: InstallHint(engine)}
}

func findTool(names []string) (string, error) {
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	for _, dir := range extraToolDirs() {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return p, nil
			}
		}
	}
	return "", exec.ErrNotFound
}
