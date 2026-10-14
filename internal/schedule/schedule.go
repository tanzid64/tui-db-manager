package schedule

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type InstallOptions struct {
	Expression     string // e.g. "0 3 * * *" (cron expression)
	BinaryPath     string // path to dbk binary; if empty, detected via os.Executable()
	ConfigDir      string // optional --config directory
	PassphraseFile string // optional --passphrase-file
	PathOverride   string // if set, used verbatim as the wrapper's PATH
	WrapperDir     string // directory for the generated wrapper script; defaults to ConfigDir/schedules
	MasterKey      string // optional DBK_MASTER_KEY to bake into the wrapper script
}

// standardPathDirs are always merged into a scheduled job's PATH so that
// client tools (mysqldump, pg_dump, mongodump, ...) resolve even when the
// scheduler runs with a stripped environment, as cPanel cron and systemd do.
var standardPathDirs = []string{
	"/usr/local/sbin",
	"/usr/local/bin",
	"/usr/sbin",
	"/usr/bin",
	"/sbin",
	"/bin",
}

// effectivePath computes the PATH baked into a job's wrapper script. An
// explicit override wins; otherwise the install-time PATH (where the user's
// tools already work) is merged with the standard dirs and the dbk binary's
// own directory, deduped and with empty segments dropped.
func effectivePath(current, binDir, override string) string {
	if override != "" {
		return override
	}
	var entries []string
	entries = append(entries, strings.Split(current, string(os.PathListSeparator))...)
	entries = append(entries, standardPathDirs...)
	if binDir != "" {
		entries = append(entries, binDir)
	}

	seen := make(map[string]bool)
	var out []string
	for _, e := range entries {
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// wrapperScriptPath returns the on-disk path of a job's wrapper script.
func wrapperScriptPath(dir, job string) string {
	return filepath.Join(dir, fmt.Sprintf("dbk-%s.sh", job))
}

// resolveWrapperDir determines where a job's wrapper script lives.
func resolveWrapperDir(opts InstallOptions) (string, error) {
	if opts.WrapperDir != "" {
		return opts.WrapperDir, nil
	}
	if opts.ConfigDir != "" {
		return filepath.Join(opts.ConfigDir, "schedules"), nil
	}
	return "", fmt.Errorf("cannot determine wrapper directory: set ConfigDir or WrapperDir")
}

// resolveBinary returns the absolute path to the dbk binary.
func resolveBinary(opts InstallOptions) (string, error) {
	bin := opts.BinaryPath
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("detect dbk executable: %w", err)
		}
		bin = exe
	}
	return filepath.Abs(bin)
}

// renderWrapperScript builds the POSIX sh wrapper that a scheduler invokes.
// It pins PATH so client tools resolve in a stripped scheduler environment,
// exports DBK_MASTER_KEY if provided, then execs the dbk run command.
func renderWrapperScript(job, pathValue, masterKey, cmdStr string) string {
	if masterKey != "" {
		return fmt.Sprintf(`#!/bin/sh
# dbk schedule wrapper for job %q — managed by dbk, do not edit.
export PATH=%q
export DBK_MASTER_KEY=%q
exec %s
`, job, pathValue, masterKey, cmdStr)
	}
	return fmt.Sprintf(`#!/bin/sh
# dbk schedule wrapper for job %q — managed by dbk, do not edit.
export PATH=%q
exec %s
`, job, pathValue, cmdStr)
}

// writeWrapper renders and writes the wrapper script for a job and returns its
// path. Both the cron and systemd schedulers invoke this path rather than the
// dbk binary directly, so PATH and passphrase handling are identical on a VPS
// and on cPanel.
func writeWrapper(job string, opts InstallOptions) (string, error) {
	dir, err := resolveWrapperDir(opts)
	if err != nil {
		return "", err
	}
	bin, err := resolveBinary(opts)
	if err != nil {
		return "", err
	}
	cmdStr, err := BuildCommand(job, opts)
	if err != nil {
		return "", err
	}
	pathValue := effectivePath(os.Getenv("PATH"), filepath.Dir(bin), opts.PathOverride)
	content := renderWrapperScript(job, pathValue, opts.MasterKey, cmdStr)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create schedule wrapper directory %s: %w", dir, err)
	}
	path := wrapperScriptPath(dir, job)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		return "", fmt.Errorf("write schedule wrapper %s: %w", path, err)
	}
	return path, nil
}

// firstQuotedField extracts the first double-quoted, Go-quoted token from a
// line (as written by %q) and unquotes it. Used to recover the wrapper path
// from an installed crontab line or systemd ExecStart for removal.
func firstQuotedField(s string) (string, bool) {
	i := strings.IndexByte(s, '"')
	if i < 0 {
		return "", false
	}
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '"':
			if v, err := strconv.Unquote(s[i : j+1]); err == nil {
				return v, true
			}
			return "", false
		}
	}
	return "", false
}

type ScheduleStatus struct {
	Installed  bool   `json:"installed"`
	Expression string `json:"expression,omitempty"`
	Target     string `json:"target"` // e.g. "crontab", "systemd", "schtasks"
	Detail     string `json:"detail,omitempty"`
}

type Scheduler interface {
	Name() string
	Install(ctx context.Context, job string, opts InstallOptions) error
	Remove(ctx context.Context, job string) error
	Status(ctx context.Context, job string) (ScheduleStatus, error)
}

// BuildCommand constructs the unattended dbk run command line.
func BuildCommand(job string, opts InstallOptions) (string, error) {
	bin, err := resolveBinary(opts)
	if err != nil {
		return "", err
	}

	cmd := fmt.Sprintf("%q", bin)
	if opts.ConfigDir != "" {
		absCfg, err := filepath.Abs(opts.ConfigDir)
		if err == nil {
			cmd += fmt.Sprintf(" --config %q", absCfg)
		} else {
			cmd += fmt.Sprintf(" --config %q", opts.ConfigDir)
		}
	}
	if opts.PassphraseFile != "" {
		absPass, err := filepath.Abs(opts.PassphraseFile)
		if err == nil {
			cmd += fmt.Sprintf(" --passphrase-file %q", absPass)
		} else {
			cmd += fmt.Sprintf(" --passphrase-file %q", opts.PassphraseFile)
		}
	}
	cmd += fmt.Sprintf(" run %q --quiet --json", job)
	return cmd, nil
}
