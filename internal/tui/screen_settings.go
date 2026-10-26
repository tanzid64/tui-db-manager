package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/config"
)

type SettingsOption int

const (
	optNotifications SettingsOption = iota
	optExportConfig
	optImportConfig
	optCheckUpdate
	optCleanUninstall
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
	// onOpenNotifications switches to the notifications screen.
	onOpenNotifications func() tea.Cmd
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
			if done := s.handleSettingsWizardFinish(s.activeOpt, s.wizard.Values); done != nil {
				cmd = tea.Batch(cmd, done)
			}
		} else if s.wizard.Canceled {
			s.isWizardOpen = false
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case rowClickMsg:
		// Settings entries open on a single click, like a menu.
		if msg.index >= 0 && msg.index < 5 {
			s.selected = msg.index
			return s, s.triggerOption(SettingsOption(s.selected))
		}
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
	case optNotifications:
		if s.onOpenNotifications != nil {
			return s.onOpenNotifications()
		}
	case optExportConfig:
		s.openExportWizard()
	case optImportConfig:
		s.openImportWizard()
	case optCheckUpdate:
		if s.onCheckUpdate != nil {
			return s.onCheckUpdate()
		}
	case optCleanUninstall:
		s.openUninstallWizard()
	}
	return nil
}

func (s *SettingsScreen) openExportWizard() {
	steps := []WizardStep{
		{
			Title:       "Export Settings Backup",
			Description: "Save jobs, storages, passwords and backup encryption keys to one file. Keep it off this server.",
			Fields: []WizardField{
				{Key: "dest", Label: "Save To", Placeholder: "~/dbk-settings-backup.yaml", Value: "~/dbk-settings-backup.yaml", Type: FieldText},
				{Key: "include_secrets", Label: "Include passwords & encryption keys (needed to restore encrypted backups)", Type: FieldCheckbox, Checked: true},
				{Key: "transfer_pass", Label: "Passphrase to protect the file", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
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

func (s *SettingsScreen) openUninstallWizard() {
	steps := []WizardStep{
		{
			Title:       "Clean Uninstall & Purge Data",
			Description: "WARNING: This will permanently delete your database, config, and schedules",
			Fields: []WizardField{
				{Key: "remove_schedules", Label: "Remove all automated backup schedules", Type: FieldCheckbox, Checked: true},
				{Key: "purge_data", Label: "Permanently delete database, config, and secrets", Type: FieldCheckbox, Checked: true},
				{Key: "remove_binary", Label: "Remove the dbk program", Type: FieldCheckbox, Checked: true},
			},
		},
	}
	s.wizard = NewWizard("Clean Uninstall", steps, nil, nil)
	s.isWizardOpen = true
}

// handleSettingsWizardFinish applies a finished wizard. It returns tea.Quit
// after an uninstall that removed the data or the program.
func (s *SettingsScreen) handleSettingsWizardFinish(opt SettingsOption, vals map[string]string) tea.Cmd {
	switch opt {
	case optExportConfig:
		dest := strings.TrimSpace(vals["dest"])
		if dest == "" {
			s.statusMsg = styleError.Render("Destination path cannot be empty")
			return nil
		}
		dest = config.ExpandPath(dest)
		if abs, err := filepath.Abs(dest); err == nil {
			dest = abs
		}
		includeSec := vals["include_secrets"] == "true"
		pass := vals["transfer_pass"]
		if includeSec && pass == "" {
			s.statusMsg = styleError.Render("✖ Enter a passphrase to protect the passwords and keys (you'll need it to import)")
			return nil
		}
		if err := s.svc.ExportBundle(dest, pass, includeSec); err != nil {
			s.statusMsg = styleError.Render("✖ Export failed: " + err.Error())
		} else if includeSec {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Settings, passwords and encryption keys saved to %s — copy it off this server", dest))
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Settings saved to %s (no passwords or keys)", dest))
		}

	case optImportConfig:
		src := strings.TrimSpace(vals["src"])
		if src == "" {
			s.statusMsg = styleError.Render("Source path cannot be empty")
			return nil
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

	case optCleanUninstall:
		removeSched := vals["remove_schedules"] == "true"
		purge := vals["purge_data"] == "true"
		removeBin := vals["remove_binary"] == "true"
		if !removeSched && !purge && !removeBin {
			s.statusMsg = styleHelp.Render("Uninstall cancelled: no actions selected")
			return nil
		}
		res, err := s.svc.Uninstall(context.Background(), removeSched, purge, removeBin)
		summary := uninstallSummary(res, err)
		if res.DataDir == "" && res.Binary == "" {
			// Nothing irreversible happened; stay in the app and show the result.
			if err != nil {
				s.statusMsg = styleError.Render("✖ " + strings.ReplaceAll(summary, "\n", " • "))
			} else {
				s.statusMsg = styleSuccess.Render("✔ " + strings.ReplaceAll(summary, "\n", " • "))
			}
			return nil
		}
		// The data or program is gone: quit, and print the summary in the
		// terminal so it isn't lost with the screen.
		exitMessage = summary
		return tea.Quit
	}
	return nil
}

// uninstallSummary describes what an uninstall did, one item per line.
func uninstallSummary(res UninstallResult, err error) string {
	var lines []string
	if n := len(res.SchedulesRemoved); n > 0 {
		lines = append(lines, fmt.Sprintf("Removed %d schedule(s)", n))
	}
	if res.DataDir != "" {
		lines = append(lines, "Deleted "+res.DataDir)
	}
	if res.Binary != "" {
		lines = append(lines, "Deleted "+res.Binary)
	}
	if err != nil {
		lines = append(lines, "Uninstall incomplete:")
		for _, e := range strings.Split(err.Error(), "\n") {
			lines = append(lines, "  ✖ "+e)
		}
	} else {
		lines = append(lines, "dbk uninstall complete.")
	}
	return strings.Join(lines, "\n")
}

func (s SettingsScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("Settings"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Notifications, portable config bundles, and updates"))
	b.WriteString("\n\n")

	options := []struct {
		title string
		desc  string
	}{
		{"Notifications", "Get a Telegram, email, or webhook message when a backup succeeds or fails"},
		{"Export Settings Backup", "Save jobs, storages, passwords and encryption keys to one file"},
		{"Import Configuration Bundle", "Merge external configuration bundle and re-encrypt secrets into local vault"},
		{"Check for Software Updates", "Check for newer releases of dbk on GitHub and upgrade in-place"},
		{"Clean Uninstall & Purge Data", "Remove all scheduled timers, configuration, secrets, and SQLite database"},
	}

	for i, opt := range options {
		prefix := "  "
		itemStyle := styleNormal
		if i == s.selected {
			prefix = styleSelected.Render("▶ ")
			itemStyle = styleSelected
		}
		b.WriteString(markRow(i, fmt.Sprintf("%s%s\n    %s", prefix, itemStyle.Render(opt.title), styleSubtitle.Render(opt.desc))) + "\n\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(hintBar(
		styleHelp.Render("↑/↓: Navigate"),
		keyHint("enter", "Enter: Open"),
		keyHint("esc", "Esc: Back"),
	))
	return RenderBox(b.String(), s.width)
}

func (s *SettingsScreen) SetStatusMsg(msg string) {
	s.statusMsg = msg
}
