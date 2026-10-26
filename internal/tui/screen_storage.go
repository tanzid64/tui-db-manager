package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type StorageScreen struct {
	svc             *Service
	selected        int
	statusMsg       string
	isWizardOpen    bool
	wizard          WizardModel
	width           int
	height          int
	isEditing       bool
	editingStorName string
	onBack          func() tea.Cmd
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

func (s *StorageScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	if s.isWizardOpen {
		s.wizard.SetSize(width, height)
	}
}

func (s StorageScreen) Update(msg tea.Msg) (StorageScreen, tea.Cmd) {
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
			if name := s.handleStorageWizardFinish(s.wizard.Values); name != "" {
				// Test right away so a wrong key or bucket shows up now,
				// not at 3 AM during the first scheduled backup.
				return s, tea.Batch(cmd, s.testCmd(name))
			}
		} else if s.wizard.Canceled {
			s.isWizardOpen = false
		}
		return s, cmd
	}

	switch msg := msg.(type) {
	case rowClickMsg:
		clickRow(&s.selected, msg.index, len(s.svc.Cfg.Storages))
		return s, nil

	case storageTestResultMsg:
		if msg.err != nil {
			s.statusMsg = styleError.Render(fmt.Sprintf("✖ Test %q failed: %v", msg.name, msg.err))
		} else {
			s.statusMsg = styleSuccess.Render(fmt.Sprintf("✔ Storage %q works (test file written and deleted)", msg.name))
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
				return s, s.testCmd(storages[s.selected].Name)
			}
		case "a":
			s.openStorageWizard(nil)
			return s, nil
		case "e":
			if len(storages) > 0 && s.selected < len(storages) {
				s.openStorageWizard(&storages[s.selected])
				return s, nil
			}
		case "d":
			if len(storages) > 0 && s.selected < len(storages) {
				delName := storages[s.selected].Name
				var users []string
				for _, j := range s.svc.Cfg.Jobs {
					if j.Storage == delName {
						users = append(users, j.Name)
					}
				}
				if len(users) > 0 {
					s.statusMsg = styleError.Render(fmt.Sprintf("✖ Storage %q is used by job(s) %s — change those jobs first", delName, strings.Join(users, ", ")))
					return s, nil
				}
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

func (s *StorageScreen) openStorageWizard(existing *config.StorageConfig) {
	nameVal := ""
	typeIdx := 0
	typeOptions := []string{"local", "s3", "gdrive"}

	pathVal := ""
	folderVal := ""
	gdriveCreds := ""
	bucketVal := ""
	regionVal := ""
	endpointVal := ""
	accessKeyVal := ""
	secretKeyVal := ""

	if existing != nil {
		s.isEditing = true
		s.editingStorName = existing.Name
		nameVal = existing.Name
		for idx, t := range typeOptions {
			if strings.EqualFold(t, existing.Type) {
				typeIdx = idx
				break
			}
		}
		switch strings.ToLower(existing.Type) {
		case "local":
			pathVal = existing.Path
		case "gdrive":
			folderVal = existing.FolderID
			if s.svc.Vault != nil {
				gdriveCreds, _ = s.svc.Vault.Get("storage/" + existing.Name + "/credentials")
			}
		case "s3":
			bucketVal = existing.Bucket
			regionVal = existing.Region
			endpointVal = existing.Endpoint
			accessKeyVal = existing.AccessKeyID
			if s.svc.Vault != nil {
				secretKeyVal, _ = s.svc.Vault.Get("storage/" + existing.Name + "/secret_key")
			}
		}
	} else {
		s.isEditing = false
		s.editingStorName = ""
		pathVal = config.DefaultBackupDir()
	}

	prefixVal := ""
	if existing != nil {
		prefixVal = existing.Prefix
	}
	paramFields := storageFields(typeOptions[typeIdx], map[string]string{
		"path": pathVal, "folder_id": folderVal, "credentials": gdriveCreds,
		"bucket": bucketVal, "region": regionVal, "endpoint": endpointVal,
		"access_key": accessKeyVal, "secret_key": secretKeyVal, "prefix": prefixVal,
	})

	steps := []WizardStep{
		{
			Title:       "Backend Type",
			Description: "Set a unique name and pick the storage driver",
			Fields: []WizardField{
				{Key: "name", Label: "Storage Name", Placeholder: "backup-data", Value: nameVal, Type: FieldText},
				{Key: "type", Label: "Backend Type", Type: FieldSelect, Options: typeOptions, SelectedOpt: typeIdx},
			},
		},
		{
			Title:       "Storage Parameters",
			Description: "Fill in the required fields for the chosen backend type",
			Fields:      paramFields,
		},
	}

	wizardTitle := "Add Storage Destination"
	if s.isEditing {
		wizardTitle = fmt.Sprintf("Edit Storage Destination: %s", existing.Name)
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
			if chosenType == "local" && strings.TrimSpace(vals["path"]) == "" {
				vals["path"] = config.DefaultBackupDir()
			}
			fields := storageFields(chosenType, vals)
			wiz.SetStepFields(1, fields)
		}
	}

	w.ValidateStep = func(stepIndex int, vals map[string]string) error {
		if stepIndex == 0 {
			name := strings.TrimSpace(vals["name"])
			if name == "" {
				return errors.New("storage name cannot be empty")
			}
			for _, existing := range s.svc.Cfg.Storages {
				if existing.Name == name && (!s.isEditing || name != s.editingStorName) {
					return fmt.Errorf("storage backend %q already exists", name)
				}
			}
		} else if stepIndex == 1 {
			stType := strings.ToLower(strings.TrimSpace(vals["type"]))
			switch stType {
			case "gdrive":
				if strings.TrimSpace(vals["folder_id"]) == "" {
					return errors.New("Google Drive folder ID cannot be empty")
				}
			case "s3":
				if strings.TrimSpace(vals["bucket"]) == "" {
					return errors.New("bucket name cannot be empty")
				}
				if strings.TrimSpace(vals["access_key"]) == "" {
					return errors.New("access key ID cannot be empty")
				}
				if strings.TrimSpace(vals["secret_key"]) == "" {
					return errors.New("secret access key cannot be empty")
				}
			default: // "local"
				if strings.TrimSpace(vals["path"]) == "" {
					return errors.New("local directory path cannot be empty")
				}
			}
		}
		return nil
	}

	s.wizard = w
	s.isWizardOpen = true
}

// handleStorageWizardFinish saves the storage and returns its name, or ""
// if nothing was saved.
func (s *StorageScreen) handleStorageWizardFinish(vals map[string]string) string {
	name := strings.TrimSpace(vals["name"])
	if name == "" {
		s.statusMsg = styleError.Render("Storage name cannot be empty")
		return ""
	}

	stType := strings.ToLower(strings.TrimSpace(vals["type"]))
	sc := config.StorageConfig{
		Name: name,
		Type: stType,
	}
	// Keep settings the wizard doesn't show (secret refs, path style).
	if s.isEditing {
		if old, ok := s.svc.Cfg.Storage(s.editingStorName); ok && old.Type == stType {
			sc.SecretKeyRef = old.SecretKeyRef
			sc.CredentialsRef = old.CredentialsRef
			sc.ForcePathStyle = old.ForcePathStyle
		}
	}

	switch stType {
	case "local":
		sc.Path = config.ExpandPath(vals["path"])
	case "s3":
		sc.Bucket = strings.TrimSpace(vals["bucket"])
		sc.Region = strings.TrimSpace(vals["region"])
		sc.Endpoint = storage.NormalizeEndpoint(vals["endpoint"])
		sc.Prefix = strings.Trim(strings.TrimSpace(vals["prefix"]), "/")
		sc.AccessKeyID = strings.TrimSpace(vals["access_key"])
		if sec := strings.TrimSpace(vals["secret_key"]); sec != "" {
			if s.svc.Vault == nil {
				_ = s.svc.CreateVault()
			}
			if s.svc.Vault != nil {
				s.svc.Vault.Set("storage/"+name+"/secret_key", sec)
				_ = s.svc.Vault.Save()
			}
		}
	case "gdrive":
		sc.FolderID = strings.TrimSpace(vals["folder_id"])
		if creds := strings.TrimSpace(vals["credentials"]); creds != "" {
			if s.svc.Vault == nil {
				_ = s.svc.CreateVault()
			}
			if s.svc.Vault != nil {
				s.svc.Vault.Set("storage/"+name+"/credentials", creds)
				_ = s.svc.Vault.Save()
			}
		}
	}

	if s.isEditing {
		updated := false
		for i, existingSt := range s.svc.Cfg.Storages {
			if existingSt.Name == s.editingStorName {
				s.svc.Cfg.Storages[i] = sc
				updated = true
				break
			}
		}
		if !updated {
			s.svc.Cfg.Storages = append(s.svc.Cfg.Storages, sc)
		}
		if s.editingStorName != name && s.svc.Vault != nil {
			s.svc.Vault.Delete("storage/" + s.editingStorName + "/secret_key")
			s.svc.Vault.Delete("storage/" + s.editingStorName + "/credentials")
			_ = s.svc.Vault.Save()
		}
		// Jobs refer to storage by name, so follow a rename.
		if s.editingStorName != name {
			for i := range s.svc.Cfg.Jobs {
				if s.svc.Cfg.Jobs[i].Storage == s.editingStorName {
					s.svc.Cfg.Jobs[i].Storage = name
				}
			}
		}
	} else {
		s.svc.Cfg.Storages = append(s.svc.Cfg.Storages, sc)
	}
	if err := s.svc.SaveConfig(); err != nil {
		s.statusMsg = styleError.Render("✖ Could not save storage: " + err.Error())
		return ""
	}
	return name
}

func (s *StorageScreen) testCmd(name string) tea.Cmd {
	s.statusMsg = styleWarning.Render(fmt.Sprintf("⟳ Testing storage %q (writing a small test file)...", name))
	svc := s.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return storageTestResultMsg{name: name, err: svc.TestStorage(ctx, name)}
	}
}

