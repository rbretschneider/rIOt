package notify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureSlog swaps slog.Default for a buffer-backed logger. Tests using it
// must not run in parallel.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// failingClient returns an http.Client whose transport always fails. The real
// client wraps that failure in a *url.Error carrying the net/http stripPassword
// URL form (user:***@host/...) inside a %q-quoted string, exactly what the
// backends see in production. The error text is never hand-built.
func failingClient() *http.Client {
	return &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp 192.0.2.1:443: connect: connection refused")
	})}
}

type scrubRow struct {
	name        string
	channel     models.NotificationChannel
	mustNotLeak []string
}

// scrubRows is the AC-034/AC-035 C2 table (ADD section 8).
func scrubRows() []scrubRow {
	return []scrubRow{
		{
			name:        "telegram",
			channel:     models.NotificationChannel{ID: 1, Name: "tg", Type: "telegram", Enabled: true, Config: cfg{"bot_token": "999:TGTOKEN", "chat_id": "42"}},
			mustNotLeak: []string{"TGTOKEN"},
		},
		{
			name:        "discord",
			channel:     models.NotificationChannel{ID: 1, Name: "dc", Type: "discord", Enabled: true, Config: cfg{"webhook_url": "https://u1:DPASS@discord.example/api/webhooks/1/DTOK"}},
			mustNotLeak: []string{"DTOK", "DPASS"},
		},
		{
			name:        "slack",
			channel:     models.NotificationChannel{ID: 1, Name: "sl", Type: "slack", Enabled: true, Config: cfg{"webhook_url": "https://hooks.slack.example/services/STOK"}},
			mustNotLeak: []string{"STOK"},
		},
		{
			name:        "ntfy",
			channel:     models.NotificationChannel{ID: 1, Name: "nt", Type: "ntfy", Enabled: true, Config: cfg{"server_url": "https://alice:NPASS@ntfy.home", "topic": "NTOPIC", "token": "NBEARER"}},
			mustNotLeak: []string{"NPASS", "NTOPIC", "alice"},
		},
		{
			name:        "gotify",
			channel:     models.NotificationChannel{ID: 1, Name: "gt", Type: "gotify", Enabled: true, Config: cfg{"server_url": "https://bob:GPASS@gotify.home", "app_token": "GTOK"}},
			mustNotLeak: []string{"GPASS", "GTOK", "bob"},
		},
		{
			name:        "webhook",
			channel:     models.NotificationChannel{ID: 1, Name: "wh", Type: "webhook", Enabled: true, Config: cfg{"url": "https://riot:WPASS@n8n.home/webhook/WTOK", "headers": map[string]interface{}{"Authorization": "Bearer WHDR"}}},
			mustNotLeak: []string{"WPASS", "WTOK"},
		},
	}
}

func newScrubDispatcher(row scrubRow) (*Dispatcher, *testutil.MockNotifyRepo) {
	repo := testutil.NewMockNotifyRepo()
	repo.Channels = []models.NotificationChannel{row.channel}
	d := NewDispatcher(repo)
	d.SetHTTPClient(failingClient())
	return d, repo
}

var scrubAlert = models.Alert{Event: &models.Event{ID: 1, Message: "m"}, DeviceID: "dev-1", Hostname: "h"}

func assertScrubbed(t *testing.T, where, text string, row scrubRow) {
	t.Helper()
	for _, leak := range row.mustNotLeak {
		assert.NotContains(t, text, leak, "%s [%s]: %q must not appear", where, row.name, leak)
	}
}

// AC-035 (C2): real http.Client failures through Dispatch, SendToChannel and
// TestChannel never leak a secret into the returned error, ErrorMsg or slog.
func TestHARDEN002_AC035_C2_RealClientErrorsScrubbed(t *testing.T) {
	for _, row := range scrubRows() {
		row := row

		t.Run(row.name+"/precondition: the unscrubbed error really contains a secret", func(t *testing.T) {
			// If the real client stopped embedding the URL, the assertions
			// below would pass vacuously. Prove the leak exists without scrubbing.
			d, _ := newScrubDispatcher(row)
			backend := d.backends[row.channel.Type](row.channel)
			err := backend.Send(context.Background(), scrubAlert)
			require.Error(t, err)
			leaked := false
			for _, s := range row.mustNotLeak {
				if bytes.Contains([]byte(err.Error()), []byte(s)) {
					leaked = true
				}
			}
			require.True(t, leaked, "precondition: raw error %q must contain at least one of %v", err.Error(), row.mustNotLeak)
		})

		t.Run(row.name+"/Dispatch", func(t *testing.T) {
			logs := captureSlog(t)
			d, repo := newScrubDispatcher(row)

			d.Dispatch(context.Background(), scrubAlert)

			require.Len(t, repo.Logs, 1, "one notification_log row expected")
			assert.Equal(t, "failed", repo.Logs[0].Status)
			assertScrubbed(t, "Dispatch ErrorMsg", repo.Logs[0].ErrorMsg, row)
			assert.Contains(t, repo.Logs[0].ErrorMsg, RedactedSecret, "Dispatch ErrorMsg must show the sentinel in place")
			assert.Contains(t, repo.Logs[0].ErrorMsg, "connection refused", "the diagnostic part must survive")
			assertScrubbed(t, "Dispatch slog", logs.String(), row)
			assert.Contains(t, logs.String(), "dispatch: send failed", "the failure must still be logged")
		})

		t.Run(row.name+"/SendToChannel", func(t *testing.T) {
			logs := captureSlog(t)
			d, repo := newScrubDispatcher(row)

			err := d.SendToChannel(context.Background(), row.channel.ID, scrubAlert)

			require.Error(t, err)
			assertScrubbed(t, "SendToChannel return", err.Error(), row)
			assert.Contains(t, err.Error(), RedactedSecret)
			require.Len(t, repo.Logs, 1)
			assertScrubbed(t, "SendToChannel ErrorMsg", repo.Logs[0].ErrorMsg, row)
			assertScrubbed(t, "SendToChannel slog", logs.String(), row)
		})

		t.Run(row.name+"/TestChannel", func(t *testing.T) {
			d, _ := newScrubDispatcher(row)

			err := d.TestChannel(context.Background(), row.channel)

			require.Error(t, err)
			assertScrubbed(t, "TestChannel return", err.Error(), row)
			assert.Contains(t, err.Error(), RedactedSecret, "TestChannel error must show the sentinel in place")
		})
	}
}
