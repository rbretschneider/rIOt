package notify

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"net/url"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Pushover sends notifications via the Pushover API.
type Pushover struct {
	apiToken string
	userKey  string
	client   *http.Client
}

// NewPushover creates a Pushover channel from a NotificationChannel config.
// Config keys: api_token, user_key.
func NewPushover(ch models.NotificationChannel) *Pushover {
	p := &Pushover{}
	if v, ok := ch.Config["api_token"].(string); ok {
		p.apiToken = v
	}
	if v, ok := ch.Config["user_key"].(string); ok {
		p.userKey = v
	}
	return p
}

func (p *Pushover) Type() string { return "pushover" }

func (p *Pushover) Send(ctx context.Context, alert models.Alert) error {
	if p.apiToken == "" {
		return fmt.Errorf("pushover: api_token not configured")
	}
	if p.userKey == "" {
		return fmt.Errorf("pushover: user_key not configured")
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

	priority := "0" // normal
	if alert.Event != nil {
		switch alert.Event.Severity {
		case models.SeverityCrit:
			priority = "1" // high
		case models.SeverityWarning:
			priority = "0" // normal
		case models.SeverityInfo:
			priority = "-1" // low
		}
	}

	form := url.Values{
		"token":    {p.apiToken},
		"user":     {p.userKey},
		"title":    {title},
		"message":  {message},
		"priority": {priority},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.pushover.net/1/messages.json",
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("pushover: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := p.client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pushover: send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pushover: unexpected status %d", resp.StatusCode)
	}
	return nil
}
