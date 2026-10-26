package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"filippo.io/age"

	"github.com/tanzid64/tui-db-manager/internal/backup"
	"github.com/tanzid64/tui-db-manager/internal/bundle"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/history"
	"github.com/tanzid64/tui-db-manager/internal/jobconn"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/notify"
	"github.com/tanzid64/tui-db-manager/internal/restore"
	"github.com/tanzid64/tui-db-manager/internal/schedule"
	"github.com/tanzid64/tui-db-manager/internal/secretref"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/tanzid64/tui-db-manager/internal/store"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

type VaultStore interface {
	Get(key string) (string, bool)
	Set(key, val string)
	Delete(key string)
	Keys() []string
	Save() error
	Export(transferPassphrase string) ([]byte, error)
	Merge(secrets map[string]string, overwrite bool) (added, updated, skipped int)
}

type Service struct {
	ConfigDir string
	PassFile  string
	Cfg       *config.Config
	Vault     VaultStore
	Store     *store.DB
	Log       *logutil.Logger
}

func NewService(configDir, passFile string) (*Service, error) {
	dir, err := config.Dir(configDir)
	if err != nil {
		return nil, err
	}
	s := &Service{
		ConfigDir: dir,
		PassFile:  passFile,
		Log:       logutil.New(io.Discard, logutil.LevelInfo),
	}

	key := store.ResolveMasterKey(dir)

	if store.Exists(dir) {
		db, err := store.Open(dir, key)
		if err == nil {
			_, _ = store.UpgradeDefaultKey(dir, db)
			s.Store = db
			s.Vault = db
			globalHistory = history.NewDB(db)
			s.Cfg, _ = db.LoadConfig()
		} else {
			// Open locked if key invalid or empty
			if lockedDB, err := store.Open(dir, ""); err == nil {
				s.Store = lockedDB
				s.Vault = lockedDB
				globalHistory = history.NewDB(lockedDB)
				s.Cfg, _ = lockedDB.LoadConfig()
			}
		}
	} else if store.LegacyFilesExist(dir) {
		if key == "" {
			key = store.GenerateMasterKey()
		}
		db, err := store.Init(dir, key)
		if err == nil {
			_ = store.SaveMasterKeyToEnv(dir, key)
			_, _ = store.MigrateFromFiles(dir, db)
			s.Store = db
			s.Vault = db
			globalHistory = history.NewDB(db)
			s.Cfg, _ = db.LoadConfig()
		}
	} else {
		// New database initialization
		if key == "" {
			key = store.GenerateMasterKey()
		}
		db, err := store.Init(dir, key)
		if err == nil {
			_ = store.SaveMasterKeyToEnv(dir, key)
			s.Store = db
			s.Vault = db
			globalHistory = history.NewDB(db)
			_ = db.SaveConfig(&config.Config{
				Version:   config.CurrentVersion,
				Jobs:      []config.Job{},
				Storages:  []config.StorageConfig{},
				Notifiers: []config.NotifierConfig{},
			})
			s.Cfg, _ = db.LoadConfig()
		} else if vault.Exists(dir) {
			if v, err := vault.Open(dir); err == nil {
				s.Vault = v
			}
			globalHistory = history.New(filepath.Join(dir, "history.jsonl"))
			_ = s.ReloadConfig()
		}
	}

	if s.Cfg == nil {
		_ = s.ReloadConfig()
	}
	// Every install starts with a ready-to-use local storage.
	if s.Store != nil && s.Cfg != nil && s.Cfg.EnsureDefaultStorage() {
		_ = s.SaveConfig()
		_ = os.MkdirAll(config.DefaultBackupDir(), 0o700)
	}
	if s.Vault == nil && vault.Exists(dir) {
		if v, err := vault.Open(dir); err == nil {
			s.Vault = v
		}
	}

	return s, nil
}

func (s *Service) ReloadConfig() error {
	if s.Store != nil {
		cfg, err := s.Store.LoadConfig()
		if err == nil && cfg != nil {
			s.Cfg = cfg
			return nil
		}
	}

	if config.Exists(s.ConfigDir) {
		cfg, err := config.Load(s.ConfigDir)
		if err != nil {
			return err
		}
		s.Cfg = cfg
	} else {
		s.Cfg = &config.Config{
			Version:   config.CurrentVersion,
			Jobs:      []config.Job{},
			Storages:  []config.StorageConfig{},
			Notifiers: []config.NotifierConfig{},
		}
	}
	return nil
}

