package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/tanzid64/tui-db-manager/internal/buildinfo"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/store"
	"github.com/tanzid64/tui-db-manager/internal/update"
	"golang.org/x/term"
)

type screenID int

const (
	screenMainMenu screenID = iota
	screenJobs
	screenStorage
	screenNotifications
	screenRestore
	screenSettings
	logsScreenID
	screenSchedule
)

type updateCheckMsg struct {
	rel   *update.Release
	err   error
	force bool
}

type updateAppliedMsg struct {
	err error
	ver string
}

type App struct {
	svc           *Service
	currentScreen screenID
	width         int
	height        int

	menuScreen     MenuScreen
	jobsScreen     JobsScreen
	storageScreen  StorageScreen
	notifyScreen   NotifyScreen
	restoreScreen  RestoreScreen
	settingsScreen SettingsScreen
	logsScreen     LogsScreen
	scheduleScreen ScheduleScreen

	updateRelease   *update.Release
	showUpdateModal bool
	modalSelected   int
	isUpdating      bool
	updateStatus    string
}

// exitMessage is printed after the TUI closes, e.g. the uninstall summary.
var exitMessage string

func NewApp(svc *Service) *App {
	app := &App{
		svc:           svc,
		currentScreen: screenMainMenu,
	}

	app.menuScreen = NewMenuScreen(func(index int) tea.Cmd {
		switch index {
		case 0:
			app.currentScreen = screenJobs
		case 1:
			app.currentScreen = screenSchedule
		case 2:
			app.currentScreen = screenRestore
		case 3:
			app.currentScreen = screenStorage
		case 4:
			app.currentScreen = logsScreenID
		case 5:
			app.currentScreen = screenSettings
		case 6:
			return tea.Quit
		}
		return nil
	})
	app.menuScreen.Notice = svc.SecurityNotice()

	toMenu := func() tea.Cmd {
		app.currentScreen = screenMainMenu
		return nil
	}

	app.jobsScreen = NewJobsScreen(svc, toMenu, func(jobName string) tea.Cmd {
		app.currentScreen = screenRestore
		return app.restoreScreen.SelectJob(jobName)
	})
	app.storageScreen = NewStorageScreen(svc, toMenu)
	toSettings := func() tea.Cmd {
		app.currentScreen = screenSettings
		return nil
	}
	app.notifyScreen = NewNotifyScreen(svc, toSettings)
	app.scheduleScreen = NewScheduleScreen(svc, toMenu)
	app.restoreScreen = NewRestoreScreen(svc, toMenu)
	app.settingsScreen = NewSettingsScreen(svc, toMenu, func() tea.Cmd {
		return app.checkUpdateCmd(true)
	})
	app.settingsScreen.onOpenNotifications = func() tea.Cmd {
		app.currentScreen = screenNotifications
		return nil
	}
	app.logsScreen = NewLogsScreen(toMenu)

	return app
}

func (a *App) Init() tea.Cmd {
	return a.checkUpdateCmd(false)
}

func (a *App) checkUpdateCmd(force bool) tea.Cmd {
	configDir := a.svc.ConfigDir
	currVer := buildinfo.Version
	return func() tea.Msg {
		var rel *update.Release
		var isNewer bool
		var err error
		if force {
			rel, isNewer, err = update.CheckForUpdate(context.Background(), "", currVer)
		} else {
			rel, isNewer, err = update.CheckCached(context.Background(), configDir, "", currVer)
		}
		if !isNewer {
			rel = nil
		}
		return updateCheckMsg{
			rel:   rel,
			err:   err,
			force: force,
		}
	}
}

