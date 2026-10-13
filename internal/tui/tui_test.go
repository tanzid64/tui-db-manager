package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tanzid64/tui-db-manager/internal/config"
)

func TestWizardNavigationAndCollection(t *testing.T) {
	steps := []WizardStep{
		{
			Title: "Step 1",
			Fields: []WizardField{
				{Key: "name", Label: "Name", Type: FieldText},
				{Key: "mode", Label: "Mode", Type: FieldSelect, Options: []string{"fast", "safe"}},
			},
		},
		{
			Title: "Step 2",
			Fields: []WizardField{
				{Key: "enabled", Label: "Enabled", Type: FieldCheckbox, Checked: false},
			},
		},
	}

	finished := false
	var finalVals map[string]string

	w := NewWizard(
		"Test Wizard",
		steps,
		func(vals map[string]string) tea.Cmd {
			finished = true
			finalVals = vals
			return nil
		},
		func() tea.Cmd {
			return nil
		},
	)

	// Step 1 Field 0 (name text input)
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mydb")})
	// Press Tab/Down to move to Field 1 (mode select)
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyTab})
	if w.ActiveField != 1 {
		t.Fatalf("expected active field 1, got %d", w.ActiveField)
	}

	// Press Right to toggle option
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRight})
	if w.Steps[0].Fields[1].SelectedOpt != 1 {
		t.Fatalf("expected selected opt 1 (safe), got %d", w.Steps[0].Fields[1].SelectedOpt)
	}

	// Press Enter to complete Step 1 and advance to Step 2
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if w.ActiveStep != 1 {
		t.Fatalf("expected active step 1, got %d", w.ActiveStep)
	}

	// In Step 2: toggle checkbox with space
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !w.Steps[1].Fields[0].Checked {
		t.Fatalf("expected checkbox checked")
	}

	// Press Enter to submit final step
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !finished {
		t.Fatalf("expected wizard to finish")
	}
	if finalVals["name"] != "mydb" {
		t.Fatalf("expected name 'mydb', got %q", finalVals["name"])
	}
	if finalVals["mode"] != "safe" {
		t.Fatalf("expected mode 'safe', got %q", finalVals["mode"])
	}
	if finalVals["enabled"] != "true" {
		t.Fatalf("expected enabled 'true', got %q", finalVals["enabled"])
	}
}

func TestWizardValidation(t *testing.T) {
	steps := []WizardStep{
		{
			Title: "Step 1",
			Fields: []WizardField{
				{Key: "name", Label: "Name", Type: FieldText},
			},
		},
		{
			Title: "Step 2",
			Fields: []WizardField{
				{Key: "other", Label: "Other", Type: FieldText},
			},
		},
	}

	w := NewWizard("Validate Wizard", steps, nil, nil)
	w.ValidateStep = func(stepIndex int, values map[string]string) error {
		if stepIndex == 0 && values["name"] == "" {
			return errors.New("name cannot be empty")
		}
		return nil
	}

	// Press Enter without entering name
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if w.ActiveStep != 0 {
		t.Fatalf("expected to stay on step 0 due to validation error")
	}
	if w.ErrMessage != "name cannot be empty" {
		t.Fatalf("expected error message 'name cannot be empty', got %q", w.ErrMessage)
	}

	// Fill name and press Enter
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("valid-name")})
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if w.ActiveStep != 1 {
		t.Fatalf("expected advance to step 1 after valid input")
	}
	if w.ErrMessage != "" {
		t.Fatalf("expected error message to be cleared, got %q", w.ErrMessage)
	}
}

func TestHistoryStore(t *testing.T) {
	h := &HistoryStore{}
	now := time.Now()

	h.Add(RunRecord{
		ID:       "run-1",
		Job:      "job-a",
		Action:   "backup",
		Status:   StatusSuccess,
		Started:  now.Add(-10 * time.Minute),
		Duration: 5 * time.Second,
	})
	h.Add(RunRecord{
		ID:       "run-2",
		Job:      "job-a",
		Action:   "backup",
		Status:   StatusFailed,
		Started:  now,
		Duration: 2 * time.Second,
		Error:    "connection refused",
	})

	all := h.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 records, got %d", len(all))
	}
	// Recent first
	if all[0].ID != "run-2" {
		t.Fatalf("expected newest record first, got %s", all[0].ID)
	}

	last := h.LastForJob("job-a")
	if last == nil || last.ID != "run-2" {
		t.Fatalf("expected last run for job-a to be run-2")
	}

	lastB := h.LastForJob("job-nonexistent")
	if lastB != nil {
		t.Fatalf("expected nil for nonexistent job")
	}
}

