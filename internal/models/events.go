package models

import "time"

type EventType string
type EventSeverity string

const (
	EventDeviceOnline  EventType = "device_online"
	EventDeviceOffline EventType = "device_offline"
	EventDiskHigh      EventType = "disk_high"
	EventMemHigh       EventType = "mem_high"
	EventUpdateAvail   EventType = "update_available"
)

const (
	SeverityInfo    EventSeverity = "info"
	SeverityWarning EventSeverity = "warning"
	SeverityCrit    EventSeverity = "critical"
)

type Event struct {
	ID        int64         `json:"id"`
	DeviceID  string        `json:"device_id"`
	Type      EventType     `json:"type"`
	Severity  EventSeverity `json:"severity"`
	Message   string        `json:"message"`
	CreatedAt time.Time     `json:"created_at"`
}

// FleetSummary contains aggregated fleet statistics.
type FleetSummary struct {
	TotalDevices  int `json:"total_devices"`
	OnlineCount   int `json:"online_count"`
	OfflineCount  int `json:"offline_count"`
	WarningCount  int `json:"warning_count"`
	RecentEvents  int `json:"recent_events"`
}