// storageFields returns the wizard fields for a storage type, prefilled
// from vals.
func storageFields(stType string, vals map[string]string) []WizardField {
	switch stType {
	case "gdrive":
		return []WizardField{
			{Key: "folder_id", Label: "Google Drive Folder ID", Placeholder: "1A2B3C4D5E... (from the folder URL)", Value: vals["folder_id"], Type: FieldText},
			{Key: "credentials", Label: "Service Account JSON (paste)", Placeholder: "{\"type\": \"service_account\", ...}", Value: vals["credentials"], Type: FieldText, IsSecret: true},
		}
	case "s3":
		return []WizardField{
			{Key: "bucket", Label: "Bucket Name", Placeholder: "my-backups", Value: vals["bucket"], Type: FieldText},
			{Key: "endpoint", Label: "Endpoint (empty = AWS)", Placeholder: "R2: <account-id>.r2.cloudflarestorage.com", Value: vals["endpoint"], Type: FieldText},
			{Key: "region", Label: "Region", Placeholder: "us-east-1 (R2: auto)", Value: vals["region"], Type: FieldText},
			{Key: "access_key", Label: "Access Key ID", Placeholder: "AKIA...", Value: vals["access_key"], Type: FieldText},
			{Key: "secret_key", Label: "Secret Access Key", Placeholder: "••••••••", Value: vals["secret_key"], Type: FieldText, IsSecret: true},
			{Key: "prefix", Label: "Folder in Bucket (optional)", Placeholder: "dbk", Value: vals["prefix"], Type: FieldText},
		}
	default: // "local"
		return []WizardField{
			{Key: "path", Label: "Folder Path", Placeholder: config.DefaultBackupDir(), Value: vals["path"], Type: FieldText},
		}
	}
}

