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

// Discord sends notifications via a Discord webhook URL.
type Discord struct {
	webhookURL string
	client     *http.Client
}

// NewDiscord creates a Discord channel from a NotificationChannel config.
// Config keys: webhook_url.
func NewDiscord(ch models.NotificationChannel) *Discord {
	d := &Discord{}
	if v, ok := ch.Config["webhook_url"].(string); ok {
		d.webhookURL = v
	}
	return d
}

func (d *Discord) Type() string { return "discord" }

func (d *Discord) Send(ctx context.Context, alert models.Alert) error {
	if d.webhookURL == "" {
		return fmt.Errorf("discord: webhook_url not configured")
	}

	title := "rIOt Alert"
	if alert.Rule != nil {
		title = alert.Rule.Name
	}

	description := ""
	if alert.Event != nil {
		description = alert.Event.Message
	}
	if alert.Hostname != "" {
		description = fmt.Sprintf("**Host:** `%s`\n%s", alert.Hostname, description)
	}

	color := 0x3B82F6 // blue
	if alert.Event != nil {
		switch alert.Event.Severity {
		case models.SeverityCrit:
			color = 0xEF4444 // red
		case models.SeverityWarning:
			color = 0xF59E0B // amber
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       title,
				"description": description,
				"color":       color,
			},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("discord: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := d.client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("discord: API error %d: %s", resp.StatusCode, string(body))
}
