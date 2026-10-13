package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tanzid64/tui-db-manager/internal/buildinfo"
	"github.com/tanzid64/tui-db-manager/internal/update"
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
	logsScreenID
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

	unlockScreen   UnlockScreen
	menuScreen     MenuScreen
	jobsScreen     JobsScreen
	storageScreen  StorageScreen
	notifyScreen   NotifyScreen
	restoreScreen  RestoreScreen
	settingsScreen SettingsScreen
	logsScreen     LogsScreen

	updateRelease   *update.Release
	showUpdateModal bool
	modalSelected   int
	isUpdating      bool
	updateStatus    string
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
			app.currentScreen = logsScreenID
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
	app.settingsScreen = NewSettingsScreen(svc, toMenu, func() tea.Cmd {
		return app.checkUpdateCmd(true)
	})
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
	cmds := []tea.Cmd{a.checkUpdateCmd(false)}
	if a.currentScreen == screenUnlock {
		cmds = append(cmds, a.unlockScreen.Init())
	}
	return tea.Batch(cmds...)
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
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.unlockScreen.SetSize(msg.Width, msg.Height)
		a.menuScreen.SetSize(msg.Width, msg.Height)
		a.jobsScreen.SetSize(msg.Width, msg.Height)
		a.storageScreen.SetSize(msg.Width, msg.Height)
		a.notifyScreen.SetSize(msg.Width, msg.Height)
		a.restoreScreen.SetSize(msg.Width, msg.Height)
		a.settingsScreen.SetSize(msg.Width, msg.Height)
		a.logsScreen.SetSize(msg.Width, msg.Height)
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
	case logsScreenID:
		a.logsScreen, cmd = a.logsScreen.Update(msg)
	}

	return a, cmd
}

func (a *App) View() string {
	var body string
	if a.showUpdateModal {
		body = a.renderUpdateModal()
	} else {
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
		case logsScreenID:
			body = a.logsScreen.View()
		}
	}

	// Persistent header
	headerText := fmt.Sprintf(" dbk %s • Secure Database Backup Manager ", buildinfo.Version)
	if a.updateRelease != nil {
		headerText += fmt.Sprintf(" • 🔔 Update Available (%s, press 'u')", a.updateRelease.TagName)
	}
	header := RenderHeader(headerText, a.width)

	var footer string
	if a.svc.IsVaultUnlocked() {
		footer = styleHelp.Render(" [Vault: Unlocked] ")
	} else {
		footer = styleWarning.Render(" [Vault: Locked] ")
	}

	view := lipgloss.JoinVertical(lipgloss.Left, header, "\n", body, "\n", footer)
	return view
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

		b.WriteString(fmt.Sprintf("%s    %s\n\n", btnUpdate, btnLater))
		b.WriteString(styleHelp.Render("Tab / ← / →: Choose • Enter: Confirm • Esc: Dismiss"))
	}

	return RenderBox(b.String(), a.width)
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