func (s *Service) SaveConfig() error {
	if s.Cfg == nil {
		return fmt.Errorf("no config loaded")
	}
	if s.Store != nil {
		return s.Store.SaveConfig(s.Cfg)
	}
	return config.Save(s.ConfigDir, s.Cfg)
}

func (s *Service) HasConfig() bool {
	if s.Store != nil {
		return true
	}
	return config.Exists(s.ConfigDir)
}

func (s *Service) HasVault() bool {
	if s.Store != nil {
		return true
	}
	return s.Vault != nil
}

func (s *Service) IsVaultUnlocked() bool {
	if s.Store != nil {
		return s.Store.IsUnlocked()
	}
	return s.Vault != nil
}

func (s *Service) TryAutoUnlock() error {
	if s.Store != nil && !s.Store.IsUnlocked() {
		key := store.ResolveMasterKey(s.ConfigDir)
		if key != "" {
			_ = s.Store.Unlock(key)
		}
	}
	return nil
}

func (s *Service) UnlockVault(pass ...string) error {
	if len(pass) > 0 && pass[0] != "" && s.Store != nil {
		return s.Store.Unlock(pass[0])
	}
	if s.Store != nil {
		key := store.ResolveMasterKey(s.ConfigDir)
		return s.Store.Unlock(key)
	}
	v, err := vault.Open(s.ConfigDir)
	if err != nil {
		return err
	}
	s.Vault = v
	return nil
}

func (s *Service) CreateVault(pass ...string) error {
	key := store.ResolveMasterKey(s.ConfigDir)
	if len(pass) > 0 && pass[0] != "" {
		key = pass[0]
	}
	if key == "" {
		if s.Store != nil || store.Exists(s.ConfigDir) {
			key = store.DefaultMasterKey // installs from older versions
		} else {
			key = store.GenerateMasterKey()
		}
	}
	if s.Store != nil && s.Store.IsUnlocked() {
		return nil
	}
	if s.Store != nil {
		return s.Store.Unlock(key)
	}
	if store.Exists(s.ConfigDir) {
		db, err := store.Open(s.ConfigDir, key)
		if err == nil {
			s.Store = db
			s.Vault = db
			globalHistory = history.NewDB(db)
			return nil
		}
	}
	db, err := store.Init(s.ConfigDir, key)
	if err != nil {
		return err
	}
	s.Store = db
	s.Vault = db
	globalHistory = history.NewDB(db)
	return nil
}

func (s *Service) BuildBackend(ctx context.Context, sc config.StorageConfig) (storage.Backend, error) {
	switch sc.Type {
	case "local":
		return storage.NewLocal(sc.Path), nil
	case "s3":
		var lookup func(string) (string, bool)
		if s.Vault != nil {
			lookup = s.Vault.Get
		}
		ref := sc.SecretKeyRef
		if ref == "" {
			ref = "storage/" + sc.Name + "/secret_key"
		}
		secret, err := secretref.Resolve(ref, lookup)
		if err != nil {
			return nil, err
		}
		be, err := storage.NewS3(ctx, storage.S3Options{
			Bucket:         sc.Bucket,
			Region:         sc.Region,
			Endpoint:       sc.Endpoint,
			Prefix:         sc.Prefix,
			AccessKeyID:    sc.AccessKeyID,
			SecretKey:      secret,
			ForcePathStyle: sc.ForcePathStyle,
		})
		if err != nil {
			return nil, err
		}
		return storage.NewMaskingBackend(be, secret), nil
	case "gdrive":
		var lookup func(string) (string, bool)
		if s.Vault != nil {
			lookup = s.Vault.Get
		}
		ref := sc.CredentialsRef
		if ref == "" {
			ref = "storage/" + sc.Name + "/credentials"
		}
		jsonCreds, err := secretref.Resolve(ref, lookup)
		if err != nil {
			return nil, err
		}
		be, err := storage.NewGDrive(ctx, storage.GDriveOptions{
			CredentialsJSON: []byte(jsonCreds),
			FolderID:        sc.FolderID,
		})
		if err != nil {
			return nil, err
		}
		var m struct {
			PrivateKey string `json:"private_key"`
		}
		_ = json.Unmarshal([]byte(jsonCreds), &m)
		return storage.NewMaskingBackend(be, m.PrivateKey), nil
	default:
		return nil, fmt.Errorf("storage type %q not supported", sc.Type)
	}
}

