package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type SettingsOption int

const (
	optScheduleInstall SettingsOption = iota
	optScheduleRemove
	optExportConfig
	optImportConfig
	optCheckUpdate
)

type SettingsScreen struct {
	svc           *Service
	selected      int
	statusMsg     string
	isWizardOpen  bool
	activeOpt     SettingsOption
	wizard        WizardModel
	width         int
	height        int
	onBack        func() tea.Cmd
	onCheckUpdate func() tea.Cmd
}

func NewSettingsScreen(svc *Service, onBack func() tea.Cmd, onCheckUpdate func() tea.Cmd) SettingsScreen {
	return SettingsScreen{
		svc:           svc,
		selected:      0,
		onBack:        onBack,
		onCheckUpdate: onCheckUpdate,
	}
}

func (s *SettingsScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	if s.isWizardOpen {
		s.wizard.SetSize(width, height)
	}
}

func (s SettingsScreen) Update(msg tea.Msg) (SettingsScreen, tea.Cmd) {
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
			s.handleSettingsWizardFinish(s.activeOpt, s.wizard.Values)
		} else if s.wizard.Canceled {
			s.isWizardOpen = false
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		optionsCount := 5
		switch msg.String() {
		case "esc":
			if s.onBack != nil {
				return s, s.onBack()
			}
		case "up", "k":
			if s.selected > 0 {
				s.selected--
			} else {
				s.selected = optionsCount - 1
			}
		case "down", "j":
			if s.selected < optionsCount-1 {
				s.selected++
			} else {
				s.selected = 0
			}
		case "enter":
			cmd := s.triggerOption(SettingsOption(s.selected))
			return s, cmd
		}
	}
	return s, nil
}

func (s *SettingsScreen) triggerOption(opt SettingsOption) tea.Cmd {
	s.activeOpt = opt
	switch opt {
	case optScheduleInstall:
		s.openScheduleInstallWizard()
	case optScheduleRemove:
		s.openScheduleRemoveWizard()
	case optExportConfig:
		s.openExportWizard()
	case optImportConfig:
		s.openImportWizard()
	case optCheckUpdate:
		if s.onCheckUpdate != nil {
			return s.onCheckUpdate()
		}
	}
	return nil
}

func (s *SettingsScreen) openScheduleInstallWizard() {
	jobNames := make([]string, 0)
	for _, j := range s.svc.Cfg.Jobs {
		jobNames = append(jobNames, j.Name)
	}
	if len(jobNames) == 0 {
		s.statusMsg = styleWarning.Render("No jobs available to schedule")
		return
	}

	steps := []WizardStep{
		{
			Title:       "Schedule Automated Backup",
			Description: "Select job and cron expression (e.g. '0 3 * * *' for 3 AM daily)",
			Fields: []WizardField{
				{Key: "job", Label: "Job", Type: FieldSelect, Options: jobNames},
				{Key: "cron", Label: "Cron Expression", Placeholder: "0 3 * * *", Type: FieldText},
			},
		},
	}
	s.wizard = NewWizard("Scheduling", steps, nil, nil)
	s.isWizardOpen = true
}

func (s *SettingsScreen) openScheduleRemoveWizard() {
	jobNames := make([]string, 0)
	for _, j := range s.svc.Cfg.Jobs {
		jobNames = append(jobNames, j.Name)
	}
	if len(jobNames) == 0 {
		s.statusMsg = styleWarning.Render("No jobs available")
		return
	}

	steps := []WizardStep{
		{
			Title:       "Remove Scheduled Backup",
			Description: "Select job schedule to remove from system scheduler",
			Fields: []WizardField{
				{Key: "job", Label: "Job to Remove", Type: FieldSelect, Options: jobNames},
			},
		},
	}
	s.wizard = NewWizard("Scheduling", steps, nil, nil)
	s.isWizardOpen = true
}

