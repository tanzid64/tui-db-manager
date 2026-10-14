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

func TestEffectivePath(t *testing.T) {
	// An explicit override is used verbatim.
	if got := effectivePath("/a:/b", "/opt/bin", "/custom/path"); got != "/custom/path" {
		t.Fatalf("override not honored: got %q", got)
	}

	// Without override: install-time PATH entries come first, standard dirs
	// and the binary's own directory are merged in, deduped, no empty segments.
	got := effectivePath("/usr/bin:/home/u/bin", "/home/u/bin", "")
	if !strings.HasPrefix(got, "/usr/bin:/home/u/bin") {
		t.Fatalf("current PATH entries should come first: %q", got)
	}
	if !strings.Contains(got, "/usr/local/bin") {
		t.Fatalf("standard dir /usr/local/bin missing: %q", got)
	}
	if n := strings.Count(":"+got+":", ":/home/u/bin:"); n != 1 {
		t.Fatalf("binDir should appear exactly once, got %d: %q", n, got)
	}
	for _, p := range strings.Split(got, ":") {
		if p == "" {
			t.Fatalf("empty PATH segment in %q", got)
		}
	}
}

func TestCronInstallWritesWrapper(t *testing.T) {
	cfgDir := t.TempDir()
	var crontabContent string
	execMock := func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		if name == "crontab" && len(args) == 1 {
			switch args[0] {
			case "-l":
				return []byte(crontabContent), nil
			case "-":
				crontabContent = string(stdin)
			case "-r":
				crontabContent = ""
			}
		}
		return nil, nil
	}

	cs := NewCronScheduler(execMock)
	opts := InstallOptions{
		Expression:     "0 3 * * *",
		BinaryPath:     "/usr/local/bin/dbk",
		ConfigDir:      cfgDir,
		PassphraseFile: filepath.Join(cfgDir, "pass"),
		MasterKey:      "super-cron-secret",
	}
	if err := cs.Install(context.Background(), "orders", opts); err != nil {
		t.Fatalf("install: %v", err)
	}

	wrapper := filepath.Join(cfgDir, "schedules", "dbk-orders.sh")
	info, err := os.Stat(wrapper)
	if err != nil {
		t.Fatalf("wrapper script not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("wrapper perm = %o, want 700", perm)
	}

	b, _ := os.ReadFile(wrapper)
	content := string(b)
	if !strings.HasPrefix(content, "#!/bin/sh") {
		t.Fatalf("wrapper missing shebang: %s", content)
	}
	if !strings.Contains(content, "export PATH=") {
		t.Fatalf("wrapper missing PATH export: %s", content)
	}
	if !strings.Contains(content, `export DBK_MASTER_KEY="super-cron-secret"`) {
		t.Fatalf("wrapper missing DBK_MASTER_KEY export: %s", content)
	}
	if !strings.Contains(content, `"/usr/local/bin/dbk"`) {
		t.Fatalf("wrapper missing dbk binary: %s", content)
	}
	if !strings.Contains(content, `run "orders" --quiet --json`) {
		t.Fatalf("wrapper missing run command: %s", content)
	}
	if !strings.Contains(content, "--passphrase-file") {
		t.Fatalf("wrapper missing passphrase flag: %s", content)
	}

	// The crontab line must invoke the wrapper, not dbk directly.
	if !strings.Contains(crontabContent, wrapper) {
		t.Fatalf("crontab does not reference wrapper %q: %s", wrapper, crontabContent)
	}
	if !strings.Contains(crontabContent, "# dbk:job:orders") {
		t.Fatalf("crontab missing job marker: %s", crontabContent)
	}

	// Remove must delete the wrapper script too.
	if err := cs.Remove(context.Background(), "orders"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Fatalf("wrapper script not deleted on remove")
	}
}

func TestCronSchedulerInstallRemoveStatus(t *testing.T) {
	cfgDir := t.TempDir()
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
		ConfigDir:  cfgDir,
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
		{"*/1 * * * *", "*-*-* *:00/1:00"},
		{"*/5 * * * *", "*-*-* *:00/5:00"},
		{"0 */2 * * *", "*-*-* 00/2:00:00"},
		{"0 9 * * 1-5", "Mon..Fri *-*-* 09:00:00"},
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

	cfgDir := t.TempDir()
	ss := NewSystemdScheduler(unitDir, execMock)
	ctx := context.Background()

	// Install
	opts := InstallOptions{
		Expression: "0 3 * * *",
		BinaryPath: "/usr/bin/dbk",
		ConfigDir:  cfgDir,
	}
	if err := ss.Install(ctx, "billing", opts); err != nil {
		t.Fatalf("install: %v", err)
	}

	servicePath := filepath.Join(unitDir, "dbk-billing.service")
	timerPath := filepath.Join(unitDir, "dbk-billing.timer")
	wrapperPath := filepath.Join(cfgDir, "schedules", "dbk-billing.sh")

	serviceBytes, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("read service unit: %v", err)
	}
	if !strings.Contains(string(serviceBytes), "ExecStart="+wrapperPath) {
		t.Fatalf("service should exec the wrapper %q: %s", wrapperPath, string(serviceBytes))
	}
	if _, err := os.Stat(wrapperPath); err != nil {
		t.Fatalf("wrapper script not written: %v", err)
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
	if _, err := os.Stat(wrapperPath); !os.IsNotExist(err) {
		t.Fatal("expected wrapper script to be deleted")
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
