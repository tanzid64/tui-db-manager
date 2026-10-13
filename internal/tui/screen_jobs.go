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
}

type jobRunResultMsg struct {
	rec RunRecord
	err error
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
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Backup succeeded! Key: %s, Size: %d bytes (%v)", msg.rec.Target, msg.rec.Size, msg.rec.Duration.Round(100)))
		}
		return s, nil

	case tea.KeyMsg:
		jobs := s.svc.Cfg.Jobs
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
		case "r":
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
				newJobs := make([]config.Job, 0)
				for i, j := range jobs {
					if i != s.selected {
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

func parseScheduleSelection(freqOption, customCron string) string {
	switch freqOption {
	case "Every 15 minutes":
		return "*/15 * * * *"
	case "Every 30 minutes":
		return "*/30 * * * *"
	case "Hourly":
		return "0 * * * *"
	case "Every 6 hours":
		return "0 */6 * * *"
	case "Every 12 hours":
		return "0 */12 * * *"
	case "Daily at midnight (00:00)":
		return "0 0 * * *"
	case "Daily at 2:00 AM (02:00)":
		return "0 2 * * *"
	case "Weekly on Sunday (00:00)":
		return "0 0 * * 0"
	case "Monthly (1st of month)":
		return "0 0 1 * *"
	case "Custom Cron Expression":
		return strings.TrimSpace(customCron)
	default:
		if strings.TrimSpace(customCron) != "" {
			return strings.TrimSpace(customCron)
		}
		return ""
	}
}

func matchSchedulePreset(cron string) (string, int, string) {
	cron = strings.TrimSpace(cron)
	if cron == "" {
		return "Manual only (No schedule)", 0, ""
	}
	switch cron {
	case "*/15 * * * *":
		return "Every 15 minutes", 1, ""
	case "*/30 * * * *":
		return "Every 30 minutes", 2, ""
	case "0 * * * *":
		return "Hourly", 3, ""
	case "0 */6 * * *":
		return "Every 6 hours", 4, ""
	case "0 */12 * * *":
		return "Every 12 hours", 5, ""
	case "0 0 * * *":
		return "Daily at midnight (00:00)", 6, ""
	case "0 2 * * *":
		return "Daily at 2:00 AM (02:00)", 7, ""
	case "0 0 * * 0":
		return "Weekly on Sunday (00:00)", 8, ""
	case "0 0 1 * *":
		return "Monthly (1st of month)", 9, ""
	default:
		return "Custom Cron Expression", 10, cron
	}
}

func (s *JobsScreen) openJobWizard(existing *config.Job) {
	storageNames := make([]string, 0)
	for _, st := range s.svc.Cfg.Storages {
		storageNames = append(storageNames, st.Name)
	}
	if len(storageNames) == 0 {
		storageNames = append(storageNames, "default-local")
	}

	selectedStorageIdx := 0
	keepLastStr := "7"
	compressChecked := true
	encryptChecked := false
	freqSelectedIdx := 0
	customCronVal := ""

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
		_, freqSelectedIdx, customCronVal = matchSchedulePreset(existing.Schedule)
	} else {
		s.isEditing = false
		s.editingJobName = ""
	}

	freqOptions := []string{
		"Manual only (No schedule)",
		"Every 15 minutes",
		"Every 30 minutes",
		"Hourly",
		"Every 6 hours",
		"Every 12 hours",
		"Daily at midnight (00:00)",
		"Daily at 2:00 AM (02:00)",
		"Weekly on Sunday (00:00)",
		"Monthly (1st of month)",
		"Custom Cron Expression",
	}

	storageStep := WizardStep{
		Title:       "Storage & Schedule",
		Description: "Target storage destination, automated run schedule, and retention count",
		Fields: []WizardField{
			{Key: "storage", Label: "Storage Destination", Type: FieldSelect, Options: storageNames, SelectedOpt: selectedStorageIdx},
			{Key: "schedule", Label: "Run Schedule", Type: FieldSelect, Options: freqOptions, SelectedOpt: freqSelectedIdx},
			{Key: "custom_cron", Label: "Custom Cron (if selected)", Placeholder: "0 2 * * *", Value: customCronVal, Type: FieldText},
			{Key: "keep_last", Label: "Keep Last Backups", Placeholder: "7", Value: keepLastStr, Type: FieldText},
			{Key: "compress", Label: "Gzip Compress", Type: FieldCheckbox, Checked: compressChecked},
			{Key: "encrypt", Label: "Age Encrypt", Type: FieldCheckbox, Checked: encryptChecked},
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
	}

	stepIdentity := WizardStep{
		Title:       "Job Identity",
		Description: "Job identification and database engine selection",
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
			Fields: []WizardField{
				{Key: "path", Label: "SQLite DB Path", Placeholder: "/path/to/db.sqlite", Value: sqlitePathVal, Type: FieldText},
			},
		}
		steps = []WizardStep{stepIdentity, sqliteStep, storageStep}
	} else {
		connStep := WizardStep{
			Title:       "Server Connection",
			Description: "Connection parameters for the database server",
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
		steps = []WizardStep{stepIdentity, connStep, stepDb, storageStep}
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
					Fields: []WizardField{
						{Key: "path", Label: "SQLite DB Path", Placeholder: "/path/to/db.sqlite", Value: vals["path"], Type: FieldText},
					},
				}
				wiz.SetSteps([]WizardStep{wiz.Steps[0], sqliteStep, storageStep})
			} else {
				portPlaceholder := "3306"
				userPlaceholder := "root"
				switch engine {
				case "postgres":
					portPlaceholder = "5432"
					userPlaceholder = "postgres"
				case "mongodb":
					portPlaceholder = "27017"
					userPlaceholder = "admin"
				}

				hostValCurr := vals["host"]
				if hostValCurr == "" {
					hostValCurr = "127.0.0.1"
				}
				portValCurr := vals["port"]
				if portValCurr == "" {
					portValCurr = portPlaceholder
				}
				userValCurr := vals["user"]
				if userValCurr == "" {
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
					Description: "Connection parameters for the database server",
					Fields:      connFields,
				}
				stepDb := WizardStep{
					Title:       "Target Database",
					Description: "Select target database from server",
					Fields: []WizardField{
						{Key: "database", Label: "Database Name", Placeholder: "app_prod", Value: vals["database"], Type: FieldText},
					},
				}
				wiz.SetSteps([]WizardStep{wiz.Steps[0], connStep, stepDb, storageStep})
			}
		} else if currentStep == 1 && engine != "sqlite" {
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

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			dbs, err := s.svc.ListDatabases(ctx, engine, config.Conn{
				Host: host,
				Port: port,
				User: user,
				TLS:  useTLS,
			}, pwd)
			cancel()

			if err == nil && len(dbs) > 0 {
				selectedDbIdx := 0
				existingDbName := vals["database"]
				for idx, d := range dbs {
					if d == existingDbName {
						selectedDbIdx = idx
						break
					}
				}
				wiz.Steps[2] = WizardStep{
					Title:       "Target Database",
					Description: fmt.Sprintf("Discovered %d databases on %s:%d — choose target database", len(dbs), host, port),
					Fields: []WizardField{
						{Key: "database", Label: "Database Name", Type: FieldSelect, Options: dbs, SelectedOpt: selectedDbIdx},
					},
				}
				InitFields(wiz.Steps[2].Fields)
			} else {
				desc := "Enter target database name manually"
				if err != nil {
					desc = fmt.Sprintf("Could not list databases (%s) — enter manually", err.Error())
				}
				wiz.Steps[2] = WizardStep{
					Title:       "Target Database",
					Description: desc,
					Fields: []WizardField{
						{Key: "database", Label: "Database Name", Placeholder: "app_prod", Value: vals["database"], Type: FieldText},
					},
				}
				InitFields(wiz.Steps[2].Fields)
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
		} else if stepIndex == 2 && engine != "sqlite" {
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

	sched := parseScheduleSelection(vals["schedule"], vals["custom_cron"])

	job := config.Job{
		Name:   name,
		Engine: engine,
		Conn: config.Conn{
			Host:     host,
			Port:     port,
			User:     strings.TrimSpace(vals["user"]),
			Database: strings.TrimSpace(vals["database"]),
			Path:     strings.TrimSpace(vals["path"]),
			TLS:      vals["tls"] == "true",
		},
		Storage:   vals["storage"],
		Retention: config.Retention{KeepLast: keepLast},
		Compress:  vals["compress"] == "true",
		Encrypt:   vals["encrypt"] == "true",
		Schedule:  sched,
	}

	// Store password in vault if provided
	if pwd := vals["password"]; pwd != "" && s.svc.Vault != nil {
		s.svc.Vault.Set("job/"+name+"/password", pwd)
		_ = s.svc.Vault.Save()
	}

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
		_ = s.svc.SaveConfig()
		s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Updated job %q successfully", name))
	} else {
		s.svc.Cfg.Jobs = append(s.svc.Cfg.Jobs, job)
		_ = s.svc.SaveConfig()
		s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Added job %q", name))
	}
}

func (s JobsScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("dbk — Backup Jobs"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Manage and execute database backup configurations"))
	b.WriteString("\n\n")

	jobs := s.svc.Cfg.Jobs
	if len(jobs) == 0 {
		b.WriteString(styleHelp.Render("No jobs configured yet. Press 'a' to add a job."))
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

			schedStr := ""
			if j.Schedule != "" {
				schedStr = styleWarning.Render(fmt.Sprintf("⏰ %s", j.Schedule))
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

			details := fmt.Sprintf("(%s → %s, keep %d)", j.Engine, j.Storage, j.Retention.KeepLast)
			line := fmt.Sprintf("%s%s  %s  %s  %s  %s\n",
				prefix,
				itemStyle.Render(j.Name),
				styleSubtitle.Render(details),
				featStr,
				schedStr,
				statusBadge,
			)
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(styleHelp.Render("r: Run • a: Add • e: Edit • d: Delete • b: Restore • Esc: Back"))
	return RenderBox(b.String(), s.width)
}
