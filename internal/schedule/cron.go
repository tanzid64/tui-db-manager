package schedule

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandExecutor runs an external command and returns its combined output.
type CommandExecutor func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)

type CronScheduler struct {
	executor CommandExecutor
}

func defaultExecutor(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func NewCronScheduler(execFn CommandExecutor) *CronScheduler {
	if execFn == nil {
		execFn = defaultExecutor
	}
	return &CronScheduler{executor: execFn}
}

func (c *CronScheduler) Name() string { return "crontab" }

func tagForJob(job string) string {
	return fmt.Sprintf("# dbk:job:%s", job)
}

func (c *CronScheduler) readCrontab(ctx context.Context) ([]string, error) {
	out, err := c.executor(ctx, nil, "crontab", "-l")
	if err != nil {
		outStr := string(out)
		// Standard output when user has no crontab yet
		if strings.Contains(outStr, "no crontab") || strings.Contains(strings.ToLower(err.Error()), "exit status 1") {
			return nil, nil
		}
		return nil, fmt.Errorf("read crontab: %w (%s)", err, strings.TrimSpace(outStr))
	}
	lines := strings.Split(string(out), "\n")
	var result []string
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		result = append(result, l)
	}
	return result, nil
}

func (c *CronScheduler) writeCrontab(ctx context.Context, lines []string) error {
	// Clean up trailing empty lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		// Remove crontab if nothing remains
		_, _ = c.executor(ctx, nil, "crontab", "-r")
		return nil
	}
	content := strings.Join(lines, "\n") + "\n"
	out, err := c.executor(ctx, []byte(content), "crontab", "-")
	if err != nil {
		return fmt.Errorf("write crontab: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *CronScheduler) Install(ctx context.Context, job string, opts InstallOptions) error {
	expr := opts.Expression
	if expr == "" {
		expr = "0 3 * * *"
	}
	// Verify basic cron expression has at least 5 fields
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("invalid cron expression %q: expected 5 fields (minute hour day month weekday)", expr)
	}

	cmdStr, err := BuildCommand(job, opts)
	if err != nil {
		return err
	}

	tag := tagForJob(job)
	newLine := fmt.Sprintf("%s %s %s", expr, cmdStr, tag)

	lines, err := c.readCrontab(ctx)
	if err != nil {
		return err
	}

	var newLines []string
	for _, l := range lines {
		if !strings.Contains(l, tag) && strings.TrimSpace(l) != "" {
			newLines = append(newLines, l)
		}
	}
	newLines = append(newLines, newLine)

	return c.writeCrontab(ctx, newLines)
}

func (c *CronScheduler) Remove(ctx context.Context, job string) error {
	tag := tagForJob(job)
	lines, err := c.readCrontab(ctx)
	if err != nil {
		return err
	}

	var newLines []string
	found := false
	for _, l := range lines {
		if strings.Contains(l, tag) {
			found = true
			continue
		}
		if strings.TrimSpace(l) != "" {
			newLines = append(newLines, l)
		}
	}

	if !found {
		return nil // Already not present
	}

	return c.writeCrontab(ctx, newLines)
}

func (c *CronScheduler) Status(ctx context.Context, job string) (ScheduleStatus, error) {
	tag := tagForJob(job)
	lines, err := c.readCrontab(ctx)
	if err != nil {
		return ScheduleStatus{Target: "crontab"}, err
	}

	for _, l := range lines {
		if strings.Contains(l, tag) {
			fields := strings.Fields(l)
			var expr string
			if len(fields) >= 5 {
				expr = strings.Join(fields[:5], " ")
			}
			return ScheduleStatus{
				Installed:  true,
				Expression: expr,
				Target:     "crontab",
				Detail:     strings.TrimSpace(l),
			}, nil
		}
	}

	return ScheduleStatus{
		Installed: false,
		Target:    "crontab",
	}, nil
}