func (s *Service) BuildNotifier(ctx context.Context, nc config.NotifierConfig) (notify.Notifier, error) {
	var lookup func(string) (string, bool)
	if s.Vault != nil {
		lookup = s.Vault.Get
	}
	switch nc.Type {
	case "telegram":
		ref := nc.BotTokenRef
		if ref == "" {
			ref = "notifier/" + nc.Name + "/bot_token"
		}
		token, err := secretref.Resolve(ref, lookup)
		if err != nil {
			return nil, err
		}
		return notify.NewTelegram(notify.TelegramOptions{
			BotToken: token,
			ChatID:   nc.ChatID,
		})
	case "webhook":
		var secret string
		if nc.TokenRef != "" {
			sec, err := secretref.Resolve(nc.TokenRef, lookup)
			if err != nil {
				return nil, err
			}
			secret = sec
		} else if s.Vault != nil {
			if sec, ok := s.Vault.Get("notifier/" + nc.Name + "/token"); ok {
				secret = sec
			}
		}
		return notify.NewWebhook(notify.WebhookOptions{
			URL:     nc.URL,
			Headers: nc.Headers,
			Secret:  secret,
		})
	case "email":
		var password string
		if nc.PasswordRef != "" {
			p, err := secretref.Resolve(nc.PasswordRef, lookup)
			if err != nil {
				return nil, err
			}
			password = p
		} else if s.Vault != nil {
			if p, ok := s.Vault.Get("notifier/" + nc.Name + "/password"); ok {
				password = p
			}
		}
		return notify.NewEmail(notify.EmailOptions{
			Host:     nc.Host,
			Port:     nc.Port,
			From:     nc.From,
			To:       nc.To,
			Username: nc.Username,
			Password: password,
			TLS:      nc.TLS,
		})
	default:
		return nil, fmt.Errorf("notifier type %q not supported", nc.Type)
	}
}

func (s *Service) ResolveJobPassword(j config.Job) (string, error) {
	ref := j.Conn.PasswordRef
	var lookup func(string) (string, bool)
	if s.Vault != nil {
		lookup = s.Vault.Get
	}
	if ref == "" {
		if s.Vault == nil {
			return "", nil
		}
		if val, ok := s.Vault.Get("job/" + j.Name + "/password"); ok {
			return val, nil
		}
		return "", nil
	}
	return secretref.Resolve(ref, lookup)
}

func (s *Service) ResolveJobIdentity(j config.Job) (string, error) {
	if s.Vault == nil {
		return "", fmt.Errorf("vault is required to decrypt backup for job %q", j.Name)
	}
	if val, ok := s.Vault.Get("job/" + j.Name + "/age_identity"); ok {
		return val, nil
	}
	return "", fmt.Errorf("age identity for job %q not found in vault", j.Name)
}

// ResolveJobSSH resolves the job's SSH tunnel settings (nil if it has none).
func (s *Service) ResolveJobSSH(j config.Job) (*db.SSHTunnel, error) {
	var lookup jobconn.Lookup
	if s.Vault != nil {
		lookup = s.Vault.Get
	}
	return jobconn.ResolveSSH(j, lookup)
}

// ListDatabases lists databases on the server, through tunnel when non-nil.
func (s *Service) ListDatabases(ctx context.Context, engine string, conn config.Conn, password string, tunnel *db.SSHTunnel) ([]string, error) {
	c := db.Conn{
		SSH:      tunnel,
		Host:     conn.Host,
		Port:     conn.Port,
		User:     conn.User,
		Password: password,
		Database: conn.Database,
		Path:     conn.Path,
		TLS:      conn.TLS,
	}
	return db.ListDatabases(ctx, engine, c)
}

