package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/tanzid64/tui-db-manager/internal/config"
)

func newTestApp(t *testing.T, w, h int) *App {
	t.Helper()
	svc, err := NewService(t.TempDir(), "")
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	app := NewApp(svc)
	app.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return app
}

// render draws a frame and waits until zone id has been registered.
func render(t *testing.T, app *App, id string) string {
	t.Helper()
	view := app.View()
	deadline := time.Now().Add(2 * time.Second)
	for zone.Get(id).IsZero() {
		if time.Now().After(deadline) {
			t.Fatalf("zone %q never registered; view:\n%s", id, view)
		}
		time.Sleep(5 * time.Millisecond)
		view = app.View()
	}
	return view
}

func click(t *testing.T, app *App, id string) {
	t.Helper()
	render(t, app, id)
	z := zone.Get(id)
	app.Update(tea.MouseMsg{X: z.StartX, Y: z.StartY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
}

func TestSmallTerminalShowsWholeMenu(t *testing.T) {
	for _, h := range []int{16, 20, 24} {
		app := newTestApp(t, 80, h)
		view := app.View()
		if got := lipgloss.Height(view); got > h {
			t.Errorf("height %d: view is %d lines", h, got)
		}
		for _, item := range []string{"Backup Jobs", "Settings", "Quit"} {
			if !strings.Contains(view, item) {
				t.Errorf("height %d: %q missing from menu:\n%s", h, item, view)
			}
		}
	}
}

func TestSmallTerminalKeepsFocusedFieldVisible(t *testing.T) {
	app := newTestApp(t, 80, 14)
	app.Update(tea.KeyMsg{Type: tea.KeyEnter}) // open Backup Jobs
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !app.jobsScreen.isWizardOpen {
		t.Fatal("wizard should be open")
	}
	// Move down to the last field of the identity step, then into the
	// storage-heavy steps; the focused line must stay on screen.
	for i := 0; i < 6; i++ {
		view := app.View()
		if got := lipgloss.Height(view); got > 14 {
			t.Fatalf("step %d: view is %d lines, terminal is 14", i, got)
		}
		if !strings.Contains(view, "▶") {
			t.Fatalf("step %d: focused field scrolled off:\n%s", i, view)
		}
		app.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
}

func TestClickMenuItemOpensScreen(t *testing.T) {
	app := newTestApp(t, 100, 40)
	click(t, app, "row:5") // Settings
	if app.currentScreen != screenSettings {
		t.Fatalf("expected Settings screen, got %v", app.currentScreen)
	}
}

func TestClickHintAndRows(t *testing.T) {
	app := newTestApp(t, 120, 40)
	app.svc.Cfg.Jobs = []config.Job{{Name: "one", Engine: "sqlite"}, {Name: "two", Engine: "sqlite"}}
	click(t, app, "row:0") // Backup Jobs
	if app.currentScreen != screenJobs {
		t.Fatalf("expected jobs screen, got %v", app.currentScreen)
	}
	click(t, app, "row:1")
	if app.jobsScreen.selected != 1 {
		t.Fatalf("click should select job 1, got %d", app.jobsScreen.selected)
	}
	click(t, app, "key:a")
	if !app.jobsScreen.isWizardOpen {
		t.Fatal("clicking 'a: Add' should open the wizard")
	}
	// Engine select is field 1 of the first step; pick "postgres" (option 1).
	click(t, app, optZoneID(1, 1))
	if got := app.jobsScreen.wizard.Steps[0].Fields[1].SelectedOpt; got != 1 {
		t.Fatalf("clicking an option should select it, got %d", got)
	}
	click(t, app, "key:esc")
	if app.jobsScreen.isWizardOpen {
		t.Fatal("clicking 'Esc: Back' should close the wizard")
	}
}

func TestWheelMovesSelection(t *testing.T) {
	app := newTestApp(t, 100, 40)
	app.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if app.menuScreen.selected != 1 {
		t.Fatalf("wheel down should move to item 1, got %d", app.menuScreen.selected)
	}
}

func TestFitLinesKeepsFocusAndHints(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "line")
	}
	lines[20] = "▶ focused"
	lines = append(lines, "hints")
	out := strings.Split(fitLines(strings.Join(lines, "\n"), 8, 1), "\n")
	if len(out) != 8 {
		t.Fatalf("want 8 lines, got %d", len(out))
	}
	joined := strings.Join(out, "\n")
	for _, want := range []string{"▶ focused", "hints", "more above", "more below"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}
