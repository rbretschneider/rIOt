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

// Slack sends notifications via a Slack incoming webhook URL.
type Slack struct {
	webhookURL string
	client     *http.Client
}

// NewSlack creates a Slack channel from a NotificationChannel config.
// Config keys: webhook_url.
func NewSlack(ch models.NotificationChannel) *Slack {
	s := &Slack{}
	if v, ok := ch.Config["webhook_url"].(string); ok {
		s.webhookURL = v
	}
	return s
}

func (s *Slack) Type() string { return "slack" }

func (s *Slack) Send(ctx context.Context, alert models.Alert) error {
	if s.webhookURL == "" {
		return fmt.Errorf("slack: webhook_url not configured")
	}

	title := "rIOt Alert"
	if alert.Rule != nil {
		title = alert.Rule.Name
	}

	message := ""
	if alert.Event != nil {
		message = alert.Event.Message
	}

	color := "#3B82F6" // blue
	emoji := ":large_blue_circle:"
	if alert.Event != nil {
		switch alert.Event.Severity {
		case models.SeverityCrit:
			color = "#EF4444"
			emoji = ":red_circle:"
		case models.SeverityWarning:
			color = "#F59E0B"
			emoji = ":warning:"
		}
	}

	text := fmt.Sprintf("%s *%s*", emoji, title)
	if alert.Hostname != "" {
		text += fmt.Sprintf("  |  `%s`", alert.Hostname)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":   color,
				"text":    text,
				"fields":  []map[string]string{{"title": "Details", "value": message, "short": "false"}},
			},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("slack: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := s.client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack: send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack: unexpected status %d", resp.StatusCode)
	}
	return nil
}
