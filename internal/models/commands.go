package models

import "time"

// Command represents a remote command sent to a device.
type Command struct {
	ID        string                 `json:"id"`
	DeviceID  string                 `json:"device_id"`
	Action    string                 `json:"action"`     // docker_stop, docker_restart, docker_start, reboot, agent_update
	Params    map[string]interface{} `json:"params"`
	Status    string                 `json:"status"`     // pending, sent, success, error
	ResultMsg string                 `json:"result_msg"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// CommandPayload is the data sent to an agent over WebSocket.
type CommandPayload struct {
	CommandID string                 `json:"command_id"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
}

// CommandResult is the response from an agent.
type CommandResult struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`  // success, error
	Message   string `json:"message"`
}

// AutoUpdatePolicy defines an auto-update rule for a container or compose stack.
type AutoUpdatePolicy struct {
	ID              int        `json:"id"`
	DeviceID        string     `json:"device_id"`
	Target          string     `json:"target"`           // container name or compose project
	IsStack         bool       `json:"is_stack"`
	ComposeWorkDir  string     `json:"compose_work_dir"`
	Enabled         bool       `json:"enabled"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}
