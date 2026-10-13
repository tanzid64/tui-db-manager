package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type LogsScreen struct {
	selected int
	onBack   func() tea.Cmd
}

func NewLogsScreen(onBack func() tea.Cmd) LogsScreen {
	return LogsScreen{
		selected: 0,
		onBack:   onBack,
	}
}

func (s LogsScreen) Update(msg tea.Msg) (LogsScreen, tea.Cmd) {
	records := globalHistory.All()
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if s.onBack != nil {
				return s, s.onBack()
			}
		case "up", "k":
			if len(records) > 0 {
				if s.selected > 0 {
					s.selected--
				} else {
					s.selected = len(records) - 1
				}
			}
		case "down", "j":
			if len(records) > 0 {
				if s.selected < len(records)-1 {
					s.selected++
				} else {
					s.selected = 0
				}
			}
		}
	}
	return s, nil
}

func (s LogsScreen) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("dbk — Execution Logs & History"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("History of recent database backup and restore operations"))
	b.WriteString("\n\n")

	records := globalHistory.All()
	if len(records) == 0 {
		b.WriteString(styleHelp.Render("No operations have been recorded yet this session."))
		b.WriteString("\n\n")
	} else {
		for i, rec := range records {
			prefix := "  "
			itemStyle := styleNormal
			if i == s.selected {
				prefix = styleSelected.Render("▶ ")
				itemStyle = styleSelected
			}

			statusBadge := styleSuccess.Render("✔ " + string(rec.Status))
			if rec.Status == StatusFailed {
				statusBadge = styleError.Render("✖ " + string(rec.Status))
			} else if rec.Status == StatusRunning {
				statusBadge = styleWarning.Render("⟳ " + string(rec.Status))
			}

			timeStr := rec.Started.Format("2006-01-02 15:04:05")
			b.WriteString(fmt.Sprintf("%s%s  %s  %s  %s  %s\n",
				prefix,
				itemStyle.Render(timeStr),
				statusBadge,
				styleFieldLabel.Render(rec.Action),
				rec.Job,
				styleHelp.Render(rec.Duration.Round(100).String()),
			))

			if rec.Error != "" {
				b.WriteString(fmt.Sprintf("    %s\n", styleError.Render("Error: "+rec.Error)))
			}
			if rec.Target != "" {
				b.WriteString(fmt.Sprintf("    %s\n", styleSubtitle.Render("Target: "+rec.Target)))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString(styleHelp.Render("↑/k, ↓/j: Scroll history • Esc: Back"))
	return styleBox.Render(b.String())
}
