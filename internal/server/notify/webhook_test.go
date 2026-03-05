package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebhook_Config(t *testing.T) {
	ch := models.NotificationChannel{
		Config: map[string]interface{}{
			"url": "https://hooks.example.com/notify",
			"headers": map[string]interface{}{
				"X-Custom": "value",
			},
		},
	}
	w := NewWebhook(ch)
	assert.Equal(t, "https://hooks.example.com/notify", w.url)
	assert.Equal(t, "value", w.headers["X-Custom"])
}

func TestNewWebhook_EmptyConfig(t *testing.T) {
	ch := models.NotificationChannel{Config: map[string]interface{}{}}
	w := NewWebhook(ch)
	assert.Empty(t, w.url)
	assert.Empty(t, w.headers)
}

func TestWebhook_Send_Success(t *testing.T) {
	var gotBody []byte
	var gotContentType, gotCustomHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotCustomHeader = r.Header.Get("X-Hook")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := &Webhook{url: srv.URL, headers: map[string]string{"X-Hook": "test"}}
	alert := models.Alert{
		Event:    &models.Event{Message: "disk full"},
		DeviceID: "dev-1",
	}
	err := wh.Send(context.Background(), alert)
	require.NoError(t, err)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "test", gotCustomHeader)

	var decoded models.Alert
	require.NoError(t, json.Unmarshal(gotBody, &decoded))
	assert.Equal(t, "dev-1", decoded.DeviceID)
}

func TestWebhook_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	wh := &Webhook{url: srv.URL, headers: make(map[string]string)}
	err := wh.Send(context.Background(), models.Alert{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 502")
}

func TestWebhook_Send_NoURL(t *testing.T) {
	wh := &Webhook{headers: make(map[string]string)}
	err := wh.Send(context.Background(), models.Alert{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "url not configured")
}

func TestWebhook_Type(t *testing.T) {
	wh := &Webhook{}
	assert.Equal(t, "webhook", wh.Type())
}
