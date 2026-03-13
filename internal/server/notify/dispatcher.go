package notify

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

// Channel is the interface for notification backends.
type Channel interface {
	Type() string
	Send(ctx context.Context, alert models.Alert) error
}

// Dispatcher fans out alerts to all enabled notification channels.
type Dispatcher struct {
	repo       db.NotifyRepository
	backends   map[string]func(models.NotificationChannel) Channel
	httpClient *http.Client // shared HTTP client for backends (optional)
}

// NewDispatcher creates a new notification dispatcher.
func NewDispatcher(repo db.NotifyRepository) *Dispatcher {
	d := &Dispatcher{
		repo:     repo,
		backends: make(map[string]func(models.NotificationChannel) Channel),
	}
	// Register built-in backends
	d.backends["ntfy"] = func(ch models.NotificationChannel) Channel {
		n := NewNtfy(ch)
		n.client = d.httpClient
		return n
	}
	d.backends["webhook"] = func(ch models.NotificationChannel) Channel {
		w := NewWebhook(ch)
		w.client = d.httpClient
		return w
	}
	d.backends["smtp"] = func(ch models.NotificationChannel) Channel {
		return NewSMTP(ch)
	}
	return d
}

// SetHTTPClient sets a shared HTTP client for all notification backends.
func (d *Dispatcher) SetHTTPClient(c *http.Client) {
	d.httpClient = c
}

// Dispatch sends an alert to all enabled notification channels and logs results.
func (d *Dispatcher) Dispatch(ctx context.Context, alert models.Alert) {
	channels, err := d.repo.ListEnabledChannels(ctx)
	if err != nil {
		slog.Error("dispatch: list channels", "error", err)
		return
	}
	if len(channels) == 0 {
		return
	}

	for _, ch := range channels {
		factory, ok := d.backends[ch.Type]
		if !ok {
			slog.Warn("dispatch: unknown channel type", "type", ch.Type)
			continue
		}
		backend := factory(ch)
		err := backend.Send(ctx, alert)

		// Log the notification attempt
		logEntry := &models.NotificationLog{
			ChannelID:   &ch.ID,
			AlertRuleID: nil,
			Status:      "sent",
		}
		if alert.Event != nil {
			logEntry.EventID = &alert.Event.ID
		}
		if alert.Rule != nil {
			logEntry.AlertRuleID = &alert.Rule.ID
		}
		if err != nil {
			logEntry.Status = "failed"
			logEntry.ErrorMsg = err.Error()
			slog.Error("dispatch: send failed", "channel", ch.Name, "type", ch.Type, "error", err)
		}
		if logErr := d.repo.LogNotification(ctx, logEntry); logErr != nil {
			slog.Error("dispatch: log notification", "error", logErr)
		}
	}
}

// TestChannel sends a test notification to a specific channel.
func (d *Dispatcher) TestChannel(ctx context.Context, ch models.NotificationChannel) error {
	factory, ok := d.backends[ch.Type]
	if !ok {
		return &UnsupportedChannelError{Type: ch.Type}
	}
	backend := factory(ch)
	return backend.Send(ctx, models.Alert{
		Event: &models.Event{
			Type:     "test",
			Severity: models.SeverityInfo,
			Message:  "Test notification from rIOt",
		},
		DeviceID: "test",
		Hostname: "test-device",
	})
}

// UnsupportedChannelError is returned for unknown channel types.
type UnsupportedChannelError struct {
	Type string
}

func (e *UnsupportedChannelError) Error() string {
	return "unsupported notification channel type: " + e.Type
}
