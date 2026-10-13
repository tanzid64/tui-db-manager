package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/config"
)

type StorageScreen struct {
	svc          *Service
	selected     int
	statusMsg    string
	isWizardOpen bool
	wizard       WizardModel
	onBack       func() tea.Cmd
}

type storageTestResultMsg struct {
	name string
	err  error
}

func NewStorageScreen(svc *Service, onBack func() tea.Cmd) StorageScreen {
	return StorageScreen{
		svc:      svc,
		selected: 0,
		onBack:   onBack,
	}
}

func (s StorageScreen) Update(msg tea.Msg) (StorageScreen, tea.Cmd) {
	if s.isWizardOpen {
		var cmd tea.Cmd
		s.wizard, cmd = s.wizard.Update(msg)
		if s.wizard.Done {
			s.isWizardOpen = false
			s.handleStorageWizardFinish(s.wizard.Values)
		} else if s.wizard.Canceled {
			s.isWizardOpen = false
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case storageTestResultMsg:
		if msg.err != nil {
			s.statusMsg = styleError.Render(fmt.Sprintf("✖ Test %q failed: %v", msg.name, msg.err))
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Storage backend %q reachable and operational!", msg.name))
		}
		return s, nil

	case tea.KeyMsg:
		storages := s.svc.Cfg.Storages
		switch msg.String() {
		case "esc":
			if s.onBack != nil {
				return s, s.onBack()
			}
		case "up", "k":
			if len(storages) > 0 {
				if s.selected > 0 {
					s.selected--
				} else {
					s.selected = len(storages) - 1
				}
			}
		case "down", "j":
			if len(storages) > 0 {
				if s.selected < len(storages)-1 {
					s.selected++
				} else {
					s.selected = 0
				}
			}
		case "t":
			if len(storages) > 0 && s.selected < len(storages) {
				name := storages[s.selected].Name
				s.statusMsg = styleWarning.Render(fmt.Sprintf("⟳ Testing connection to %q...", name))
				return s, func() tea.Msg {
					err := s.svc.TestStorage(context.Background(), name)
					return storageTestResultMsg{name: name, err: err}
				}
			}
		case "a":
			s.openAddStorageWizard()
			return s, nil
		case "d":
			if len(storages) > 0 && s.selected < len(storages) {
				delName := storages[s.selected].Name
				newStorages := make([]config.StorageConfig, 0)
				for i, st := range storages {
					if i != s.selected {
						newStorages = append(newStorages, st)
					}
				}
				s.svc.Cfg.Storages = newStorages
				_ = s.svc.SaveConfig()
				if s.selected >= len(newStorages) && s.selected > 0 {
					s.selected = len(newStorages) - 1
				}
				s.statusMsg = styleSuccess.Render(fmt.Sprintf("Storage %q deleted", delName))
			}
		}
	}
	return s, nil
}

func (s *StorageScreen) openAddStorageWizard() {
	steps := []WizardStep{
		{
			Title:       "Backend Type",
			Description: "Set a unique name and pick the storage driver",
			Fields: []WizardField{
				{Key: "name", Label: "Storage Name", Placeholder: "backup-s3", Type: FieldText},
				{Key: "type", Label: "Backend Type", Type: FieldSelect, Options: []string{"local", "s3", "gdrive"}},
			},
		},
		{
			Title:       "Storage Parameters",
			Description: "Fill in the required fields for the chosen backend type",
			Fields: []WizardField{
				{Key: "path", Label: "Local Path (for local)", Placeholder: "/var/backups", Type: FieldText},
				{Key: "bucket", Label: "Bucket (for S3)", Placeholder: "my-backups", Type: FieldText},
				{Key: "region", Label: "Region (for S3)", Placeholder: "us-east-1", Type: FieldText},
				{Key: "endpoint", Label: "Endpoint (optional S3)", Placeholder: "https://r2.cloudflarestorage.com", Type: FieldText},
				{Key: "access_key", Label: "Access Key ID (S3)", Placeholder: "AKIA...", Type: FieldText},
				{Key: "secret_key", Label: "Secret Key (S3, Vault)", Placeholder: "••••••••", Type: FieldText, IsSecret: true},
				{Key: "folder_id", Label: "Folder ID (for GDrive)", Placeholder: "1A2B3C...", Type: FieldText},
				{Key: "credentials", Label: "Service Account JSON (GDrive, Vault)", Placeholder: "{\"type\": ...}", Type: FieldText, IsSecret: true},
			},
		},
	}

	s.wizard = NewWizard(
		"Add Storage Destination",
		steps,
		nil,
		nil,
	)
	s.isWizardOpen = true
}

func (s *StorageScreen) handleStorageWizardFinish(vals map[string]string) {
	name := strings.TrimSpace(vals["name"])
	if name == "" {
		s.statusMsg = styleError.Render("Storage name cannot be empty")
		return
	}

	stType := vals["type"]
	sc := config.StorageConfig{
		Name: name,
		Type: stType,
	}

	switch stType {
	case "local":
		sc.Path = vals["path"]
	case "s3":
		sc.Bucket = vals["bucket"]
		sc.Region = vals["region"]
		sc.Endpoint = vals["endpoint"]
		sc.AccessKeyID = vals["access_key"]
		if sec := vals["secret_key"]; sec != "" && s.svc.Vault != nil {
			s.svc.Vault.Set("storage/"+name+"/secret_key", sec)
			_ = s.svc.Vault.Save()
		}
	case "gdrive":
		sc.FolderID = vals["folder_id"]
		if creds := vals["credentials"]; creds != "" && s.svc.Vault != nil {
			s.svc.Vault.Set("storage/"+name+"/credentials", creds)
			_ = s.svc.Vault.Save()
		}
	}

	s.svc.Cfg.Storages = append(s.svc.Cfg.Storages, sc)
	_ = s.svc.SaveConfig()
	s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Added storage backend %q", name))
}

func (s StorageScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("dbk — Storage Destinations"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Manage storage backends for backup archiving"))
	b.WriteString("\n\n")

	storages := s.svc.Cfg.Storages
	if len(storages) == 0 {
		b.WriteString(styleHelp.Render("No storage backends configured yet. Press 'a' to add one."))
		b.WriteString("\n\n")
	} else {
		for i, st := range storages {
			prefix := "  "
			itemStyle := styleNormal
			if i == s.selected {
				prefix = styleSelected.Render("▶ ")
				itemStyle = styleSelected
			}

			loc := ""
			switch st.Type {
			case "local":
				loc = st.Path
			case "s3":
				loc = fmt.Sprintf("s3://%s (%s)", st.Bucket, st.Region)
			case "gdrive":
				loc = fmt.Sprintf("gdrive: %s", st.FolderID)
			}

			b.WriteString(fmt.Sprintf("%s%s  %s  %s\n",
				prefix,
				itemStyle.Render(st.Name),
				styleSubtitle.Render(fmt.Sprintf("[%s]", st.Type)),
				loc,
			))
		}
		b.WriteString("\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(styleHelp.Render("t: Test backend • a: Add storage • d: Delete • Esc: Back"))
	return styleBox.Render(b.String())
}
