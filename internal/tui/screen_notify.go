package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/config"
)

type NotifyScreen struct {
	svc          *Service
	selected     int
	statusMsg    string
	isWizardOpen bool
	wizard       WizardModel
	onBack       func() tea.Cmd
}

type notifyTestResultMsg struct {
	name string
	err  error
}

func NewNotifyScreen(svc *Service, onBack func() tea.Cmd) NotifyScreen {
	return NotifyScreen{
		svc:      svc,
		selected: 0,
		onBack:   onBack,
	}
}

func (s NotifyScreen) Update(msg tea.Msg) (NotifyScreen, tea.Cmd) {
	if s.isWizardOpen {
		var cmd tea.Cmd
		s.wizard, cmd = s.wizard.Update(msg)
		return s, cmd
	}

	switch msg := msg.(type) {
	case notifyTestResultMsg:
		if msg.err != nil {
			s.statusMsg = styleError.Render(fmt.Sprintf("✖ Test %q failed: %v", msg.name, msg.err))
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Notification sent successfully via %q!", msg.name))
		}
		return s, nil

	case tea.KeyMsg:
		notifiers := s.svc.Cfg.Notifiers
		switch msg.String() {
		case "esc":
			if s.onBack != nil {
				return s, s.onBack()
			}
		case "up", "k":
			if len(notifiers) > 0 {
				if s.selected > 0 {
					s.selected--
				} else {
					s.selected = len(notifiers) - 1
				}
			}
		case "down", "j":
			if len(notifiers) > 0 {
				if s.selected < len(notifiers)-1 {
					s.selected++
				} else {
					s.selected = 0
				}
			}
		case "t":
			if len(notifiers) > 0 && s.selected < len(notifiers) {
				name := notifiers[s.selected].Name
				s.statusMsg = styleWarning.Render(fmt.Sprintf("⟳ Sending test message via %q...", name))
				return s, func() tea.Msg {
					err := s.svc.TestNotifier(context.Background(), name)
					return notifyTestResultMsg{name: name, err: err}
				}
			}
		case "a":
			s.openAddNotifierWizard()
			return s, nil
		case "d":
			if len(notifiers) > 0 && s.selected < len(notifiers) {
				delName := notifiers[s.selected].Name
				newNotifiers := make([]config.NotifierConfig, 0)
				for i, n := range notifiers {
					if i != s.selected {
						newNotifiers = append(newNotifiers, n)
					}
				}
				s.svc.Cfg.Notifiers = newNotifiers
				_ = s.svc.SaveConfig()
				if s.selected >= len(newNotifiers) && s.selected > 0 {
					s.selected = len(newNotifiers) - 1
				}
				s.statusMsg = styleSuccess.Render(fmt.Sprintf("Notifier %q deleted", delName))
			}
		}
	}
	return s, nil
}

