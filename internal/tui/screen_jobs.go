package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/jobconn"
)

type JobsScreen struct {
	svc            *Service
	selected       int
	statusMsg      string
	isWizardOpen   bool
	wizard         WizardModel
	width          int
	height         int
	isEditing      bool
	editingJobName string
	onBack         func() tea.Cmd
	onRestoreJob   func(jobName string) tea.Cmd
	pendingDelete  string // job name awaiting a second 'd' to confirm
}

type jobRunResultMsg struct {
	rec RunRecord
	err error
}

type jobTestResultMsg struct {
	name string
	err  error
}

func NewJobsScreen(svc *Service, onBack func() tea.Cmd, onRestoreJob func(string) tea.Cmd) JobsScreen {
	return JobsScreen{
		svc:          svc,
		selected:     0,
		onBack:       onBack,
		onRestoreJob: onRestoreJob,
	}
}

func (s *JobsScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	if s.isWizardOpen {
		s.wizard.SetSize(width, height)
	}
}

func (s JobsScreen) Update(msg tea.Msg) (JobsScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.SetSize(msg.Width, msg.Height)
		if s.isWizardOpen {
			var cmd tea.Cmd
			s.wizard, cmd = s.wizard.Update(msg)
			return s, cmd
		}
		return s, nil
	}

	if s.isWizardOpen {
		var cmd tea.Cmd
		s.wizard, cmd = s.wizard.Update(msg)
		if s.wizard.Done {
			s.isWizardOpen = false
			s.handleJobWizardFinish(s.wizard.Values)
		} else if s.wizard.Canceled {
			s.isWizardOpen = false
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case jobRunResultMsg:
		if msg.err != nil {
			s.statusMsg = styleError.Render("✖ Run failed: " + msg.err.Error())
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Backup saved: %s (%s, %s)", msg.rec.Target, humanSize(msg.rec.Size), msg.rec.Duration.Round(time.Millisecond)))
		}
		return s, nil

	case jobTestResultMsg:
		if msg.err != nil {
			s.statusMsg = styleError.Render(fmt.Sprintf("✖ Cannot connect for %q: %v", msg.name, msg.err))
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Database connection for %q works", msg.name))
		}
		return s, nil

	case rowClickMsg:
		// Clicking only selects: Enter on a job starts a backup, which should
		// not happen by accident. Use the "Back up now" hint instead.
		s.pendingDelete = ""
		clickRow(&s.selected, msg.index, len(s.svc.Cfg.Jobs))
		return s, nil

	case tea.KeyMsg:
		jobs := s.svc.Cfg.Jobs
		if msg.String() != "d" {
			s.pendingDelete = ""
		}
		switch msg.String() {
		case "esc":
			if s.onBack != nil {
				return s, s.onBack()
			}
		case "up", "k":
			if len(jobs) > 0 {
				if s.selected > 0 {
					s.selected--
				} else {
					s.selected = len(jobs) - 1
				}
			}
		case "down", "j":
			if len(jobs) > 0 {
				if s.selected < len(jobs)-1 {
					s.selected++
				} else {
					s.selected = 0
				}
			}
		case "t":
			if len(jobs) > 0 && s.selected < len(jobs) {
				name := jobs[s.selected].Name
				s.statusMsg = styleWarning.Render(fmt.Sprintf("⟳ Testing database connection for %q...", name))
				return s, func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					return jobTestResultMsg{name: name, err: s.svc.TestJob(ctx, name)}
				}
			}
		case "r", "enter":
			if len(jobs) > 0 && s.selected < len(jobs) {
				job := jobs[s.selected]
				s.statusMsg = styleWarning.Render(fmt.Sprintf("⟳ Running backup for %q...", job.Name))
				return s, func() tea.Msg {
					rec, err := s.svc.RunBackup(context.Background(), job.Name)
					return jobRunResultMsg{rec: rec, err: err}
				}
			}
		case "a":
			s.openJobWizard(nil)
			return s, nil
		case "e":
			if len(jobs) > 0 && s.selected < len(jobs) {
				s.openJobWizard(&jobs[s.selected])
				return s, nil
			}
		case "b":
			if len(jobs) > 0 && s.selected < len(jobs) && s.onRestoreJob != nil {
				return s, s.onRestoreJob(jobs[s.selected].Name)
			}
		case "d":
			if len(jobs) > 0 && s.selected < len(jobs) {
				delJob := jobs[s.selected].Name
				if s.pendingDelete != delJob {
					s.pendingDelete = delJob
					s.statusMsg = styleWarning.Render(fmt.Sprintf("Press d again to delete job %q (its backup files are kept)", delJob))
					return s, nil
				}
				s.pendingDelete = ""
				if jobs[s.selected].Schedule != "" {
					_ = s.svc.RemoveSchedule(delJob)
				}
				newJobs := make([]config.Job, 0)
				for _, j := range s.svc.Cfg.Jobs {
					if j.Name != delJob {
						newJobs = append(newJobs, j)
					}
				}
				s.svc.Cfg.Jobs = newJobs
				_ = s.svc.SaveConfig()
				if s.selected >= len(newJobs) && s.selected > 0 {
					s.selected = len(newJobs) - 1
				}
				s.statusMsg = styleSuccess.Render(fmt.Sprintf("Job %q deleted", delJob))
			}
		}
	}
	return s, nil
}

