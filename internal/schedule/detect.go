package schedule

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// DetectScheduler chooses the default system scheduler for the current operating system.
// On Linux, it detects whether systemd user services are functional; if so, it chooses systemd,
// otherwise falling back to crontab.
// On Windows, it chooses Task Scheduler (schtasks).
// On macOS and other Unixes, it chooses crontab.
func DetectScheduler() Scheduler {
	if runtime.GOOS == "windows" {
		return NewSchtasksScheduler(nil)
	}

	if runtime.GOOS == "linux" {
		if hasWorkingUserSystemd() {
			return NewSystemdScheduler("", nil)
		}
		return NewCronScheduler(nil)
	}

	return NewCronScheduler(nil)
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