func (s *Service) RunBackup(ctx context.Context, jobName string) (RunRecord, error) {
	start := time.Now()
	rec := RunRecord{
		ID:      fmt.Sprintf("%s-%d", jobName, start.Unix()),
		Job:     jobName,
		Action:  "backup",
		Started: start,
		Status:  StatusRunning,
	}

	deps := backup.Deps{
		Cfg: s.Cfg,
		Engine: func(name string) (db.Engine, bool) {
			return db.Get(name)
		},
		Backend: func(sc config.StorageConfig) (storage.Backend, error) {
			return s.BuildBackend(ctx, sc)
		},
		ResolvePassword: s.ResolveJobPassword,
		ResolveIdentity: s.ResolveJobIdentity,
		ResolveSSH:      s.ResolveJobSSH,
		Notifier: func(nc config.NotifierConfig) (notify.Notifier, error) {
			return s.BuildNotifier(ctx, nc)
		},
		Now: time.Now,
		Log: s.Log,
	}

	obj, err := backup.Run(ctx, deps, jobName)
	rec.Duration = time.Since(start)
	if err != nil {
		rec.Status = StatusFailed
		rec.Error = err.Error()
		globalHistory.Append(rec)
		return rec, err
	}

	rec.Status = StatusSuccess
	rec.Size = obj.Size
	rec.Target = obj.Key
	globalHistory.Append(rec)
	return rec, nil
}

// ListBackups lists the job's backups in every configured storage, newest
// first. Backups in a storage that can't be read are skipped and its error is
// returned in warnings.
func (s *Service) ListBackups(ctx context.Context, jobName string) (backups []restore.Backup, warnings []error) {
	return restore.FindBackups(ctx, s.Cfg, jobName, func(sc config.StorageConfig) (storage.Backend, error) {
		return s.BuildBackend(ctx, sc)
	})
}

// RunRestore restores the backup key from the named storage. An empty
// storageName means the job's current storage.
func (s *Service) RunRestore(ctx context.Context, jobName, storageName, key, toDB string) error {
	start := time.Now()
	rec := RunRecord{
		ID:      fmt.Sprintf("restore-%s-%d", jobName, start.Unix()),
		Job:     jobName,
		Action:  "restore",
		Started: start,
		Status:  StatusRunning,
		Target:  key,
	}

	job, ok := s.Cfg.Job(jobName)
	if !ok {
		return fmt.Errorf("job %q not found", jobName)
	}
	if storageName == "" {
		storageName = job.Storage
	}
	sc, ok := s.Cfg.Storage(storageName)
	if !ok {
		return fmt.Errorf("storage %q not found", storageName)
	}
	be, err := s.BuildBackend(ctx, sc)
	if err != nil {
		return err
	}

	deps := restore.Deps{
		Cfg: s.Cfg,
		Engine: func(name string) (db.Engine, bool) {
			return db.Get(name)
		},
		Backend: func() storage.Backend {
			return be
		},
		ResolvePassword: s.ResolveJobPassword,
		ResolveIdentity: s.ResolveJobIdentity,
		ResolveSSH:      s.ResolveJobSSH,
		Log:             s.Log,
	}

	err = restore.Run(ctx, deps, jobName, key, toDB, true)
	rec.Duration = time.Since(start)
	if err != nil {
		rec.Status = StatusFailed
		rec.Error = err.Error()
		globalHistory.Append(rec)
		return err
	}

	rec.Status = StatusSuccess
	globalHistory.Append(rec)
	return nil
}

func (s *Service) TestStorage(ctx context.Context, name string) error {
	sc, ok := s.Cfg.Storage(name)
	if !ok {
		return fmt.Errorf("storage %q not found", name)
	}
	be, err := s.BuildBackend(ctx, sc)
	if err != nil {
		return err
	}
	return storage.Verify(ctx, be)
}

func (s *Service) TestNotifier(ctx context.Context, name string) error {
	nc, ok := s.Cfg.Notifier(name)
	if !ok {
		return fmt.Errorf("notifier %q not found", name)
	}
	n, err := s.BuildNotifier(ctx, nc)
	if err != nil {
		return err
	}
	return n.Send(ctx, notify.Event{
		Type:    notify.EventBackupSucceeded,
		Job:     "test",
		Started: time.Now(),
		Elapsed: 0,
	})
}

// schedulerFor and removeAllSchedules are variables so tests can avoid the
// real system scheduler.
var (
	schedulerFor       = schedule.DetectScheduler
	removeAllSchedules = schedule.RemoveEverywhere
)

