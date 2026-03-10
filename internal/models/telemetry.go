package models

import "time"

// HeartbeatData is the lightweight ping payload.
type HeartbeatData struct {
	Uptime          uint64  `json:"uptime"`
	CPUPercent      float64 `json:"cpu_percent"`
	MemPercent      float64 `json:"mem_percent"`
	LoadAvg1m       float64 `json:"load_avg_1m"`
	DiskRootPercent float64 `json:"disk_root_percent"`
	AgentVersion    string  `json:"agent_version,omitempty"`
	LogErrors       int     `json:"log_errors,omitempty"`
}

// Heartbeat wraps a heartbeat with metadata.
type Heartbeat struct {
	ID        int64         `json:"id,omitempty"`
	DeviceID  string        `json:"device_id"`
	Timestamp time.Time     `json:"timestamp"`
	Data      HeartbeatData `json:"data"`
}

// LogEntry represents a single journal log entry from a device.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Priority  int       `json:"priority"`
	Unit      string    `json:"unit"`
	Message   string    `json:"message"`
}

// FullTelemetryData contains all collected system information.
type FullTelemetryData struct {
	System   *SystemInfo   `json:"system,omitempty"`
	OS       *OSInfo       `json:"os,omitempty"`
	CPU      *CPUInfo      `json:"cpu,omitempty"`
	Memory   *MemoryInfo   `json:"memory,omitempty"`
	Disks    *DiskInfo     `json:"disks,omitempty"`
	Network  *NetworkInfo  `json:"network,omitempty"`
	Updates  *UpdateInfo   `json:"updates,omitempty"`
	Services []ServiceInfo `json:"services,omitempty"`
	Procs    *ProcessInfo  `json:"processes,omitempty"`
	Docker   *DockerInfo   `json:"docker,omitempty"`
	Security *SecurityInfo `json:"security,omitempty"`
	Logs     []LogEntry    `json:"logs,omitempty"`
	UPS      *UPSInfo      `json:"ups,omitempty"`
}

// TelemetrySnapshot wraps full telemetry with metadata.
type TelemetrySnapshot struct {
	ID        int64             `json:"id,omitempty"`
	DeviceID  string            `json:"device_id"`
	Timestamp time.Time         `json:"timestamp"`
	Data      FullTelemetryData `json:"data"`
}

// SystemInfo holds hardware identity info.
type SystemInfo struct {
	Hostname       string `json:"hostname"`
	DeviceUUID     string `json:"device_uuid"`
	Arch           string `json:"arch"`
	CPUModel       string `json:"cpu_model"`
	CPUCores       int    `json:"cpu_cores"`
	CPUThreads     int    `json:"cpu_threads"`
	TotalRAMMB     int64  `json:"total_ram_mb"`
	BoardModel     string `json:"board_model,omitempty"`
	SerialNumber   string `json:"serial_number,omitempty"`
	BIOSVersion    string `json:"bios_version,omitempty"`
	BIOSDate       string `json:"bios_date,omitempty"`
	Virtualization string `json:"virtualization,omitempty"`
}

// OSInfo holds operating system details.
type OSInfo struct {
	Name         string `json:"name"`
	ID           string `json:"id"`
	Version      string `json:"version"`
	Codename     string `json:"codename,omitempty"`
	Kernel       string `json:"kernel"`
	KernelArch   string `json:"kernel_arch"`
	InitSystem   string `json:"init_system,omitempty"`
	BootTime     int64  `json:"boot_time"`
	Uptime       uint64 `json:"uptime"`
	Timezone     string `json:"timezone,omitempty"`
	Locale       string `json:"locale,omitempty"`
}

// CPUInfo holds CPU metrics.
type CPUInfo struct {
	UsagePercent  float64   `json:"usage_percent"`
	PerCore       []float64 `json:"per_core,omitempty"`
	LoadAvg1m     float64   `json:"load_avg_1m"`
	LoadAvg5m     float64   `json:"load_avg_5m"`
	LoadAvg15m    float64   `json:"load_avg_15m"`
	Temperature   *float64  `json:"temperature,omitempty"`
	FreqCurrent   *float64  `json:"freq_current,omitempty"`
	FreqMax       *float64  `json:"freq_max,omitempty"`
}

