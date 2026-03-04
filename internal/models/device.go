package models

import "time"

type DeviceStatus string

const (
	DeviceStatusOnline  DeviceStatus = "online"
	DeviceStatusOffline DeviceStatus = "offline"
	DeviceStatusWarning DeviceStatus = "warning"
)

type Device struct {
	ID              string            `json:"id"`
	ShortID         string            `json:"short_id"`
	Hostname        string            `json:"hostname"`
	Arch            string            `json:"arch"`
	AgentVersion    string            `json:"agent_version,omitempty"`
	PrimaryIP       string            `json:"primary_ip,omitempty"`
	Status          DeviceStatus      `json:"status"`
	Tags            []string          `json:"tags"`
	HardwareProfile *HardwareProfile  `json:"hardware_profile,omitempty"`
	LastHeartbeat   *time.Time        `json:"last_heartbeat,omitempty"`
	LastTelemetry   *time.Time        `json:"last_telemetry,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type HardwareProfile struct {
	CPUModel         string `json:"cpu_model"`
	CPUCores         int    `json:"cpu_cores"`
	CPUThreads       int    `json:"cpu_threads"`
	TotalRAMMB       int64  `json:"total_ram_mb"`
	BoardModel       string `json:"board_model,omitempty"`
	SerialNumber     string `json:"serial_number,omitempty"`
	BIOSVersion      string `json:"bios_version,omitempty"`
	BIOSDate         string `json:"bios_date,omitempty"`
	Virtualization   string `json:"virtualization,omitempty"`
}

type DeviceRegistration struct {
	Hostname        string           `json:"hostname"`
	Arch            string           `json:"arch"`
	AgentVersion    string           `json:"agent_version,omitempty"`
	Tags            []string         `json:"tags,omitempty"`
	DeviceID        string           `json:"device_id,omitempty"` // Set on re-registration
	HardwareProfile *HardwareProfile `json:"hardware_profile"`
}

type DeviceRegistrationResponse struct {
	DeviceID string `json:"device_id"`
	ShortID  string `json:"short_id"`
	APIKey   string `json:"api_key"`
}
