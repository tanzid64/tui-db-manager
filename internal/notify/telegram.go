package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type TelegramOptions struct {
	BotToken string
	ChatID   string
	BaseURL  string // Optional, defaults to "https://api.telegram.org"
	Client   *http.Client
}

type TelegramNotifier struct {
	botToken string
	chatID   string
	baseURL  string
	client   *http.Client
}

func NewTelegram(opts TelegramOptions) (*TelegramNotifier, error) {
	if opts.BotToken == "" {
		return nil, fmt.Errorf("telegram: bot token is required")
	}
	if opts.ChatID == "" {
		return nil, fmt.Errorf("telegram: chat ID is required")
	}
	base := opts.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &TelegramNotifier{
		botToken: opts.BotToken,
		chatID:   opts.ChatID,
		baseURL:  strings.TrimRight(base, "/"),
		client:   client,
	}, nil
}

func (t *TelegramNotifier) Name() string { return "telegram" }

func (t *TelegramNotifier) formatMessage(ev Event) string {
	switch ev.Type {
	case EventBackupSucceeded:
		return fmt.Sprintf("✅ <b>Backup Succeeded:</b> <code>%s</code>\nSize: %d bytes\nDuration: %s",
			ev.Job, ev.Size, ev.Elapsed.Round(time.Millisecond))
	case EventBackupFailed:
		return fmt.Sprintf("❌ <b>Backup FAILED:</b> <code>%s</code>\nError: <code>%s</code>\nDuration: %s",
			ev.Job, ev.Err, ev.Elapsed.Round(time.Millisecond))
	case EventRestoreDone:
		return fmt.Sprintf("🔄 <b>Restore Done:</b> <code>%s</code>\nDuration: %s",
			ev.Job, ev.Elapsed.Round(time.Millisecond))
	case EventTest:
		return "🔔 <b>[dbk] Test Notification:</b> connection OK"
	default:
		msg := ev.Message
		if msg == "" {
			msg = string(ev.Type)
		}
		return fmt.Sprintf("ℹ️ <b>[dbk] Notification (%s):</b> %s", ev.Job, msg)
	}
}

func (t *TelegramNotifier) Send(ctx context.Context, ev Event) error {
	text := t.formatMessage(ev)
	return t.sendMessage(ctx, text)
}

func (t *TelegramNotifier) Test(ctx context.Context) error {
	return t.Send(ctx, Event{Type: EventTest, Job: "test"})
}

func (t *TelegramNotifier) sendMessage(ctx context.Context, text string) error {
	reqBody, err := json.Marshal(map[string]string{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "HTML",
	})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", t.baseURL, t.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return t.maskError(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return t.maskError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return t.maskError(fmt.Errorf("telegram API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	return nil
}

func (t *TelegramNotifier) maskError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if t.botToken != "" {
		msg = strings.ReplaceAll(msg, t.botToken, "***")
	}
	return fmt.Errorf("%s", msg)
}
