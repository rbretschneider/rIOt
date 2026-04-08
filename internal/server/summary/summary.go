// Package summary generates Markdown device summary documents from telemetry data.
package summary

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// summaryTemplate is the Markdown template for device summaries.
// Parsed once at package init via template.Must.
var summaryTemplate = template.Must(
	template.New("summary").Funcs(template.FuncMap{
		"formatBytes":  formatBytes,
		"formatFreq":   formatFreq,
		"formatSpeed":  formatSpeed,
		"formatUptime": formatUptime,
		"escapePipe":   escapePipe,
		"joinStrings":  strings.Join,
	}).Parse(rawTemplate),
)

// summaryContainer holds only the safe, non-sensitive container fields (SEC-002).
// Explicitly excludes Env, Labels, Mounts, Networks, Ports and all other fields
// that could contain secrets.
type summaryContainer struct {
	Name   string
	Image  string
	State  string
	Status string
}

// summaryData is the template-facing struct. It holds only inventory-appropriate
// data, acting as a security filter between raw telemetry and the template (AD-006).
// String fields that come from agent-supplied telemetry may contain Markdown special
// characters; use escapePipe in the template for all table cell values.
type summaryData struct {
	// Header
	Hostname  string
	ShortID   string
	Generated string

	// System Identity (from SystemInfo)
	DeviceUUID     string
	BoardModel     string
	SerialNumber   string
	BIOSVersion    string
	BIOSDate       string
	Arch           string
	Virtualization string

	// OS
	OSName     string
	OSVersion  string
	OSCodename string
	Kernel     string
	KernelArch string
	InitSystem string
	Timezone   string
	Locale     string
	Uptime     uint64

	// CPU
	CPUModel   string
	CPUCores   int
	CPUThreads int
	CPUFreqMax *float64

	// Memory (MB)
	TotalRAMMB  int64
	TotalSwapMB int64

	// Storage
	DiskDrives  []models.DiskDrive
	Filesystems []models.Filesystem

	// GPUs (from HardwareInfo.GPUs — static inventory, not runtime metrics)
	GPUs    []models.GPUInfo
	HasGPUs bool

	// Network
	NetworkInterfaces []models.NetworkInterface
	DefaultGateway    string
	DNSServers        []string
	FQDN              string

	// USB
	USBDevices []models.USBDevice
	HasUSB     bool

	// Docker (safe subset only — see summaryContainer)
	HasDocker        bool
	DockerVersion    string
	TotalContainers  int
	Running          int
	Stopped          int
	Paused           int
	StorageDriver    string
	Containers       []summaryContainer

	// UPS
	HasUPS          bool
	UPSName         string
	UPSModel        string
	UPSManufacturer string
	UPSStatus       string
	UPSBatCharge    string
	UPSLoad         string
}

// Render generates a Markdown summary document from the given device and snapshot.
// Returns the rendered string or an error if template execution fails.
func Render(device *models.Device, snapshot *models.TelemetrySnapshot) (string, error) {
	d := buildSummaryData(device, snapshot)

	var buf bytes.Buffer
	if err := summaryTemplate.Execute(&buf, d); err != nil {
		return "", fmt.Errorf("execute summary template: %w", err)
	}
	return buf.String(), nil
}

