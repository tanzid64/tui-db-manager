package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestTelegramSuccess(t *testing.T) {
	var receivedBody map[string]string
	var receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	n, err := NewTelegram(TelegramOptions{
		BotToken: "12345:ABC-DEF",
		ChatID:   "999888",
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("new telegram: %v", err)
	}

	ev := Event{
		Type:    EventBackupSucceeded,
		Job:     "prod-db",
		Size:    1048576,
		Started: time.Now().Add(-5 * time.Second),
		Elapsed: 5 * time.Second,
	}

	if err := n.Send(context.Background(), ev); err != nil {
		t.Fatalf("send: %v", err)
	}

	if receivedPath != "/bot12345:ABC-DEF/sendMessage" {
		t.Errorf("unexpected path: %q", receivedPath)
	}
	if receivedBody["chat_id"] != "999888" {
		t.Errorf("unexpected chat_id: %q", receivedBody["chat_id"])
	}
	if !strings.Contains(receivedBody["text"], "prod-db") {
		t.Errorf("text missing job name: %q", receivedBody["text"])
	}
	if !strings.Contains(receivedBody["text"], "1048576 bytes") {
		t.Errorf("text missing size: %q", receivedBody["text"])
	}
}

func TestTelegramFailureAndTokenMasking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"description": "chat not found"}`))
	}))
	defer srv.Close()

	secretToken := "secret-bot-token-xyz"
	n, err := NewTelegram(TelegramOptions{
		BotToken: secretToken,
		ChatID:   "123",
		BaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = n.Test(context.Background())
	if err == nil {
		t.Fatal("expected error on 400 status")
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("token leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("expected error description, got: %v", err)
	}
}

func TestTelegramValidation(t *testing.T) {
	_, err := NewTelegram(TelegramOptions{ChatID: "123"})
	if err == nil {
		t.Error("expected error for missing bot token")
	}
	_, err = NewTelegram(TelegramOptions{BotToken: "abc"})
	if err == nil {
		t.Error("expected error for missing chat ID")
	}
}

func TestWebhookSuccess(t *testing.T) {
	var receivedEvent Event
	var authHeader string
	var customHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		customHeader = r.Header.Get("X-Custom")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedEvent)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n, err := NewWebhook(WebhookOptions{
		URL:     srv.URL,
		Secret:  "wh-secret-key-123",
		Headers: map[string]string{"X-Custom": "custom-val"},
	})
	if err != nil {
		t.Fatalf("new webhook: %v", err)
	}

	ev := Event{
		Type:    EventBackupFailed,
		Job:     "orders-db",
		Err:     "disk full",
		Elapsed: 2 * time.Second,
	}

	if err := n.Send(context.Background(), ev); err != nil {
		t.Fatalf("send: %v", err)
	}

	if authHeader != "Bearer wh-secret-key-123" {
		t.Errorf("unexpected auth header: %q", authHeader)
	}
	if customHeader != "custom-val" {
		t.Errorf("unexpected custom header: %q", customHeader)
	}
	if receivedEvent.Type != EventBackupFailed || receivedEvent.Job != "orders-db" || receivedEvent.Err != "disk full" {
		t.Errorf("unexpected event received: %+v", receivedEvent)
	}
}

func TestWebhookErrorAndMasking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`failed on server with token wh-secret-key-123`))
	}))
	defer srv.Close()

	n, err := NewWebhook(WebhookOptions{
		URL:    srv.URL,
		Secret: "wh-secret-key-123",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = n.Test(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "wh-secret-key-123") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestEmailSuccessWithMockSender(t *testing.T) {
	var sentAddr string
	var sentFrom string
	var sentTo []string
	var sentMsg string

	sender := func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		sentAddr = addr
		sentFrom = from
		sentTo = to
		sentMsg = string(msg)
		return nil
	}

	n, err := NewEmail(EmailOptions{
		Host:     "smtp.example.com",
		Port:     587,
		From:     "backups@example.com",
		To:       []string{"admin@example.com"},
		Username: "user",
		Password: "password",
		Sender:   sender,
	})
	if err != nil {
		t.Fatalf("new email: %v", err)
	}

	ev := Event{
		Type:    EventBackupSucceeded,
		Job:     "main-db",
		Size:    2048,
		Started: time.Now().Add(-10 * time.Second),
		Elapsed: 10 * time.Second,
	}

	if err := n.Send(context.Background(), ev); err != nil {
		t.Fatalf("send: %v", err)
	}

	if sentAddr != "smtp.example.com:587" {
		t.Errorf("unexpected addr: %q", sentAddr)
	}
	if sentFrom != "backups@example.com" {
		t.Errorf("unexpected from: %q", sentFrom)
	}
	if len(sentTo) != 1 || sentTo[0] != "admin@example.com" {
		t.Errorf("unexpected to: %+v", sentTo)
	}
	if !strings.Contains(sentMsg, "Subject: [dbk] Backup Succeeded: main-db") {
		t.Errorf("unexpected subject in msg: %s", sentMsg)
	}
	if !strings.Contains(sentMsg, "Size: 2048 bytes") {
		t.Errorf("missing size in msg: %s", sentMsg)
	}
}

func TestEmailPasswordMasking(t *testing.T) {
	secretPass := "smtp-secret-pass-999"
	sender := func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		return errors.New("smtp auth error for password " + secretPass)
	}

	n, err := NewEmail(EmailOptions{
		Host:     "smtp.example.com",
		From:     "a@b.com",
		To:       []string{"c@d.com"},
		Password: secretPass,
		Sender:   sender,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = n.Send(context.Background(), Event{Type: EventTest, Job: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secretPass) {
		t.Fatalf("password leaked in error: %v", err)
	}
}

func TestEmailValidation(t *testing.T) {
	_, err := NewEmail(EmailOptions{From: "a@b.com", To: []string{"c@d.com"}})
	if err == nil {
		t.Error("expected error for missing host")
	}
	_, err = NewEmail(EmailOptions{Host: "smtp.example.com", To: []string{"c@d.com"}})
	if err == nil {
		t.Error("expected error for missing from")
	}
	_, err = NewEmail(EmailOptions{Host: "smtp.example.com", From: "a@b.com"})
	if err == nil {
		t.Error("expected error for missing to")
	}
}
