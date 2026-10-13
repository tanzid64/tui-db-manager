package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Config{
		Version: 1,
		Jobs: []Job{{
			Name:      "main",
			Engine:    "mysql",
			Conn:      Conn{Host: "127.0.0.1", Port: 3306, User: "root", Database: "app"},
			Storage:   "local",
			Retention: Retention{KeepLast: 7},
		}},
		Storages: []StorageConfig{{Name: "local", Type: "local", Path: "/var/backups/dbk"}},
	}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600, got %o", perm)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	j, ok := got.Job("main")
	if !ok || j.Engine != "mysql" || j.Conn.Port != 3306 {
		t.Fatalf("job round-trip mismatch: %+v ok=%v", j, ok)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("expected empty config v1, got %+v", got)
	}
}

func TestSaveLoadWithPipelineFields(t *testing.T) {
	dir := t.TempDir()
	c := &Config{
		Version: 1,
		Jobs: []Job{{
			Name: "enc", Engine: "postgres",
			Conn:    Conn{Host: "localhost", Port: 5432, User: "pg", Database: "app"},
			Storage: "s3", Compress: true, Encrypt: true,
			EncryptRecipient: "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p",
		}},
		Storages: []StorageConfig{{Name: "s3", Type: "s3"}},
	}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	j, ok := got.Job("enc")
	if !ok {
		t.Fatal("job not found")
	}
	if !j.Compress || !j.Encrypt || j.EncryptRecipient == "" {
		t.Fatalf("pipeline fields lost: compress=%v encrypt=%v recipient=%q", j.Compress, j.Encrypt, j.EncryptRecipient)
	}
}

func TestSaveLoadWithNotifiers(t *testing.T) {
	dir := t.TempDir()
	c := &Config{
		Version: 1,
		Jobs: []Job{{
			Name:      "prod",
			Engine:    "mysql",
			Storage:   "local",
			Notifiers: []string{"tg", "slack"},
		}},
		Notifiers: []NotifierConfig{
			{Name: "tg", Type: "telegram", ChatID: "12345", BotTokenRef: "env:TG_TOKEN"},
			{Name: "slack", Type: "webhook", URL: "https://hooks.slack.com/services/xxx"},
			{Name: "mail", Type: "email", Host: "smtp.mail.com", Port: 587, From: "a@b.com", To: []string{"c@d.com"}},
		},
	}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	j, ok := got.Job("prod")
	if !ok || len(j.Notifiers) != 2 || j.Notifiers[0] != "tg" {
		t.Fatalf("job notifiers mismatch: %+v", j)
	}
	tg, ok := got.Notifier("tg")
	if !ok || tg.ChatID != "12345" || tg.BotTokenRef != "env:TG_TOKEN" {
		t.Fatalf("telegram notifier mismatch: %+v", tg)
	}
	slack, ok := got.Notifier("slack")
	if !ok || slack.URL != "https://hooks.slack.com/services/xxx" {
		t.Fatalf("webhook notifier mismatch: %+v", slack)
	}
	mail, ok := got.Notifier("mail")
	if !ok || mail.Host != "smtp.mail.com" || len(mail.To) != 1 {
		t.Fatalf("email notifier mismatch: %+v", mail)
	}
}

func TestConfigMerge(t *testing.T) {
	c1 := &Config{
		Jobs: []Job{
			{Name: "j1", Engine: "mysql"},
			{Name: "j2", Engine: "postgres"},
		},
		Storages: []StorageConfig{
			{Name: "s1", Type: "local", Path: "/path1"},
		},
		Notifiers: []NotifierConfig{
			{Name: "n1", Type: "webhook", URL: "http://old"},
		},
	}

	c2 := &Config{
		Jobs: []Job{
			{Name: "j2", Engine: "sqlite"}, // Conflict with j2
			{Name: "j3", Engine: "mongodb"},
		},
		Storages: []StorageConfig{
			{Name: "s1", Type: "local", Path: "/path2"}, // Conflict with s1
			{Name: "s2", Type: "s3", Bucket: "b"},
		},
		Notifiers: []NotifierConfig{
			{Name: "n1", Type: "webhook", URL: "http://new"}, // Conflict with n1
			{Name: "n2", Type: "telegram", ChatID: "123"},
		},
	}

	// 1. Merge without overwrite
	res := c1.Merge(c2, false)
	if res.JobsAdded != 1 || res.JobsSkipped != 1 || res.JobsUpdated != 0 {
		t.Fatalf("unexpected jobs merge: %+v", res)
	}
	if res.StoragesAdded != 1 || res.StoragesSkipped != 1 || res.StoragesUpdated != 0 {
		t.Fatalf("unexpected storages merge: %+v", res)
	}
	if res.NotifiersAdded != 1 || res.NotifiersSkipped != 1 || res.NotifiersUpdated != 0 {
		t.Fatalf("unexpected notifiers merge: %+v", res)
	}
	// Check j2 is still postgres
	j2, _ := c1.Job("j2")
	if j2.Engine != "postgres" {
		t.Fatalf("expected j2 to remain postgres, got %s", j2.Engine)
	}

	// 2. Merge with overwrite
	res = c1.Merge(c2, true)
	if res.JobsUpdated != 2 || res.JobsSkipped != 0 {
		t.Fatalf("unexpected jobs overwrite: %+v", res)
	}
	j2, _ = c1.Job("j2")
	if j2.Engine != "sqlite" {
		t.Fatalf("expected j2 to be overwritten with sqlite, got %s", j2.Engine)
	}
	s1, _ := c1.Storage("s1")
	if s1.Path != "/path2" {
		t.Fatalf("expected s1 to be overwritten, got %s", s1.Path)
	}
	n1, _ := c1.Notifier("n1")
	if n1.URL != "http://new" {
		t.Fatalf("expected n1 to be overwritten, got %s", n1.URL)
	}
}
