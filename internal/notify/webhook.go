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

type WebhookOptions struct {
	URL     string
	Headers map[string]string
	Secret  string // Optional token/secret for Authorization
	Client  *http.Client
}

type WebhookNotifier struct {
	url     string
	headers map[string]string
	secret  string
	client  *http.Client
}

func NewWebhook(opts WebhookOptions) (*WebhookNotifier, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("webhook: URL is required")
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	headers := make(map[string]string)
	for k, v := range opts.Headers {
		headers[k] = v
	}
	return &WebhookNotifier{
		url:     opts.URL,
		headers: headers,
		secret:  opts.Secret,
		client:  client,
	}, nil
}

func (w *WebhookNotifier) Name() string { return "webhook" }

func (w *WebhookNotifier) Send(ctx context.Context, ev Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(payload))
	if err != nil {
		return w.maskError(err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}
	if w.secret != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", "Bearer "+w.secret)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return w.maskError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return w.maskError(fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	return nil
}

func (w *WebhookNotifier) Test(ctx context.Context) error {
	return w.Send(ctx, Event{
		Type:    EventTest,
		Job:     "test",
		Message: "test webhook connectivity",
		Started: time.Now(),
	})
}

func (w *WebhookNotifier) maskError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if w.secret != "" {
		msg = strings.ReplaceAll(msg, w.secret, "***")
	}
	return fmt.Errorf("%s", msg)
}
