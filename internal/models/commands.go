package models

import "time"

// Command represents a remote command sent to a device.
type Command struct {
	ID         string                 `json:"id"`
	DeviceID   string                 `json:"device_id"`
	Action     string                 `json:"action"`     // docker_stop, docker_restart, docker_start, reboot, agent_update
	Params     map[string]interface{} `json:"params"`
	Status     string                 `json:"status"`     // pending, sent, success, error
	ResultMsg  string                 `json:"result_msg"`
	DurationMs *int64                 `json:"duration_ms,omitempty"`
	ExitCode   *int                   `json:"exit_code,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// CommandOutput holds captured stdout/stderr from a command execution.
type CommandOutput struct {
	ID        int64     `json:"id"`
	CommandID string    `json:"command_id"`
	Stream    string    `json:"stream"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// CommandPayload is the data sent to an agent over WebSocket.
type CommandPayload struct {
	CommandID string                 `json:"command_id"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
}

// CommandResult is the response from an agent.
type CommandResult struct {
	CommandID  string `json:"command_id"`
	Status     string `json:"status"`  // success, error
	Message    string `json:"message"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	DurationMs *int64 `json:"duration_ms,omitempty"`
	Output     string `json:"output,omitempty"`
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
