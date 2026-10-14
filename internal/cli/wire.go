package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/notify"
	"github.com/tanzid64/tui-db-manager/internal/secretref"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/tanzid64/tui-db-manager/internal/store"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

// SecretGetter abstracts reading secrets by key. Both *store.DB and *vault.Store satisfy this.
type SecretGetter interface {
	Get(key string) (string, bool)
}

func buildBackend(ctx context.Context, sc config.StorageConfig, v SecretGetter) (storage.Backend, error) {
	switch sc.Type {
	case "local":
		return storage.NewLocal(sc.Path), nil
	case "s3":
		secret, err := resolveStorageSecret(sc, v)
		if err != nil {
			return nil, err
		}
		be, err := storage.NewS3(ctx, storage.S3Options{
			Bucket: sc.Bucket, Region: sc.Region, Endpoint: sc.Endpoint, Prefix: sc.Prefix,
			AccessKeyID: sc.AccessKeyID, SecretKey: secret, ForcePathStyle: sc.ForcePathStyle,
		})
		if err != nil {
			return nil, err
		}
		return storage.NewMaskingBackend(be, secret), nil
	case "gdrive":
		jsonCreds, err := resolveGDriveCredentials(sc, v)
		if err != nil {
			return nil, err
		}
		be, err := storage.NewGDrive(ctx, storage.GDriveOptions{
			CredentialsJSON: []byte(jsonCreds), FolderID: sc.FolderID,
		})
		if err != nil {
			return nil, err
		}
		return storage.NewMaskingBackend(be, extractPrivateKey(jsonCreds)), nil
	default:
		return nil, fmt.Errorf("storage type %q not supported", sc.Type)
	}
}

func resolveGDriveCredentials(sc config.StorageConfig, v SecretGetter) (string, error) {
	var lookup func(string) (string, bool)
	if v != nil {
		lookup = v.Get
	}
	ref := sc.CredentialsRef
	if ref == "" {
		ref = "storage/" + sc.Name + "/credentials"
	}
	return secretref.Resolve(ref, lookup)
}

// extractPrivateKey pulls the private_key value out of a service-account JSON so
// it can be masked from errors. Returns "" if not found (masking then no-ops).
func extractPrivateKey(saJSON string) string {
	var m struct {
		PrivateKey string `json:"private_key"`
	}
	if err := json.Unmarshal([]byte(saJSON), &m); err != nil {
		return ""
	}
	return m.PrivateKey
}

// resolveStorageSecret resolves an S3 secret key from the storage's SecretKeyRef
// (env:/file:/vault), defaulting to the vault key "storage/<name>/secret_key".
func resolveStorageSecret(sc config.StorageConfig, v SecretGetter) (string, error) {
	var lookup func(string) (string, bool)
	if v != nil {
		lookup = v.Get
	}
	ref := sc.SecretKeyRef
	if ref == "" {
		ref = "storage/" + sc.Name + "/secret_key"
	}
	return secretref.Resolve(ref, lookup)
}

func passFileOf(cmd *cobra.Command) string {
	pf, _ := cmd.Flags().GetString("passphrase-file")
	return pf
}

func engineResolver() func(string) (db.Engine, bool) {
	return func(name string) (db.Engine, bool) { return db.Get(name) }
}

// resolvePassword returns a closure that resolves a job's DB password from a
// PasswordRef (env:/file:) or the vault key "job/<name>/password".
func resolvePassword(v SecretGetter) func(config.Job) (string, error) {
	return func(j config.Job) (string, error) {
		ref := j.Conn.PasswordRef
		var lookup func(string) (string, bool)
		if v != nil {
			lookup = v.Get
		}
		if ref == "" {
			// No explicit ref: look up the conventional vault key; empty is allowed
			// (e.g. sqlite or trust auth).
			if v == nil {
				return "", nil
			}
			if val, ok := v.Get("job/" + j.Name + "/password"); ok {
				return val, nil
			}
			return "", nil
		}
		return secretref.Resolve(ref, lookup)
	}
}

// resolveIdentity returns a closure that resolves a job's age identity (private key)
// from the vault key "job/<name>/age_identity" for decrypting backups.
func resolveIdentity(v SecretGetter) func(config.Job) (string, error) {
	return func(j config.Job) (string, error) {
		if v == nil {
			return "", fmt.Errorf("vault is required to decrypt backup for job %q", j.Name)
		}
		if val, ok := v.Get("job/" + j.Name + "/age_identity"); ok {
			return val, nil
		}
		return "", fmt.Errorf("age identity for job %q not found in vault (expected key %q)", j.Name, "job/"+j.Name+"/age_identity")
	}
}

// maybeOpenVault opens the vault or database store if it exists, else returns nil.
func maybeOpenVault(dir string) (SecretGetter, error) {
	if store.Exists(dir) {
		key := store.ResolveMasterKey(dir)
		return store.Open(dir, key)
	}
	if vault.Exists(dir) {
		return vault.Open(dir)
	}
	return nil, nil
}

func errUnknownJob(name string) error      { return fmt.Errorf("job %q not found", name) }
func errUnknownStorage(name string) error  { return fmt.Errorf("storage %q not found", name) }
func errUnknownNotifier(name string) error { return fmt.Errorf("notifier %q not found", name) }

func buildNotifier(ctx context.Context, nc config.NotifierConfig, v SecretGetter) (notify.Notifier, error) {
	switch nc.Type {
	case "telegram":
		token, err := resolveNotifierSecret(nc.Name, nc.BotTokenRef, "bot_token", v)
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
			s, err := resolveNotifierSecret(nc.Name, nc.TokenRef, "token", v)
			if err != nil {
				return nil, err
			}
			secret = s
		} else if v != nil {
			if s, ok := v.Get("notifier/" + nc.Name + "/token"); ok {
				secret = s
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
			p, err := resolveNotifierSecret(nc.Name, nc.PasswordRef, "password", v)
			if err != nil {
				return nil, err
			}
			password = p
		} else if v != nil {
			if p, ok := v.Get("notifier/" + nc.Name + "/password"); ok {
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

func resolveNotifierSecret(notifierName, ref, keyName string, v SecretGetter) (string, error) {
	var lookup func(string) (string, bool)
	if v != nil {
		lookup = v.Get
	}
	if ref == "" {
		ref = "notifier/" + notifierName + "/" + keyName
	}
	return secretref.Resolve(ref, lookup)
}
