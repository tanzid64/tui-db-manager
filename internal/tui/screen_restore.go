package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/restore"
)

type restoreState int

const (
	stateSelectJob restoreState = iota
	stateBrowseBackups
	stateConfirmRestore
)

type RestoreScreen struct {
	svc          *Service
	state        restoreState
	selectedJob  int
	selectedObj  int
	backups      []restore.Backup
	statusMsg    string
	loading      bool
	toDBInput    textinput.Model
	confirmInput textinput.Model
	width        int
	height       int
	onBack       func() tea.Cmd
}

type backupListResultMsg struct {
	backups  []restore.Backup
	warnings []error
}

type restoreResultMsg struct {
	err error
}

func NewRestoreScreen(svc *Service, onBack func() tea.Cmd) RestoreScreen {
	toDB := textinput.New()
	toDB.Placeholder = "Target database (leave empty for original)"

	conf := textinput.New()
	conf.Placeholder = "Type RESTORE to confirm"

	return RestoreScreen{
		svc:          svc,
		state:        stateSelectJob,
		selectedJob:  0,
		selectedObj:  0,
		backups:      []restore.Backup{},
		toDBInput:    toDB,
		confirmInput: conf,
		onBack:       onBack,
	}
}

func (s *RestoreScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	s.toDBInput.Width = ResponsiveWidth(width) - 20
	s.confirmInput.Width = ResponsiveWidth(width) - 20
}

func (s *RestoreScreen) SelectJob(jobName string) tea.Cmd {
	jobs := s.svc.Cfg.Jobs
	for i, j := range jobs {
		if j.Name == jobName {
			s.selectedJob = i
			s.state = stateBrowseBackups
			s.loading = true
			return s.fetchBackups(jobName)
		}
	}
	return nil
}

func (s *RestoreScreen) fetchBackups(jobName string) tea.Cmd {
	return func() tea.Msg {
		backups, warnings := s.svc.ListBackups(context.Background(), jobName)
		return backupListResultMsg{backups: backups, warnings: warnings}
	}
}

