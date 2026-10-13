package schedule

import (
	"context"
	"fmt"
	"strings"
)

type SchtasksScheduler struct {
	executor CommandExecutor
}

func NewSchtasksScheduler(execFn CommandExecutor) *SchtasksScheduler {
	if execFn == nil {
		execFn = defaultExecutor
	}
	return &SchtasksScheduler{executor: execFn}
}

func (s *SchtasksScheduler) Name() string { return "schtasks" }

func taskNameForJob(job string) string {
	return fmt.Sprintf("dbk_%s", job)
}

func (s *SchtasksScheduler) Install(ctx context.Context, job string, opts InstallOptions) error {
	cmdStr, err := BuildCommand(job, opts)
	if err != nil {
		return err
	}

	taskName := taskNameForJob(job)
	timeStr := "03:00"

	// Parse basic cron time "minute hour ...":
	fields := strings.Fields(opts.Expression)
	if len(fields) >= 2 {
		min, hour := fields[0], fields[1]
		if len(hour) == 1 {
			hour = "0" + hour
		}
		if len(min) == 1 {
			min = "0" + min
		}
		if hour != "*" && min != "*" {
			timeStr = fmt.Sprintf("%s:%s", hour, min)
		}
	}

	args := []string{
		"/create",
		"/tn", taskName,
		"/tr", cmdStr,
		"/sc", "daily",
		"/st", timeStr,
		"/f",
	}

	out, err := s.executor(ctx, nil, "schtasks", args...)
	if err != nil {
		return fmt.Errorf("create scheduled task %s: %w (%s)", taskName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *SchtasksScheduler) Remove(ctx context.Context, job string) error {
	taskName := taskNameForJob(job)
	args := []string{"/delete", "/tn", taskName, "/f"}
	out, err := s.executor(ctx, nil, "schtasks", args...)
	if err != nil {
		outStr := string(out)
		if strings.Contains(outStr, "cannot find") || strings.Contains(outStr, "ERROR: The system cannot find the file specified.") {
			return nil
		}
		return fmt.Errorf("delete scheduled task %s: %w (%s)", taskName, err, strings.TrimSpace(outStr))
	}
	return nil
}

func (s *SchtasksScheduler) Status(ctx context.Context, job string) (ScheduleStatus, error) {
	taskName := taskNameForJob(job)
	args := []string{"/query", "/tn", taskName}
	out, err := s.executor(ctx, nil, "schtasks", args...)
	if err != nil {
		return ScheduleStatus{
			Installed: false,
			Target:    "schtasks",
		}, nil
	}

	return ScheduleStatus{
		Installed: true,
		Target:    "schtasks",
		Detail:    strings.TrimSpace(string(out)),
	}, nil
}
