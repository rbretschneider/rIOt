package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Webhook sends alerts as JSON POST to a configured URL.
type Webhook struct {
	url     string
	headers map[string]string
}

// NewWebhook creates a Webhook channel from a NotificationChannel config.
// Config keys: url, headers (optional map[string]string).
func NewWebhook(ch models.NotificationChannel) *Webhook {
	w := &Webhook{
		headers: make(map[string]string),
	}
	if v, ok := ch.Config["url"].(string); ok {
		w.url = v
	}
	if v, ok := ch.Config["headers"].(map[string]interface{}); ok {
		for k, val := range v {
			if s, ok := val.(string); ok {
				w.headers[k] = s
			}
		}
	}
	return w
}

func (w *Webhook) Type() string { return "webhook" }

func (w *Webhook) Send(ctx context.Context, alert models.Alert) error {
	if w.url == "" {
		return fmt.Errorf("webhook: url not configured")
	}

	payload, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("webhook: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("webhook: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d", resp.StatusCode)
	}
	return nil
}
