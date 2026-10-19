package notify

import (
	"context"
	"fmt"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatch_FanOut(t *testing.T) {
	notifyRepo := testutil.NewMockNotifyRepo()
	notifyRepo.Channels = []models.NotificationChannel{
		{ID: 1, Name: "ntfy-1", Type: "ntfy", Enabled: true, Config: map[string]interface{}{"topic": "test"}},
		{ID: 2, Name: "webhook-1", Type: "webhook", Enabled: true, Config: map[string]interface{}{"url": "http://example.com"}},
	}

	d := NewDispatcher(notifyRepo)

	// Override backends to capture sends
	var sent []string
	d.backends["ntfy"] = func(ch models.NotificationChannel) Channel {
		return &mockChannel{name: ch.Name, sentTo: &sent}
	}
	d.backends["webhook"] = func(ch models.NotificationChannel) Channel {
		return &mockChannel{name: ch.Name, sentTo: &sent}
	}

	d.Dispatch(context.Background(), models.Alert{
		Event:    &models.Event{ID: 1, Message: "test"},
		DeviceID: "dev-1",
	})

	assert.Equal(t, []string{"ntfy-1", "webhook-1"}, sent)
	assert.Len(t, notifyRepo.Logs, 2)
	assert.Equal(t, "sent", notifyRepo.Logs[0].Status)
}

func TestDispatch_FailedChannel(t *testing.T) {
	notifyRepo := testutil.NewMockNotifyRepo()
	notifyRepo.Channels = []models.NotificationChannel{
		{ID: 1, Name: "broken", Type: "ntfy", Enabled: true, Config: map[string]interface{}{}},
	}

	d := NewDispatcher(notifyRepo)
	d.backends["ntfy"] = func(ch models.NotificationChannel) Channel {
		return &mockChannel{err: fmt.Errorf("connection refused")}
	}

	d.Dispatch(context.Background(), models.Alert{
		Event:    &models.Event{ID: 1, Message: "test"},
		DeviceID: "dev-1",
	})

	require.Len(t, notifyRepo.Logs, 1)
	assert.Equal(t, "failed", notifyRepo.Logs[0].Status)
	assert.Contains(t, notifyRepo.Logs[0].ErrorMsg, "connection refused")
}

// [HARDEN-002][AC-035] A failing send whose error text contains the channel's
// webhook URL is scrubbed from ErrorMsg, the slog record and the
// SendToChannel return value.
func TestHARDEN002_AC035_DispatchFailureLogsScrubbed(t *testing.T) {
	const url = "https://discord.example/api/webhooks/1/TOK"
	newSetup := func(t *testing.T) (*Dispatcher, *testutil.MockNotifyRepo) {
		repo := testutil.NewMockNotifyRepo()
		repo.Channels = []models.NotificationChannel{
			{ID: 1, Name: "dc", Type: "discord", Enabled: true, Config: map[string]interface{}{"webhook_url": url}},
		}
		d := NewDispatcher(repo)
		d.backends["discord"] = func(ch models.NotificationChannel) Channel {
			return &mockChannel{err: fmt.Errorf("discord: send: Post %q: boom", url)}
		}
		return d, repo
	}

	t.Run("Dispatch", func(t *testing.T) {
		logs := captureSlog(t)
		d, repo := newSetup(t)
		d.Dispatch(context.Background(), models.Alert{Event: &models.Event{ID: 1, Message: "m"}, DeviceID: "d"})

		require.Len(t, repo.Logs, 1)
		assert.Equal(t, "failed", repo.Logs[0].Status)
		assert.NotContains(t, repo.Logs[0].ErrorMsg, url, "ErrorMsg must not contain the webhook URL")
		assert.NotContains(t, repo.Logs[0].ErrorMsg, "TOK", "ErrorMsg must not contain the token")
		assert.Contains(t, repo.Logs[0].ErrorMsg, RedactedSecret)
		assert.Contains(t, repo.Logs[0].ErrorMsg, "boom", "the diagnostic part must survive")
		assert.NotContains(t, logs.String(), "TOK", "no slog attribute may contain the token")
		assert.NotContains(t, logs.String(), "api/webhooks", "no slog attribute may contain the URL")
	})

	t.Run("SendToChannel", func(t *testing.T) {
		logs := captureSlog(t)
		d, repo := newSetup(t)
		err := d.SendToChannel(context.Background(), 1, models.Alert{Event: &models.Event{ID: 1, Message: "m"}, DeviceID: "d"})

		require.Error(t, err)
		assert.NotContains(t, err.Error(), "TOK", "the returned error is logged by the alert worker")
		assert.NotContains(t, err.Error(), url)
		require.Len(t, repo.Logs, 1)
		assert.NotContains(t, repo.Logs[0].ErrorMsg, "TOK")
		assert.NotContains(t, logs.String(), "TOK")
	})

	t.Run("a successful send is untouched", func(t *testing.T) {
		repo := testutil.NewMockNotifyRepo()
		repo.Channels = []models.NotificationChannel{{ID: 1, Name: "dc", Type: "discord", Enabled: true, Config: map[string]interface{}{"webhook_url": url}}}
		d := NewDispatcher(repo)
		d.backends["discord"] = func(ch models.NotificationChannel) Channel { return &mockChannel{} }
		assert.NoError(t, d.SendToChannel(context.Background(), 1, models.Alert{Event: &models.Event{ID: 1}}))
		d.Dispatch(context.Background(), models.Alert{Event: &models.Event{ID: 1}})
		for _, l := range repo.Logs {
			assert.Equal(t, "sent", l.Status)
			assert.Empty(t, l.ErrorMsg)
		}
	})
}

func TestDispatch_NoChannels(t *testing.T) {
	notifyRepo := testutil.NewMockNotifyRepo()
	d := NewDispatcher(notifyRepo)

	// Should not panic
	d.Dispatch(context.Background(), models.Alert{Event: &models.Event{Message: "test"}})
	assert.Empty(t, notifyRepo.Logs)
}

func TestTestChannel(t *testing.T) {
	notifyRepo := testutil.NewMockNotifyRepo()
	d := NewDispatcher(notifyRepo)

	var sentAlert models.Alert
	d.backends["ntfy"] = func(ch models.NotificationChannel) Channel {
		return &mockChannel{captureAlert: &sentAlert}
	}

	ch := models.NotificationChannel{Type: "ntfy", Config: map[string]interface{}{}}
	err := d.TestChannel(context.Background(), ch)
	require.NoError(t, err)
	assert.Equal(t, "test-device", sentAlert.Hostname)
}

func TestTestChannel_UnknownType(t *testing.T) {
	notifyRepo := testutil.NewMockNotifyRepo()
	d := NewDispatcher(notifyRepo)

	ch := models.NotificationChannel{Type: "carrier_pigeon"}
	err := d.TestChannel(context.Background(), ch)
	assert.Error(t, err)
	assert.IsType(t, &UnsupportedChannelError{}, err)
}

// mockChannel is a test helper for the Channel interface.
type mockChannel struct {
	name         string
	sentTo       *[]string
	err          error
	captureAlert *models.Alert
}

func (m *mockChannel) Type() string { return "mock" }

func (m *mockChannel) Send(ctx context.Context, alert models.Alert) error {
	if m.captureAlert != nil {
		*m.captureAlert = alert
	}
	if m.err != nil {
		return m.err
	}
	if m.sentTo != nil {
		*m.sentTo = append(*m.sentTo, m.name)
	}
	return nil
}
