package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Ntfy sends notifications via ntfy.sh or a self-hosted ntfy server.
type Ntfy struct {
	serverURL string
	topic     string
	token     string
	priority  string
}

// NewNtfy creates an Ntfy channel from a NotificationChannel config.
// Config keys: server_url, topic, token (optional), priority (optional).
func NewNtfy(ch models.NotificationChannel) *Ntfy {
	n := &Ntfy{
		serverURL: "https://ntfy.sh",
		priority:  "default",
	}
	if v, ok := ch.Config["server_url"].(string); ok && v != "" {
		n.serverURL = strings.TrimRight(v, "/")
	}
	if v, ok := ch.Config["topic"].(string); ok {
		n.topic = v
	}
	if v, ok := ch.Config["token"].(string); ok {
		n.token = v
	}
	if v, ok := ch.Config["priority"].(string); ok && v != "" {
		n.priority = v
	}
	return n
}

func (n *Ntfy) Type() string { return "ntfy" }

func (n *Ntfy) Send(ctx context.Context, alert models.Alert) error {
	if n.topic == "" {
		return fmt.Errorf("ntfy: topic not configured")
	}

	url := n.serverURL + "/" + n.topic

	title := "rIOt Alert"
	if alert.Rule != nil {
		title = alert.Rule.Name
	}

	body := ""
	if alert.Event != nil {
		body = alert.Event.Message
	}
	if alert.Hostname != "" {
		body = fmt.Sprintf("[%s] %s", alert.Hostname, body)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("ntfy: create request: %w", err)
	}

	req.Header.Set("Title", title)
	req.Header.Set("Priority", n.mapPriority(alert))
	if alert.Event != nil {
		req.Header.Set("Tags", string(alert.Event.Severity))
	}
	if n.token != "" {
		req.Header.Set("Authorization", "Bearer "+n.token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (n *Ntfy) mapPriority(alert models.Alert) string {
	if n.priority != "default" && n.priority != "" {
		return n.priority
	}
	if alert.Event == nil {
		return "default"
	}
	switch alert.Event.Severity {
	case models.SeverityCrit:
		return "urgent"
	case models.SeverityWarning:
		return "high"
	default:
		return "default"
	}
}
