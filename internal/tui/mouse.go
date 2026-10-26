package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Mouse support: screens mark clickable parts of their view with these
// helpers, and App turns a click on a marked zone into an ordinary message
// (a key press, rowClickMsg, ...), so screens need little mouse-specific code.

func init() { zone.NewGlobal() }

// frameZones lists the zone IDs marked while rendering the current frame, so
// a click can be matched against them.
var frameZones []string

func markZone(id, s string) string {
	frameZones = append(frameZones, id)
	return zone.Mark(id, s)
}

// rowClickMsg is sent when list row index is clicked. Screens select the row;
// clicking the row that is already selected acts like Enter.
type rowClickMsg struct{ index int }

// fieldClickMsg is sent when a wizard field is clicked.
type fieldClickMsg struct{ index int }

// optionClickMsg is sent when a choice of a wizard select field is clicked.
type optionClickMsg struct{ field, option int }

// markRow makes a list row clickable.
func markRow(i int, s string) string { return markZone("row:"+strconv.Itoa(i), s) }

// keyHint renders "label" as a clickable button that presses key.
func keyHint(key, label string) string {
	return markZone("key:"+key, styleHelp.Render(label))
}

// hintBar joins clickable key hints into the help line at the bottom of a
// screen.
func hintBar(hints ...string) string {
	return strings.Join(hints, styleHelp.Render(" • "))
}

// keyMsgFor builds the key press a clicked hint stands for.
func keyMsgFor(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case " ", "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// mouseToMsg translates a mouse event into the message it stands for, or nil
// if it hit nothing clickable.
func mouseToMsg(m tea.MouseMsg) tea.Msg {
	switch m.Button {
	case tea.MouseButtonWheelUp:
		if m.Action == tea.MouseActionPress {
			return tea.KeyMsg{Type: tea.KeyUp}
		}
		return nil
	case tea.MouseButtonWheelDown:
		if m.Action == tea.MouseActionPress {
			return tea.KeyMsg{Type: tea.KeyDown}
		}
		return nil
	}
	if m.Button != tea.MouseButtonLeft || m.Action != tea.MouseActionPress {
		return nil
	}
	// Later zones are drawn on top (e.g. a choice inside a field), so check
	// them first.
	for i := len(frameZones) - 1; i >= 0; i-- {
		id := frameZones[i]
		if !zone.Get(id).InBounds(m) {
			continue
		}
		kind, arg, _ := strings.Cut(id, ":")
		switch kind {
		case "key":
			return keyMsgFor(arg)
		case "row":
			n, _ := strconv.Atoi(arg)
			return rowClickMsg{index: n}
		case "field":
			n, _ := strconv.Atoi(arg)
			return fieldClickMsg{index: n}
		case "opt":
			f, o, _ := strings.Cut(arg, ":")
			fi, _ := strconv.Atoi(f)
			oi, _ := strconv.Atoi(o)
			return optionClickMsg{field: fi, option: oi}
		}
	}
	return nil
}

// clickRow applies a row click to a list: it selects the row, or reports
// activate=true when the row was already selected.
func clickRow(selected *int, index, count int) (activate bool) {
	if index < 0 || index >= count {
		return false
	}
	if *selected == index {
		return true
	}
	*selected = index
	return false
}

func optZoneID(field, option int) string { return fmt.Sprintf("opt:%d:%d", field, option) }
