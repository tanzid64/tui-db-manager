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

// ResponsiveWidth calculates an optimal content/box width based on terminal width.
func ResponsiveWidth(termWidth int) int {
	if termWidth <= 0 {
		return 80
	}
	w := termWidth - 4
	if w < 48 {
		w = 48
	}
	if w > 120 {
		w = 120
	}
	return w
}

// RenderBox wraps content in a rounded border box scaled responsively to termWidth.
func RenderBox(content string, termWidth int) string {
	b := styleBox.Copy()
	if termWidth > 0 {
		b = b.Width(ResponsiveWidth(termWidth))
	}
	return b.Render(content)
}

// RenderHeader renders the top title bar spanning the responsive width.
func RenderHeader(text string, termWidth int) string {
	h := styleHeader.Copy()
	if termWidth > 0 {
		h = h.Width(ResponsiveWidth(termWidth) + 2)
	}
	return h.Render(text)
}
