package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/backup"
	"github.com/tanzid64/tui-db-manager/internal/bundle"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/history"
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
			key = "dbk-default"
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
			key = "dbk-default"
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
	key := "dbk-default"
	if len(pass) > 0 && pass[0] != "" {
		key = pass[0]
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

func (s *Service) ListDatabases(ctx context.Context, engine string, conn config.Conn, password string) ([]string, error) {
	c := db.Conn{
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

func (s *Service) ListBackups(ctx context.Context, jobName string) ([]storage.Object, error) {
	job, ok := s.Cfg.Job(jobName)
	if !ok {
		return nil, fmt.Errorf("job %q not found", jobName)
	}
	sc, ok := s.Cfg.Storage(job.Storage)
	if !ok {
		return nil, fmt.Errorf("storage %q not found", job.Storage)
	}
	be, err := s.BuildBackend(ctx, sc)
	if err != nil {
		return nil, err
	}
	return be.List(ctx, job.Name+"/")
}

func (s *Service) RunRestore(ctx context.Context, jobName, key, toDB string) error {
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
	sc, ok := s.Cfg.Storage(job.Storage)
	if !ok {
		return fmt.Errorf("storage %q not found", job.Storage)
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
	_, err = be.List(ctx, "")
	return err
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

func (s *Service) InstallSchedule(jobName, expr string) error {
	drv := schedule.DetectScheduler()
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
	return drv.Install(context.Background(), jobName, schedule.InstallOptions{
		Expression:     expr,
		ConfigDir:      s.ConfigDir,
		PassphraseFile: s.PassFile,
		MasterKey:      masterKey,
	})
}

func (s *Service) RemoveSchedule(jobName string) error {
	drv := schedule.DetectScheduler()
	return drv.Remove(context.Background(), jobName)
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

func (s *Service) Uninstall(ctx context.Context, removeSchedules, purgeData bool) error {
	if removeSchedules {
		sched := schedule.DetectScheduler()
		if s.Cfg != nil {
			for _, j := range s.Cfg.Jobs {
				_ = sched.Remove(ctx, j.Name)
			}
		}
		_ = os.RemoveAll(filepath.Join(s.ConfigDir, "schedules"))
	}
	if purgeData {
		if s.Store != nil {
			_ = s.Store.Close()
		}
		_ = os.RemoveAll(s.ConfigDir)
		s.Cfg = &config.Config{Version: config.CurrentVersion}
		s.Vault = nil
		s.Store = nil
	}
	return nil
}
