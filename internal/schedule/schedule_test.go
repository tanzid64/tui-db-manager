package schedule

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommand(t *testing.T) {
	opts := InstallOptions{
		Expression:     "0 3 * * *",
		BinaryPath:     "/usr/local/bin/dbk",
		ConfigDir:      "/etc/dbk",
		PassphraseFile: "/etc/dbk/pass.txt",
	}

	cmd, err := BuildCommand("my-job", opts)
	if err != nil {
		t.Fatalf("build command: %v", err)
	}

	if !strings.Contains(cmd, `"/usr/local/bin/dbk"`) {
		t.Errorf("missing binary in command: %s", cmd)
	}
	if !strings.Contains(cmd, `run "my-job" --quiet --json`) {
		t.Errorf("missing run command: %s", cmd)
	}
	if !strings.Contains(cmd, `--config "/etc/dbk"`) {
		t.Errorf("missing config flag: %s", cmd)
	}
	if !strings.Contains(cmd, `--passphrase-file "/etc/dbk/pass.txt"`) {
		t.Errorf("missing passphrase flag: %s", cmd)
	}
}

func TestCronSchedulerInstallRemoveStatus(t *testing.T) {
	var crontabContent string

	execMock := func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		if name == "crontab" {
			if len(args) == 1 && args[0] == "-l" {
				return []byte(crontabContent), nil
			}
			if len(args) == 1 && args[0] == "-" {
				crontabContent = string(stdin)
				return nil, nil
			}
			if len(args) == 1 && args[0] == "-r" {
				crontabContent = ""
				return nil, nil
			}
		}
		return nil, nil
	}

	cs := NewCronScheduler(execMock)
	ctx := context.Background()

	// Initial status
	st, err := cs.Status(ctx, "orders")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Installed {
		t.Fatal("expected job not to be installed initially")
	}

	// Install
	opts := InstallOptions{
		Expression: "15 2 * * *",
		BinaryPath: "/bin/dbk",
	}
	if err := cs.Install(ctx, "orders", opts); err != nil {
		t.Fatalf("install: %v", err)
	}

	if !strings.Contains(crontabContent, "# dbk:job:orders") {
		t.Fatalf("crontab missing job marker: %s", crontabContent)
	}
	if !strings.Contains(crontabContent, "15 2 * * *") {
		t.Fatalf("crontab missing expression: %s", crontabContent)
	}

	// Status after install
	st, err = cs.Status(ctx, "orders")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Installed || st.Expression != "15 2 * * *" || st.Target != "crontab" {
		t.Fatalf("unexpected status: %+v", st)
	}

	// Overwrite install with new expression
	opts.Expression = "30 4 * * *"
	if err := cs.Install(ctx, "orders", opts); err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	if strings.Contains(crontabContent, "15 2 * * *") {
		t.Fatalf("old expression still present: %s", crontabContent)
	}
	if !strings.Contains(crontabContent, "30 4 * * *") {
		t.Fatalf("new expression not found: %s", crontabContent)
	}

	// Remove
	if err := cs.Remove(ctx, "orders"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if strings.Contains(crontabContent, "orders") {
		t.Fatalf("job still present in crontab: %s", crontabContent)
	}

	// Status after remove
	st, err = cs.Status(ctx, "orders")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Installed {
		t.Fatal("expected job not to be installed after remove")
	}
}

func TestCronSchedulerInvalidExpression(t *testing.T) {
	cs := NewCronScheduler(func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		return nil, nil
	})
	err := cs.Install(context.Background(), "job", InstallOptions{Expression: "only four fields"})
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestCronToOnCalendar(t *testing.T) {
	tests := []struct {
		cron string
		want string
	}{
		{"0 3 * * *", "*-*-* 03:00:00"},
		{"15 14 * * *", "*-*-* 14:15:00"},
		{"0 * * * *", "hourly"},
		{"0 0 * * *", "daily"},
		{"daily", "daily"},
	}

	for _, tt := range tests {
		got := CronToOnCalendar(tt.cron)
		if got != tt.want {
			t.Errorf("CronToOnCalendar(%q) = %q, want %q", tt.cron, got, tt.want)
		}
	}
}

func TestSystemdSchedulerInstallRemoveStatus(t *testing.T) {
	unitDir := t.TempDir()
	var executedCommands [][]string

	execMock := func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		executedCommands = append(executedCommands, append([]string{name}, args...))
		if len(args) >= 3 && args[1] == "is-active" {
			return []byte("active\n"), nil
		}
		return nil, nil
	}

	ss := NewSystemdScheduler(unitDir, execMock)
	ctx := context.Background()

	// Install
	opts := InstallOptions{
		Expression: "0 3 * * *",
		BinaryPath: "/usr/bin/dbk",
	}
	if err := ss.Install(ctx, "billing", opts); err != nil {
		t.Fatalf("install: %v", err)
	}

	servicePath := filepath.Join(unitDir, "dbk-billing.service")
	timerPath := filepath.Join(unitDir, "dbk-billing.timer")

	serviceBytes, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("read service unit: %v", err)
	}
	if !strings.Contains(string(serviceBytes), "ExecStart=\"/usr/bin/dbk\" run \"billing\" --quiet --json") {
		t.Fatalf("unexpected service content: %s", string(serviceBytes))
	}

	timerBytes, err := os.ReadFile(timerPath)
	if err != nil {
		t.Fatalf("read timer unit: %v", err)
	}
	if !strings.Contains(string(timerBytes), "OnCalendar=*-*-* 03:00:00") {
		t.Fatalf("unexpected timer content: %s", string(timerBytes))
	}

	// Status
	st, err := ss.Status(ctx, "billing")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Installed || st.Expression != "*-*-* 03:00:00" || st.Target != "systemd" {
		t.Fatalf("unexpected status: %+v", st)
	}

	// Remove
	if err := ss.Remove(ctx, "billing"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		t.Fatal("expected service file to be deleted")
	}
	if _, err := os.Stat(timerPath); !os.IsNotExist(err) {
		t.Fatal("expected timer file to be deleted")
	}

	// Status after remove
	st, err = ss.Status(ctx, "billing")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Installed {
		t.Fatal("expected installed to be false")
	}
}

func TestSchtasksScheduler(t *testing.T) {
	var lastArgs []string
	execMock := func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		lastArgs = args
		if len(args) > 0 && args[0] == "/query" {
			return []byte("dbk_sales Ready"), nil
		}
		return nil, nil
	}

	st := NewSchtasksScheduler(execMock)
	ctx := context.Background()

	opts := InstallOptions{
		Expression: "0 4 * * *",
		BinaryPath: "/usr/bin/dbk",
	}

	if err := st.Install(ctx, "sales", opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	if lastArgs[1] != "/tn" || lastArgs[2] != "dbk_sales" || lastArgs[7] != "/st" || lastArgs[8] != "04:00" {
		t.Fatalf("unexpected schtasks args: %+v", lastArgs)
	}

	stat, err := st.Status(ctx, "sales")
	if err != nil || !stat.Installed || stat.Target != "schtasks" {
		t.Fatalf("unexpected status: %+v", stat)
	}

	if err := st.Remove(ctx, "sales"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if lastArgs[0] != "/delete" || lastArgs[2] != "dbk_sales" {
		t.Fatalf("unexpected delete args: %+v", lastArgs)
	}
}