// buildSummaryData populates summaryData from the device and snapshot.
func buildSummaryData(device *models.Device, snapshot *models.TelemetrySnapshot) summaryData {
	d := summaryData{
		Hostname:  device.Hostname,
		ShortID:   device.ShortID,
		Generated: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}

	tel := snapshot.Data

	// System identity — prefer live telemetry, fall back to hardware profile
	if tel.System != nil {
		d.DeviceUUID = tel.System.DeviceUUID
		d.BoardModel = tel.System.BoardModel
		d.SerialNumber = tel.System.SerialNumber
		d.BIOSVersion = tel.System.BIOSVersion
		d.BIOSDate = tel.System.BIOSDate
		d.Arch = tel.System.Arch
		d.Virtualization = tel.System.Virtualization
	} else if device.HardwareProfile != nil {
		d.BoardModel = device.HardwareProfile.BoardModel
		d.SerialNumber = device.HardwareProfile.SerialNumber
		d.BIOSVersion = device.HardwareProfile.BIOSVersion
		d.BIOSDate = device.HardwareProfile.BIOSDate
	}
	if d.Arch == "" {
		d.Arch = device.Arch
	}

	// OS
	if tel.OS != nil {
		d.OSName = tel.OS.Name
		d.OSVersion = tel.OS.Version
		d.OSCodename = tel.OS.Codename
		d.Kernel = tel.OS.Kernel
		d.KernelArch = tel.OS.KernelArch
		d.InitSystem = tel.OS.InitSystem
		d.Timezone = tel.OS.Timezone
		d.Locale = tel.OS.Locale
		d.Uptime = tel.OS.Uptime
	}

	// CPU — identity fields only (no per-core usage, no load averages)
	if tel.CPU != nil {
		d.CPUFreqMax = tel.CPU.FreqMax
	}
	// CPU model/cores come from SystemInfo or HardwareProfile
	if tel.System != nil {
		d.CPUModel = tel.System.CPUModel
		d.CPUCores = tel.System.CPUCores
		d.CPUThreads = tel.System.CPUThreads
	} else if device.HardwareProfile != nil {
		d.CPUModel = device.HardwareProfile.CPUModel
		d.CPUCores = device.HardwareProfile.CPUCores
		d.CPUThreads = device.HardwareProfile.CPUThreads
	}

	// Memory — totals only (no usage percentages or cache details)
	if tel.Memory != nil {
		d.TotalRAMMB = tel.Memory.TotalMB
		d.TotalSwapMB = tel.Memory.SwapTotalMB
	} else if tel.System != nil {
		d.TotalRAMMB = tel.System.TotalRAMMB
	} else if device.HardwareProfile != nil {
		d.TotalRAMMB = device.HardwareProfile.TotalRAMMB
	}

	// Storage
	// Physical drives from HardwareInfo.DiskDrives (has Serial field)
	if tel.Hardware != nil {
		d.DiskDrives = tel.Hardware.DiskDrives
		d.GPUs = tel.Hardware.GPUs
		d.HasGPUs = len(tel.Hardware.GPUs) > 0
	}
	// Filesystems from DiskInfo
	if tel.Disks != nil {
		d.Filesystems = tel.Disks.Filesystems
	}

	// Network
	if tel.Network != nil {
		d.NetworkInterfaces = tel.Network.Interfaces
		d.DefaultGateway = tel.Network.DefaultGateway
		d.DNSServers = tel.Network.DNSServers
		d.FQDN = tel.Network.FQDN
	}

	// USB
	if tel.USB != nil && len(tel.USB.Devices) > 0 {
		d.USBDevices = tel.USB.Devices
		d.HasUSB = true
	}

	// Docker — only include safe fields (SEC-002: explicitly exclude Env, Labels, Mounts, Networks)
	if tel.Docker != nil && tel.Docker.Available {
		d.HasDocker = true
		d.DockerVersion = tel.Docker.Version
		d.TotalContainers = tel.Docker.TotalContainers
		d.Running = tel.Docker.Running
		d.Stopped = tel.Docker.Stopped
		d.Paused = tel.Docker.Paused
		d.StorageDriver = tel.Docker.StorageDriver
		for _, c := range tel.Docker.Containers {
			d.Containers = append(d.Containers, summaryContainer{
				Name:   c.Name,
				Image:  c.Image,
				State:  c.State,
				Status: c.Status,
			})
		}
	}

	// UPS
	// Battery charge and load are formatted here (not in the template) because
	// UPSInfo uses *float64 pointer fields. Pre-formatting avoids template reflection
	// (deref helpers) and keeps the template readable. The em-dash fallback satisfies
	// FR-002: the value is always present in the struct, but the section is only
	// rendered when HasUPS is true.
	if tel.UPS != nil {
		d.HasUPS = true
		d.UPSName = tel.UPS.Name
		d.UPSModel = tel.UPS.Model
		d.UPSManufacturer = tel.UPS.Manufacturer
		d.UPSStatus = tel.UPS.Status
		if tel.UPS.BatteryCharge != nil {
			d.UPSBatCharge = fmt.Sprintf("%.1f%%", *tel.UPS.BatteryCharge)
		} else {
			d.UPSBatCharge = "—"
		}
		if tel.UPS.Load != nil {
			d.UPSLoad = fmt.Sprintf("%.1f%%", *tel.UPS.Load)
		} else {
			d.UPSLoad = "—"
		}
	}

	return d
}