func (a *App) applyUpdateCmd(rel *update.Release) tea.Cmd {
	return func() tea.Msg {
		err := update.ApplyUpdate(context.Background(), rel, "")
		return updateAppliedMsg{
			err: err,
			ver: rel.TagName,
		}
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.MouseMsg); ok {
		translated := mouseToMsg(m)
		if translated == nil {
			return a, nil
		}
		msg = translated
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.menuScreen.SetSize(msg.Width, msg.Height)
		a.jobsScreen.SetSize(msg.Width, msg.Height)
		a.storageScreen.SetSize(msg.Width, msg.Height)
		a.notifyScreen.SetSize(msg.Width, msg.Height)
		a.restoreScreen.SetSize(msg.Width, msg.Height)
		a.settingsScreen.SetSize(msg.Width, msg.Height)
		a.logsScreen.SetSize(msg.Width, msg.Height)
		a.scheduleScreen.SetSize(msg.Width, msg.Height)
		return a, nil

	case updateCheckMsg:
		if msg.err != nil {
			if msg.force {
				a.settingsScreen.SetStatusMsg(styleError.Render(fmt.Sprintf("Update check failed: %v", msg.err)))
			}
			return a, nil
		}
		if msg.rel != nil {
			a.updateRelease = msg.rel
			a.showUpdateModal = true
			a.modalSelected = 0
			a.updateStatus = ""
		} else if msg.force {
			a.settingsScreen.SetStatusMsg(styleSuccess.Render(fmt.Sprintf("✔ You are on the latest version (%s)", buildinfo.Version)))
		}
		return a, nil

	case updateAppliedMsg:
		a.isUpdating = false
		if msg.err != nil {
			a.updateStatus = styleError.Render(fmt.Sprintf("Update failed: %v", msg.err))
		} else {
			a.updateStatus = styleSuccess.Render(fmt.Sprintf("Successfully upgraded to %s! Please restart dbk.", msg.ver))
			a.updateRelease = nil
		}
		return a, nil

	case rowClickMsg:
		// Buttons in the update dialog act on a single click.
		if a.showUpdateModal {
			if !a.isUpdating && a.updateStatus == "" && msg.index >= 0 && msg.index <= 1 {
				a.modalSelected = msg.index
				return a.Update(tea.KeyMsg{Type: tea.KeyEnter})
			}
			return a, nil
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		}

		if a.showUpdateModal {
			switch msg.String() {
			case "left", "right", "tab", "h", "l":
				if !a.isUpdating {
					if a.modalSelected == 0 {
						a.modalSelected = 1
					} else {
						a.modalSelected = 0
					}
				}
				return a, nil
			case "enter":
				if a.isUpdating {
					return a, nil
				}
				if a.updateRelease == nil {
					a.showUpdateModal = false
					return a, nil
				}
				if a.modalSelected == 0 {
					a.isUpdating = true
					a.updateStatus = "Downloading & applying update..."
					return a, a.applyUpdateCmd(a.updateRelease)
				}
				a.showUpdateModal = false
				return a, nil
			case "esc", "q":
				if !a.isUpdating {
					a.showUpdateModal = false
				}
				return a, nil
			}
			return a, nil
		}

		if a.currentScreen == screenMainMenu && msg.String() == "u" {
			if a.updateRelease != nil {
				a.showUpdateModal = true
				a.modalSelected = 0
				return a, nil
			}
			return a, a.checkUpdateCmd(true)
		}
	}

	var cmd tea.Cmd
	switch a.currentScreen {
	case screenMainMenu:
		a.menuScreen, cmd = a.menuScreen.Update(msg)
	case screenJobs:
		a.jobsScreen, cmd = a.jobsScreen.Update(msg)
	case screenStorage:
		a.storageScreen, cmd = a.storageScreen.Update(msg)
	case screenNotifications:
		a.notifyScreen, cmd = a.notifyScreen.Update(msg)
	case screenRestore:
		a.restoreScreen, cmd = a.restoreScreen.Update(msg)
	case screenSettings:
		a.settingsScreen, cmd = a.settingsScreen.Update(msg)
	case logsScreenID:
		a.logsScreen, cmd = a.logsScreen.Update(msg)
	case screenSchedule:
		a.scheduleScreen, cmd = a.scheduleScreen.Update(msg)
	}

	return a, cmd
}

// layoutFor sets the box size limits for a terminal of the given height.
// Outside the box the app draws the header and the vault footer.
func layoutFor(height int) {
	compactLayout = height > 0 && height < 30
	if height <= 0 {
		boxMaxLines = 0
		return
	}
	chrome := 2 + 6 // header + footer, blank spacer lines (2 each side)
	box := 2 + 2    // border + vertical padding
	if compactLayout {
		chrome = 2
		box = 2
	}
	boxMaxLines = height - chrome - box
	if boxMaxLines < 3 {
		boxMaxLines = 3
	}
}

