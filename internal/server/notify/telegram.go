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

// Telegram sends notifications via the Telegram Bot API.
type Telegram struct {
	botToken string
	chatID   string
	client   *http.Client
}

// NewTelegram creates a Telegram channel from a NotificationChannel config.
// Config keys: bot_token, chat_id.
func NewTelegram(ch models.NotificationChannel) *Telegram {
	t := &Telegram{}
	if v, ok := ch.Config["bot_token"].(string); ok {
		t.botToken = v
	}
	if v, ok := ch.Config["chat_id"].(string); ok {
		t.chatID = v
	}
	return t
}

func (t *Telegram) Type() string { return "telegram" }

func (t *Telegram) Send(ctx context.Context, alert models.Alert) error {
	if t.botToken == "" {
		return fmt.Errorf("telegram: bot_token not configured")
	}
	if t.chatID == "" {
		return fmt.Errorf("telegram: chat_id not configured")
	}

	title := "rIOt Alert"
	if alert.Rule != nil {
		title = alert.Rule.Name
	}

	message := ""
	if alert.Event != nil {
		message = alert.Event.Message
	}

	// Format with severity emoji
	emoji := severityEmoji(alert)
	text := fmt.Sprintf("%s *%s*", emoji, escapeMarkdown(title))
	if alert.Hostname != "" {
		text += fmt.Sprintf("\nHost: `%s`", alert.Hostname)
	}
	if message != "" {
		text += fmt.Sprintf("\n%s", escapeMarkdown(message))
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)

	payload, _ := json.Marshal(map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := t.client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram: API error %d: %s", resp.StatusCode, string(body))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func severityEmoji(alert models.Alert) string {
	if alert.Event == nil {
		return "\u2139\ufe0f" // info
	}
	switch alert.Event.Severity {
	case models.SeverityCrit:
		return "\U0001f534" // red circle
	case models.SeverityWarning:
		return "\U0001f7e1" // yellow circle
	default:
		return "\U0001f535" // blue circle
	}
}

func escapeMarkdown(s string) string {
	for _, c := range []string{"_", "*", "`", "["} {
		s = replaceAll(s, c, "\\"+c)
	}
	return s
}

func replaceAll(s, old, new string) string {
	result := ""
	for i := 0; i < len(s); i++ {
		if i < len(s)-len(old)+1 && s[i:i+len(old)] == old {
			result += new
			i += len(old) - 1
		} else {
			result += string(s[i])
		}
	}
	return result
}
