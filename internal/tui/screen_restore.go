package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/storage"
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
	backups      []storage.Object
	statusMsg    string
	loading      bool
	toDBInput    textinput.Model
	confirmInput textinput.Model
	width        int
	height       int
	onBack       func() tea.Cmd
}

type backupListResultMsg struct {
	objs []storage.Object
	err  error
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
		backups:      []storage.Object{},
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
		objs, err := s.svc.ListBackups(context.Background(), jobName)
		return backupListResultMsg{objs: objs, err: err}
	}
}

func (s RestoreScreen) Update(msg tea.Msg) (RestoreScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.SetSize(msg.Width, msg.Height)
		return s, nil
	case backupListResultMsg:
		s.loading = false
		if msg.err != nil {
			s.statusMsg = styleError.Render("✖ Error listing backups: " + msg.err.Error())
			s.backups = nil
		} else {
			s.backups = msg.objs
			s.selectedObj = 0
			if len(msg.objs) == 0 {
				s.statusMsg = styleWarning.Render("No historical backups found for this job.")
			} else {
				s.statusMsg = ""
			}
		}
		return s, nil

	case restoreResultMsg:
		s.loading = false
		if msg.err != nil {
			s.statusMsg = styleError.Render("✖ Restore failed: " + msg.err.Error())
		} else {
			s.statusMsg = styleSuccess.Render("✔ Database successfully restored!")
			s.state = stateBrowseBackups
		}
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
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
				backupKey := s.backups[s.selectedObj].Key
				toDB := strings.TrimSpace(s.toDBInput.Value())

				s.loading = true
				s.statusMsg = styleWarning.Render("⟳ Executing restore pipeline...")
				return s, func() tea.Msg {
					err := s.svc.RunRestore(context.Background(), jobName, backupKey, toDB)
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
	b.WriteString(styleTitle.Render("dbk — Restore Database"))
	b.WriteString("\n")

	switch s.state {
	case stateSelectJob:
		b.WriteString(styleSubtitle.Render("Select a backup job to browse historical archives:"))
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
				b.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, itemStyle.Render(j.Name), styleSubtitle.Render(fmt.Sprintf("(%s, storage: %s)", j.Engine, j.Storage))))
			}
			b.WriteString("\n")
		}

	case stateBrowseBackups:
		jobs := s.svc.Cfg.Jobs
		jobName := "unknown"
		if len(jobs) > s.selectedJob {
			jobName = jobs[s.selectedJob].Name
		}
		b.WriteString(styleSubtitle.Render(fmt.Sprintf("Browsing backups for job: %s", styleSelected.Render(jobName))))
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
				modStr := obj.ModTime.Format("2006-01-02 15:04:05")
				if obj.ModTime.IsZero() {
					modStr = "-"
				}
				b.WriteString(fmt.Sprintf("%s%s  %s  %s\n",
					prefix,
					itemStyle.Render(obj.Key),
					styleSubtitle.Render(fmt.Sprintf("%d bytes", obj.Size)),
					styleHelp.Render(modStr),
				))
			}
			b.WriteString("\n")
		}

	case stateConfirmRestore:
		jobs := s.svc.Cfg.Jobs
		jobName := jobs[s.selectedJob].Name
		backupKey := s.backups[s.selectedObj].Key

		b.WriteString(styleSubtitle.Render("Destructive Database Restore Guard"))
		b.WriteString("\n\n")
		b.WriteString(styleWarning.Render(fmt.Sprintf("⚠ WARNING: Restoring %q into job %q will overwrite existing data!", backupKey, jobName)))
		b.WriteString("\n\n")

		b.WriteString(styleFieldLabel.Render("Target Database / Path override (optional):"))
		b.WriteString("\n")
		b.WriteString(s.toDBInput.View())
		b.WriteString("\n\n")

		b.WriteString(styleFieldLabel.Render("Confirmation (type RESTORE):"))
		b.WriteString("\n")
		b.WriteString(s.confirmInput.View())
		b.WriteString("\n\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	if s.state == stateConfirmRestore {
		b.WriteString(styleHelp.Render("Enter: Confirm & Restore • Esc: Cancel"))
	} else {
		b.WriteString(styleHelp.Render("↑/k, ↓/j: Navigate • Enter: Select • Esc: Back"))
	}

	return RenderBox(b.String(), s.width)
}
