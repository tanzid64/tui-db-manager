package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/config"
)

type NotifyScreen struct {
	svc             *Service
	selected        int
	statusMsg       string
	isWizardOpen    bool
	wizard          WizardModel
	width           int
	height          int
	isEditing       bool
	editingChanName string
	onBack          func() tea.Cmd
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

func (s *NotifyScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	if s.isWizardOpen {
		s.wizard.SetSize(width, height)
	}
}

func (s NotifyScreen) Update(msg tea.Msg) (NotifyScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.SetSize(msg.Width, msg.Height)
		if s.isWizardOpen {
			var cmd tea.Cmd
			s.wizard, cmd = s.wizard.Update(msg)
			return s, cmd
		}
		return s, nil
	}

	if s.isWizardOpen {
		var cmd tea.Cmd
		s.wizard, cmd = s.wizard.Update(msg)
		if s.wizard.Done {
			s.isWizardOpen = false
			s.handleNotifyWizardFinish(s.wizard.Values)
		} else if s.wizard.Canceled {
			s.isWizardOpen = false
		}
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
			s.openNotifyWizard(nil)
			return s, nil
		case "e":
			if len(notifiers) > 0 && s.selected < len(notifiers) {
				s.openNotifyWizard(&notifiers[s.selected])
				return s, nil
			}
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

func (s *NotifyScreen) openNotifyWizard(existing *config.NotifierConfig) {
	nameVal := ""
	typeIdx := 0
	typeOptions := []string{"telegram", "email", "webhook"}

	botTokenVal := ""
	chatIDVal := ""
	smtpHostVal := "smtp.example.com"
	smtpPortVal := "587"
	smtpFromVal := "backups@example.com"
	smtpToVal := ""
	smtpUserVal := ""
	smtpPassVal := ""
	smtpTLSChecked := false
	webhookURLVal := "https://example.com/webhook"
	webhookTokVal := ""

	if existing != nil {
		s.isEditing = true
		s.editingChanName = existing.Name
		nameVal = existing.Name
		for idx, t := range typeOptions {
			if strings.EqualFold(t, existing.Type) {
				typeIdx = idx
				break
			}
		}
		switch strings.ToLower(existing.Type) {
		case "telegram":
			chatIDVal = existing.ChatID
			if s.svc.Vault != nil {
				botTokenVal, _ = s.svc.Vault.Get("notifier/" + existing.Name + "/bot_token")
			}
		case "email":
			smtpHostVal = existing.Host
			if existing.Port > 0 {
				smtpPortVal = strconv.Itoa(existing.Port)
			}
			smtpFromVal = existing.From
			smtpToVal = strings.Join(existing.To, ", ")
			smtpUserVal = existing.Username
			smtpTLSChecked = existing.TLS
			if s.svc.Vault != nil {
				smtpPassVal, _ = s.svc.Vault.Get("notifier/" + existing.Name + "/password")
			}
		case "webhook":
			webhookURLVal = existing.URL
			if s.svc.Vault != nil {
				webhookTokVal, _ = s.svc.Vault.Get("notifier/" + existing.Name + "/token")
			}
		}
	} else {
		s.isEditing = false
		s.editingChanName = ""
	}

	var paramFields []WizardField
	nType := typeOptions[typeIdx]
	switch nType {
	case "email":
		paramFields = []WizardField{
			{Key: "host", Label: "SMTP Host", Placeholder: "smtp.mailgun.org", Value: smtpHostVal, Type: FieldText},
			{Key: "port", Label: "SMTP Port", Placeholder: "587", Value: smtpPortVal, Type: FieldText},
			{Key: "from", Label: "From Address", Placeholder: "backups@example.com", Value: smtpFromVal, Type: FieldText},
			{Key: "to", Label: "To Address", Placeholder: "ops@example.com", Value: smtpToVal, Type: FieldText},
			{Key: "username", Label: "SMTP Username", Placeholder: "user@example.com", Value: smtpUserVal, Type: FieldText},
			{Key: "password", Label: "SMTP Password (Vault)", Placeholder: "••••••••", Value: smtpPassVal, Type: FieldText, IsSecret: true},
			{Key: "tls", Label: "Implicit TLS (Port 465)", Type: FieldCheckbox, Checked: smtpTLSChecked},
		}
	case "webhook":
		paramFields = []WizardField{
			{Key: "url", Label: "Webhook URL", Placeholder: "https://example.com/webhook", Value: webhookURLVal, Type: FieldText},
			{Key: "token", Label: "Signing Token / Secret (Vault)", Placeholder: "••••••••", Value: webhookTokVal, Type: FieldText, IsSecret: true},
		}
	default: // "telegram"
		paramFields = []WizardField{
			{Key: "bot_token", Label: "Bot Token (Vault)", Placeholder: "123456:ABC-DEF...", Value: botTokenVal, Type: FieldText, IsSecret: true},
			{Key: "chat_id", Label: "Chat ID", Placeholder: "-10012345678", Value: chatIDVal, Type: FieldText},
		}
	}

	steps := []WizardStep{
		{
			Title:       "Notifier Type",
			Description: "Set a unique name and select the notification channel type",
			Fields: []WizardField{
				{Key: "name", Label: "Channel Name", Placeholder: "alerts-channel", Value: nameVal, Type: FieldText},
				{Key: "type", Label: "Channel Type", Type: FieldSelect, Options: typeOptions, SelectedOpt: typeIdx},
			},
		},
		{
			Title:       "Channel Parameters",
			Description: "Fill in the parameters for your chosen notification channel",
			Fields:      paramFields,
		},
	}

	wizardTitle := "Add Notification Channel"
	if s.isEditing {
		wizardTitle = fmt.Sprintf("Edit Notification Channel: %s", existing.Name)
	}

	w := NewWizard(
		wizardTitle,
		steps,
		nil,
		nil,
	)
	if s.width > 0 {
		w.SetSize(s.width, s.height)
	}

	w.OnStepAdvance = func(wiz *WizardModel, currentStep int) {
		if currentStep == 0 {
			vals := wiz.CollectValues()
			chosenType := strings.ToLower(strings.TrimSpace(vals["type"]))
			var fields []WizardField
			switch chosenType {
			case "email":
				portCurr := vals["port"]
				if portCurr == "" {
					portCurr = "587"
				}
				fields = []WizardField{
					{Key: "host", Label: "SMTP Host", Placeholder: "smtp.mailgun.org", Value: vals["host"], Type: FieldText},
					{Key: "port", Label: "SMTP Port", Placeholder: "587", Value: portCurr, Type: FieldText},
					{Key: "from", Label: "From Address", Placeholder: "backups@example.com", Value: vals["from"], Type: FieldText},
					{Key: "to", Label: "To Address", Placeholder: "ops@example.com", Value: vals["to"], Type: FieldText},
					{Key: "username", Label: "SMTP Username", Placeholder: "user@example.com", Value: vals["username"], Type: FieldText},
					{Key: "password", Label: "SMTP Password (Vault)", Placeholder: "••••••••", Value: vals["password"], Type: FieldText, IsSecret: true},
					{Key: "tls", Label: "Implicit TLS (Port 465)", Type: FieldCheckbox, Checked: vals["tls"] == "true"},
				}
			case "webhook":
				fields = []WizardField{
					{Key: "url", Label: "Webhook URL", Placeholder: "https://example.com/webhook", Value: vals["url"], Type: FieldText},
					{Key: "token", Label: "Signing Token / Secret (Vault)", Placeholder: "••••••••", Value: vals["token"], Type: FieldText, IsSecret: true},
				}
			default: // "telegram"
				fields = []WizardField{
					{Key: "bot_token", Label: "Bot Token (Vault)", Placeholder: "123456:ABC-DEF...", Value: vals["bot_token"], Type: FieldText, IsSecret: true},
					{Key: "chat_id", Label: "Chat ID", Placeholder: "-10012345678", Value: vals["chat_id"], Type: FieldText},
				}
			}
			wiz.SetStepFields(1, fields)
		}
	}

	w.ValidateStep = func(stepIndex int, vals map[string]string) error {
		if stepIndex == 0 {
			name := strings.TrimSpace(vals["name"])
			if name == "" {
				return errors.New("channel name cannot be empty")
			}
			for _, existing := range s.svc.Cfg.Notifiers {
				if existing.Name == name && (!s.isEditing || name != s.editingChanName) {
					return fmt.Errorf("notification channel %q already exists", name)
				}
			}
		} else if stepIndex == 1 {
			chosenType := strings.ToLower(strings.TrimSpace(vals["type"]))
			switch chosenType {
			case "telegram":
				if strings.TrimSpace(vals["chat_id"]) == "" {
					return errors.New("telegram chat ID cannot be empty")
				}
			case "email":
				if strings.TrimSpace(vals["host"]) == "" {
					return errors.New("SMTP host cannot be empty")
				}
				if strings.TrimSpace(vals["to"]) == "" {
					return errors.New("recipient address (To) cannot be empty")
				}
			case "webhook":
				if strings.TrimSpace(vals["url"]) == "" {
					return errors.New("webhook URL cannot be empty")
				}
			}
		}
		return nil
	}

	s.wizard = w
	s.isWizardOpen = true
}

func (s *NotifyScreen) handleNotifyWizardFinish(vals map[string]string) {
	name := strings.TrimSpace(vals["name"])
	if name == "" {
		s.statusMsg = styleError.Render("Channel name cannot be empty")
		return
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

	if s.isEditing {
		updated := false
		for i, existingN := range s.svc.Cfg.Notifiers {
			if existingN.Name == s.editingChanName {
				s.svc.Cfg.Notifiers[i] = nc
				updated = true
				break
			}
		}
		if !updated {
			s.svc.Cfg.Notifiers = append(s.svc.Cfg.Notifiers, nc)
		}
		if s.editingChanName != name && s.svc.Vault != nil {
			s.svc.Vault.Delete("notifier/" + s.editingChanName + "/bot_token")
			s.svc.Vault.Delete("notifier/" + s.editingChanName + "/password")
			s.svc.Vault.Delete("notifier/" + s.editingChanName + "/token")
			_ = s.svc.Vault.Save()
		}
		_ = s.svc.SaveConfig()
		s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Updated notification channel %q", name))
	} else {
		s.svc.Cfg.Notifiers = append(s.svc.Cfg.Notifiers, nc)
		_ = s.svc.SaveConfig()
		s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Added notifier %q", name))
	}
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

	b.WriteString(styleHelp.Render("t: Test channel • a: Add • e: Edit • d: Delete • Esc: Back"))
	return RenderBox(b.String(), s.width)
}
