package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type LogsScreen struct {
	selected int
	width    int
	height   int
	onBack   func() tea.Cmd
}

func NewLogsScreen(onBack func() tea.Cmd) LogsScreen {
	return LogsScreen{
		selected: 0,
		onBack:   onBack,
	}
}

func (s *LogsScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s LogsScreen) Update(msg tea.Msg) (LogsScreen, tea.Cmd) {
	records := recentFirst(globalHistory.All(), 20)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.SetSize(msg.Width, msg.Height)
		return s, nil
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
	b.WriteString(styleTitle.Render("History"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Last 20 backups and restores, newest first"))
	b.WriteString("\n\n")

	records := recentFirst(globalHistory.All(), 20)
	if len(records) == 0 {
		b.WriteString(styleHelp.Render("Nothing has run yet. Backups from the TUI, CLI and schedules all show up here."))
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
	return RenderBox(b.String(), s.width)
}

// recentFirst returns up to n records ordered newest first.
func recentFirst(recs []RunRecord, n int) []RunRecord {
	out := append([]RunRecord(nil), recs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