func (a *App) View() string {
	frameZones = frameZones[:0]
	layoutFor(a.height)
	var body string
	if a.showUpdateModal {
		body = a.renderUpdateModal()
	} else {
		switch a.currentScreen {
		case screenMainMenu:
			body = a.menuScreen.View()
		case screenJobs:
			body = a.jobsScreen.View()
		case screenStorage:
			body = a.storageScreen.View()
		case screenNotifications:
			body = a.notifyScreen.View()
		case screenRestore:
			body = a.restoreScreen.View()
		case screenSettings:
			body = a.settingsScreen.View()
		case logsScreenID:
			body = a.logsScreen.View()
		case screenSchedule:
			body = a.scheduleScreen.View()
		}
	}

	// Persistent header
	headerText := fmt.Sprintf(" dbk %s • Database Backup ", buildinfo.Version)
	if a.updateRelease != nil {
		headerText += fmt.Sprintf(" • 🔔 Update Available (%s, press 'u')", a.updateRelease.TagName)
	}
	header := RenderHeader(headerText, a.width)

	var footer string
	if a.svc.IsVaultUnlocked() {
		footer = styleHelp.Render(" [Vault: Ready] ")
	} else if a.svc.HasVault() {
		footer = styleWarning.Render(" [Vault: Locked] ")
	} else {
		footer = styleError.Render(" [Vault: Unavailable] ")
	}

	var view string
	if compactLayout {
		view = lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	} else {
		view = lipgloss.JoinVertical(lipgloss.Left, header, "\n", body, "\n", footer)
	}
	return zone.Scan(view)
}

func (a *App) renderUpdateModal() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("🚀 Software Update Available"))
	b.WriteString("\n\n")

	if a.updateRelease != nil {
		b.WriteString("A new release of dbk is available:\n\n")
		b.WriteString(fmt.Sprintf("  Current version: %s\n", styleSubtitle.Render(buildinfo.Version)))
		b.WriteString(fmt.Sprintf("  Latest release:  %s\n\n", styleSuccess.Render(a.updateRelease.TagName)))
		if a.updateRelease.Body != "" {
			notes := a.updateRelease.Body
			lines := strings.Split(strings.TrimSpace(notes), "\n")
			if len(lines) > 5 {
				notes = strings.Join(lines[:5], "\n") + "\n..."
			}
			b.WriteString(styleSubtitle.Render("Release Notes:\n"+notes) + "\n\n")
		}
	}

	if a.isUpdating {
		b.WriteString(styleWarning.Render("⏳ "+a.updateStatus) + "\n\n")
		b.WriteString(styleHelp.Render("Please wait while the binary is verified and replaced in-place..."))
	} else if a.updateStatus != "" {
		b.WriteString(a.updateStatus + "\n\n")
		b.WriteString(styleHelp.Render("Press Enter or Esc to dismiss"))
	} else {
		btnUpdate := "[ Update Now ]"
		btnLater := "[ Remind Me Later ]"

		if a.modalSelected == 0 {
			btnUpdate = styleSelected.Render("▶ [ Update Now ]")
			btnLater = styleNormal.Render("  [ Remind Me Later ]")
		} else {
			btnUpdate = styleNormal.Render("  [ Update Now ]")
			btnLater = styleSelected.Render("▶ [ Remind Me Later ]")
		}

		b.WriteString(fmt.Sprintf("%s    %s\n\n", markRow(0, btnUpdate), markRow(1, btnLater)))
		b.WriteString(hintBar(keyHint("tab", "Tab / ← / →: Choose"), keyHint("enter", "Enter: Confirm"), keyHint("esc", "Esc: Dismiss")))
	}

	return RenderBox(b.String(), a.width)
}

// Run launches the Bubble Tea TUI application.
func Run(configDir, passFile string) error {
	dir, err := config.Dir(configDir)
	if err != nil {
		return err
	}

	if store.Exists(dir) {
		key := store.ResolveMasterKey(dir)
		if key == "" && term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(os.Stderr, "Enter Master Key: ")
			if b, err := term.ReadPassword(int(os.Stdin.Fd())); err == nil {
				fmt.Fprintln(os.Stderr)
				_ = os.Setenv(store.MasterKeyEnvVar, string(b))
			}
		}
	}

	svc, err := NewService(configDir, passFile)
	if err != nil {
		return fmt.Errorf("init tui service: %w", err)
	}

	app := NewApp(svc)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	if exitMessage != "" {
		fmt.Println(exitMessage)
	}
	return err
}
