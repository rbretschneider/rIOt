package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Gotify sends notifications to a self-hosted Gotify server.
type Gotify struct {
	serverURL string
	appToken  string
	client    *http.Client
}

// NewGotify creates a Gotify channel from a NotificationChannel config.
// Config keys: server_url, app_token.
func NewGotify(ch models.NotificationChannel) *Gotify {
	g := &Gotify{}
	if v, ok := ch.Config["server_url"].(string); ok {
		g.serverURL = strings.TrimRight(v, "/")
	}
	if v, ok := ch.Config["app_token"].(string); ok {
		g.appToken = v
	}
	return g
}

func (g *Gotify) Type() string { return "gotify" }

func (g *Gotify) Send(ctx context.Context, alert models.Alert) error {
	if g.serverURL == "" {
		return fmt.Errorf("gotify: server_url not configured")
	}
	if g.appToken == "" {
		return fmt.Errorf("gotify: app_token not configured")
	}

	title := "rIOt Alert"
	if alert.Rule != nil {
		title = alert.Rule.Name
	}

	message := ""
	if alert.Event != nil {
		message = alert.Event.Message
	}
	if alert.Hostname != "" {
		message = fmt.Sprintf("[%s] %s", alert.Hostname, message)
	}

	priority := 4 // normal
	if alert.Event != nil {
		switch alert.Event.Severity {
		case models.SeverityCrit:
			priority = 8
		case models.SeverityWarning:
			priority = 5
		case models.SeverityInfo:
			priority = 2
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"title":    title,
		"message":  message,
		"priority": priority,
	})

	url := fmt.Sprintf("%s/message?token=%s", g.serverURL, g.appToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("gotify: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := g.client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gotify: send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gotify: unexpected status %d", resp.StatusCode)
	}
	return nil
}
