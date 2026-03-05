package models

import "time"

// AlertRule defines a configurable threshold-based alert.
type AlertRule struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	Metric          string    `json:"metric"`           // mem_percent, disk_percent, updates, container_died, container_oom, device_offline
	Operator        string    `json:"operator"`          // >, <, >=, <=, ==, !=
	Threshold       float64   `json:"threshold"`
	Severity        string    `json:"severity"`
	DeviceFilter    string    `json:"device_filter"`     // empty=all, comma-separated device IDs or tags
	CooldownSeconds int       `json:"cooldown_seconds"`
	Notify          bool      `json:"notify"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// NotificationChannel represents a configured notification destination.
type NotificationChannel struct {
	ID        int64                  `json:"id"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`    // ntfy, webhook
	Enabled   bool                   `json:"enabled"`
	Config    map[string]interface{} `json:"config"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// NotificationLog records a sent (or failed) notification.
type NotificationLog struct {
	ID          int64     `json:"id"`
	ChannelID   *int64    `json:"channel_id"`
	EventID     *int64    `json:"event_id"`
	AlertRuleID *int64    `json:"alert_rule_id"`
	Status      string    `json:"status"`    // sent, failed
	ErrorMsg    string    `json:"error_msg"`
	CreatedAt   time.Time `json:"created_at"`
}

// Alert is the payload passed to notification channels.
type Alert struct {
	Rule     *AlertRule `json:"rule"`
	Event    *Event     `json:"event"`
	DeviceID string     `json:"device_id"`
	Hostname string     `json:"hostname"`
	Value    float64    `json:"value,omitempty"`
}
