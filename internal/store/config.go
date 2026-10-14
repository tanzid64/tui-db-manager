package store

import (
	"encoding/json"
	"fmt"

	"github.com/tanzid64/tui-db-manager/internal/config"
)

// LoadConfig reads jobs, storages, and notifiers from the database and constructs a config.Config.
func (d *DB) LoadConfig() (*config.Config, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	cfg := &config.Config{
		Version:   config.CurrentVersion,
		Jobs:      []config.Job{},
		Storages:  []config.StorageConfig{},
		Notifiers: []config.NotifierConfig{},
	}

	// 1. Jobs
	jobRows, err := d.db.Query(`SELECT
		name, engine, conn_host, conn_port, conn_user, conn_database, conn_path, conn_tls, conn_password_ref,
		storage, retention_keep_last, compress, encrypt, schedule, encrypt_recipient, notifiers
	FROM jobs ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("load jobs: %w", err)
	}
	defer jobRows.Close()

	for jobRows.Next() {
		var j config.Job
		var tlsInt, compressInt, encryptInt int
		var notifiersJSON string

		err := jobRows.Scan(
			&j.Name, &j.Engine, &j.Conn.Host, &j.Conn.Port, &j.Conn.User, &j.Conn.Database, &j.Conn.Path, &tlsInt, &j.Conn.PasswordRef,
			&j.Storage, &j.Retention.KeepLast, &compressInt, &encryptInt, &j.Schedule, &j.EncryptRecipient, &notifiersJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		j.Conn.TLS = (tlsInt != 0)
		j.Compress = (compressInt != 0)
		j.Encrypt = (encryptInt != 0)
		if notifiersJSON != "" {
			_ = json.Unmarshal([]byte(notifiersJSON), &j.Notifiers)
		}
		cfg.Jobs = append(cfg.Jobs, j)
	}

	// 2. Storages
	storageRows, err := d.db.Query(`SELECT
		name, type, path, bucket, region, endpoint, prefix, access_key_id, force_path_style, secret_key_ref, folder_id, credentials_ref
	FROM storages ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("load storages: %w", err)
	}
	defer storageRows.Close()

	for storageRows.Next() {
		var s config.StorageConfig
		var forcePathStyleInt int
		err := storageRows.Scan(
			&s.Name, &s.Type, &s.Path, &s.Bucket, &s.Region, &s.Endpoint, &s.Prefix,
			&s.AccessKeyID, &forcePathStyleInt, &s.SecretKeyRef, &s.FolderID, &s.CredentialsRef,
		)
		if err != nil {
			return nil, fmt.Errorf("scan storage: %w", err)
		}
		s.ForcePathStyle = (forcePathStyleInt != 0)
		cfg.Storages = append(cfg.Storages, s)
	}

	// 3. Notifiers
	notifierRows, err := d.db.Query(`SELECT
		name, type, bot_token_ref, chat_id, host, port, from_addr, to_addrs, username, password_ref, tls, url, headers, token_ref
	FROM notifiers ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("load notifiers: %w", err)
	}
	defer notifierRows.Close()

	for notifierRows.Next() {
		var n config.NotifierConfig
		var tlsInt int
		var toAddrsJSON, headersJSON string
		err := notifierRows.Scan(
			&n.Name, &n.Type, &n.BotTokenRef, &n.ChatID, &n.Host, &n.Port, &n.From,
			&toAddrsJSON, &n.Username, &n.PasswordRef, &tlsInt, &n.URL, &headersJSON, &n.TokenRef,
		)
		if err != nil {
			return nil, fmt.Errorf("scan notifier: %w", err)
		}
		n.TLS = (tlsInt != 0)
		if toAddrsJSON != "" {
			_ = json.Unmarshal([]byte(toAddrsJSON), &n.To)
		}
		if headersJSON != "" {
			_ = json.Unmarshal([]byte(headersJSON), &n.Headers)
		}
		cfg.Notifiers = append(cfg.Notifiers, n)
	}

	return cfg, nil
}

// SaveConfig updates the jobs, storages, and notifiers tables in SQLite with the given Config.
func (d *DB) SaveConfig(c *config.Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Clear existing rows
	if _, err := tx.Exec("DELETE FROM jobs; DELETE FROM storages; DELETE FROM notifiers;"); err != nil {
		return fmt.Errorf("clear config tables: %w", err)
	}

	// Insert jobs
	jobStmt, err := tx.Prepare(`INSERT INTO jobs (
		name, engine, conn_host, conn_port, conn_user, conn_database, conn_path, conn_tls, conn_password_ref,
		storage, retention_keep_last, compress, encrypt, schedule, encrypt_recipient, notifiers
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer jobStmt.Close()

	for _, j := range c.Jobs {
		notifiersJSON, _ := json.Marshal(j.Notifiers)
		tlsInt := 0
		if j.Conn.TLS {
			tlsInt = 1
		}
		compressInt := 0
		if j.Compress {
			compressInt = 1
		}
		encryptInt := 0
		if j.Encrypt {
			encryptInt = 1
		}
		_, err := jobStmt.Exec(
			j.Name, j.Engine, j.Conn.Host, j.Conn.Port, j.Conn.User, j.Conn.Database, j.Conn.Path, tlsInt, j.Conn.PasswordRef,
			j.Storage, j.Retention.KeepLast, compressInt, encryptInt, j.Schedule, j.EncryptRecipient, string(notifiersJSON),
		)
		if err != nil {
			return fmt.Errorf("insert job %q: %w", j.Name, err)
		}
	}

	// Insert storages
	storageStmt, err := tx.Prepare(`INSERT INTO storages (
		name, type, path, bucket, region, endpoint, prefix, access_key_id, force_path_style, secret_key_ref, folder_id, credentials_ref
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer storageStmt.Close()

	for _, s := range c.Storages {
		forceInt := 0
		if s.ForcePathStyle {
			forceInt = 1
		}
		_, err := storageStmt.Exec(
			s.Name, s.Type, s.Path, s.Bucket, s.Region, s.Endpoint, s.Prefix,
			s.AccessKeyID, forceInt, s.SecretKeyRef, s.FolderID, s.CredentialsRef,
		)
		if err != nil {
			return fmt.Errorf("insert storage %q: %w", s.Name, err)
		}
	}

	// Insert notifiers
	notifierStmt, err := tx.Prepare(`INSERT INTO notifiers (
		name, type, bot_token_ref, chat_id, host, port, from_addr, to_addrs, username, password_ref, tls, url, headers, token_ref
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer notifierStmt.Close()

	for _, n := range c.Notifiers {
		toAddrsJSON, _ := json.Marshal(n.To)
		headersJSON, _ := json.Marshal(n.Headers)
		tlsInt := 0
		if n.TLS {
			tlsInt = 1
		}
		_, err := notifierStmt.Exec(
			n.Name, n.Type, n.BotTokenRef, n.ChatID, n.Host, n.Port, n.From,
			string(toAddrsJSON), n.Username, n.PasswordRef, tlsInt, n.URL, string(headersJSON), n.TokenRef,
		)
		if err != nil {
			return fmt.Errorf("insert notifier %q: %w", n.Name, err)
		}
	}

	return tx.Commit()
}