func (s *SettingsScreen) openExportWizard() {
	steps := []WizardStep{
		{
			Title:       "Export Configuration Bundle",
			Description: "Create a portable YAML/JSON bundle",
			Fields: []WizardField{
				{Key: "dest", Label: "Destination File", Placeholder: "dbk-export.yaml", Type: FieldText},
				{Key: "include_secrets", Label: "Include Re-encrypted Secrets", Type: FieldCheckbox, Checked: true},
				{Key: "transfer_pass", Label: "Transfer Passphrase", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
			},
		},
	}
	s.wizard = NewWizard("Config Export", steps, nil, nil)
	s.isWizardOpen = true
}

func (s *SettingsScreen) openImportWizard() {
	steps := []WizardStep{
		{
			Title:       "Import Configuration Bundle",
			Description: "Merge a portable bundle into current configuration",
			Fields: []WizardField{
				{Key: "src", Label: "Source File", Placeholder: "dbk-export.yaml", Type: FieldText},
				{Key: "transfer_pass", Label: "Transfer Passphrase (if encrypted)", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
				{Key: "overwrite", Label: "Overwrite Conflicting Items", Type: FieldCheckbox, Checked: false},
			},
		},
	}
	s.wizard = NewWizard("Config Import", steps, nil, nil)
	s.isWizardOpen = true
}

func (s *SettingsScreen) handleSettingsWizardFinish(opt SettingsOption, vals map[string]string) {
	switch opt {
	case optScheduleInstall:
		job := vals["job"]
		cron := vals["cron"]
		if err := s.svc.InstallSchedule(job, cron); err != nil {
			s.statusMsg = styleError.Render("✖ Schedule install failed: " + err.Error())
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Automated schedule installed for job %q!", job))
		}

	case optScheduleRemove:
		job := vals["job"]
		if err := s.svc.RemoveSchedule(job); err != nil {
			s.statusMsg = styleError.Render("✖ Schedule removal failed: " + err.Error())
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Schedule removed for job %q!", job))
		}

	case optExportConfig:
		dest := strings.TrimSpace(vals["dest"])
		if dest == "" {
			s.statusMsg = styleError.Render("Destination path cannot be empty")
			return
		}
		includeSec := vals["include_secrets"] == "true"
		pass := vals["transfer_pass"]
		if err := s.svc.ExportBundle(dest, pass, includeSec); err != nil {
			s.statusMsg = styleError.Render("✖ Export failed: " + err.Error())
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Export saved to %s!", dest))
		}

	case optImportConfig:
		src := strings.TrimSpace(vals["src"])
		if src == "" {
			s.statusMsg = styleError.Render("Source path cannot be empty")
			return
		}
		pass := vals["transfer_pass"]
		ow := vals["overwrite"] == "true"
		res, err := s.svc.ImportBundle(src, pass, ow)
		if err != nil {
			s.statusMsg = styleError.Render("✖ Import failed: " + err.Error())
		} else {
			_ = s.svc.SaveConfig()
			if s.svc.Vault != nil {
				_ = s.svc.Vault.Save()
			}
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Imported: %d jobs, %d storages, %d notifiers",
				res.ConfigResult.JobsAdded+res.ConfigResult.JobsUpdated,
				res.ConfigResult.StoragesAdded+res.ConfigResult.StoragesUpdated,
				res.ConfigResult.NotifiersAdded+res.ConfigResult.NotifiersUpdated))
		}
	}
}

func (s SettingsScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("dbk — System Settings"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Manage automated schedules, portable bundles, and system updates"))
	b.WriteString("\n\n")

	options := []struct {
		title string
		desc  string
	}{
		{"Install Automated Job Schedule", "Register unattended backup timer with OS scheduler (systemd/cron/Task Scheduler)"},
		{"Remove Automated Job Schedule", "Unregister scheduled timer from OS scheduler"},
		{"Export Configuration Bundle", "Export portable YAML/JSON config bundle with optional transfer-passphrase secrets"},
		{"Import Configuration Bundle", "Merge external configuration bundle and re-encrypt secrets into local vault"},
		{"Check for Software Updates", "Check for newer releases of dbk on GitHub and upgrade in-place"},
	}

	for i, opt := range options {
		prefix := "  "
		itemStyle := styleNormal
		if i == s.selected {
			prefix = styleSelected.Render("▶ ")
			itemStyle = styleSelected
		}
		b.WriteString(fmt.Sprintf("%s%s\n    %s\n\n", prefix, itemStyle.Render(opt.title), styleSubtitle.Render(opt.desc)))
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(styleHelp.Render("↑/k, ↓/j: Navigate • Enter: Open • Esc: Back"))
	return RenderBox(b.String(), s.width)
}

func (s *SettingsScreen) SetStatusMsg(msg string) {
	s.statusMsg = msg
}