// InstallSchedule registers the job with the OS scheduler (crontab on VPS and
// cPanel) and remembers the expression on the job.
func (s *Service) InstallSchedule(jobName, expr string) error {
	if expr == "" {
		expr = "0 3 * * *"
	}
	masterKey := ""
	if s.Store != nil {
		masterKey = s.Store.Passphrase()
	}
	if masterKey == "" {
		masterKey = store.ResolveMasterKey(s.ConfigDir)
	}
	masterKey = store.KeyForSchedule(s.ConfigDir, masterKey)
	ctx := context.Background()
	// Clear any timer an older dbk version installed in another scheduler.
	_ = removeAllSchedules(ctx, jobName)
	if err := schedulerFor().Install(ctx, jobName, schedule.InstallOptions{
		Expression:     expr,
		ConfigDir:      s.ConfigDir,
		PassphraseFile: s.PassFile,
		MasterKey:      masterKey,
	}); err != nil {
		return err
	}
	return s.setJobSchedule(jobName, expr)
}

// RemoveSchedule unregisters the job from every scheduler and forgets the
// expression.
func (s *Service) RemoveSchedule(jobName string) error {
	if err := removeAllSchedules(context.Background(), jobName); err != nil {
		return err
	}
	return s.setJobSchedule(jobName, "")
}

// ScheduleStatus reports whether the OS scheduler has the job installed.
func (s *Service) ScheduleStatus(jobName string) (schedule.ScheduleStatus, error) {
	return schedulerFor().Status(context.Background(), jobName)
}

// SchedulerName is the scheduler new schedules are installed into.
func (s *Service) SchedulerName() string { return schedulerFor().Name() }

// ScheduleLogFile is where scheduled runs of the job write their output.
func (s *Service) ScheduleLogFile(jobName string) string {
	return schedule.LogFile(s.ConfigDir, jobName)
}

func (s *Service) setJobSchedule(jobName, expr string) error {
	for i := range s.Cfg.Jobs {
		if s.Cfg.Jobs[i].Name == jobName {
			s.Cfg.Jobs[i].Schedule = expr
		}
	}
	return s.SaveConfig()
}

// TestJob checks that the job's database is reachable with its credentials.
func (s *Service) TestJob(ctx context.Context, jobName string) error {
	job, ok := s.Cfg.Job(jobName)
	if !ok {
		return fmt.Errorf("job %q not found", jobName)
	}
	eng, ok := db.Get(job.Engine)
	if !ok {
		return fmt.Errorf("unknown engine %q", job.Engine)
	}
	pw, err := s.ResolveJobPassword(job)
	if err != nil {
		return err
	}
	tunnel, err := s.ResolveJobSSH(job)
	if err != nil {
		return err
	}
	if err := eng.Test(ctx, db.Conn{
		Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
		Password: pw, Database: job.Conn.Database, Path: job.Conn.Path, TLS: job.Conn.TLS,
		SSH: tunnel,
	}); err != nil {
		return err
	}
	// The connection works, but backups also need their own tools.
	if tc, ok := eng.(db.ToolChecker); ok {
		if err := tc.CheckTools(db.ActionBackup, db.ActionRestore); err != nil {
			return fmt.Errorf("connection OK, but backups will fail: %w", err)
		}
	}
	return nil
}

// ToolsMissing returns the missing-tools error for engine, or nil. Results
// are cached briefly because screens call it on every render.
func (s *Service) ToolsMissing(engine string) error {
	toolCache.mu.Lock()
	defer toolCache.mu.Unlock()
	if c, ok := toolCache.m[engine]; ok && time.Since(c.at) < 10*time.Second {
		return c.err
	}
	err := db.CheckTools(engine, db.AllActions...)
	if toolCache.m == nil {
		toolCache.m = map[string]toolCheck{}
	}
	toolCache.m[engine] = toolCheck{err: err, at: time.Now()}
	return err
}

type toolCheck struct {
	err error
	at  time.Time
}

var toolCache struct {
	mu sync.Mutex
	m  map[string]toolCheck
}

