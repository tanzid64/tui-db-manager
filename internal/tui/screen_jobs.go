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
	svc          *Service
	selected     int
	statusMsg    string
	isWizardOpen bool
	wizard       WizardModel
	onBack       func() tea.Cmd
	onRestoreJob func(jobName string) tea.Cmd
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

func (s JobsScreen) Update(msg tea.Msg) (JobsScreen, tea.Cmd) {
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
			s.openAddJobWizard()
			return s, nil
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

func (s *JobsScreen) openAddJobWizard() {
	storageNames := make([]string, 0)
	for _, st := range s.svc.Cfg.Storages {
		storageNames = append(storageNames, st.Name)
	}
	if len(storageNames) == 0 {
		storageNames = append(storageNames, "default-local")
	}

	storageStep := WizardStep{
		Title:       "Storage & Retention",
		Description: "Select target storage backend and backup retention count",
		Fields: []WizardField{
			{Key: "storage", Label: "Storage Destination", Type: FieldSelect, Options: storageNames},
			{Key: "keep_last", Label: "Keep Last Backups", Placeholder: "7", Type: FieldText},
			{Key: "compress", Label: "Gzip Compress", Type: FieldCheckbox, Checked: true},
			{Key: "encrypt", Label: "Age Encrypt", Type: FieldCheckbox, Checked: false},
		},
	}

	stepIdentity := WizardStep{
		Title:       "Job Identity",
		Description: "Set the job name and target database engine",
		Fields: []WizardField{
			{Key: "name", Label: "Job Name", Placeholder: "production-mysql", Type: FieldText},
			{Key: "engine", Label: "Database Engine", Type: FieldSelect, Options: []string{"mysql", "postgres", "mongodb", "sqlite"}},
		},
	}

	stepConn := WizardStep{
		Title:       "Server Connection",
		Description: "Connection parameters for the database server",
		Fields: []WizardField{
			{Key: "host", Label: "Host", Placeholder: "127.0.0.1", Type: FieldText},
			{Key: "port", Label: "Port", Placeholder: "3306", Type: FieldText},
			{Key: "user", Label: "Username", Placeholder: "root", Type: FieldText},
			{Key: "password", Label: "Password (Vault)", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
			{Key: "tls", Label: "Use TLS", Type: FieldCheckbox, Checked: false},
		},
	}

	stepDb := WizardStep{
		Title:       "Target Database",
		Description: "Select target database from server",
		Fields: []WizardField{
			{Key: "database", Label: "Database Name", Placeholder: "app_prod", Type: FieldText},
		},
	}

	steps := []WizardStep{stepIdentity, stepConn, stepDb, storageStep}

	w := NewWizard(
		"Add New Backup Job",
		steps,
		nil,
		nil,
	)

	w.OnStepAdvance = func(wiz *WizardModel, currentStep int) {
		vals := wiz.CollectValues()
		engine := strings.ToLower(strings.TrimSpace(vals["engine"]))
		if engine == "" {
			engine = "mysql"
		}

		if currentStep == 0 {
			if engine == "sqlite" {
				sqliteStep := WizardStep{
					Title:       "SQLite Connection",
					Description: "Specify path to SQLite database file",
					Fields: []WizardField{
						{Key: "path", Label: "SQLite Path", Placeholder: "/path/to/db.sqlite", Type: FieldText},
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
				connFields := []WizardField{
					{Key: "host", Label: "Host", Placeholder: "127.0.0.1", Type: FieldText},
					{Key: "port", Label: "Port", Placeholder: portPlaceholder, Type: FieldText},
					{Key: "user", Label: "Username", Placeholder: userPlaceholder, Type: FieldText},
					{Key: "password", Label: "Password (Vault)", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
					{Key: "tls", Label: "Use TLS", Type: FieldCheckbox, Checked: false},
				}
				connStep := WizardStep{
					Title:       "Server Connection",
					Description: "Connection parameters for the database server",
					Fields:      connFields,
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
				wiz.Steps[2] = WizardStep{
					Title:       "Target Database",
					Description: fmt.Sprintf("Discovered %d databases on %s:%d — choose target database", len(dbs), host, port),
					Fields: []WizardField{
						{Key: "database", Label: "Database Name", Type: FieldSelect, Options: dbs},
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
						{Key: "database", Label: "Database Name", Placeholder: "app_prod", Type: FieldText},
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
				if existing.Name == name {
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
	}

	// Store password in vault if provided
	if pwd := vals["password"]; pwd != "" && s.svc.Vault != nil {
		s.svc.Vault.Set("job/"+name+"/password", pwd)
		_ = s.svc.Vault.Save()
	}

	s.svc.Cfg.Jobs = append(s.svc.Cfg.Jobs, job)
	_ = s.svc.SaveConfig()
	s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Added job %q", name))
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

			lastRun := globalHistory.LastForJob(j.Name)
			statusBadge := styleHelp.Render("(Never run)")
			if lastRun != nil {
				if lastRun.Status == StatusSuccess {
					statusBadge = styleSuccess.Render("✔ OK")
				} else {
					statusBadge = styleError.Render("✖ FAILED")
				}
			}

			b.WriteString(fmt.Sprintf("%s%s  %s  %s  %s\n",
				prefix,
				itemStyle.Render(j.Name),
				styleSubtitle.Render(fmt.Sprintf("(%s → %s, keep %d)", j.Engine, j.Storage, j.Retention.KeepLast)),
				featStr,
				statusBadge,
			))
		}
		b.WriteString("\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(styleHelp.Render("r: Run now • a: Add job • d: Delete • b: Backups/Restore • Esc: Back"))
	return styleBox.Render(b.String())
}
