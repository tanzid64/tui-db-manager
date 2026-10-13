package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tanzid64/tui-db-manager/internal/buildinfo"
)

type screenID int

const (
	screenUnlock screenID = iota
	screenMainMenu
	screenJobs
	screenStorage
	screenNotifications
	screenRestore
	screenSettings
	screenLogs
)

type App struct {
	svc           *Service
	currentScreen screenID
	width         int
	height        int

	unlockScreen   UnlockScreen
	menuScreen     MenuScreen
	jobsScreen     JobsScreen
	storageScreen  StorageScreen
	notifyScreen   NotifyScreen
	restoreScreen  RestoreScreen
	settingsScreen SettingsScreen
	logsScreen     LogsScreen
}

func NewApp(svc *Service) *App {
	app := &App{
		svc: svc,
	}

	app.unlockScreen = NewUnlockScreen(svc, func() tea.Cmd {
		app.currentScreen = screenMainMenu
		return nil
	})

	app.menuScreen = NewMenuScreen(func(index int) tea.Cmd {
		switch index {
		case 0:
			app.currentScreen = screenJobs
		case 1:
			app.currentScreen = screenStorage
		case 2:
			app.currentScreen = screenNotifications
		case 3:
			app.currentScreen = screenRestore
		case 4:
			app.currentScreen = screenSettings
		case 5:
			app.currentScreen = screenLogs
		case 6:
			return tea.Quit
		}
		return nil
	})

	toMenu := func() tea.Cmd {
		app.currentScreen = screenMainMenu
		return nil
	}

	app.jobsScreen = NewJobsScreen(svc, toMenu, func(jobName string) tea.Cmd {
		app.currentScreen = screenRestore
		return app.restoreScreen.SelectJob(jobName)
	})
	app.storageScreen = NewStorageScreen(svc, toMenu)
	app.notifyScreen = NewNotifyScreen(svc, toMenu)
	app.restoreScreen = NewRestoreScreen(svc, toMenu)
	app.settingsScreen = NewSettingsScreen(svc, toMenu)
	app.logsScreen = NewLogsScreen(toMenu)

	// Check if vault is already unlocked via env/passfile, or if no vault exists yet
	if err := svc.TryAutoUnlock(); err == nil && svc.IsVaultUnlocked() {
		app.currentScreen = screenMainMenu
	} else if !svc.HasVault() {
		app.currentScreen = screenUnlock
	} else {
		app.currentScreen = screenUnlock
	}

	return app
}

func (a *App) Init() tea.Cmd {
	if a.currentScreen == screenUnlock {
		return a.unlockScreen.Init()
	}
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		}
	}

	var cmd tea.Cmd
	switch a.currentScreen {
	case screenUnlock:
		a.unlockScreen, cmd = a.unlockScreen.Update(msg)
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
	case screenLogs:
		a.logsScreen, cmd = a.logsScreen.Update(msg)
	}

	return a, cmd
}

func (a *App) View() string {
	var body string
	switch a.currentScreen {
	case screenUnlock:
		body = a.unlockScreen.View()
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
	case screenLogs:
		body = a.logsScreen.View()
	}

	// Persistent header
	header := styleHeader.Render(fmt.Sprintf(" dbk %s • Secure Database Backup Manager ", buildinfo.Version))
	var footer string
	if a.svc.IsVaultUnlocked() {
		footer = styleHelp.Render(" [Vault: Unlocked] ")
	} else {
		footer = styleWarning.Render(" [Vault: Locked] ")
	}

	view := lipgloss.JoinVertical(lipgloss.Left, header, "\n", body, "\n", footer)
	return view
}

// Run launches the Bubble Tea TUI application.
func Run(configDir, passFile string) error {
	svc, err := NewService(configDir, passFile)
	if err != nil {
		return fmt.Errorf("init tui service: %w", err)
	}

	app := NewApp(svc)
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
