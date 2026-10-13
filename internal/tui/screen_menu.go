package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type MenuItem struct {
	Title string
	Desc  string
}

type MenuScreen struct {
	items    []MenuItem
	selected int
	onSelect func(index int) tea.Cmd
}

func NewMenuScreen(onSelect func(index int) tea.Cmd) MenuScreen {
	items := []MenuItem{
		{Title: "Jobs", Desc: "Configure database backup jobs, run on-demand backups, and view status"},
		{Title: "Storage", Desc: "Manage storage destinations (Local, AWS S3, Cloudflare R2, Google Drive)"},
		{Title: "Notifications", Desc: "Manage notification channels (Telegram, Email, Webhooks)"},
		{Title: "Restore", Desc: "Browse historical backups, verify checksums, and safely restore databases"},
		{Title: "Settings", Desc: "Vault passphrase, scheduling (cron/systemd/Task Scheduler), export/import"},
		{Title: "Logs / History", Desc: "View recent backup runs, elapsed durations, sizes, and error details"},
		{Title: "Quit", Desc: "Exit dbk"},
	}
	return MenuScreen{
		items:    items,
		selected: 0,
		onSelect: onSelect,
	}
}

func (m MenuScreen) Update(msg tea.Msg) (MenuScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			} else {
				m.selected = len(m.items) - 1
			}
		case "down", "j":
			if m.selected < len(m.items)-1 {
				m.selected++
			} else {
				m.selected = 0
			}
		case "enter":
			if m.onSelect != nil {
				return m, m.onSelect(m.selected)
			}
		case "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m MenuScreen) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("dbk — Secure Database Backup"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Main Menu"))
	b.WriteString("\n\n")

	for i, it := range m.items {
		prefix := "  "
		itemStyle := styleNormal
		if i == m.selected {
			prefix = styleSelected.Render("▶ ")
			itemStyle = styleSelected
		}
		b.WriteString(fmt.Sprintf("%s%s\n", prefix, itemStyle.Render(it.Title)))
		b.WriteString(fmt.Sprintf("    %s\n\n", styleHelp.Render(it.Desc)))
	}

	b.WriteString(styleHelp.Render("↑/k, ↓/j: Navigate • Enter: Select • q: Quit"))
	return styleBox.Render(b.String())
}