// formatBytes converts megabytes to a human-readable string.
func formatBytes(mb int64) string {
	if mb <= 0 {
		return "—"
	}
	gb := mb / 1024
	if gb >= 1 {
		return fmt.Sprintf("%d MB (%d GB)", mb, gb)
	}
	return fmt.Sprintf("%d MB", mb)
}

// formatFreq converts MHz to a human-readable frequency string.
func formatFreq(mhz *float64) string {
	if mhz == nil {
		return "—"
	}
	if *mhz >= 1000 {
		return fmt.Sprintf("%.2f GHz", *mhz/1000)
	}
	return fmt.Sprintf("%.0f MHz", *mhz)
}

// formatSpeed formats a link speed in Mbps.
func formatSpeed(mbps *int) string {
	if mbps == nil {
		return "—"
	}
	if *mbps >= 1000 {
		return fmt.Sprintf("%d Gbps", *mbps/1000)
	}
	return fmt.Sprintf("%d Mbps", *mbps)
}

// formatUptime converts seconds to "Xd Yh Zm" format.
func formatUptime(seconds uint64) string {
	if seconds == 0 {
		return "—"
	}
	d := seconds / 86400
	h := (seconds % 86400) / 3600
	m := (seconds % 3600) / 60
	if d > 0 {
		return fmt.Sprintf("%dd %dh %dm", d, h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// escapePipe replaces pipe characters in table cell values to prevent Markdown
// table corruption (SEC-003).
func escapePipe(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// rawTemplate is the Markdown template string.
// text/template is used (not html/template) because the output is plain text Markdown.
// html/template would HTML-escape characters like < > & in field values, corrupting
// valid Markdown output.
//
// FR-002: Sections whose data is absent must be omitted entirely. Each optional
// section is wrapped in {{- if .HasXxx}} or {{- if .Field}} so it is skipped when
// the corresponding summaryData field is zero/nil/empty. The System Identity, OS,
// CPU, and Memory sections are always rendered — they come from system/os_info
// collectors that are present on every registered device.
//
// Pipe characters in telemetry values are escaped via the escapePipe function to
// prevent Markdown table corruption (SEC-003).
const rawTemplate = `# Device Summary: {{.Hostname | escapePipe}}

**Device ID:** {{.ShortID}}
**Generated:** {{.Generated}}

---

## System Identity

| Field | Value |
|-------|-------|
| Hostname | {{.Hostname | escapePipe}} |
| Device UUID | {{.DeviceUUID | escapePipe}} |
| Board Model | {{.BoardModel | escapePipe}} |
| Serial Number | {{.SerialNumber | escapePipe}} |
| BIOS Version | {{.BIOSVersion | escapePipe}} |
| BIOS Date | {{.BIOSDate | escapePipe}} |
| Architecture | {{.Arch | escapePipe}} |
| Virtualization | {{.Virtualization | escapePipe}} |

## Operating System

| Field | Value |
|-------|-------|
| OS | {{.OSName | escapePipe}} {{.OSVersion | escapePipe}} |
| Codename | {{.OSCodename | escapePipe}} |
| Kernel | {{.Kernel | escapePipe}} ({{.KernelArch | escapePipe}}) |
| Init System | {{.InitSystem | escapePipe}} |
| Timezone | {{.Timezone | escapePipe}} |
| Locale | {{.Locale | escapePipe}} |
| Uptime | {{formatUptime .Uptime}} |

## CPU

| Field | Value |
|-------|-------|
| Model | {{.CPUModel | escapePipe}} |
| Cores | {{.CPUCores}} |
| Threads | {{.CPUThreads}} |
| Max Frequency | {{formatFreq .CPUFreqMax}} |

## Memory

| Field | Value |
|-------|-------|
| Total RAM | {{formatBytes .TotalRAMMB}} |
| Total Swap | {{formatBytes .TotalSwapMB}} |
{{- if or .DiskDrives .Filesystems}}

## Storage
{{if .DiskDrives}}
### Physical Drives ({{len .DiskDrives}})

| Name | Model | Serial | Type | Size | SMART Health |
|------|-------|--------|------|------|--------------|
{{- range .DiskDrives}}
| {{.Name | escapePipe}} | {{.Model | escapePipe}} | {{.Serial | escapePipe}} | {{.Type | escapePipe}} | {{printf "%.1f GB" .SizeGB}} | {{.SmartHealth | escapePipe}} |
{{- end}}
{{end -}}
{{if .Filesystems}}
### Filesystems ({{len .Filesystems}})

| Mount Point | Device | Type | Total | Used | Free |
|-------------|--------|------|-------|------|------|
{{- range .Filesystems}}
| {{.MountPoint | escapePipe}} | {{.Device | escapePipe}} | {{.FSType | escapePipe}} | {{printf "%.1f GB" .TotalGB}} | {{printf "%.1f GB" .UsedGB}} | {{printf "%.1f GB" .FreeGB}} |
{{- end}}
{{end -}}
{{- end}}
{{- if .HasGPUs}}

## GPUs

| Vendor | Model | VRAM | PCI Slot | Driver |
|--------|-------|------|----------|--------|
{{- range .GPUs}}
| {{.Vendor | escapePipe}} | {{.Model | escapePipe}} | {{if gt .VRAMMB 0}}{{printf "%d MB" .VRAMMB}}{{else}}—{{end}} | {{.PCISlot | escapePipe}} | {{.Driver | escapePipe}} |
{{- end}}
{{- end}}
{{- if .NetworkInterfaces}}

## Network Interfaces

| Name | MAC | IPv4 | IPv6 | State | Speed |
|------|-----|------|------|-------|-------|
{{- range .NetworkInterfaces}}
| {{.Name | escapePipe}} | {{.MAC | escapePipe}} | {{joinStrings .IPv4 ", " | escapePipe}} | {{joinStrings .IPv6 ", " | escapePipe}} | {{.State | escapePipe}} | {{formatSpeed .SpeedMbps}} |
{{- end}}
{{if .DefaultGateway}}
**Default Gateway:** {{.DefaultGateway | escapePipe}}
{{- end}}
{{if .DNSServers}}
**DNS Servers:** {{joinStrings .DNSServers ", " | escapePipe}}
{{- end}}
{{if .FQDN}}
**FQDN:** {{.FQDN | escapePipe}}
{{- end}}
{{- end}}
{{- if .HasUSB}}

## USB Devices

| Vendor | Product | Description | VID:PID | Serial | Class | Speed |
|--------|---------|-------------|---------|--------|-------|-------|
{{- range .USBDevices}}
| {{.Vendor | escapePipe}} | {{.Product | escapePipe}} | {{.Description | escapePipe}} | {{.VendorID | escapePipe}}:{{.ProductID | escapePipe}} | {{.Serial | escapePipe}} | {{.DeviceClass | escapePipe}} | {{printf "%.0f Mbps" .SpeedMbps}} |
{{- end}}
{{- end}}
{{- if .HasDocker}}

## Docker

| Field | Value |
|-------|-------|
| Version | {{.DockerVersion | escapePipe}} |
| Total Containers | {{.TotalContainers}} |
| Running | {{.Running}} |
| Stopped | {{.Stopped}} |
| Paused | {{.Paused}} |
| Storage Driver | {{.StorageDriver | escapePipe}} |
{{if .Containers}}
### Containers

| Name | Image | State | Status |
|------|-------|-------|--------|
{{- range .Containers}}
| {{.Name | escapePipe}} | {{.Image | escapePipe}} | {{.State | escapePipe}} | {{.Status | escapePipe}} |
{{- end}}
{{- end}}
{{- end}}
{{- if .HasUPS}}

## UPS

| Field | Value |
|-------|-------|
| Name | {{.UPSName | escapePipe}} |
| Model | {{.UPSModel | escapePipe}} |
| Manufacturer | {{.UPSManufacturer | escapePipe}} |
| Status | {{.UPSStatus | escapePipe}} |
| Battery Charge | {{.UPSBatCharge | escapePipe}} |
| Load | {{.UPSLoad | escapePipe}} |
{{- end}}
`
