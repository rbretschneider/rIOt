package models

import "time"

type EventType string
type EventSeverity string

const (
	EventDeviceOnline           EventType = "device_online"
	EventDeviceOffline          EventType = "device_offline"
	EventDiskHigh               EventType = "disk_high"
	EventMemHigh                EventType = "mem_high"
	EventUpdateAvail            EventType = "update_available"
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
	EventServiceStopped         EventType = "service_stopped"
	EventServiceFailed          EventType = "service_failed"
	EventProcessMissing         EventType = "process_missing"
	EventNICDown                EventType = "nic_down"
	EventCommandSent            EventType = "command_sent"
	EventCommandCompleted       EventType = "command_completed"
	EventAgentUpdateAvail       EventType = "agent_update_available"
	EventAgentUpdateStarted     EventType = "agent_update_started"
	EventAgentUpdateCompleted   EventType = "agent_update_completed"
	EventAgentUpdateFailed      EventType = "agent_update_failed"
	EventLogErrors              EventType = "log_errors"
	EventContainerHighCPU       EventType = "container_high_cpu"
	EventContainerHighMem       EventType = "container_high_mem"
	EventContainerCPUOverLimit  EventType = "container_cpu_over_limit"
	EventUPSOnBattery           EventType = "ups_on_battery"
	EventUPSLowBattery          EventType = "ups_low_battery"
	EventUPSRestored            EventType = "ups_restored"
	EventCertExpiring           EventType = "cert_expiring"
	EventCertExpired            EventType = "cert_expired"
	EventUSBDisconnected        EventType = "usb_disconnected"
	EventDiskSmartFailing       EventType = "disk_smart_failing"
	EventDiskSmartTemp          EventType = "disk_smart_temp"
	EventGPUTemp                EventType = "gpu_temp"
	EventGPUMetric              EventType = "gpu_metric"
	EventNginx5xxHigh           EventType = "nginx_5xx_high"
	EventNginx4xxHigh           EventType = "nginx_4xx_high"
	EventNginxRequestHigh       EventType = "nginx_request_high"
	EventAuthFailure            EventType = "auth_failure"
	EventDetectorInitialized    EventType = "detector_initialized"
	EventRebootRequired         EventType = "reboot_required"

	// EXPECT-001: a window whose end-of-life checks were skipped or delayed, and
	// the summary of escalation pushes held by the per-device ceiling.
	EventExpectBackstopSkipped   EventType = "expect_backstop_skipped"
	EventExpectNotificationsHeld EventType = "expect_notifications_held"

	// Extra-updater step start and end, one pair per updater per os_update
	// command (EXTRA-UPD-001 FR-045).
	EventExtraUpdaterStarted  EventType = "extra_updater_started"
	EventExtraUpdaterFinished EventType = "extra_updater_finished"

	// EXPECT-002: server-written outcome events of an agent self-update record.
	// None is routed through alert rules, none is ever dispatched to a channel
	// and none consumes the per-device push ceiling.
	EventAgentSelfUpdateFailed      EventType = "agent_self_update_failed"
	EventAgentSelfUpdateUnconfirmed EventType = "agent_self_update_unconfirmed"
	EventAgentSelfUpdatePaused      EventType = "agent_self_update_paused"
)

// Event.CommandAttr values written by EXPECT-001 (the agent and the server's
// attribution rule already use "agent" and "inferred").
const (
	// CommandAttrExpected marks an info event that records a suppressed
	// occurrence inside an expected-activity window.
	CommandAttrExpected = "expected"
	// CommandAttrEscalation marks the single alert raised when a window ends and
	// its target is still down.
	CommandAttrEscalation = "escalation"
	// CommandAttrInferred marks an event the server linked to a command or record
	// by its own rule (the Docker attribution and, since EXPECT-002, the
	// agent_update_* events).
	CommandAttrInferred = "inferred"
	// CommandAttrOutcome marks the single outcome event of an agent self-update
	// record (EXPECT-002 AD-012).
	CommandAttrOutcome = "outcome"
)

// AgentEvent is the payload agents push for self-reported events (e.g. auto-updates).
// CommandID, Updater and Outcome are set only on the two extra-updater event
// types; they are omitted otherwise so older servers see the same JSON.
type AgentEvent struct {
	Type      EventType     `json:"type"`
	Severity  EventSeverity `json:"severity"`
	Message   string        `json:"message"`
	CommandID string        `json:"command_id,omitempty"`
	Updater   string        `json:"updater,omitempty"`
	Outcome   string        `json:"outcome,omitempty"`
	// Reason is the skip reason of an extra_updater_finished event whose outcome
	// is "skipped". The server accepts it only from the closed skip-reason set
	// and composes the stored message itself (DIETPI-PATH-001 FR-014).
	Reason string `json:"reason,omitempty"`
}

// DockerEvent is the payload agents push for Docker container state changes.
type DockerEvent struct {
	CommandAttr   string `json:"-"`
	CommandID     string `json:"command_id,omitempty"`
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Action        string `json:"action"` // start, stop, die, create, destroy, oom
	Image         string `json:"image,omitempty"`
	// TargetKeys are the command target keys of this container (its name key and
	// its compose key), computed by the server and never read from the agent.
	TargetKeys []string `json:"-"`
}

const (
	SeverityInfo    EventSeverity = "info"
	SeverityWarning EventSeverity = "warning"
	SeverityCrit    EventSeverity = "critical"
)

type Event struct {
	CommandID      string        `json:"command_id,omitempty"`
	CommandAttr    string        `json:"command_attr,omitempty"`
	ID             int64         `json:"id"`
	DeviceID       string        `json:"device_id"`
	Type           EventType     `json:"type"`
	Severity       EventSeverity `json:"severity"`
	Message        string        `json:"message"`
	ContainerID    string        `json:"container_id,omitempty"`
	ContainerName  string        `json:"container_name,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	AcknowledgedAt *time.Time    `json:"acknowledged_at,omitempty"`
}

// ServerLog is a server log entry stored in the database.
type ServerLog struct {
	ID        int64          `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	Source    string         `json:"source,omitempty"`
}

// FleetSummary contains aggregated fleet statistics.
type FleetSummary struct {
	TotalDevices int `json:"total_devices"`
	OnlineCount  int `json:"online_count"`
	OfflineCount int `json:"offline_count"`
	WarningCount int `json:"warning_count"`
	RecentEvents int `json:"recent_events"`
}
