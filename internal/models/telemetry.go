package models

import (
	"strings"
	"time"
)

// HeartbeatData is the lightweight ping payload.
type HeartbeatData struct {
	Uptime              uint64  `json:"uptime"`
	CPUPercent          float64 `json:"cpu_percent"`
	MemPercent          float64 `json:"mem_percent"`
	LoadAvg1m           float64 `json:"load_avg_1m"`
	DiskRootPercent     float64 `json:"disk_root_percent"`
	DiskReadBytesPerSec float64 `json:"disk_read_bytes_sec,omitempty"`
	DiskWriteBytesPerSec float64 `json:"disk_write_bytes_sec,omitempty"`
	DiskIOPercent       float64 `json:"disk_io_percent,omitempty"`
	AgentVersion        string  `json:"agent_version,omitempty"`
	LogErrors           int     `json:"log_errors,omitempty"`
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
	Security   *SecurityInfo  `json:"security,omitempty"`
	Logs       []LogEntry     `json:"logs,omitempty"`
	UPS        *UPSInfo       `json:"ups,omitempty"`
	WebServers *WebServerInfo `json:"web_servers,omitempty"`
	USB        *USBInfo        `json:"usb,omitempty"`
	Hardware   *HardwareInfo   `json:"hardware,omitempty"`
	CronJobs       *CronInfo            `json:"cron_jobs,omitempty"`
	ContainerLogs  []ContainerLogEntry  `json:"container_logs,omitempty"`
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
	IsPool         bool    `json:"is_pool,omitempty"`
}

// PoolFSTypes is the authoritative list of filesystem types classified as storage pools.
// Mirror list also exists in web/src/utils/filesystem.ts (POOL_FS_TYPES) for frontend fallback.
// When adding a new pool type, both locations must be updated.
// Device-path-based detection (mdraid, LVM) is handled in IsPoolFilesystem.
var PoolFSTypes = []string{
	"bcachefs",
	"btrfs",
	"fuse.mergerfs",
	"fuse.shfs",
	"fuse.unionfs",
	"mergerfs",
	"shfs",
	"zfs",
}

