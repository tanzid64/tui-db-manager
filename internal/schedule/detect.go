package schedule

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// DetectScheduler chooses the system scheduler for the current machine.
//
// crontab is preferred whenever it is available: it is the only scheduler on
// cPanel shared hosting, and on a VPS it keeps running after you log out of
// SSH (systemd *user* timers stop at logout unless lingering is enabled).
// systemd user timers are used only when there is no crontab.
func DetectScheduler() Scheduler {
	if runtime.GOOS == "windows" {
		return NewSchtasksScheduler(nil)
	}
	if _, err := exec.LookPath("crontab"); err == nil {
		return NewCronScheduler(nil)
	}
	if runtime.GOOS == "linux" && hasWorkingUserSystemd() {
		return NewSystemdScheduler("", nil)
	}
	return NewCronScheduler(nil)
}

// RemoveEverywhere removes a job's schedule from every scheduler dbk may have
// used (older versions preferred systemd), so no stale timer keeps running.
func RemoveEverywhere(ctx context.Context, job string) error {
	if runtime.GOOS == "windows" {
		return NewSchtasksScheduler(nil).Remove(ctx, job)
	}
	var firstErr error
	if _, err := exec.LookPath("crontab"); err == nil {
		if err := NewCronScheduler(nil).Remove(ctx, job); err != nil {
			firstErr = err
		}
	}
	if runtime.GOOS == "linux" {
		_ = NewSystemdScheduler("", nil).Remove(ctx, job)
	}
	return firstErr
}

func hasWorkingUserSystemd() bool {
	// systemctl must be present on PATH
	systemctl, err := exec.LookPath("systemctl")
	if err != nil || systemctl == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Verify that the user systemd instance responds
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "is-system-running")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Output might be "degraded", which is still functional
		s := string(out)
		if s == "running\n" || s == "degraded\n" || s == "starting\n" {
			return true
		}
		return false
	}
	return true
}
