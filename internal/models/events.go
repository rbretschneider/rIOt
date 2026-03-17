package models

import "time"

type EventType string
type EventSeverity string

const (
	EventDeviceOnline    EventType = "device_online"
	EventDeviceOffline   EventType = "device_offline"
	EventDiskHigh        EventType = "disk_high"
	EventMemHigh         EventType = "mem_high"
	EventUpdateAvail     EventType = "update_available"
	EventContainerStart         EventType = "container_started"
	EventContainerStop          EventType = "container_stopped"
	EventContainerDied          EventType = "container_died"
	EventContainerOOM           EventType = "container_oom_killed"
	EventContainerCreated       EventType = "container_created"
	EventContainerDestroyed     EventType = "container_destroyed"
	EventContainerPaused        EventType = "container_paused"
	EventContainerUnpaused      EventType = "container_unpaused"
	EventContainerUpdateStarted EventType = "container_update_started"
	EventContainerUpdateDone    EventType = "container_update_completed"
	EventContainerUpdateFailed  EventType = "container_update_failed"
	EventServiceStopped  EventType = "service_stopped"
	EventServiceFailed   EventType = "service_failed"
	EventProcessMissing  EventType = "process_missing"
	EventNICDown         EventType = "nic_down"
	EventCommandSent          EventType = "command_sent"
	EventCommandCompleted     EventType = "command_completed"
	EventAgentUpdateAvail     EventType = "agent_update_available"
	EventAgentUpdateStarted   EventType = "agent_update_started"
	EventAgentUpdateCompleted EventType = "agent_update_completed"
	EventAgentUpdateFailed    EventType = "agent_update_failed"
	EventLogErrors            EventType = "log_errors"
	EventContainerHighCPU      EventType = "container_high_cpu"
	EventContainerHighMem      EventType = "container_high_mem"
	EventContainerCPUOverLimit EventType = "container_cpu_over_limit"
	EventUPSOnBattery         EventType = "ups_on_battery"
	EventUPSLowBattery        EventType = "ups_low_battery"
	EventUPSRestored          EventType = "ups_restored"
	EventCertExpiring         EventType = "cert_expiring"
	EventCertExpired          EventType = "cert_expired"
	EventUSBDisconnected      EventType = "usb_disconnected"
	EventDiskSmartFailing     EventType = "disk_smart_failing"
	EventDiskSmartTemp        EventType = "disk_smart_temp"
)

// AgentEvent is the payload agents push for self-reported events (e.g. auto-updates).
type AgentEvent struct {
	Type     EventType     `json:"type"`
	Severity EventSeverity `json:"severity"`
	Message  string        `json:"message"`
}

// DockerEvent is the payload agents push for Docker container state changes.
type DockerEvent struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Action        string `json:"action"` // start, stop, die, create, destroy, oom
	Image         string `json:"image,omitempty"`
}

const (
	SeverityInfo    EventSeverity = "info"
	SeverityWarning EventSeverity = "warning"
	SeverityCrit    EventSeverity = "critical"
)

type Event struct {
	ID             int64         `json:"id"`
	DeviceID       string        `json:"device_id"`
	Type           EventType     `json:"type"`
	Severity       EventSeverity `json:"severity"`
	Message        string        `json:"message"`
	CreatedAt      time.Time     `json:"created_at"`
	AcknowledgedAt *time.Time    `json:"acknowledged_at,omitempty"`
}

// ServerLog is a server log entry stored in the database.
type ServerLog struct {
	ID        int64             `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Attrs     map[string]any    `json:"attrs,omitempty"`
	Source    string             `json:"source,omitempty"`
}

// FleetSummary contains aggregated fleet statistics.
type FleetSummary struct {
	TotalDevices  int `json:"total_devices"`
	OnlineCount   int `json:"online_count"`
	OfflineCount  int `json:"offline_count"`
	WarningCount  int `json:"warning_count"`
	RecentEvents  int `json:"recent_events"`
}