func TestAppNavigationLifecycle(t *testing.T) {
	dir := t.TempDir()

	svc, err := NewService(dir, "")
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	// Initially no vault: starts on screenUnlock
	app := NewApp(svc)
	if app.currentScreen != screenUnlock {
		t.Fatalf("expected screenUnlock on initial launch without vault, got %d", app.currentScreen)
	}

	// Check UnlockScreen view
	view := app.View()
	if view == "" {
		t.Fatalf("expected non-empty view")
	}

	// Create vault via unlock screen
	app.unlockScreen.input.SetValue("test-passphrase-123")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if app.currentScreen != screenMainMenu {
		t.Fatalf("expected transition to screenMainMenu after unlocking, got %d", app.currentScreen)
	}

	// Navigate to Jobs screen (menu item 0)
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.currentScreen != screenJobs {
		t.Fatalf("expected screenJobs, got %d", app.currentScreen)
	}

	// Press Esc to return to main menu
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.currentScreen != screenMainMenu {
		t.Fatalf("expected screenMainMenu after Esc, got %d", app.currentScreen)
	}

	// Down to Storage (item 1) and enter
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.currentScreen != screenStorage {
		t.Fatalf("expected screenStorage, got %d", app.currentScreen)
	}

	// Esc back to menu
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.currentScreen != screenMainMenu {
		t.Fatalf("expected screenMainMenu, got %d", app.currentScreen)
	}

	// Change passphrase via service
	if err := svc.ChangePassphrase("new-passphrase-456"); err != nil {
		t.Fatalf("ChangePassphrase: %v", err)
	}

	// Verify we can re-open vault with new passphrase
	svc2, err := NewService(dir, "")
	if err != nil {
		t.Fatalf("NewService 2: %v", err)
	}
	if err := svc2.UnlockVault("new-passphrase-456"); err != nil {
		t.Fatalf("UnlockVault with new pass failed: %v", err)
	}
}