func (s *NotifyScreen) openAddNotifierWizard() {
	steps := []WizardStep{
		{
			Title:       "Notifier Type",
			Description: "Set a unique name and select the notification channel type",
			Fields: []WizardField{
				{Key: "name", Label: "Channel Name", Placeholder: "alerts-telegram", Type: FieldText},
				{Key: "type", Label: "Channel Type", Type: FieldSelect, Options: []string{"telegram", "email", "webhook"}},
			},
		},
		{
			Title:       "Channel Parameters",
			Description: "Fill in the parameters for your chosen notification channel",
			Fields: []WizardField{
				// Telegram
				{Key: "bot_token", Label: "Bot Token (Telegram, Vault)", Placeholder: "123456:ABC-DEF...", Type: FieldText, IsSecret: true},
				{Key: "chat_id", Label: "Chat ID (Telegram)", Placeholder: "-10012345678", Type: FieldText},
				// Email
				{Key: "host", Label: "SMTP Host (Email)", Placeholder: "smtp.mailgun.org", Type: FieldText},
				{Key: "port", Label: "SMTP Port (Email)", Placeholder: "587", Type: FieldText},
				{Key: "from", Label: "From Address (Email)", Placeholder: "backups@example.com", Type: FieldText},
				{Key: "to", Label: "To Address (Email)", Placeholder: "ops@example.com", Type: FieldText},
				{Key: "username", Label: "SMTP Username (Email)", Placeholder: "user@example.com", Type: FieldText},
				{Key: "password", Label: "SMTP Password (Email, Vault)", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
				{Key: "tls", Label: "Implicit TLS (Email, Port 465)", Type: FieldCheckbox, Checked: false},
				// Webhook
				{Key: "url", Label: "Webhook URL", Placeholder: "https://example.com/webhook", Type: FieldText},
				{Key: "token", Label: "Signing Token / Secret (Webhook, Vault)", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
			},
		},
	}

	s.wizard = NewWizard(
		"Add Notification Channel",
		steps,
		func(vals map[string]string) tea.Cmd {
			name := strings.TrimSpace(vals["name"])
			if name == "" {
				s.statusMsg = styleError.Render("Channel name cannot be empty")
				s.isWizardOpen = false
				return nil
			}

			nType := vals["type"]
			nc := config.NotifierConfig{
				Name: name,
				Type: nType,
			}

			switch nType {
			case "telegram":
				nc.ChatID = vals["chat_id"]
				if tok := vals["bot_token"]; tok != "" && s.svc.Vault != nil {
					s.svc.Vault.Set("notifier/"+name+"/bot_token", tok)
					_ = s.svc.Vault.Save()
				}
			case "email":
				port, _ := strconv.Atoi(vals["port"])
				if port <= 0 {
					port = 587
				}
				nc.Host = vals["host"]
				nc.Port = port
				nc.From = vals["from"]
				if to := vals["to"]; to != "" {
					nc.To = []string{to}
				}
				nc.Username = vals["username"]
				nc.TLS = vals["tls"] == "true"
				if pwd := vals["password"]; pwd != "" && s.svc.Vault != nil {
					s.svc.Vault.Set("notifier/"+name+"/password", pwd)
					_ = s.svc.Vault.Save()
				}
			case "webhook":
				nc.URL = vals["url"]
				if tok := vals["token"]; tok != "" && s.svc.Vault != nil {
					s.svc.Vault.Set("notifier/"+name+"/token", tok)
					_ = s.svc.Vault.Save()
				}
			}

			s.svc.Cfg.Notifiers = append(s.svc.Cfg.Notifiers, nc)
			_ = s.svc.SaveConfig()
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Added notifier %q", name))
			s.isWizardOpen = false
			return nil
		},
		func() tea.Cmd {
			s.isWizardOpen = false
			return nil
		},
	)
	s.isWizardOpen = true
}

func (s NotifyScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("dbk — Notification Channels"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Manage alert delivery for backup successes and failures"))
	b.WriteString("\n\n")

	notifiers := s.svc.Cfg.Notifiers
	if len(notifiers) == 0 {
		b.WriteString(styleHelp.Render("No notification channels configured yet. Press 'a' to add one."))
		b.WriteString("\n\n")
	} else {
		for i, n := range notifiers {
			prefix := "  "
			itemStyle := styleNormal
			if i == s.selected {
				prefix = styleSelected.Render("▶ ")
				itemStyle = styleSelected
			}

			dest := ""
			switch n.Type {
			case "telegram":
				dest = fmt.Sprintf("chat: %s", n.ChatID)
			case "email":
				dest = fmt.Sprintf("smtp: %s:%d (to: %s)", n.Host, n.Port, strings.Join(n.To, ","))
			case "webhook":
				dest = n.URL
			}

			b.WriteString(fmt.Sprintf("%s%s  %s  %s\n",
				prefix,
				itemStyle.Render(n.Name),
				styleSubtitle.Render(fmt.Sprintf("[%s]", n.Type)),
				dest,
			))
		}
		b.WriteString("\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(styleHelp.Render("t: Test channel • a: Add notifier • d: Delete • Esc: Back"))
	return styleBox.Render(b.String())
}
