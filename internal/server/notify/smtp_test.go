package notify

import (
	"context"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSMTP_Defaults(t *testing.T) {
	ch := models.NotificationChannel{
		Config: map[string]interface{}{
			"from": "riot@example.com",
			"to":   "admin@example.com",
		},
	}
	s := NewSMTP(ch)
	assert.Equal(t, "localhost", s.host)
	assert.Equal(t, 587, s.port)
	assert.Equal(t, "riot@example.com", s.from)
	assert.Equal(t, []string{"admin@example.com"}, s.to)
	assert.True(t, s.starttls)
	assert.Empty(t, s.username)
	assert.Empty(t, s.password)
}

func TestNewSMTP_CustomConfig(t *testing.T) {
	ch := models.NotificationChannel{
		Config: map[string]interface{}{
			"host":     "smtp.gmail.com",
			"port":     float64(465),
			"username": "user@gmail.com",
			"password": "apppassword",
			"from":     "riot@gmail.com",
			"to":       "admin@example.com, ops@example.com",
			"starttls": false,
		},
	}
	s := NewSMTP(ch)
	assert.Equal(t, "smtp.gmail.com", s.host)
	assert.Equal(t, 465, s.port)
	assert.Equal(t, "user@gmail.com", s.username)
	assert.Equal(t, "apppassword", s.password)
	assert.Equal(t, "riot@gmail.com", s.from)
	assert.Equal(t, []string{"admin@example.com", "ops@example.com"}, s.to)
	assert.False(t, s.starttls)
}

func TestSMTP_Send_NoFrom(t *testing.T) {
	s := &SMTP{to: []string{"admin@example.com"}}
	err := s.Send(context.Background(), models.Alert{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from address not configured")
}

func TestSMTP_Send_NoTo(t *testing.T) {
	s := &SMTP{from: "riot@example.com"}
	err := s.Send(context.Background(), models.Alert{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no recipients configured")
}

func TestSMTP_Type(t *testing.T) {
	s := &SMTP{}
	assert.Equal(t, "smtp", s.Type())
}

func TestBuildEmail(t *testing.T) {
	msg := buildEmail(
		"riot@example.com",
		[]string{"admin@example.com", "ops@example.com"},
		"High Memory",
		"[server1] RAM at 95%",
		"critical",
	)
	s := string(msg)
	assert.Contains(t, s, "From: riot@example.com")
	assert.Contains(t, s, "To: admin@example.com, ops@example.com")
	assert.Contains(t, s, "Subject: [rIOt] High Memory")
	assert.Contains(t, s, "X-Priority: 1")
	assert.Contains(t, s, "[server1] RAM at 95%")
}

func TestBuildEmail_WarningPriority(t *testing.T) {
	msg := buildEmail("a@b.com", []string{"c@d.com"}, "test", "body", "warning")
	assert.Contains(t, string(msg), "X-Priority: 2")
}

func TestBuildEmail_InfoPriority(t *testing.T) {
	msg := buildEmail("a@b.com", []string{"c@d.com"}, "test", "body", "info")
	assert.Contains(t, string(msg), "X-Priority: 3")
}

func TestNewSMTP_EmptyToIgnored(t *testing.T) {
	ch := models.NotificationChannel{
		Config: map[string]interface{}{
			"from": "riot@example.com",
			"to":   "admin@example.com, , ops@example.com",
		},
	}
	s := NewSMTP(ch)
	assert.Equal(t, []string{"admin@example.com", "ops@example.com"}, s.to)
}

func TestSMTP_Send_DialError(t *testing.T) {
	s := &SMTP{
		host:     "127.0.0.1",
		port:     1, // unlikely to have an SMTP server
		from:     "riot@example.com",
		to:       []string{"admin@example.com"},
		starttls: false,
	}
	err := s.Send(context.Background(), models.Alert{
		Event: &models.Event{Message: "test"},
	})
	assert.Error(t, err)
}