// EnsureEncryptionKey makes sure an encrypted job has an age key pair: the
// public key goes on the job, the private key into the vault. An existing
// key pair is kept so older backups stay restorable.
func (s *Service) EnsureEncryptionKey(job *config.Job) error {
	if !job.Encrypt {
		return nil
	}
	if s.Vault == nil {
		return fmt.Errorf("vault is not available to store the encryption key")
	}
	if job.EncryptRecipient != "" {
		if _, ok := s.Vault.Get("job/" + job.Name + "/age_identity"); ok {
			return nil
		}
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("generate encryption key: %w", err)
	}
	s.Vault.Set("job/"+job.Name+"/age_identity", id.String())
	if err := s.Vault.Save(); err != nil {
		return err
	}
	job.EncryptRecipient = id.Recipient().String()
	return nil
}

func (s *Service) ExportBundle(destPath, transferPass string, includeSecrets bool) error {
	pass := ""
	if includeSecrets {
		pass = transferPass
	}
	b, err := bundle.Export(s.Cfg, s.Vault, pass)
	if err != nil {
		return err
	}
	return b.Save(destPath)
}

func (s *Service) ImportBundle(srcPath, transferPass string, overwrite bool) (*bundle.ImportResult, error) {
	b, err := bundle.Load(srcPath)
	if err != nil {
		return nil, err
	}
	res, err := b.Import(s.ConfigDir, s.Vault, transferPass, overwrite)
	if err != nil {
		return nil, err
	}
	_ = s.ReloadConfig()
	return res, nil
}

// UninstallResult says what Uninstall removed, for the message shown after
// dbk exits.
type UninstallResult struct {
	SchedulesRemoved []string
	DataDir          string // set when the data directory was deleted
	Binary           string // set when the dbk program was deleted
}

// Uninstall removes schedules, the data directory, and the dbk program as
// asked. It keeps going after a failure and returns every error joined, so a
// partial uninstall is reported instead of claimed as complete.
func (s *Service) Uninstall(ctx context.Context, removeSchedules, purgeData, removeBinary bool) (UninstallResult, error) {
	var res UninstallResult
	var errs []error
	schedulesDir := filepath.Join(s.ConfigDir, "schedules")

	if removeSchedules {
		var jobs []string
		if s.Cfg != nil {
			for _, j := range s.Cfg.Jobs {
				jobs = append(jobs, j.Name)
			}
		}
		// Wrapper scripts left by jobs that were since deleted or renamed.
		if entries, err := os.ReadDir(schedulesDir); err == nil {
			for _, e := range entries {
				name := e.Name()
				if strings.HasPrefix(name, "dbk-") && strings.HasSuffix(name, ".sh") {
					job := strings.TrimSuffix(strings.TrimPrefix(name, "dbk-"), ".sh")
					if !slices.Contains(jobs, job) {
						jobs = append(jobs, job)
					}
				}
			}
		}
		failed := false
		for _, job := range jobs {
			if err := removeAllSchedules(ctx, job); err != nil {
				errs = append(errs, fmt.Errorf("remove schedule for %q: %w", job, err))
				failed = true
				continue
			}
			res.SchedulesRemoved = append(res.SchedulesRemoved, job)
		}
		// Keep the wrapper scripts if a schedule is still installed, so a
		// retry can find it.
		if !failed {
			if err := os.RemoveAll(schedulesDir); err != nil {
				errs = append(errs, fmt.Errorf("remove schedule scripts: %w", err))
			}
		}
	}

	if purgeData {
		if s.Store != nil {
			_ = s.Store.Close()
		}
		if err := os.RemoveAll(s.ConfigDir); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", s.ConfigDir, err))
		} else {
			res.DataDir = s.ConfigDir
		}
		s.Cfg = &config.Config{Version: config.CurrentVersion}
		s.Vault = nil
		s.Store = nil
	}

	if removeBinary {
		exe, err := os.Executable()
		if err == nil {
			exe, err = filepath.EvalSymlinks(exe)
		}
		if err == nil {
			err = os.Remove(exe)
			if err != nil {
				err = fmt.Errorf("delete %s: %w (run: sudo rm %q)", exe, err, exe)
			} else {
				res.Binary = exe
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return res, errors.Join(errs...)
}

// SecurityNotice returns a warning when the vault is still protected by the
// public default master key (only possible when DBK_MASTER_KEY forces it).
func (s *Service) SecurityNotice() string {
	if s.Store != nil && s.Store.UsesDefaultKey() {
		return "Saved passwords use the public default master key. Unset DBK_MASTER_KEY and reopen dbk to replace it."
	}
	return ""
}
