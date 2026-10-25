package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// schedulePreset is a common backup frequency offered in the schedule picker.
type schedulePreset struct {
	Label string
	Cron  string
}

var schedulePresets = []schedulePreset{
	{"Every day at 03:00", "0 3 * * *"},
	{"Every 12 hours", "0 */12 * * *"},
	{"Every 6 hours", "0 */6 * * *"},
	{"Every hour", "0 * * * *"},
	{"Every Sunday at 03:00", "0 3 * * 0"},
}

const customPresetLabel = "Custom (cron expression)"

// describeCron turns a cron expression into a short readable label.
func describeCron(expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "not scheduled"
	}
	for _, p := range schedulePresets {
		if p.Cron == expr {
			return p.Label
		}
	}
	return "cron: " + expr
}

// ScheduleScreen lists jobs with their automatic backup schedule.
type ScheduleScreen struct {
	svc          *Service
	selected     int
	statusMsg    string
	isWizardOpen bool
	wizard       WizardModel
	wizardJob    string
	width        int
	height       int
	onBack       func() tea.Cmd
}

func NewScheduleScreen(svc *Service, onBack func() tea.Cmd) ScheduleScreen {
	return ScheduleScreen{svc: svc, onBack: onBack}
}

func (s *ScheduleScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	if s.isWizardOpen {
		s.wizard.SetSize(width, height)
	}
}

func (s ScheduleScreen) Update(msg tea.Msg) (ScheduleScreen, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		s.SetSize(ws.Width, ws.Height)
		return s, nil
	}

	if s.isWizardOpen {
		var cmd tea.Cmd
		s.wizard, cmd = s.wizard.Update(msg)
		if s.wizard.Done {
			s.isWizardOpen = false
			s.applySchedule(s.wizardJob, s.wizard.Values)
		} else if s.wizard.Canceled {
			s.isWizardOpen = false
		}
		return s, cmd
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	jobs := s.svc.Cfg.Jobs
	switch key.String() {
	case "esc", "q":
		if s.onBack != nil {
			return s, s.onBack()
		}
	case "up", "k":
		if len(jobs) > 0 {
			s.selected = (s.selected - 1 + len(jobs)) % len(jobs)
		}
	case "down", "j":
		if len(jobs) > 0 {
			s.selected = (s.selected + 1) % len(jobs)
		}
	case "enter", "s":
		if s.selected < len(jobs) {
			s.openWizard(jobs[s.selected].Name, jobs[s.selected].Schedule)
		}
	case "x", "d":
		if s.selected < len(jobs) {
			name := jobs[s.selected].Name
			if err := s.svc.RemoveSchedule(name); err != nil {
				s.statusMsg = styleError.Render("✖ Could not remove schedule: " + err.Error())
			} else {
				s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Automatic backups turned off for %q", name))
			}
		}
	}
	return s, nil
}

func (s *ScheduleScreen) openWizard(jobName, current string) {
	options := make([]string, 0, len(schedulePresets)+1)
	selected := 0
	custom := ""
	matched := current == ""
	for i, p := range schedulePresets {
		options = append(options, p.Label)
		if p.Cron == current {
			selected = i
			matched = true
		}
	}
	options = append(options, customPresetLabel)
	if !matched {
		selected = len(options) - 1
		custom = current
	}

	steps := []WizardStep{{
		Title:       "How often should " + jobName + " be backed up?",
		Description: "Use ←/→ to pick a frequency. For Custom, enter a cron expression like '30 2 * * *'.",
		Fields: []WizardField{
			{Key: "preset", Label: "Frequency", Type: FieldSelect, Options: options, SelectedOpt: selected},
			{Key: "cron", Label: "Custom cron (only for Custom)", Placeholder: "30 2 * * *", Value: custom, Type: FieldText},
		},
	}}
	w := NewWizard("Automatic Backup Schedule", steps, nil, nil)
	if s.width > 0 {
		w.SetSize(s.width, s.height)
	}
	w.ValidateStep = func(_ int, vals map[string]string) error {
		if vals["preset"] != customPresetLabel {
			return nil
		}
		if len(strings.Fields(vals["cron"])) != 5 {
			return errors.New("cron needs 5 fields: minute hour day month weekday (e.g. '30 2 * * *')")
		}
		return nil
	}
	s.wizard = w
	s.wizardJob = jobName
	s.isWizardOpen = true
}

func (s *ScheduleScreen) applySchedule(jobName string, vals map[string]string) {
	expr := ""
	for _, p := range schedulePresets {
		if p.Label == vals["preset"] {
			expr = p.Cron
		}
	}
	if expr == "" {
		expr = strings.Join(strings.Fields(vals["cron"]), " ")
	}
	if err := s.svc.InstallSchedule(jobName, expr); err != nil {
		s.statusMsg = styleError.Render("✖ Could not install schedule: " + err.Error())
		return
	}
	s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ %q will back up %s (via %s). Log: %s",
		jobName, strings.ToLower(describeCron(expr)), s.svc.SchedulerName(), s.svc.ScheduleLogFile(jobName)))
}

func (s ScheduleScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render("Schedules"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Automatic backups run in the background via " + s.svc.SchedulerName() + ", even after you log out"))
	b.WriteString("\n\n")

	jobs := s.svc.Cfg.Jobs
	if len(jobs) == 0 {
		b.WriteString(styleHelp.Render("No backup jobs yet. Add one under Backup Jobs first."))
		b.WriteString("\n\n")
	}
	for i, j := range jobs {
		prefix, itemStyle := "  ", styleNormal
		if i == s.selected {
			prefix, itemStyle = styleSelected.Render("▶ "), styleSelected
		}
		state := styleHelp.Render("not scheduled")
		if j.Schedule != "" {
			if st, err := s.svc.ScheduleStatus(j.Name); err == nil && st.Installed {
				state = styleSuccess.Render("● " + describeCron(j.Schedule))
			} else {
				state = styleWarning.Render("⚠ " + describeCron(j.Schedule) + " (not installed — press Enter to fix)")
			}
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", prefix, itemStyle.Render(fmt.Sprintf("%-20s", j.Name)), state))
	}
	b.WriteString("\n")
	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}
	b.WriteString(styleHelp.Render("Enter: Set schedule • x: Turn off • Esc: Back"))
	return RenderBox(b.String(), s.width)
}
