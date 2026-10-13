package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var (
	// Base Colors
	colorPrimary = lipgloss.Color("#7D56F4") // Purple/Indigo
	colorAccent  = lipgloss.Color("#04B575") // Emerald Green
	colorWarning = lipgloss.Color("#FFB86C") // Amber/Yellow
	colorError   = lipgloss.Color("#FF5555") // Red
	colorSubtle  = lipgloss.Color("#6272A4") // Muted Blue/Gray
	colorBgDark  = lipgloss.Color("#21222C") // Dark Background
	colorFgLight = lipgloss.Color("#F8F8F2") // Off-white

	// Styles
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFgLight).
			Background(colorPrimary).
			Padding(0, 1)

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	styleSelected = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorFgLight)

	styleError = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorError)

	styleSuccess = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	styleWarning = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWarning)

	styleHelp = lipgloss.NewStyle().
			Foreground(colorSubtle)

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(1, 2)

	styleFieldLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	styleActiveField = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(colorAccent).
				PaddingLeft(1)
)

func init() {
	if os.Getenv("NO_COLOR") != "" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}
