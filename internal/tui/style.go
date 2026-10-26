package tui

import (
	"os"
	"strings"

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
	if w < 20 {
		w = 20
	}
	if w > 120 {
		w = 120
	}
	return w
}

// focusMark marks the line a screen wants kept on screen when the terminal is
// too short to show everything. It is zero-width and removed before display.
// Screens without one keep the selected "▶" row visible instead.
const focusMark = "\u200b"

// Set by App from the terminal size before each frame.
var (
	// boxMaxLines is how many content lines fit inside a box (0 = no limit).
	boxMaxLines int
	// compactLayout trims padding and spacing on short terminals.
	compactLayout bool
)

// RenderBox wraps content in a rounded border box scaled responsively to
// termWidth. On a short terminal it shows only the part of the content around
// the focused line, so the box never grows past the screen.
func RenderBox(content string, termWidth int) string {
	b := styleBox.Copy()
	padX := 2
	if compactLayout {
		b = b.Padding(0, 1)
		padX = 1
	}
	// The last line is the key-hint bar. Wrap it separately so all of it
	// stays on screen even when it takes two lines.
	body, hints := content, ""
	if i := strings.LastIndex(content, "\n"); i >= 0 {
		body, hints = content[:i], content[i+1:]
	}
	if termWidth > 0 {
		w := ResponsiveWidth(termWidth)
		b = b.Width(w)
		// Wrap first, so the lines counted below are the lines shown.
		wrap := lipgloss.NewStyle().Width(w - 2*padX)
		body, hints = wrap.Render(body), wrap.Render(hints)
	}
	max := boxMaxLines
	if max > 0 {
		max -= lipgloss.Height(hints) - 1
	}
	return b.Render(fitLines(body+"\n"+hints, max, lipgloss.Height(hints)))
}

// fitLines keeps at most max lines of content: the key hints at the end
// always, plus a window around the focused line, with "more" markers where
// lines were left out.
//
// The last hintLines lines (the key-hint bar) are kept as a unit and count as
// one line against max.
func fitLines(content string, max, hintLines int) string {
	lines := strings.Split(content, "\n")
	if hintLines < 1 {
		hintLines = 1
	}
	if hintLines > len(lines) {
		hintLines = len(lines)
	}
	if max <= 0 || len(lines)-(hintLines-1) <= max {
		return strings.ReplaceAll(content, focusMark, "")
	}
	if max < 2 {
		max = 2
	}
	focus := -1
	for i, l := range lines {
		if strings.Contains(l, focusMark) {
			focus = i
			break
		}
	}
	if focus < 0 {
		for i, l := range lines {
			if strings.Contains(l, "▶") {
				focus = i
				break
			}
		}
	}
	if focus < 0 {
		focus = 0
	}

	hints := lines[len(lines)-hintLines:]
	body := lines[:len(lines)-hintLines]
	avail := max - 1
	if focus >= len(body) {
		focus = len(body) - 1
	}
	start := focus - 2 // a little context above the focused line
	if start > len(body)-avail {
		start = len(body) - avail
	}
	if start < 0 {
		start = 0
	}
	end := start + avail
	view := append([]string(nil), body[start:end]...)
	// With room to spare, say that lines are hidden. The focused line sits at
	// least two lines from either edge here, so it is never replaced.
	if avail >= 5 {
		if start > 0 {
			view[0] = styleHelp.Render("  ↑ more above")
		}
		if end < len(body) {
			view[len(view)-1] = styleHelp.Render("  ↓ more below")
		}
	}
	view = append(view, hints...)
	return strings.ReplaceAll(strings.Join(view, "\n"), focusMark, "")
}

// RenderHeader renders the top title bar spanning the responsive width.
func RenderHeader(text string, termWidth int) string {
	h := styleHeader.Copy().MaxHeight(1)
	if termWidth > 0 {
		h = h.Width(ResponsiveWidth(termWidth) + 2)
	}
	return h.Render(text)
}
