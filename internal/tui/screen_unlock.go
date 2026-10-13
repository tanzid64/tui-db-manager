package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type UnlockScreen struct {
	svc        *Service
	input      textinput.Model
	isInit     bool
	errMsg     string
	width      int
	height     int
	onUnlocked func() tea.Cmd
}

func NewUnlockScreen(svc *Service, onUnlocked func() tea.Cmd) UnlockScreen {
	ti := textinput.New()
	ti.Placeholder = "Enter master passphrase"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Focus()

	return UnlockScreen{
		svc:        svc,
		input:      ti,
		isInit:     !svc.HasVault(),
		onUnlocked: onUnlocked,
	}
}

func (s *UnlockScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	s.input.Width = ResponsiveWidth(width) - 20
}

func (s UnlockScreen) Init() tea.Cmd {
	return textinput.Blink
}

func (s UnlockScreen) Update(msg tea.Msg) (UnlockScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.SetSize(msg.Width, msg.Height)
		return s, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			pass := s.input.Value()
			if pass == "" {
				s.errMsg = "passphrase cannot be empty"
				return s, nil
			}
			var err error
			if s.isInit {
				err = s.svc.CreateVault(pass)
			} else {
				err = s.svc.UnlockVault(pass)
			}
			if err != nil {
				s.errMsg = err.Error()
				return s, nil
			}
			s.errMsg = ""
			if s.onUnlocked != nil {
				return s, s.onUnlocked()
			}
			return s, nil
		case "esc", "ctrl+c":
			return s, tea.Quit
		}
	}

	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s UnlockScreen) View() string {
	var b strings.Builder
	if s.isInit {
		b.WriteString(styleTitle.Render("Welcome to dbk — First-Time Vault Initialization"))
		b.WriteString("\n")
		b.WriteString(styleSubtitle.Render("No vault found. Choose a strong master passphrase to encrypt your credentials:"))
	} else {
		b.WriteString(styleTitle.Render("dbk — Unlock Vault"))
		b.WriteString("\n")
		b.WriteString(styleSubtitle.Render("Enter your master passphrase to unlock stored credentials:"))
	}
	b.WriteString("\n\n")
	b.WriteString(s.input.View())
	b.WriteString("\n\n")

	if s.errMsg != "" {
		b.WriteString(styleError.Render("✖ "+s.errMsg) + "\n\n")
	}

	b.WriteString(styleHelp.Render("Enter: Submit • Esc / Ctrl+C: Quit"))
	return RenderBox(b.String(), s.width)
}
