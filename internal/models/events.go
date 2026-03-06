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
	EventContainerStart  EventType = "container_started"
	EventContainerStop   EventType = "container_stopped"
	EventContainerDied   EventType = "container_died"
	EventContainerOOM    EventType = "container_oom_killed"
	EventServiceStopped  EventType = "service_stopped"
	EventServiceFailed   EventType = "service_failed"
	EventProcessMissing  EventType = "process_missing"
	EventNICDown         EventType = "nic_down"
)

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

// FleetSummary contains aggregated fleet statistics.
type FleetSummary struct {
	TotalDevices  int `json:"total_devices"`
	OnlineCount   int `json:"online_count"`
	OfflineCount  int `json:"offline_count"`
	WarningCount  int `json:"warning_count"`
	RecentEvents  int `json:"recent_events"`
}