func (s *JobsScreen) openJobWizard(existing *config.Job) {
	if s.svc.Cfg.EnsureDefaultStorage() {
		_ = s.svc.SaveConfig()
	}
	storageNames := make([]string, 0)
	selectedStorageIdx := 0
	for i, st := range s.svc.Cfg.Storages {
		storageNames = append(storageNames, st.Name)
		if st.Name == config.DefaultStorageName {
			selectedStorageIdx = i
		}
	}

	keepLastStr := "7"
	compressChecked := true
	encryptChecked := true // new jobs encrypt by default

	if existing != nil {
		s.isEditing = true
		s.editingJobName = existing.Name
		for idx, stName := range storageNames {
			if stName == existing.Storage {
				selectedStorageIdx = idx
				break
			}
		}
		if existing.Retention.KeepLast > 0 {
			keepLastStr = strconv.Itoa(existing.Retention.KeepLast)
		}
		compressChecked = existing.Compress
		encryptChecked = existing.Encrypt
	} else {
		s.isEditing = false
		s.editingJobName = ""
	}

	storageStep := WizardStep{
		Title:       "Storage & Retention",
		Description: "Target storage destination, retention count, and compression/encryption",
		Fields: []WizardField{
			{Key: "storage", Label: "Storage Destination", Type: FieldSelect, Options: storageNames, SelectedOpt: selectedStorageIdx},
			{Key: "keep_last", Label: "Keep Last Backups", Placeholder: "7", Value: keepLastStr, Type: FieldText},
			{Key: "compress", Label: "Gzip Compress", Type: FieldCheckbox, Checked: compressChecked},
			{Key: "encrypt", Label: "Encrypt backups (recommended)", Type: FieldCheckbox, Checked: encryptChecked},
		},
	}

	engineOptions := []string{"mysql", "postgres", "mongodb", "sqlite"}
	engineSelectedIdx := 0
	jobNameVal := ""
	sqlitePathVal := ""
	hostVal := "127.0.0.1"
	portVal := "3306"
	userVal := "root"
	pwdVal := ""
	tlsChecked := false
	dbVal := ""
	sshVals := map[string]string{}

	if existing != nil {
		jobNameVal = existing.Name
		for idx, eng := range engineOptions {
			if strings.EqualFold(eng, existing.Engine) {
				engineSelectedIdx = idx
				break
			}
		}
		sqlitePathVal = existing.Conn.Path
		if existing.Conn.Host != "" {
			hostVal = existing.Conn.Host
		}
		if existing.Conn.Port > 0 {
			portVal = strconv.Itoa(existing.Conn.Port)
		}
		userVal = existing.Conn.User
		dbVal = existing.Conn.Database
		tlsChecked = existing.Conn.TLS
		if s.svc.Vault != nil {
			pwdVal, _ = s.svc.Vault.Get("job/" + existing.Name + "/password")
		}
		if ssh := existing.Conn.SSH; ssh != nil {
			sshVals["ssh_host"] = ssh.Host
			if ssh.Port > 0 {
				sshVals["ssh_port"] = strconv.Itoa(ssh.Port)
			}
			sshVals["ssh_user"] = ssh.User
			sshVals["ssh_key"] = ssh.KeyPath
			if s.svc.Vault != nil {
				sshVals["ssh_password"], _ = s.svc.Vault.Get(jobconn.SSHPasswordVaultKey(existing.Name))
				if _, ok := s.svc.Vault.Get(jobconn.SSHKeyVaultKey(existing.Name)); ok {
					sshVals["ssh_key_in_vault"] = "true"
				}
			}
		}
	}

	stepIdentity := WizardStep{
		Title:       "Job Identity",
		Description: "Job identification and database engine selection. " + s.toolSummary(engineOptions),
		Fields: []WizardField{
			{Key: "name", Label: "Job Name", Placeholder: "production-backup", Value: jobNameVal, Type: FieldText},
			{Key: "engine", Label: "Database Engine", Type: FieldSelect, Options: engineOptions, SelectedOpt: engineSelectedIdx},
		},
	}

	var steps []WizardStep
	if existing != nil && strings.ToLower(existing.Engine) == "sqlite" {
		sqliteStep := WizardStep{
			Title:       "SQLite Configuration",
			Description: "Path to SQLite database file (no network host/credentials needed)",
			Warning:     s.toolWarning("sqlite"),
			Fields: []WizardField{
				{Key: "path", Label: "SQLite DB Path", Placeholder: "/path/to/db.sqlite", Value: sqlitePathVal, Type: FieldText},
			},
		}
		steps = []WizardStep{stepIdentity, sqliteStep, storageStep}
	} else {
		connStep := WizardStep{
			Title:       "Server Connection",
			Description: connStepDescription,
			Warning:     s.toolWarning(engineOptions[engineSelectedIdx]),
			Fields: []WizardField{
				{Key: "host", Label: "Host", Placeholder: "127.0.0.1", Value: hostVal, Type: FieldText},
				{Key: "port", Label: "Port", Placeholder: portVal, Value: portVal, Type: FieldText},
				{Key: "user", Label: "Username", Placeholder: userVal, Value: userVal, Type: FieldText},
				{Key: "password", Label: "Password (Vault)", Placeholder: "••••••••", Value: pwdVal, Type: FieldText, IsSecret: true},
				{Key: "tls", Label: "Use TLS", Type: FieldCheckbox, Checked: tlsChecked},
			},
		}
		stepDb := WizardStep{
			Title:       "Target Database",
			Description: "Select target database from server",
			Fields: []WizardField{
				{Key: "database", Label: "Database Name", Placeholder: "app_prod", Value: dbVal, Type: FieldText},
			},
		}
		steps = []WizardStep{stepIdentity, connStep, sshStep(sshVals), stepDb, storageStep}
	}

	wizardTitle := "Add New Backup Job"
	if s.isEditing {
		wizardTitle = fmt.Sprintf("Edit Backup Job: %s", existing.Name)
	}

	w := NewWizard(
		wizardTitle,
		steps,
		nil,
		nil,
	)
	if s.width > 0 {
		w.SetSize(s.width, s.height)
	}

	w.OnStepAdvance = func(wiz *WizardModel, currentStep int) {
		vals := wiz.CollectValues()
		engine := strings.ToLower(strings.TrimSpace(vals["engine"]))
		if engine == "" {
			engine = "mysql"
		}

		if currentStep == 0 {
			if engine == "sqlite" {
				sqliteStep := WizardStep{
					Title:       "SQLite Configuration",
					Description: "Path to SQLite database file (no network host/credentials needed)",
					Warning:     s.toolWarning("sqlite"),
					Fields: []WizardField{
						{Key: "path", Label: "SQLite DB Path", Placeholder: "/path/to/db.sqlite", Value: vals["path"], Type: FieldText},
					},
				}
				wiz.SetSteps([]WizardStep{wiz.Steps[0], sqliteStep, storageStep})
			} else {
				portPlaceholder, userPlaceholder := engineDefaults(engine)

				hostValCurr := vals["host"]
				if hostValCurr == "" {
					hostValCurr = "127.0.0.1"
				}
				// Switch port/user to the new engine's defaults unless the user
				// typed their own values (e.g. a cPanel database user).
				portValCurr := vals["port"]
				if isDefaultPort(portValCurr) {
					portValCurr = portPlaceholder
				}
				userValCurr := vals["user"]
				if isDefaultUser(userValCurr) {
					userValCurr = userPlaceholder
				}

				connFields := []WizardField{
					{Key: "host", Label: "Host", Placeholder: "127.0.0.1", Value: hostValCurr, Type: FieldText},
					{Key: "port", Label: "Port", Placeholder: portPlaceholder, Value: portValCurr, Type: FieldText},
					{Key: "user", Label: "Username", Placeholder: userPlaceholder, Value: userValCurr, Type: FieldText},
					{Key: "password", Label: "Password (Vault)", Placeholder: "••••••••", Value: vals["password"], Type: FieldText, IsSecret: true},
					{Key: "tls", Label: "Use TLS", Type: FieldCheckbox, Checked: vals["tls"] == "true"},
				}
				connStep := WizardStep{
					Title:       "Server Connection",
					Description: connStepDescription,
					Warning:     s.toolWarning(engine),
					Fields:      connFields,
				}
				stepDb := WizardStep{
					Title:       "Target Database",
					Description: "Select target database from server",
					Fields: []WizardField{
						{Key: "database", Label: "Database Name", Placeholder: "app_prod", Value: vals["database"], Type: FieldText},
					},
				}
				wiz.SetSteps([]WizardStep{wiz.Steps[0], connStep, sshStep(vals), stepDb, storageStep})
			}
		} else if currentStep == stepSSH && engine != "sqlite" {
			host := strings.TrimSpace(vals["host"])
			if host == "" {
				host = "127.0.0.1"
			}
			port, _ := strconv.Atoi(vals["port"])
			if port <= 0 {
				switch engine {
				case "postgres":
					port = 5432
				case "mongodb":
					port = 27017
				default:
					port = 3306
				}
			}
			user := strings.TrimSpace(vals["user"])
			pwd := vals["password"]
			useTLS := vals["tls"] == "true"

			timeout := 3 * time.Second
			if strings.TrimSpace(vals["ssh_host"]) != "" {
				timeout = 15 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			tunnel, err := s.wizardTunnel(ctx, vals)
			var dbs []string
			if err == nil {
				dbs, err = s.svc.ListDatabases(ctx, engine, config.Conn{
					Host: host,
					Port: port,
					User: user,
					TLS:  useTLS,
				}, pwd, tunnel)
			}
			cancel()
			via := ""
			if tunnel != nil {
				via = " via ssh " + tunnel.Host
			}

			if err == nil && len(dbs) > 0 {
				selectedDbIdx := 0
				existingDbName := vals["database"]
				for idx, d := range dbs {
					if d == existingDbName {
						selectedDbIdx = idx
						break
					}
				}
				wiz.Steps[stepDatabase] = WizardStep{
					Title:       "Target Database",
					Description: fmt.Sprintf("Discovered %d databases on %s:%d%s — choose target database", len(dbs), host, port, via),
					Fields: []WizardField{
						{Key: "database", Label: "Database Name", Type: FieldSelect, Options: dbs, SelectedOpt: selectedDbIdx},
					},
				}
				InitFields(wiz.Steps[stepDatabase].Fields)
			} else {
				desc := "Enter target database name manually"
				if err != nil {
					desc = fmt.Sprintf("Could not list databases (%s) — enter manually", err.Error())
				}
				wiz.Steps[stepDatabase] = WizardStep{
					Title:       "Target Database",
					Description: desc,
					Fields: []WizardField{
						{Key: "database", Label: "Database Name", Placeholder: "app_prod", Value: vals["database"], Type: FieldText},
					},
				}
				InitFields(wiz.Steps[stepDatabase].Fields)
			}
		}
	}

	w.ValidateStep = func(stepIndex int, vals map[string]string) error {
		engine := strings.ToLower(strings.TrimSpace(vals["engine"]))
		if stepIndex == 0 {
			name := strings.TrimSpace(vals["name"])
			if name == "" {
				return errors.New("job name cannot be empty")
			}
			for _, existing := range s.svc.Cfg.Jobs {
				if existing.Name == name && (!s.isEditing || name != s.editingJobName) {
					return fmt.Errorf("job %q already exists", name)
				}
			}
		} else if stepIndex == 1 {
			if engine == "sqlite" {
				if strings.TrimSpace(vals["path"]) == "" {
					return errors.New("SQLite path cannot be empty")
				}
			}
		} else if stepIndex == stepSSH && engine != "sqlite" {
			return validateSSHValues(vals)
		} else if stepIndex == stepDatabase && engine != "sqlite" {
			if strings.TrimSpace(vals["database"]) == "" {
				return errors.New("database name cannot be empty")
			}
		}
		return nil
	}

	s.wizard = w
	s.isWizardOpen = true
}

func (s *JobsScreen) handleJobWizardFinish(vals map[string]string) {
	name := strings.TrimSpace(vals["name"])
	if name == "" {
		s.statusMsg = styleError.Render("Job name cannot be empty")
		return
	}

	engine := strings.ToLower(strings.TrimSpace(vals["engine"]))
	port, _ := strconv.Atoi(vals["port"])
	if port <= 0 && engine != "sqlite" {
		switch engine {
		case "postgres":
			port = 5432
		case "mongodb":
			port = 27017
		default:
			port = 3306
		}
	}
	keepLast, _ := strconv.Atoi(vals["keep_last"])
	if keepLast <= 0 {
		keepLast = 7
	}

	host := strings.TrimSpace(vals["host"])
	if host == "" && engine != "sqlite" {
		host = "127.0.0.1"
	}

	// Start from the existing job so fields the wizard doesn't show
	// (notifiers, encryption key, schedule) survive an edit.
	var job config.Job
	if s.isEditing {
		if existing, ok := s.svc.Cfg.Job(s.editingJobName); ok {
			job = existing
		}
	}
	prevSSH := job.Conn.SSH
	job.Name = name
	job.Engine = engine
	job.Conn = config.Conn{
		Host:        host,
		Port:        port,
		User:        strings.TrimSpace(vals["user"]),
		Database:    strings.TrimSpace(vals["database"]),
		Path:        config.ExpandPath(vals["path"]),
		TLS:         vals["tls"] == "true",
		PasswordRef: job.Conn.PasswordRef,
	}
	if engine == "sqlite" {
		job.Conn.Host, job.Conn.Port = "", 0
	} else {
		job.Conn.SSH = sshConfigFromValues(vals, prevSSH)
	}
	job.Storage = vals["storage"]
	job.Retention = config.Retention{KeepLast: keepLast}
	job.Compress = vals["compress"] == "true"
	job.Encrypt = vals["encrypt"] == "true"

	renamed := s.isEditing && s.editingJobName != name
	if renamed && s.svc.Vault != nil {
		// Carry the encryption key over so existing backups stay restorable.
		if id, ok := s.svc.Vault.Get("job/" + s.editingJobName + "/age_identity"); ok {
			s.svc.Vault.Set("job/"+name+"/age_identity", id)
		}
	}
	if err := s.svc.EnsureEncryptionKey(&job); err != nil {
		s.statusMsg = styleError.Render("✖ " + err.Error())
		return
	}

	// Store password in vault if provided
	if pwd := vals["password"]; pwd != "" {
		if s.svc.Vault == nil {
			_ = s.svc.CreateVault()
		}
		if s.svc.Vault != nil {
			s.svc.Vault.Set("job/"+name+"/password", pwd)
			_ = s.svc.Vault.Save()
		}
	}

	oldName := ""
	if s.isEditing {
		oldName = s.editingJobName
	}
	sshNote := s.saveJobSSH(&job, oldName, vals)

	if s.isEditing {
		updated := false
		for i, existingJob := range s.svc.Cfg.Jobs {
			if existingJob.Name == s.editingJobName {
				s.svc.Cfg.Jobs[i] = job
				updated = true
				break
			}
		}
		if !updated {
			s.svc.Cfg.Jobs = append(s.svc.Cfg.Jobs, job)
		}
		if s.editingJobName != name && s.svc.Vault != nil {
			s.svc.Vault.Delete("job/" + s.editingJobName + "/password")
			_ = s.svc.Vault.Save()
		}
		if err := s.svc.SaveConfig(); err != nil {
			s.statusMsg = styleError.Render("✖ Could not save job: " + err.Error())
			return
		}
		// A scheduled job's timer runs it by name, so move the schedule too.
		if renamed && job.Schedule != "" {
			_ = s.svc.RemoveSchedule(s.editingJobName)
			_ = s.svc.InstallSchedule(name, job.Schedule)
		}
		msg := fmt.Sprintf("✔ Updated job %q", name)
		if sshNote != "" {
			msg += " — " + sshNote
		}
		s.statusMsg = styleSuccess.Render(msg)
	} else {
		s.svc.Cfg.Jobs = append(s.svc.Cfg.Jobs, job)
		if err := s.svc.SaveConfig(); err != nil {
			s.statusMsg = styleError.Render("✖ Could not save job: " + err.Error())
			return
		}
		s.selected = len(s.svc.Cfg.Jobs) - 1
		msg := fmt.Sprintf("✔ Added job %q — press t to test it, r to run a backup now", name)
		if sshNote != "" {
			msg += " — " + sshNote
		}
		if job.Encrypt {
			msg += ". Backups are encrypted: save a settings backup (Settings → Export Settings Backup) or they can't be restored on a new server"
		}
		s.statusMsg = styleSuccess.Render(msg)
	}
}

func (s JobsScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("Backup Jobs"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Each job backs up one database to a storage"))
	b.WriteString("\n\n")

	jobs := s.svc.Cfg.Jobs
	if len(jobs) == 0 {
		b.WriteString(styleHelp.Render("No jobs yet. Press 'a' to add your first database."))
		b.WriteString("\n\n")
	} else {
		for i, j := range jobs {
			prefix := "  "
			itemStyle := styleNormal
			if i == s.selected {
				prefix = styleSelected.Render("▶ ")
				itemStyle = styleSelected
			}

			features := make([]string, 0)
			if j.Conn.SSH != nil {
				features = append(features, "ssh")
			}
			if j.Compress {
				features = append(features, "gz")
			}
			if j.Encrypt {
				features = append(features, "age")
			}
			featStr := ""
			if len(features) > 0 {
				featStr = fmt.Sprintf("[%s]", strings.Join(features, "+"))
			}
			if s.svc.ToolsMissing(j.Engine) != nil {
				featStr += " " + styleWarning.Render("⚠ tools missing (press t for details)")
			}
			if st, ok := s.svc.Cfg.Storage(j.Storage); ok && !j.Encrypt && st.Type != "local" {
				featStr += " " + styleWarning.Render("⚠ not encrypted")
			}

			lastRun := globalHistory.LastForJob(j.Name)
			statusBadge := styleHelp.Render("(Never run)")
			if lastRun != nil {
				if lastRun.Status == StatusSuccess {
					statusBadge = styleSuccess.Render("✔ OK")
				} else {
					statusBadge = styleError.Render("✖ FAILED")
				}
			}

			details := fmt.Sprintf("(%s → %s, keep %d, %s)", j.Engine, j.Storage, j.Retention.KeepLast, strings.ToLower(describeCron(j.Schedule)))
			line := fmt.Sprintf("%s%s  %s  %s  %s",
				prefix,
				itemStyle.Render(j.Name),
				styleSubtitle.Render(details),
				featStr,
				statusBadge,
			)
			b.WriteString(markRow(i, line) + "\n")
		}
		b.WriteString("\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(hintBar(
		keyHint("r", "Enter/r: Back up now"),
		keyHint("t", "t: Test"),
		keyHint("a", "a: Add"),
		keyHint("e", "e: Edit"),
		keyHint("d", "d: Delete"),
		keyHint("b", "b: Restore"),
		keyHint("esc", "Esc: Back"),
	))
	return RenderBox(b.String(), s.width)
}

// engineDefaults returns the default port and user for a database engine.
func engineDefaults(engine string) (port, user string) {
	switch engine {
	case "postgres":
		return "5432", "postgres"
	case "mongodb":
		return "27017", ""
	default:
		return "3306", "root"
	}
}

func isDefaultPort(p string) bool {
	switch strings.TrimSpace(p) {
	case "", "3306", "5432", "27017":
		return true
	}
	return false
}

func isDefaultUser(u string) bool {
	switch strings.TrimSpace(u) {
	case "", "root", "postgres", "admin":
		return true
	}
	return false
}

// humanSize formats a byte count as KB/MB/GB.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