func TestTUIServiceConfigAndBundle(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewService(dir, "")
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if err := svc.CreateVault("vault-pass"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	svc.Cfg.Jobs = []config.Job{
		{
			Name:      "test-job",
			Engine:    "sqlite",
			Storage:   "local",
			Conn:      config.Conn{Path: "/tmp/test.db"},
			Retention: config.Retention{KeepLast: 3},
		},
	}
	if err := svc.SaveConfig(); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Export bundle
	exportFile := filepath.Join(dir, "export.yaml")
	if err := svc.ExportBundle(exportFile, "transfer-pass", true); err != nil {
		t.Fatalf("ExportBundle: %v", err)
	}
	if _, err := os.Stat(exportFile); err != nil {
		t.Fatalf("Export file not found: %v", err)
	}

	// Import bundle into new dir
	importDir := t.TempDir()
	importSvc, err := NewService(importDir, "")
	if err != nil {
		t.Fatalf("NewService import: %v", err)
	}
	if err := importSvc.CreateVault("import-vault-pass"); err != nil {
		t.Fatalf("CreateVault import: %v", err)
	}

	res, err := importSvc.ImportBundle(exportFile, "transfer-pass", true)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if res.ConfigResult.JobsAdded != 1 {
		t.Fatalf("expected 1 job added, got %d", res.ConfigResult.JobsAdded)
	}
	if len(importSvc.Cfg.Jobs) != 1 || importSvc.Cfg.Jobs[0].Name != "test-job" {
		t.Fatalf("expected job 'test-job' imported")
	}
}

func TestJobsScreenWizardEnterCompletion(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewService(dir, "")
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := svc.CreateVault("vault-pass"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	screen := NewJobsScreen(svc, nil, nil)

	// Press 'a' to open wizard
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !screen.isWizardOpen {
		t.Fatalf("expected wizard to be open")
	}

	// Step 1: Job Identity
	// Field 0: Name (text) -> enter "my-prod-db"
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("my-prod-db")})
	// Press Enter to move to Field 1 (engine select)
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveField != 1 {
		t.Fatalf("expected active field 1, got %d", screen.wizard.ActiveField)
	}
	// Press Enter on Field 1 (last field of Step 1) -> advances to Step 2
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveStep != 1 {
		t.Fatalf("expected step 1 (Database Connection), got %d", screen.wizard.ActiveStep)
	}

	// Step 2: Database Connection
	// Fields: 0: host, 1: port, 2: user, 3: password, 4: database, 5: path, 6: tls
	// Press Enter 6 times to move across fields to field 6 (tls checkbox)
	for f := 0; f < 6; f++ {
		screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if screen.wizard.ActiveField != 6 {
		t.Fatalf("expected active field 6 (tls), got %d", screen.wizard.ActiveField)
	}
	// Press Enter on Field 6 (last field of Step 2) -> advances to Step 3
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveStep != 2 {
		t.Fatalf("expected step 2 (Storage & Retention), got %d", screen.wizard.ActiveStep)
	}

	// Step 3: Storage & Retention
	// Fields: 0: storage (select), 1: keep_last (text), 2: compress (checkbox), 3: encrypt (checkbox)
	// Press Enter on field 0 -> field 1
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveField != 1 {
		t.Fatalf("expected active field 1, got %d", screen.wizard.ActiveField)
	}
	// Press Enter on field 1 -> field 2
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveField != 2 {
		t.Fatalf("expected active field 2, got %d", screen.wizard.ActiveField)
	}
	// Press Enter on field 2 -> field 3 (Age Encrypt)
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveField != 3 {
		t.Fatalf("expected active field 3 (encrypt), got %d", screen.wizard.ActiveField)
	}

	// Now press Enter on field 3 (final field of final step)
	// This was where the user was stuck!
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Verify wizard is now closed and Done
	if screen.isWizardOpen {
		t.Fatalf("expected wizard to close after Enter on final field")
	}
	if len(svc.Cfg.Jobs) != 1 {
		t.Fatalf("expected 1 job saved in config, got %d", len(svc.Cfg.Jobs))
	}
	if svc.Cfg.Jobs[0].Name != "my-prod-db" {
		t.Fatalf("expected job name 'my-prod-db', got %q", svc.Cfg.Jobs[0].Name)
	}
}

func TestStorageScreenDynamicWizard(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewService(dir, "")
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := svc.CreateVault("vault-pass"); err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	screen := NewStorageScreen(svc, nil)

	// --- 1. Test Local storage creation (Step 2 should only have 1 field: path) ---
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !screen.isWizardOpen {
		t.Fatalf("expected storage wizard to be open")
	}

	// Step 0: Name (field 0) -> "my-local"
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("my-local")})
	// Press Enter to move to Field 1 (type select)
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveField != 1 {
		t.Fatalf("expected active field 1, got %d", screen.wizard.ActiveField)
	}
	// Option 0 is "local", Option 1 is "gdrive", Option 2 is "s3"
	if screen.wizard.Steps[0].Fields[1].Options[0] != "local" {
		t.Fatalf("expected option 0 to be local, got %s", screen.wizard.Steps[0].Fields[1].Options[0])
	}
	if screen.wizard.Steps[0].Fields[1].Options[1] != "gdrive" {
		t.Fatalf("expected option 1 to be gdrive, got %s", screen.wizard.Steps[0].Fields[1].Options[1])
	}
	if screen.wizard.Steps[0].Fields[1].Options[2] != "s3" {
		t.Fatalf("expected option 2 to be s3, got %s", screen.wizard.Steps[0].Fields[1].Options[2])
	}

	// Press Enter on Field 1 to advance to Step 1
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveStep != 1 {
		t.Fatalf("expected active step 1, got %d", screen.wizard.ActiveStep)
	}
	// Verify Step 1 for "local" has ONLY 1 field!
	if len(screen.wizard.Steps[1].Fields) != 1 {
		t.Fatalf("expected exactly 1 field for local storage, got %d", len(screen.wizard.Steps[1].Fields))
	}
	if screen.wizard.Steps[1].Fields[0].Key != "path" {
		t.Fatalf("expected field key 'path', got %s", screen.wizard.Steps[1].Fields[0].Key)
	}

	// Enter local path
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/var/backups")})
	// Press Enter on path (final field of final step) -> should finish!
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Verify wizard finishes and storage is saved
	if screen.isWizardOpen {
		t.Fatalf("expected wizard to close after Enter on local path")
	}
	if len(svc.Cfg.Storages) != 1 {
		t.Fatalf("expected 1 storage saved, got %d", len(svc.Cfg.Storages))
	}
	st := svc.Cfg.Storages[0]
	if st.Name != "my-local" || st.Type != "local" || st.Path != "/var/backups" {
		t.Fatalf("unexpected storage config: %+v", st)
	}

	// --- 2. Test GDrive storage creation (Step 2 should only have 2 fields: folder_id & credentials) ---
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("my-gdrive")})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter}) // to type select
	// Right arrow to select gdrive (option 1)
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRight})
	if screen.wizard.Steps[0].Fields[1].SelectedOpt != 1 {
		t.Fatalf("expected selected opt 1 for gdrive")
	}
	// Press Enter to advance to Step 1
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(screen.wizard.Steps[1].Fields) != 2 {
		t.Fatalf("expected exactly 2 fields for gdrive storage, got %d", len(screen.wizard.Steps[1].Fields))
	}
	if screen.wizard.Steps[1].Fields[0].Key != "folder_id" || screen.wizard.Steps[1].Fields[1].Key != "credentials" {
		t.Fatalf("unexpected fields for gdrive: %+v", screen.wizard.Steps[1].Fields)
	}

	// Enter folder_id
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("folder123")})
	// Enter moves to credentials
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.wizard.ActiveField != 1 {
		t.Fatalf("expected active field 1, got %d", screen.wizard.ActiveField)
	}
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("{\"dummy\":\"json\"}")})
	// Enter on credentials (final field) finishes!
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.isWizardOpen {
		t.Fatalf("expected wizard to close after Enter on gdrive credentials")
	}
	if len(svc.Cfg.Storages) != 2 {
		t.Fatalf("expected 2 storages saved, got %d", len(svc.Cfg.Storages))
	}
	gSt := svc.Cfg.Storages[1]
	if gSt.Name != "my-gdrive" || gSt.Type != "gdrive" || gSt.FolderID != "folder123" {
		t.Fatalf("unexpected gdrive storage config: %+v", gSt)
	}

	// --- 3. Test S3 storage creation (Step 2 should only have 5 fields) ---
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("my-s3")})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter}) // to type select
	// Right arrow to select s3 (option 2)
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRight})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRight})
	if screen.wizard.Steps[0].Fields[1].SelectedOpt != 2 {
		t.Fatalf("expected selected opt 2 for s3")
	}
	// Press Enter to advance to Step 1
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(screen.wizard.Steps[1].Fields) != 5 {
		t.Fatalf("expected exactly 5 fields for s3 storage, got %d", len(screen.wizard.Steps[1].Fields))
	}
	// Fill bucket
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("test-bucket")})
	// Enter to region
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("us-west-2")})
	// Enter to endpoint (optional, skip)
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Enter to access_key
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("AKIA123")})
	// Enter to secret_key
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("secret456")})
	// Enter on secret_key finishes!
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if screen.isWizardOpen {
		t.Fatalf("expected wizard to close after Enter on s3 secret_key")
	}
	if len(svc.Cfg.Storages) != 3 {
		t.Fatalf("expected 3 storages saved, got %d", len(svc.Cfg.Storages))
	}
	s3St := svc.Cfg.Storages[2]
	if s3St.Name != "my-s3" || s3St.Type != "s3" || s3St.Bucket != "test-bucket" || s3St.Region != "us-west-2" || s3St.AccessKeyID != "AKIA123" {
		t.Fatalf("unexpected s3 storage config: %+v", s3St)
	}
}


