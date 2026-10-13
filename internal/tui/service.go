package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/backup"
	"github.com/tanzid64/tui-db-manager/internal/bundle"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/notify"
	"github.com/tanzid64/tui-db-manager/internal/restore"
	"github.com/tanzid64/tui-db-manager/internal/schedule"
	"github.com/tanzid64/tui-db-manager/internal/secretref"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

type Service struct {
	ConfigDir  string
	PassFile   string
	Cfg        *config.Config
	Vault      *vault.Store
	passphrase string
	Log        *logutil.Logger
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
	_ = s.ReloadConfig()
	return s, nil
}

func (s *Service) ReloadConfig() error {
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
	return config.Save(s.ConfigDir, s.Cfg)
}

func (s *Service) HasConfig() bool {
	return config.Exists(s.ConfigDir)
}

func (s *Service) HasVault() bool {
	return vault.Exists(s.ConfigDir)
}

func (s *Service) IsVaultUnlocked() bool {
	return s.Vault != nil
}

func (s *Service) TryAutoUnlock() error {
	if !s.HasVault() {
		return nil
	}
	pass, err := vault.ReadPassphrase(s.ConfigDir, s.PassFile, false)
	if err != nil {
		return err
	}
	return s.UnlockVault(pass)
}

func (s *Service) UnlockVault(passphrase string) error {
	v, err := vault.Open(s.ConfigDir, passphrase)
	if err != nil {
		return err
	}
	s.Vault = v
	s.passphrase = passphrase
	return nil
}

func (s *Service) CreateVault(passphrase string) error {
	v, err := vault.Create(s.ConfigDir, passphrase)
	if err != nil {
		return err
	}
	s.Vault = v
	s.passphrase = passphrase
	return nil
}

func (s *Service) ChangePassphrase(newPass string) error {
	if s.Vault == nil {
		return fmt.Errorf("vault is locked")
	}
	if err := s.Vault.ChangePassphrase(newPass); err != nil {
		return err
	}
	s.passphrase = newPass
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
		globalHistory.Add(rec)
		return rec, err
	}

	rec.Status = StatusSuccess
	rec.Size = obj.Size
	rec.Target = obj.Key
	globalHistory.Add(rec)
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
		globalHistory.Add(rec)
		return err
	}

	rec.Status = StatusSuccess
	globalHistory.Add(rec)
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
	return drv.Install(context.Background(), jobName, schedule.InstallOptions{
		Expression:     expr,
		ConfigDir:      s.ConfigDir,
		PassphraseFile: s.PassFile,
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