// MemoryInfo holds RAM/swap metrics.
type MemoryInfo struct {
	TotalMB      int64   `json:"total_mb"`
	UsedMB       int64   `json:"used_mb"`
	FreeMB       int64   `json:"free_mb"`
	CachedMB     int64   `json:"cached_mb"`
	BuffersMB    int64   `json:"buffers_mb"`
	SwapTotalMB  int64   `json:"swap_total_mb"`
	SwapUsedMB   int64   `json:"swap_used_mb"`
	UsagePercent float64 `json:"usage_percent"`
}

// DiskInfo holds block device and filesystem info.
type DiskInfo struct {
	BlockDevices []BlockDevice `json:"block_devices,omitempty"`
	Filesystems  []Filesystem  `json:"filesystems,omitempty"`
}

type BlockDevice struct {
	Name        string `json:"name"`
	Model       string `json:"model,omitempty"`
	SizeGB      float64 `json:"size_gb"`
	Type        string `json:"type,omitempty"` // HDD, SSD, NVMe, SD
	SmartStatus string `json:"smart_status,omitempty"`
}

type Filesystem struct {
	MountPoint     string  `json:"mount_point"`
	Device         string  `json:"device"`
	FSType         string  `json:"fs_type"`
	TotalGB        float64 `json:"total_gb"`
	UsedGB         float64 `json:"used_gb"`
	FreeGB         float64 `json:"free_gb"`
	UsagePercent   float64 `json:"usage_percent"`
	MountOptions   string  `json:"mount_options,omitempty"`
	IsNetworkMount bool    `json:"is_network_mount"`
}

// NetworkInfo holds network interface details.
type NetworkInfo struct {
	Interfaces     []NetworkInterface `json:"interfaces,omitempty"`
	DefaultGateway string             `json:"default_gateway,omitempty"`
	DNSServers     []string           `json:"dns_servers,omitempty"`
	FQDN           string             `json:"fqdn,omitempty"`
}

type NetworkInterface struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac,omitempty"`
	IPv4      []string `json:"ipv4,omitempty"`
	IPv6      []string `json:"ipv6,omitempty"`
	State     string   `json:"state"`
	SpeedMbps *int     `json:"speed_mbps,omitempty"`
	BytesSent uint64   `json:"bytes_sent"`
	BytesRecv uint64   `json:"bytes_recv"`
}

// UpdateInfo holds package manager and update details.
type UpdateInfo struct {
	PackageManager       string          `json:"package_manager,omitempty"`
	TotalInstalled       int             `json:"total_installed"`
	PendingUpdates       int             `json:"pending_updates"`
	PendingSecurityCount int             `json:"pending_security_count"`
	PendingKernelUpdate  bool            `json:"pending_kernel_update"`
	PendingKernelVersion string          `json:"pending_kernel_version,omitempty"`
	Updates              []PendingUpdate `json:"updates,omitempty"`
	LastCheckTime        *time.Time      `json:"last_check_time,omitempty"`
	UnattendedUpgrades   bool            `json:"unattended_upgrades"`
}

type PendingUpdate struct {
	Name       string `json:"name"`
	CurrentVer string `json:"current_ver"`
	NewVer     string `json:"new_ver"`
	IsSecurity bool   `json:"is_security"`
}

// ServiceInfo holds systemd service details.
type ServiceInfo struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Enabled   bool   `json:"enabled"`
	PID       int    `json:"pid,omitempty"`
	MemoryMB  float64 `json:"memory_mb,omitempty"`
}

// ProcessInfo holds top processes.
type ProcessInfo struct {
	TopByCPU    []ProcessEntry `json:"top_by_cpu,omitempty"`
	TopByMemory []ProcessEntry `json:"top_by_memory,omitempty"`
}

type ProcessEntry struct {
	PID     int32   `json:"pid"`
	Name    string  `json:"name"`
	CPU     float64 `json:"cpu_percent"`
	MemPct  float64 `json:"mem_percent"`
	MemMB   float64 `json:"mem_mb"`
	User    string  `json:"user"`
	Command string  `json:"command,omitempty"`
}

