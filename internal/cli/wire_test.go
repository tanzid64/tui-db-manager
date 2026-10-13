package cli

import (
	"context"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
)

func TestBuildBackendLocal(t *testing.T) {
	be, err := buildBackend(context.Background(), config.StorageConfig{Name: "l", Type: "local", Path: t.TempDir()}, nil)
	if err != nil || be.Name() != "local" {
		t.Fatalf("local backend: %v name=%v", err, be)
	}
}

func TestBuildBackendS3ResolvesSecretFromEnvRef(t *testing.T) {
	t.Setenv("S3_SECRET", "theSecret")
	be, err := buildBackend(context.Background(), config.StorageConfig{
		Name: "r2", Type: "s3", Bucket: "b", Region: "auto", AccessKeyID: "AK",
		SecretKeyRef: "env:S3_SECRET",
	}, nil)
	if err != nil {
		t.Fatalf("s3 backend: %v", err)
	}
	if be.Name() != "s3" {
		t.Fatalf("expected s3 backend, got %q", be.Name())
	}
}

func TestBuildBackendS3MissingSecretErrors(t *testing.T) {
	_, err := buildBackend(context.Background(), config.StorageConfig{
		Name: "r2", Type: "s3", Bucket: "b", AccessKeyID: "AK", SecretKeyRef: "env:DOES_NOT_EXIST",
	}, nil)
	if err == nil {
		t.Fatal("expected error when the secret key cannot be resolved")
	}
}

func TestBuildNotifierTelegram(t *testing.T) {
	t.Setenv("TG_BOT_TOKEN", "123:TOKEN")
	n, err := buildNotifier(context.Background(), config.NotifierConfig{
		Name:        "tg",
		Type:        "telegram",
		ChatID:      "456",
		BotTokenRef: "env:TG_BOT_TOKEN",
	}, nil)
	if err != nil {
		t.Fatalf("build telegram notifier: %v", err)
	}
	if n.Name() != "telegram" {
		t.Fatalf("expected telegram notifier, got %q", n.Name())
	}
}

func TestBuildNotifierWebhook(t *testing.T) {
	t.Setenv("HOOK_TOKEN", "secret-token")
	n, err := buildNotifier(context.Background(), config.NotifierConfig{
		Name:     "slack",
		Type:     "webhook",
		URL:      "https://example.com/hook",
		TokenRef: "env:HOOK_TOKEN",
	}, nil)
	if err != nil {
		t.Fatalf("build webhook notifier: %v", err)
	}
	if n.Name() != "webhook" {
		t.Fatalf("expected webhook notifier, got %q", n.Name())
	}
}

func TestBuildNotifierEmail(t *testing.T) {
	n, err := buildNotifier(context.Background(), config.NotifierConfig{
		Name: "mail",
		Type: "email",
		Host: "smtp.example.com",
		Port: 587,
		From: "a@b.com",
		To:   []string{"c@d.com"},
	}, nil)
	if err != nil {
		t.Fatalf("build email notifier: %v", err)
	}
	if n.Name() != "email" {
		t.Fatalf("expected email notifier, got %q", n.Name())
	}
}

func TestBuildNotifierUnknownType(t *testing.T) {
	_, err := buildNotifier(context.Background(), config.NotifierConfig{
		Name: "unknown",
		Type: "pigeon",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unsupported notifier type")
	}
}