func (s RestoreScreen) Update(msg tea.Msg) (RestoreScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.SetSize(msg.Width, msg.Height)
		return s, nil
	case backupListResultMsg:
		s.loading = false
		s.backups = msg.backups
		s.selectedObj = 0
		var parts []string
		for _, w := range msg.warnings {
			parts = append(parts, "⚠ Could not read "+w.Error())
		}
		if len(msg.backups) == 0 && len(msg.warnings) == 0 {
			parts = append(parts, "No backups found for this job in any storage.")
		}
		s.statusMsg = ""
		if len(parts) > 0 {
			s.statusMsg = styleWarning.Render(strings.Join(parts, "\n"))
		}
		return s, nil

	case restoreResultMsg:
		s.loading = false
		if msg.err != nil {
			s.statusMsg = styleError.Render("✖ Restore failed: " + msg.err.Error())
		} else {
			s.statusMsg = styleSuccess.Render("✔ Restore finished — the database now matches the selected backup")
			s.state = stateBrowseBackups
		}
		return s, nil

	case rowClickMsg:
		// Click selects; clicking the selected row again opens it (the
		// restore itself still needs RESTORE typed on the next screen).
		if s.loading {
			return s, nil
		}
		activate := false
		switch s.state {
		case stateSelectJob:
			activate = clickRow(&s.selectedJob, msg.index, len(s.svc.Cfg.Jobs))
		case stateBrowseBackups:
			activate = clickRow(&s.selectedObj, msg.index, len(s.backups))
		}
		if activate {
			return s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		}
		return s, nil

	case fieldClickMsg:
		if s.state == stateConfirmRestore {
			if msg.index == 0 {
				s.confirmInput.Blur()
				s.toDBInput.Focus()
			} else {
				s.toDBInput.Blur()
				s.confirmInput.Focus()
			}
		}
		return s, nil

	case tea.KeyMsg:
		if s.loading && msg.String() != "esc" {
			return s, nil
		}
		switch msg.String() {
		case "tab", "shift+tab":
			if s.state == stateConfirmRestore {
				if s.confirmInput.Focused() {
					s.confirmInput.Blur()
					s.toDBInput.Focus()
				} else {
					s.toDBInput.Blur()
					s.confirmInput.Focus()
				}
				return s, nil
			}
		case "esc":
			switch s.state {
			case stateConfirmRestore:
				s.state = stateBrowseBackups
				s.statusMsg = ""
				return s, nil
			case stateBrowseBackups:
				s.state = stateSelectJob
				s.statusMsg = ""
				return s, nil
			case stateSelectJob:
				if s.onBack != nil {
					return s, s.onBack()
				}
			}

		case "up", "k":
			if s.state == stateSelectJob {
				jobs := s.svc.Cfg.Jobs
				if len(jobs) > 0 {
					if s.selectedJob > 0 {
						s.selectedJob--
					} else {
						s.selectedJob = len(jobs) - 1
					}
				}
			} else if s.state == stateBrowseBackups {
				if len(s.backups) > 0 {
					if s.selectedObj > 0 {
						s.selectedObj--
					} else {
						s.selectedObj = len(s.backups) - 1
					}
				}
			}

		case "down", "j":
			if s.state == stateSelectJob {
				jobs := s.svc.Cfg.Jobs
				if len(jobs) > 0 {
					if s.selectedJob < len(jobs)-1 {
						s.selectedJob++
					} else {
						s.selectedJob = 0
					}
				}
			} else if s.state == stateBrowseBackups {
				if len(s.backups) > 0 {
					if s.selectedObj < len(s.backups)-1 {
						s.selectedObj++
					} else {
						s.selectedObj = 0
					}
				}
			}

		case "enter":
			if s.state == stateSelectJob {
				jobs := s.svc.Cfg.Jobs
				if len(jobs) > 0 && s.selectedJob < len(jobs) {
					s.state = stateBrowseBackups
					s.loading = true
					s.statusMsg = styleWarning.Render("⟳ Loading backups from storage backend...")
					return s, s.fetchBackups(jobs[s.selectedJob].Name)
				}
			} else if s.state == stateBrowseBackups {
				if len(s.backups) > 0 && s.selectedObj < len(s.backups) {
					s.state = stateConfirmRestore
					s.toDBInput.Reset()
					s.toDBInput.Blur()
					s.confirmInput.Reset()
					s.confirmInput.Focus()
					s.statusMsg = ""
					return s, nil
				}
			} else if s.state == stateConfirmRestore {
				if strings.TrimSpace(s.confirmInput.Value()) != "RESTORE" {
					s.statusMsg = styleError.Render("You must type RESTORE exactly to confirm destructive overwrite.")
					return s, nil
				}
				jobs := s.svc.Cfg.Jobs
				jobName := jobs[s.selectedJob].Name
				chosen := s.backups[s.selectedObj]
				toDB := strings.TrimSpace(s.toDBInput.Value())

				s.loading = true
				s.statusMsg = styleWarning.Render("⟳ Executing restore pipeline...")
				return s, func() tea.Msg {
					err := s.svc.RunRestore(context.Background(), jobName, chosen.Storage, chosen.Key, toDB)
					return restoreResultMsg{err: err}
				}
			}
		}
	}

	if s.state == stateConfirmRestore {
		var cmd1, cmd2 tea.Cmd
		s.toDBInput, cmd1 = s.toDBInput.Update(msg)
		s.confirmInput, cmd2 = s.confirmInput.Update(msg)
		return s, tea.Batch(cmd1, cmd2)
	}

	return s, nil
}

