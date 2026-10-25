package cli

import (
	"context"
	"os"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/schedule"
)

// TestMain keeps every test in this package away from the real system
// scheduler: by default schedules go to an in-memory fake crontab, and
// removals go through whatever defaultScheduler a test has installed.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "dbk-cli-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	var crontab []byte
	defaultScheduler = schedule.NewCronScheduler(func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "-l" {
			return crontab, nil
		}
		if len(args) > 0 && args[0] == "-r" {
			crontab = nil
			return nil, nil
		}
		crontab = append([]byte(nil), stdin...)
		return nil, nil
	})
	removeAllSchedules = func(ctx context.Context, job string) error {
		return defaultScheduler.Remove(ctx, job)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