func (s StorageScreen) View() string {
	if s.isWizardOpen {
		return s.wizard.View()
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render("Storage"))
	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("Where backups are kept. s3 works with AWS S3, Cloudflare R2, MinIO, Backblaze B2, Wasabi"))
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
				loc = "s3://" + st.Bucket
				if st.Prefix != "" {
					loc += "/" + st.Prefix
				}
				if st.Endpoint != "" {
					loc += " @ " + strings.TrimPrefix(st.Endpoint, "https://")
				}
			case "gdrive":
				loc = fmt.Sprintf("gdrive: %s", st.FolderID)
			}

			b.WriteString(markRow(i, fmt.Sprintf("%s%s  %s  %s",
				prefix,
				itemStyle.Render(st.Name),
				styleSubtitle.Render(fmt.Sprintf("[%s]", st.Type)),
				loc,
			)) + "\n")
		}
		b.WriteString("\n")
	}

	if s.statusMsg != "" {
		b.WriteString(s.statusMsg + "\n\n")
	}

	b.WriteString(hintBar(
		keyHint("t", "t: Test"),
		keyHint("a", "a: Add"),
		keyHint("e", "e: Edit"),
		keyHint("d", "d: Delete"),
		keyHint("esc", "Esc: Back"),
	))
	return RenderBox(b.String(), s.width)
}
