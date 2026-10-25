package tui

import (
	"context"
	"os"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/schedule"
)

// TestMain keeps tests away from the real crontab and home directory.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "dbk-tui-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	var crontab []byte
	fake := schedule.NewCronScheduler(func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		switch {
		case len(args) > 0 && args[0] == "-l":
			return crontab, nil
		case len(args) > 0 && args[0] == "-r":
			crontab = nil
		default:
			crontab = append([]byte(nil), stdin...)
		}
		return nil, nil
	})
	schedulerFor = func() schedule.Scheduler { return fake }
	removeAllSchedules = fake.Remove
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
