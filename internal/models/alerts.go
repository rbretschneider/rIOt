package models

import "time"

// AlertRule defines a configurable threshold-based alert.
type AlertRule struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	Metric          string    `json:"metric"`           // mem_percent, disk_percent, updates, container_died, container_oom, device_offline, service_state, nic_state, process_missing
	Operator        string    `json:"operator"`          // >, <, >=, <=, ==, !=
	Threshold       float64   `json:"threshold"`
	TargetName      string    `json:"target_name"`       // named target (service name, NIC name, process name)
	TargetState     string    `json:"target_state"`      // state to match ("stopped", "failed", "DOWN", "absent")
	Severity        string    `json:"severity"`
	IncludeDevices  string    `json:"include_devices"`   // empty=all, comma-separated hostnames
	ExcludeDevices  string    `json:"exclude_devices"`   // comma-separated hostnames to exclude
	CooldownSeconds int       `json:"cooldown_seconds"`
	Notify          bool      `json:"notify"`
	TemplateID      string    `json:"template_id"`       // reference to a predefined template
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// AlertTemplate is a predefined alert rule template for quick creation.
type AlertTemplate struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Category        string `json:"category"`         // service, network, process, system, container
	Metric          string `json:"metric"`
	Operator        string `json:"operator"`
	Threshold       float64 `json:"threshold"`
	TargetState     string `json:"target_state,omitempty"`
	Severity        string `json:"severity"`
	CooldownSeconds int    `json:"cooldown_seconds"`
	NeedsTargetName bool   `json:"needs_target_name"`
	Description     string `json:"description"`
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