func (s RestoreScreen) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Restore"))
	b.WriteString("\n")

	switch s.state {
	case stateSelectJob:
		b.WriteString(styleSubtitle.Render("Pick the job whose backup you want to restore:"))
		b.WriteString("\n\n")

		jobs := s.svc.Cfg.Jobs
		if len(jobs) == 0 {
			b.WriteString(styleHelp.Render("No jobs configured yet."))
			b.WriteString("\n\n")
		} else {
			for i, j := range jobs {
				prefix := "  "
				itemStyle := styleNormal
				if i == s.selectedJob {
					prefix = styleSelected.Render("▶ ")
					itemStyle = styleSelected
				}
				b.WriteString(markRow(i, fmt.Sprintf("%s%s  %s", prefix, itemStyle.Render(j.Name), styleSubtitle.Render(fmt.Sprintf("(%s, storage: %s)", j.Engine, j.Storage)))) + "\n")
			}
			b.WriteString("\n")
		}

	case stateBrowseBackups:
		jobs := s.svc.Cfg.Jobs
		jobName := "unknown"
		if len(jobs) > s.selectedJob {
			jobName = jobs[s.selectedJob].Name
		}
		b.WriteString(styleSubtitle.Render(fmt.Sprintf("Backups for job %s (from all storages, newest first):", styleSelected.Render(jobName))))
		b.WriteString("\n\n")

		if s.loading {
			b.WriteString(styleWarning.Render("⟳ Loading remote backups..."))
			b.WriteString("\n\n")
		} else if len(s.backups) == 0 {
			b.WriteString(styleHelp.Render("No backups available for this job."))
			b.WriteString("\n\n")
		} else {
			for i, obj := range s.backups {
				prefix := "  "
				itemStyle := styleNormal
				if i == s.selectedObj {
					prefix = styleSelected.Render("▶ ")
					itemStyle = styleSelected
				}
				modStr := obj.ModTime.Local().Format("2006-01-02 15:04")
				if obj.ModTime.IsZero() {
					modStr = "-"
				}
				latest := ""
				if i == 0 {
					latest = styleSuccess.Render(" (latest)")
				}
				b.WriteString(markRow(i, fmt.Sprintf("%s%s  %s  %s  %s%s",
					prefix,
					itemStyle.Render(modStr),
					styleSubtitle.Render(fmt.Sprintf("%-9s", humanSize(obj.Size))),
					styleFieldLabel.Render("["+obj.Storage+"]"),
					styleHelp.Render(obj.Key),
					latest,
				)) + "\n")
			}
			b.WriteString("\n")
		}

	case stateConfirmRestore:
		jobs := s.svc.Cfg.Jobs
		jobName := jobs[s.selectedJob].Name
		backupKey := s.backups[s.selectedObj].Key
		fromStorage := s.backups[s.selectedObj].Storage

		target := jobs[s.selectedJob].Conn.Database
		if jobs[s.selectedJob].Conn.Path != "" {
			target = jobs[s.selectedJob].Conn.Path
		}
		b.WriteString(styleSubtitle.Render("Confirm restore"))
		b.WriteString("\n\n")
		b.WriteString(fmt.Sprintf("Backup:   %s\nFrom:     %s storage\nJob:      %s\nInto:     %s\n\n", backupKey, fromStorage, jobName, target))
		b.WriteString(styleWarning.Render("⚠ This replaces the current data in that database."))
		b.WriteString("\n\n")

		toDB, confirm := s.toDBInput.View(), s.confirmInput.View()
		if s.toDBInput.Focused() {
			toDB += focusMark
		} else {
			confirm += focusMark
		}
		b.WriteString(markZone("field:0", styleFieldLabel.Render("Restore into a different database instead (optional, Tab to edit):")+"\n"+toDB))
		b.WriteString("\n\n")

		b.WriteString(markZone("field:1", styleFieldLabel.Render("Confirmation (type RESTORE):")+"\n"+confirm))
		b.WriteString("\n\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	if s.state == stateConfirmRestore {
		b.WriteString(hintBar(
			keyHint("enter", "Type RESTORE then Enter"),
			keyHint("tab", "Tab: Switch field"),
			keyHint("esc", "Esc: Cancel"),
		))
	} else {
		b.WriteString(hintBar(
			styleHelp.Render("↑/↓: Navigate"),
			keyHint("enter", "Enter: Select"),
			keyHint("esc", "Esc: Back"),
		))
	}

	return RenderBox(b.String(), s.width)
}
