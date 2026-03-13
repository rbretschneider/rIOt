package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SMTP sends alert notifications via email.
type SMTP struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       []string // recipient addresses
	starttls bool
}

// NewSMTP creates an SMTP channel from a NotificationChannel config.
// Config keys: host, port, username, password, from, to (comma-separated), starttls.
func NewSMTP(ch models.NotificationChannel) *SMTP {
	s := &SMTP{
		host:     "localhost",
		port:     587,
		starttls: true,
	}
	if v, ok := ch.Config["host"].(string); ok && v != "" {
		s.host = v
	}
	if v, ok := ch.Config["port"].(float64); ok {
		s.port = int(v)
	}
	if v, ok := ch.Config["username"].(string); ok {
		s.username = v
	}
	if v, ok := ch.Config["password"].(string); ok {
		s.password = v
	}
	if v, ok := ch.Config["from"].(string); ok {
		s.from = v
	}
	if v, ok := ch.Config["to"].(string); ok && v != "" {
		for _, addr := range strings.Split(v, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				s.to = append(s.to, addr)
			}
		}
	}
	if v, ok := ch.Config["starttls"].(bool); ok {
		s.starttls = v
	}
	return s
}

func (s *SMTP) Type() string { return "smtp" }

func (s *SMTP) Send(_ context.Context, alert models.Alert) error {
	if s.from == "" {
		return fmt.Errorf("smtp: from address not configured")
	}
	if len(s.to) == 0 {
		return fmt.Errorf("smtp: no recipients configured")
	}

	subject := "rIOt Alert"
	if alert.Rule != nil {
		subject = alert.Rule.Name
	}

	body := ""
	if alert.Event != nil {
		body = alert.Event.Message
	}
	if alert.Hostname != "" {
		body = fmt.Sprintf("[%s] %s", alert.Hostname, body)
	}

	severity := "info"
	if alert.Event != nil && alert.Event.Severity != "" {
		severity = string(alert.Event.Severity)
	}

	msg := buildEmail(s.from, s.to, subject, body, severity)

	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))

	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}

	if s.starttls {
		return s.sendWithStartTLS(addr, auth, msg)
	}
	return smtp.SendMail(addr, auth, s.from, s.to, msg)
}

func (s *SMTP) sendWithStartTLS(addr string, auth smtp.Auth, msg []byte) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: dial: %w", err)
	}

	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: new client: %w", err)
	}
	defer c.Close()

	if err := c.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
		return fmt.Errorf("smtp: starttls: %w", err)
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}
	if err := c.Mail(s.from); err != nil {
		return fmt.Errorf("smtp: mail from: %w", err)
	}
	for _, rcpt := range s.to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp: rcpt to %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: close data: %w", err)
	}
	return c.Quit()
}

func buildEmail(from string, to []string, subject, body, severity string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: [rIOt] " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("X-Priority: ")
	switch severity {
	case "critical":
		b.WriteString("1")
	case "warning":
		b.WriteString("2")
	default:
		b.WriteString("3")
	}
	b.WriteString("\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
	return []byte(b.String())
}