// IsPoolFilesystem returns true if the filesystem should be classified as a
// storage pool based on its filesystem type or device path.
//
// Detection is additive: a match on any rule returns true.
// Rules are checked in order: filesystem type list, mdraid device prefix,
// device-mapper prefix (with exclusions for Docker and live-boot), dm- kernel name.
//
// Exclusions apply only to device-path detection:
//   - /dev/mapper/docker-* (Docker storage driver, not user-facing storage)
//   - /dev/mapper/live-rw and /dev/mapper/live-base (live-boot overlays)
//
// The mirror of this logic lives in web/src/utils/filesystem.ts (isPoolFilesystem).
// Both locations must be kept in sync when detection rules change.
func IsPoolFilesystem(fsType, device string) bool {
	// 1. Filesystem type check
	for _, t := range PoolFSTypes {
		if fsType == t {
			return true
		}
	}

	// 2. mdraid: /dev/md*
	if strings.HasPrefix(device, "/dev/md") {
		return true
	}

	// 3. Device-mapper: /dev/mapper/*
	if strings.HasPrefix(device, "/dev/mapper/") {
		// Exclude Docker device-mapper volumes
		if strings.HasPrefix(device, "/dev/mapper/docker-") {
			return false
		}
		// Exclude live-boot overlay devices
		if device == "/dev/mapper/live-rw" || device == "/dev/mapper/live-base" {
			return false
		}
		return true
	}

	// 4. Device-mapper kernel name: /dev/dm-*
	if strings.HasPrefix(device, "/dev/dm-") {
		return true
	}

	return false
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
	NetworkMode     string            `json:"network_mode,omitempty"`
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

// ContainerLogEntry holds a single log line from a Docker container.
type ContainerLogEntry struct {
	ID            int64     `json:"id,omitempty"`
	DeviceID      string    `json:"device_id,omitempty"`
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Timestamp     time.Time `json:"timestamp"`
	Stream        string    `json:"stream"` // "stdout" or "stderr"
	Line          string    `json:"line"`
}

// USBInfo holds USB device information.
type USBInfo struct {
	Devices []USBDevice `json:"devices,omitempty"`
}

// USBDevice represents a single USB device.
type USBDevice struct {
	Bus         string  `json:"bus"`
	Device      string  `json:"device"`
	VendorID    string  `json:"vendor_id"`
	ProductID   string  `json:"product_id"`
	Vendor      string  `json:"vendor,omitempty"`
	Product     string  `json:"product,omitempty"`
	Serial      string  `json:"serial,omitempty"`
	Description string  `json:"description"`
	DeviceClass string  `json:"device_class,omitempty"`
	SpeedMbps   float64 `json:"speed_mbps,omitempty"`
	SysPath     string  `json:"sys_path,omitempty"`
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

// WebServerInfo holds discovered reverse proxy / web server details.
type WebServerInfo struct {
	Servers []ProxyServer `json:"servers,omitempty"`
}

// ProxyServer represents a single detected proxy server (nginx, Caddy, etc.).
type ProxyServer struct {
	Name           string            `json:"name"`
	Version        string            `json:"version,omitempty"`
	Status         string            `json:"status"`
	PID            int               `json:"pid,omitempty"`
	ConfigPath     string            `json:"config_path,omitempty"`
	ConfigValid    *bool             `json:"config_valid,omitempty"`
	ConfigError    string            `json:"config_error,omitempty"`
	Sites          []ProxySite       `json:"sites,omitempty"`
	Certs          []ProxyCert       `json:"certs,omitempty"`
	Upstreams      []ProxyUpstream   `json:"upstreams,omitempty"`
	SecurityConfig *ProxySecurityCfg `json:"security_config,omitempty"`
}

// ProxySite represents a virtual host / site block.
type ProxySite struct {
	ServerNames []string `json:"server_names,omitempty"`
	Listen      []string `json:"listen,omitempty"`
	Root        string   `json:"root,omitempty"`
	ProxyPass   string   `json:"proxy_pass,omitempty"`
	SSLCert     string   `json:"ssl_cert,omitempty"`
	Enabled     bool     `json:"enabled"`
	ConfigFile  string   `json:"config_file,omitempty"`
}

// ProxyCert represents an SSL/TLS certificate found in proxy configuration.
type ProxyCert struct {
	FilePath    string   `json:"file_path"`
	Subject     string   `json:"subject,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	SANs        []string `json:"sans,omitempty"`
	NotBefore   string   `json:"not_before,omitempty"`
	NotAfter    string   `json:"not_after,omitempty"`
	DaysLeft    int      `json:"days_left"`
	KeyType     string   `json:"key_type,omitempty"`
	IsCA        bool     `json:"is_ca,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
}

// ProxyUpstream represents a named upstream/backend group.
type ProxyUpstream struct {
	Name    string           `json:"name"`
	Servers []UpstreamServer `json:"servers,omitempty"`
}

// UpstreamServer represents a single backend server in an upstream group.
type UpstreamServer struct {
	Address string `json:"address"`
	Weight  int    `json:"weight,omitempty"`
	Backup  bool   `json:"backup,omitempty"`
	Down    bool   `json:"down,omitempty"`
}

// ProxySecurityCfg holds security-related proxy configuration.
type ProxySecurityCfg struct {
	RateLimiting    []RateLimitRule    `json:"rate_limiting,omitempty"`
	AccessControls  []AccessRule       `json:"access_controls,omitempty"`
	SecurityHeaders map[string]string  `json:"security_headers,omitempty"`
	AllowedMethods  []string           `json:"allowed_methods,omitempty"`
	CORSOrigins     []string           `json:"cors_origins,omitempty"`
}

// RateLimitRule represents a rate limiting configuration.
type RateLimitRule struct {
	Zone  string `json:"zone"`
	Rate  string `json:"rate"`
	Burst int    `json:"burst,omitempty"`
}

// AccessRule represents an allow/deny access control rule.
type AccessRule struct {
	Directive string `json:"directive"`
	Value     string `json:"value"`
	Location  string `json:"location,omitempty"`
}

// HardwareInfo holds detailed hardware device information.
type HardwareInfo struct {
	PCIDevices  []PCIDevice  `json:"pci_devices,omitempty"`
	DiskDrives  []DiskDrive  `json:"disk_drives,omitempty"`
	SerialPorts []SerialPort `json:"serial_ports,omitempty"`
	GPUs        []GPUInfo    `json:"gpus,omitempty"`
}

// PCIDevice represents a PCI/PCIe device.
type PCIDevice struct {
	Slot           string `json:"slot"`
	VendorID       string `json:"vendor_id"`
	DeviceID       string `json:"device_id"`
	Vendor         string `json:"vendor,omitempty"`
	Device         string `json:"device,omitempty"`
	Description    string `json:"description"`
	ClassCode      string `json:"class_code,omitempty"`
	ClassName      string `json:"class_name,omitempty"`
	Driver         string `json:"driver,omitempty"`
	SubsysVendorID string `json:"subsys_vendor_id,omitempty"`
	SubsysDeviceID string `json:"subsys_device_id,omitempty"`
	NUMANode       string `json:"numa_node,omitempty"`
	IRQ            string `json:"irq,omitempty"`
}

// DiskDrive represents a physical disk drive.
type DiskDrive struct {
	Name      string  `json:"name"`
	Model     string  `json:"model,omitempty"`
	Serial    string  `json:"serial,omitempty"`
	Rev       string  `json:"rev,omitempty"`
	SizeBytes int64   `json:"size_bytes"`
	SizeGB    float64 `json:"size_gb"`
	Type      string  `json:"type,omitempty"` // HDD, SSD, NVMe, SD/eMMC
	Transport string  `json:"transport,omitempty"`
	Removable bool    `json:"removable,omitempty"`
	Scheduler string  `json:"scheduler,omitempty"`
	// SMART health data (populated via smartctl if available)
	SmartHealth        string  `json:"smart_health,omitempty"`         // PASSED, FAILED, UNKNOWN
	SmartTemp          *int    `json:"smart_temp,omitempty"`           // Celsius
	SmartPowerOnHours  *int64  `json:"smart_power_on_hours,omitempty"`
	SmartReallocated   *int64  `json:"smart_reallocated,omitempty"`    // Reallocated sector count
	SmartPendingSector *int64  `json:"smart_pending_sector,omitempty"` // Current pending sector count
	SmartAvailable     bool    `json:"smart_available"`                // Whether smartctl returned data
}

// SerialPort represents a serial port.
type SerialPort struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Type   string `json:"type,omitempty"` // UART, USB-Serial, USB-ACM, ARM-UART
	Driver string `json:"driver,omitempty"`
}

// GPUInfo represents a graphics processing unit.
type GPUInfo struct {
	Vendor      string `json:"vendor,omitempty"`
	Model       string `json:"model,omitempty"`
	PCISlot     string `json:"pci_slot,omitempty"`
	Driver      string `json:"driver,omitempty"`
	VRAMMB      int    `json:"vram_mb,omitempty"`
	Description string `json:"description"`
}

// CronInfo holds cron job and systemd timer information.
type CronInfo struct {
	Jobs   []CronJob   `json:"jobs,omitempty"`
	Timers []CronTimer `json:"timers,omitempty"`
}

type CronJob struct {
	User     string `json:"user"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Source   string `json:"source"`
	Enabled  bool   `json:"enabled"`
	NextRun  string `json:"next_run,omitempty"`
	LastRun  string `json:"last_run,omitempty"`
	TaskName string `json:"task_name,omitempty"`
}

type CronTimer struct {
	Name     string `json:"name"`
	Calendar string `json:"calendar"`
	NextRun  string `json:"next_run,omitempty"`
	LastRun  string `json:"last_run,omitempty"`
	Unit     string `json:"unit"`
	Enabled  bool   `json:"enabled"`
}
