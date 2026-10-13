package schedule

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type InstallOptions struct {
	Expression     string // e.g. "0 3 * * *" (cron expression)
	BinaryPath     string // path to dbk binary; if empty, detected via os.Executable()
	ConfigDir      string // optional --config directory
	PassphraseFile string // optional --passphrase-file
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
	bin := opts.BinaryPath
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("detect dbk executable: %w", err)
		}
		bin = exe
	}
	bin, err := filepath.Abs(bin)
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
