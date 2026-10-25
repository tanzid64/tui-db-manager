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
	width    int
	height   int
	onSelect func(index int) tea.Cmd
	Notice   string // security warning shown above the menu
}

func NewMenuScreen(onSelect func(index int) tea.Cmd) MenuScreen {
	items := []MenuItem{
		{Title: "Backup Jobs", Desc: "Add a database, run a backup now, test the connection"},
		{Title: "Schedules", Desc: "Back up automatically (daily, hourly, ...)"},
		{Title: "Restore", Desc: "Pick a backup and restore it into the database"},
		{Title: "Storage", Desc: "Where backups are kept: local folder, S3/R2 bucket, Google Drive"},
		{Title: "History", Desc: "Recent backups and restores, with errors"},
		{Title: "Settings", Desc: "Notifications, export/import, updates, uninstall"},
		{Title: "Quit", Desc: "Exit dbk"},
	}
	return MenuScreen{
		items:    items,
		selected: 0,
		onSelect: onSelect,
	}
}

func (m *MenuScreen) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m MenuScreen) Update(msg tea.Msg) (MenuScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
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
	b.WriteString(styleTitle.Render("dbk — Database Backup"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Main Menu"))
	b.WriteString("\n\n")
	if m.Notice != "" {
		b.WriteString(styleWarning.Render("⚠ " + m.Notice))
		b.WriteString("\n\n")
	}

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

	b.WriteString(styleHelp.Render("↑/k, ↓/j: Navigate • Enter: Select • u: Updates • q: Quit"))
	return RenderBox(b.String(), m.width)
}