// DockerInfo holds Docker daemon and container details.
type DockerInfo struct {
	Available       bool            `json:"available"`
	Version         string          `json:"version,omitempty"`
	APIVersion      string          `json:"api_version,omitempty"`
	TotalContainers int             `json:"total_containers"`
	Running         int             `json:"running"`
	Stopped         int             `json:"stopped"`
	Paused          int             `json:"paused"`
	ImagesTotal     int             `json:"images_total"`
	StorageDriver   string          `json:"storage_driver,omitempty"`
	DockerRootDir   string          `json:"docker_root_dir,omitempty"`
	Containers      []ContainerInfo `json:"containers,omitempty"`
}

// ContainerInfo holds rich per-container data.
type ContainerInfo struct {
	ID            string            `json:"id"`
	ShortID       string            `json:"short_id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	State         string            `json:"state"`
	Status        string            `json:"status"`
	Created       int64             `json:"created"`
	Ports         []PortMapping     `json:"ports,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	CPUPercent    float64           `json:"cpu_percent"`
	MemUsage      int64             `json:"mem_usage"`
	MemLimit      int64             `json:"mem_limit"`
	CPULimit      int64             `json:"cpu_limit,omitempty"` // NanoCPUs (1e9 = 1 core)
	RestartPolicy string            `json:"restart_policy,omitempty"`
	RestartCount  int               `json:"restart_count,omitempty"`
	Mounts        []MountInfo       `json:"mounts,omitempty"`
	Networks      []NetworkAttach   `json:"networks,omitempty"`
	Env           []EnvVar          `json:"env,omitempty"`
	HealthStatus  string            `json:"health_status,omitempty"`
	RepoURL         string            `json:"repo_url,omitempty"`
	Riot            *RiotLabels       `json:"riot,omitempty"`
	UpdateAvailable *bool             `json:"update_available,omitempty"`
}

// PortMapping represents a container port binding.
type PortMapping struct {
	ContainerPort string `json:"container_port"`
	HostPort      string `json:"host_port,omitempty"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"host_ip,omitempty"`
}

// MountInfo describes a container volume or bind mount.
type MountInfo struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only"`
}

// NetworkAttach describes a container's connection to a Docker network.
type NetworkAttach struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	MacAddr   string `json:"mac_addr,omitempty"`
}

// EnvVar holds a parsed KEY=VALUE environment variable.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// RiotLabels holds parsed riot.* label values.
type RiotLabels struct {
	Group       string   `json:"group,omitempty"`
	Name        string   `json:"name,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Description string   `json:"description,omitempty"`
	URL         string   `json:"url,omitempty"`
	Priority    int      `json:"priority"`
	Hide        bool     `json:"hide,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// UPSInfo holds NUT UPS status.
type UPSInfo struct {
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	OnBattery      bool     `json:"on_battery"`
	LowBattery     bool     `json:"low_battery"`
	BatteryCharge  *float64 `json:"battery_charge,omitempty"`
	BatteryRuntime *int     `json:"battery_runtime,omitempty"`
	InputVoltage   *float64 `json:"input_voltage,omitempty"`
	OutputVoltage  *float64 `json:"output_voltage,omitempty"`
	Load           *float64 `json:"load,omitempty"`
	Model          string   `json:"model,omitempty"`
	Manufacturer   string   `json:"manufacturer,omitempty"`
}

// ContainerMetric holds a single per-container CPU/memory data point.
type ContainerMetric struct {
	ID            int64     `json:"id,omitempty"`
	DeviceID      string    `json:"device_id"`
	ContainerName string    `json:"container_name"`
	ContainerID   string    `json:"container_id"`
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemUsage      int64     `json:"mem_usage"`
	MemLimit      int64     `json:"mem_limit"`
	CPULimit      int64     `json:"cpu_limit,omitempty"` // NanoCPUs (1e9 = 1 core)
}

// SecurityInfo holds security-related info.
type SecurityInfo struct {
	SELinux        string `json:"selinux,omitempty"`
	AppArmor       string `json:"apparmor,omitempty"`
	FirewallStatus string `json:"firewall_status,omitempty"`
	FailedLogins24h int   `json:"failed_logins_24h"`
	LoggedInUsers  int    `json:"logged_in_users"`
	OpenPorts      []int  `json:"open_ports,omitempty"`
}
