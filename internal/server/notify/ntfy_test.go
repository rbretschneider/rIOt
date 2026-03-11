package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNtfy_Defaults(t *testing.T) {
	ch := models.NotificationChannel{
		Config: map[string]interface{}{
			"topic": "alerts",
		},
	}
	n := NewNtfy(ch)
	assert.Equal(t, "https://ntfy.sh", n.serverURL)
	assert.Equal(t, "alerts", n.topic)
	assert.Equal(t, "default", n.priority)
	assert.Empty(t, n.token)
	assert.Equal(t, map[string]string{
		"info": "default", "warning": "high", "critical": "high",
	}, n.priorityMap)
}

func TestNewNtfy_CustomConfig(t *testing.T) {
	ch := models.NotificationChannel{
		Config: map[string]interface{}{
			"server_url": "https://my-ntfy.example.com/",
			"topic":      "my-topic",
			"token":      "tk_abc123",
			"priority":   "high",
		},
	}
	n := NewNtfy(ch)
	assert.Equal(t, "https://my-ntfy.example.com", n.serverURL, "trailing slash trimmed")
	assert.Equal(t, "my-topic", n.topic)
	assert.Equal(t, "tk_abc123", n.token)
	assert.Equal(t, "high", n.priority)
}

func TestNtfy_Send_Success(t *testing.T) {
	var gotTitle, gotPriority, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := &Ntfy{
		serverURL:   srv.URL,
		topic:       "test",
		token:       "mytoken",
		priority:    "default",
		priorityMap: map[string]string{"info": "default", "warning": "high", "critical": "high"},
	}
	err := n.Send(context.Background(), models.Alert{
		Rule:  &models.AlertRule{Name: "High Memory"},
		Event: &models.Event{Message: "RAM at 95%", Severity: models.SeverityCrit},
	})
	require.NoError(t, err)
	assert.Equal(t, "High Memory", gotTitle)
	assert.Equal(t, "high", gotPriority, "crit severity maps to high by default")
	assert.Equal(t, "Bearer mytoken", gotAuth)
}

func TestNtfy_Send_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := &Ntfy{serverURL: srv.URL, topic: "test", priority: "default"}
	err := n.Send(context.Background(), models.Alert{
		Event: &models.Event{Message: "test"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestNtfy_Send_NoTopic(t *testing.T) {
	n := &Ntfy{serverURL: "https://ntfy.sh", priority: "default"}
	err := n.Send(context.Background(), models.Alert{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "topic not configured")
}

func TestNtfy_MapPriority(t *testing.T) {
	defaultMap := map[string]string{"info": "default", "warning": "high", "critical": "high"}

	tests := []struct {
		name        string
		priority    string
		priorityMap map[string]string
		severity    models.EventSeverity
		want        string
	}{
		{"override priority", "max", defaultMap, models.SeverityCrit, "max"},
		{"crit default", "default", defaultMap, models.SeverityCrit, "high"},
		{"warning default", "default", defaultMap, models.SeverityWarning, "high"},
		{"info default", "default", defaultMap, models.SeverityInfo, "default"},
		{"nil event", "default", defaultMap, "", "default"},
		{"custom map crit→urgent", "default", map[string]string{"info": "low", "warning": "default", "critical": "urgent"}, models.SeverityCrit, "urgent"},
		{"custom map info→min", "default", map[string]string{"info": "min", "warning": "high", "critical": "high"}, models.SeverityInfo, "min"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Ntfy{priority: tt.priority, priorityMap: tt.priorityMap}
			alert := models.Alert{}
			if tt.severity != "" {
				alert.Event = &models.Event{Severity: tt.severity}
			}
			assert.Equal(t, tt.want, n.mapPriority(alert))
		})
	}
}

func TestNewNtfy_CustomPriorityMap(t *testing.T) {
	ch := models.NotificationChannel{
		Config: map[string]interface{}{
			"topic": "alerts",
			"priority_map": map[string]interface{}{
				"info":     "min",
				"critical": "urgent",
			},
		},
	}
	n := NewNtfy(ch)
	assert.Equal(t, "min", n.priorityMap["info"], "overridden from config")
	assert.Equal(t, "high", n.priorityMap["warning"], "kept default")
	assert.Equal(t, "urgent", n.priorityMap["critical"], "overridden from config")
}
