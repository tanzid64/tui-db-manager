package schedule

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type SystemdScheduler struct {
	unitDir  string
	executor CommandExecutor
}

func NewSystemdScheduler(unitDir string, execFn CommandExecutor) *SystemdScheduler {
	if unitDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			unitDir = filepath.Join(home, ".config", "systemd", "user")
		} else {
			unitDir = filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user")
		}
	}
	if execFn == nil {
		execFn = defaultExecutor
	}
	return &SystemdScheduler{
		unitDir:  unitDir,
		executor: execFn,
	}
}

func (s *SystemdScheduler) Name() string { return "systemd" }

func serviceUnitName(job string) string { return fmt.Sprintf("dbk-%s.service", job) }
func timerUnitName(job string) string   { return fmt.Sprintf("dbk-%s.timer", job) }

// CronToOnCalendar converts a standard 5-field cron expression to a systemd OnCalendar string.
// If the expression is already in OnCalendar format (e.g. "daily", contains "-"), it is returned as is.
func CronToOnCalendar(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return expr
	}
	min, hour, dom, mon, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Hourly: "0 * * * *" -> "hourly"
	if min == "0" && hour == "*" && dom == "*" && mon == "*" && dow == "*" {
		return "hourly"
	}
	// Daily midnight: "0 0 * * *" -> "daily"
	if min == "0" && hour == "0" && dom == "*" && mon == "*" && dow == "*" {
		return "daily"
	}

	// Handle simple daily: "0 3 * * *" -> "*-*-* 03:00:00"
	if dom == "*" && mon == "*" && dow == "*" {
		h, errH := strconv.Atoi(hour)
		m, errM := strconv.Atoi(min)
		if errH == nil && errM == nil {
			return fmt.Sprintf("*-*-* %02d:%02d:00", h, m)
		}
	}

	formatPart := func(f string) string {
		if f == "*" {
			return "*"
		}
		if strings.HasPrefix(f, "*/") {
			return "00/" + strings.TrimPrefix(f, "*/")
		}
		if n, err := strconv.Atoi(f); err == nil {
			return fmt.Sprintf("%02d", n)
		}
		if strings.Contains(f, "-") && !strings.Contains(f, "..") {
			parts := strings.Split(f, "-")
			if len(parts) == 2 {
				p0 := parts[0]
				p1 := parts[1]
				if n0, err := strconv.Atoi(p0); err == nil {
					p0 = fmt.Sprintf("%02d", n0)
				}
				if n1, err := strconv.Atoi(p1); err == nil {
					p1 = fmt.Sprintf("%02d", n1)
				}
				return p0 + ".." + p1
			}
		}
		return f
	}

	minPart := formatPart(min)
	hourPart := formatPart(hour)
	domPart := formatPart(dom)
	monPart := formatPart(mon)

	datePart := "*-"
	if mon == "*" {
		datePart += "*-"
	} else {
		datePart += monPart + "-"
	}
	if dom == "*" {
		datePart += "*"
	} else {
		datePart += domPart
	}

	timePart := fmt.Sprintf("%s:%s:00", hourPart, minPart)

	// Day of week conversion
	dowMap := map[string]string{
		"0": "Sun", "7": "Sun",
		"1": "Mon", "2": "Tue", "3": "Wed", "4": "Thu", "5": "Fri", "6": "Sat",
	}
	dowPart := dow
	if mapped, ok := dowMap[dow]; ok {
		dowPart = mapped
	} else if strings.Contains(dow, "-") {
		parts := strings.Split(dow, "-")
		if len(parts) == 2 {
			p1, ok1 := dowMap[parts[0]]
			p2, ok2 := dowMap[parts[1]]
			if ok1 && ok2 {
				dowPart = p1 + ".." + p2
			}
		}
	}

	if dow == "*" {
		return fmt.Sprintf("%s %s", datePart, timePart)
	}
	return fmt.Sprintf("%s %s %s", dowPart, datePart, timePart)
}

func (s *SystemdScheduler) Install(ctx context.Context, job string, opts InstallOptions) error {
	expr := opts.Expression
	if expr == "" {
		expr = "0 3 * * *"
	}
	onCalendar := CronToOnCalendar(expr)

	wrapper, err := writeWrapper(job, opts)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(s.unitDir, 0o755); err != nil {
		return fmt.Errorf("create systemd user directory %s: %w", s.unitDir, err)
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=dbk backup for %s
After=network.target

[Service]
Type=oneshot
ExecStart=%s
`, job, wrapper)

	timerContent := fmt.Sprintf(`[Unit]
Description=dbk backup timer for %s

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, job, onCalendar)

	servicePath := filepath.Join(s.unitDir, serviceUnitName(job))
	timerPath := filepath.Join(s.unitDir, timerUnitName(job))

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0o644); err != nil {
		return fmt.Errorf("write systemd service unit: %w", err)
	}
	if err := os.WriteFile(timerPath, []byte(timerContent), 0o644); err != nil {
		return fmt.Errorf("write systemd timer unit: %w", err)
	}

	_, _ = s.executor(ctx, nil, "systemctl", "--user", "daemon-reload")
	out, err := s.executor(ctx, nil, "systemctl", "--user", "enable", "--now", timerUnitName(job))
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		if outStr != "" {
			return fmt.Errorf("enable systemd timer %s: %w (%s)", timerUnitName(job), err, outStr)
		}
		return fmt.Errorf("enable systemd timer %s: %w", timerUnitName(job), err)
	}

	// Without lingering, user timers stop when the user logs out of SSH.
	_, _ = s.executor(ctx, nil, "loginctl", "enable-linger")

	return nil
}

func (s *SystemdScheduler) Remove(ctx context.Context, job string) error {
	timerName := timerUnitName(job)
	serviceName := serviceUnitName(job)

	_, _ = s.executor(ctx, nil, "systemctl", "--user", "disable", "--now", timerName)

	// Remove the wrapper script referenced by the service unit's ExecStart.
	servicePath := filepath.Join(s.unitDir, serviceName)
	if b, err := os.ReadFile(servicePath); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if wrapper, ok := strings.CutPrefix(strings.TrimSpace(line), "ExecStart="); ok {
				_ = os.Remove(wrapper)
				break
			}
		}
	}

	_ = os.Remove(filepath.Join(s.unitDir, timerName))
	_ = os.Remove(servicePath)

	_, _ = s.executor(ctx, nil, "systemctl", "--user", "daemon-reload")
	return nil
}

func (s *SystemdScheduler) Status(ctx context.Context, job string) (ScheduleStatus, error) {
	timerPath := filepath.Join(s.unitDir, timerUnitName(job))
	if _, err := os.Stat(timerPath); err != nil {
		return ScheduleStatus{
			Installed: false,
			Target:    "systemd",
		}, nil
	}

	// Read timer unit to inspect OnCalendar
	b, err := os.ReadFile(timerPath)
	if err != nil {
		return ScheduleStatus{Target: "systemd"}, err
	}

	var onCalendar string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "OnCalendar=") {
			onCalendar = strings.TrimPrefix(strings.TrimSpace(line), "OnCalendar=")
			break
		}
	}

	out, err := s.executor(ctx, nil, "systemctl", "--user", "is-active", timerUnitName(job))
	statusStr := strings.TrimSpace(string(out))

	return ScheduleStatus{
		Installed:  true,
		Expression: onCalendar,
		Target:     "systemd",
		Detail:     fmt.Sprintf("active: %s (%s)", statusStr, timerPath),
	}, nil
}
