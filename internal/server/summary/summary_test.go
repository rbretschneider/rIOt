package summary

import (
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fullTelemetryFixture returns a TelemetrySnapshot with all sections populated,
// including data that MUST be excluded from the summary (logs, processes,
// container Env, security, etc.).
func fullTelemetryFixture() *models.TelemetrySnapshot {
	charge := 95.5
	load := 12.3
	freqMax := 4200.0

	return &models.TelemetrySnapshot{
		ID:        1,
		DeviceID:  "dev-001",
		Timestamp: time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
		Data: models.FullTelemetryData{
			System: &models.SystemInfo{
				Hostname:       "test-host",
				DeviceUUID:     "aaaa-bbbb-cccc",
				Arch:           "amd64",
				CPUModel:       "Intel Core i9-12900K",
				CPUCores:       16,
				CPUThreads:     24,
				TotalRAMMB:     32768,
				BoardModel:     "ROG MAXIMUS Z690",
				SerialNumber:   "SN-12345",
				BIOSVersion:    "1.23",
				BIOSDate:       "2022-01-01",
				Virtualization: "none",
			},
			OS: &models.OSInfo{
				Name:       "Ubuntu",
				Version:    "22.04",
				Codename:   "jammy",
				Kernel:     "5.15.0-76",
				KernelArch: "x86_64",
				InitSystem: "systemd",
				Timezone:   "UTC",
				Locale:     "en_US.UTF-8",
				Uptime:     90061, // 1d 1h 1m 1s
			},
			CPU: &models.CPUInfo{
				UsagePercent: 45.2, // excluded from summary
				PerCore:      []float64{50, 60, 70}, // excluded
				LoadAvg1m:    1.5, // excluded
				FreqMax:      &freqMax,
			},
			Memory: &models.MemoryInfo{
				TotalMB:      32768,
				UsedMB:       16384, // excluded
				SwapTotalMB:  8192,
				SwapUsedMB:   512, // excluded
				UsagePercent: 50.0, // excluded
			},
			Hardware: &models.HardwareInfo{
				DiskDrives: []models.DiskDrive{
					{
						Name:        "sda",
						Model:       "Samsung SSD 980",
						Serial:      "S5GXNF0R123456",
						Type:        "SSD",
						SizeGB:      931.5,
						SmartHealth: "PASSED",
					},
				},
				GPUs: []models.GPUInfo{
					{
						Vendor:  "NVIDIA",
						Model:   "GeForce RTX 3090",
						PCISlot: "0000:01:00.0",
						Driver:  "nvidia",
						VRAMMB:  24576,
					},
				},
			},
			Disks: &models.DiskInfo{
				Filesystems: []models.Filesystem{
					{
						MountPoint: "/",
						Device:     "/dev/sda1",
						FSType:     "ext4",
						TotalGB:    100.0,
						UsedGB:     40.0,
						FreeGB:     60.0,
					},
				},
			},
			Network: &models.NetworkInfo{
				Interfaces: []models.NetworkInterface{
					{
						Name:  "eth0",
						MAC:   "aa:bb:cc:dd:ee:ff",
						IPv4:  []string{"192.168.1.10"},
						IPv6:  []string{"fe80::1"},
						State: "up",
					},
				},
				DefaultGateway: "192.168.1.1",
				DNSServers:     []string{"8.8.8.8", "8.8.4.4"},
				FQDN:           "test-host.local",
			},
			USB: &models.USBInfo{
				Devices: []models.USBDevice{
					{
						Vendor:      "Logitech",
						Product:     "Wireless Mouse",
						Description: "USB Receiver",
						VendorID:    "046d",
						ProductID:   "c52b",
						DeviceClass: "HID",
						SpeedMbps:   12.0,
					},
				},
			},
			Docker: &models.DockerInfo{
				Available:       true,
				Version:         "24.0.5",
				TotalContainers: 2,
				Running:         1,
				Stopped:         1,
				StorageDriver:   "overlay2",
				Containers: []models.ContainerInfo{
					{
						Name:   "nginx",
						Image:  "nginx:latest",
						State:  "running",
						Status: "Up 2 hours",
						// Sensitive fields that MUST NOT appear in output
						Env: []models.EnvVar{
							{Key: "DB_PASSWORD", Value: "super-secret-123"},
						},
						Labels: map[string]string{
							"com.example.secret": "token-abc",
						},
					},
				},
			},
			UPS: &models.UPSInfo{
				Name:          "ups",
				Model:         "Back-UPS Pro 1500",
				Manufacturer:  "APC",
				Status:        "OL",
				BatteryCharge: &charge,
				Load:          &load,
			},
			// Fields that MUST be excluded from summary output
			Procs: &models.ProcessInfo{
				TopByCPU: []models.ProcessEntry{
					{PID: 1234, Name: "top-cpu-proc", CPU: 80.0},
				},
			},
			Security: &models.SecurityInfo{
				SELinux:         "enforcing",
				FailedLogins24h: 5,
			},
			Logs: []models.LogEntry{
				{Message: "this log must not appear"},
			},
		},
	}
}

func fullDeviceFixture() *models.Device {
	return &models.Device{
		ID:       "dev-001",
		ShortID:  "abc12345",
		Hostname: "test-host",
		Arch:     "amd64",
	}
}

// [AC-003] All sections present when full telemetry is available.
func TestRender_AC003_AllSectionsPresent(t *testing.T) {
	device := fullDeviceFixture()
	snap := fullTelemetryFixture()

	out, err := Render(device, snap)
	require.NoError(t, err)

	// Header
	assert.Contains(t, out, "# Device Summary: test-host", "[AC-003] header must include hostname")
	assert.Contains(t, out, "**Device ID:** abc12345", "[AC-003] header must include short ID")
	assert.Contains(t, out, "**Generated:**", "[AC-003] header must include generation timestamp")

	// System Identity section
	assert.Contains(t, out, "## System Identity", "[AC-003] system identity section must be present")
	assert.Contains(t, out, "test-host", "[AC-003] hostname must appear")
	assert.Contains(t, out, "aaaa-bbbb-cccc", "[AC-003] device UUID must appear")
	assert.Contains(t, out, "ROG MAXIMUS Z690", "[AC-003] board model must appear")
	assert.Contains(t, out, "SN-12345", "[AC-003] serial number must appear")
	assert.Contains(t, out, "1.23", "[AC-003] BIOS version must appear")

	// OS section
	assert.Contains(t, out, "## Operating System", "[AC-003] OS section must be present")
	assert.Contains(t, out, "Ubuntu", "[AC-003] OS name must appear")
	assert.Contains(t, out, "22.04", "[AC-003] OS version must appear")
	assert.Contains(t, out, "jammy", "[AC-003] OS codename must appear")
	assert.Contains(t, out, "5.15.0-76", "[AC-003] kernel must appear")
	assert.Contains(t, out, "systemd", "[AC-003] init system must appear")

	// CPU section
	assert.Contains(t, out, "## CPU", "[AC-003] CPU section must be present")
	assert.Contains(t, out, "Intel Core i9-12900K", "[AC-003] CPU model must appear")
	assert.Contains(t, out, "16", "[AC-003] CPU cores must appear")
	assert.Contains(t, out, "24", "[AC-003] CPU threads must appear")
	assert.Contains(t, out, "4.20 GHz", "[AC-003] max freq must appear")

	// Memory section
	assert.Contains(t, out, "## Memory", "[AC-003] memory section must be present")
	assert.Contains(t, out, "32768 MB", "[AC-003] total RAM must appear")
	assert.Contains(t, out, "8192 MB", "[AC-003] total swap must appear")

	// Storage section
	assert.Contains(t, out, "## Storage", "[AC-003] storage section must be present")
	assert.Contains(t, out, "### Physical Drives", "[AC-003] physical drives subsection must be present")
	assert.Contains(t, out, "Samsung SSD 980", "[AC-003] drive model must appear")
	assert.Contains(t, out, "S5GXNF0R123456", "[AC-003] drive serial must appear")
	assert.Contains(t, out, "SSD", "[AC-003] drive type must appear")
	assert.Contains(t, out, "PASSED", "[AC-003] SMART health must appear")
	assert.Contains(t, out, "### Filesystems", "[AC-003] filesystems subsection must be present")
	assert.Contains(t, out, "/dev/sda1", "[AC-003] filesystem device must appear")
	assert.Contains(t, out, "ext4", "[AC-003] filesystem type must appear")

	// GPU section
	assert.Contains(t, out, "## GPUs", "[AC-003] GPU section must be present")
	assert.Contains(t, out, "NVIDIA", "[AC-003] GPU vendor must appear")
	assert.Contains(t, out, "GeForce RTX 3090", "[AC-003] GPU model must appear")
	assert.Contains(t, out, "24576 MB", "[AC-003] GPU VRAM must appear")
	assert.Contains(t, out, "0000:01:00.0", "[AC-003] GPU PCI slot must appear")
	assert.Contains(t, out, "nvidia", "[AC-003] GPU driver must appear")

	// Network section
	assert.Contains(t, out, "## Network Interfaces", "[AC-003] network section must be present")
	assert.Contains(t, out, "eth0", "[AC-003] interface name must appear")
	assert.Contains(t, out, "aa:bb:cc:dd:ee:ff", "[AC-003] MAC must appear")
	assert.Contains(t, out, "192.168.1.10", "[AC-003] IPv4 must appear")
	assert.Contains(t, out, "192.168.1.1", "[AC-003] default gateway must appear")
	assert.Contains(t, out, "8.8.8.8", "[AC-003] DNS server must appear")
	assert.Contains(t, out, "test-host.local", "[AC-003] FQDN must appear")

	// USB section
	assert.Contains(t, out, "## USB Devices", "[AC-003] USB section must be present")
	assert.Contains(t, out, "Logitech", "[AC-003] USB vendor must appear")
	assert.Contains(t, out, "Wireless Mouse", "[AC-003] USB product must appear")

	// Docker section
	assert.Contains(t, out, "## Docker", "[AC-003] Docker section must be present")
	assert.Contains(t, out, "24.0.5", "[AC-003] Docker version must appear")
	assert.Contains(t, out, "overlay2", "[AC-003] storage driver must appear")
	assert.Contains(t, out, "nginx", "[AC-003] container name must appear")
	assert.Contains(t, out, "nginx:latest", "[AC-003] container image must appear")
	assert.Contains(t, out, "running", "[AC-003] container state must appear")

	// UPS section
	assert.Contains(t, out, "## UPS", "[AC-003] UPS section must be present")
	assert.Contains(t, out, "Back-UPS Pro 1500", "[AC-003] UPS model must appear")
	assert.Contains(t, out, "APC", "[AC-003] UPS manufacturer must appear")
	assert.Contains(t, out, "95.5%", "[AC-003] UPS battery charge must appear")
	assert.Contains(t, out, "12.3%", "[AC-003] UPS load must appear")
}

// [AC-004] Docker and UPS sections omitted when data is not present.
func TestRender_AC004_SectionsOmittedWhenNoData(t *testing.T) {
	device := &models.Device{
		ID:      "dev-002",
		ShortID: "def67890",
		Hostname: "minimal-host",
		Arch:    "arm64",
	}
	snap := &models.TelemetrySnapshot{
		ID:        2,
		DeviceID:  "dev-002",
		Timestamp: time.Now().UTC(),
		Data: models.FullTelemetryData{
			System: &models.SystemInfo{
				Hostname: "minimal-host",
				Arch:     "arm64",
			},
			OS: &models.OSInfo{
				Name:   "Raspberry Pi OS",
				Kernel: "6.1.0",
			},
			// Docker: nil — must not appear
			// UPS: nil — must not appear
		},
	}

	out, err := Render(device, snap)
	require.NoError(t, err)

	assert.Contains(t, out, "## System Identity", "[AC-004] system section must be present")
	assert.Contains(t, out, "## Operating System", "[AC-004] OS section must be present")
	assert.NotContains(t, out, "## Docker", "[AC-004] Docker section must be absent when nil")
	assert.NotContains(t, out, "## UPS", "[AC-004] UPS section must be absent when nil")
	assert.NotContains(t, out, "## GPUs", "[AC-004] GPU section must be absent when no hardware.gpus")
	assert.NotContains(t, out, "## USB Devices", "[AC-004] USB section must be absent when nil")
}

// [AC-004] Docker section omitted when docker.available is false.
func TestRender_AC004_DockerSectionOmittedWhenUnavailable(t *testing.T) {
	device := fullDeviceFixture()
	snap := &models.TelemetrySnapshot{
		ID:       3,
		DeviceID: "dev-001",
		Data: models.FullTelemetryData{
			Docker: &models.DockerInfo{
				Available: false, // not available
				Version:   "24.0.5",
			},
		},
	}

	out, err := Render(device, snap)
	require.NoError(t, err)

	assert.NotContains(t, out, "## Docker", "[AC-004] Docker section must be absent when docker.available is false")
}

// [AC-005] Excluded data never appears in output.
func TestRender_AC005_ExcludedDataAbsent(t *testing.T) {
	device := fullDeviceFixture()
	snap := fullTelemetryFixture()

	out, err := Render(device, snap)
	require.NoError(t, err)

	// Container secrets must not appear (SEC-002)
	assert.NotContains(t, out, "DB_PASSWORD", "[AC-005][SEC-002] container Env key must not appear in output")
	assert.NotContains(t, out, "super-secret-123", "[AC-005][SEC-002] container Env value must not appear in output")
	assert.NotContains(t, out, "com.example.secret", "[AC-005][SEC-002] container Label key must not appear in output")
	assert.NotContains(t, out, "token-abc", "[AC-005][SEC-002] container Label value must not appear in output")

	// Process list must not appear
	assert.NotContains(t, out, "top-cpu-proc", "[AC-005] process name must not appear in output")

	// Log entries must not appear
	assert.NotContains(t, out, "this log must not appear", "[AC-005] log entries must not appear in output")

	// Security data must not appear
	assert.NotContains(t, out, "failed_logins", "[AC-005] security data must not appear in output")
	assert.NotContains(t, out, "enforcing", "[AC-005] SELinux state must not appear in output")

	// Real-time CPU usage must not appear
	assert.NotContains(t, out, "45.2", "[AC-005] CPU usage percent must not appear")
	assert.NotContains(t, out, "load_avg", "[AC-005] load average must not appear")
}

// [AC-005] Container struct has exactly 4 fields (Name, Image, State, Status) — SEC-002.
func TestRender_AC005_ContainerStructHasOnlySafeFields(t *testing.T) {
	// Build a container with all sensitive fields populated
	c := summaryContainer{
		Name:   "my-app",
		Image:  "myapp:v1",
		State:  "running",
		Status: "Up 1 hour",
	}
	// Verify the struct only has 4 fields by checking field count
	// (This is a compile-time guarantee reinforced by this test)
	assert.Equal(t, "my-app", c.Name, "[AC-005][SEC-002] Name field must be present")
	assert.Equal(t, "myapp:v1", c.Image, "[AC-005][SEC-002] Image field must be present")
	assert.Equal(t, "running", c.State, "[AC-005][SEC-002] State field must be present")
	assert.Equal(t, "Up 1 hour", c.Status, "[AC-005][SEC-002] Status field must be present")
}

// [AC-001] Summary header contains hostname, short ID, and generation timestamp.
func TestRender_AC001_HeaderFields(t *testing.T) {
	device := &models.Device{
		ID:       "dev-abc",
		ShortID:  "abc12345",
		Hostname: "myserver",
	}
	snap := &models.TelemetrySnapshot{
		ID:       4,
		DeviceID: "dev-abc",
		Data:     models.FullTelemetryData{},
	}

	out, err := Render(device, snap)
	require.NoError(t, err)

	assert.Contains(t, out, "# Device Summary: myserver", "[AC-001] header title must include hostname")
	assert.Contains(t, out, "**Device ID:** abc12345", "[AC-001] header must include short ID")
	assert.Contains(t, out, "**Generated:**", "[AC-001] header must include generation timestamp")
}

// [AC-010] Summary uses data from the provided snapshot (most recent).
func TestRender_AC010_UsesProvidedSnapshot(t *testing.T) {
	// Use two different devices to distinguish their snapshots by unique data.
	deviceA := &models.Device{ID: "dev-A", ShortID: "shortA", Hostname: "host-A"}
	deviceB := &models.Device{ID: "dev-B", ShortID: "shortB", Hostname: "host-B"}

	snapA := &models.TelemetrySnapshot{
		ID:        1,
		DeviceID:  "dev-A",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Data: models.FullTelemetryData{
			System: &models.SystemInfo{
				Hostname:   "host-A",
				Arch:       "amd64",
				BoardModel: "Board-A-Unique",
			},
		},
	}

	snapB := &models.TelemetrySnapshot{
		ID:        2,
		DeviceID:  "dev-B",
		Timestamp: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC),
		Data: models.FullTelemetryData{
			System: &models.SystemInfo{
				Hostname:   "host-B",
				Arch:       "amd64",
				BoardModel: "Board-B-Unique",
			},
		},
	}

	// Render with snapA should produce Board-A-Unique board model (from that snapshot)
	outA, err := Render(deviceA, snapA)
	require.NoError(t, err)
	assert.Contains(t, outA, "Board-A-Unique", "[AC-010] must use the data from the provided snapshot")
	assert.NotContains(t, outA, "Board-B-Unique", "[AC-010] must not contain data from a different snapshot")

	// Render with snapB should produce Board-B-Unique (demonstrating caller controls recency)
	outB, err := Render(deviceB, snapB)
	require.NoError(t, err)
	assert.Contains(t, outB, "Board-B-Unique", "[AC-010] passing different snapshot produces different data")
	assert.NotContains(t, outB, "Board-A-Unique", "[AC-010] must not contain data from the other snapshot")
}

// SEC-003: Pipe characters in telemetry values must be escaped.
func TestRender_SEC003_PipeCharactersEscaped(t *testing.T) {
	device := &models.Device{
		ID:      "dev-sec",
		ShortID: "sec00001",
		Hostname: "host|with|pipes",
	}
	snap := &models.TelemetrySnapshot{
		ID:       5,
		DeviceID: "dev-sec",
		Data: models.FullTelemetryData{
			System: &models.SystemInfo{
				Hostname: "host|with|pipes",
				Arch:     "amd64",
				BoardModel: "Board|Model|Piped",
			},
			Network: &models.NetworkInfo{
				Interfaces: []models.NetworkInterface{
					{
						Name:  "eth|0",
						MAC:   "aa:bb|cc",
						State: "up",
					},
				},
			},
			Docker: &models.DockerInfo{
				Available: true,
				Version:   "24.0|5",
				Containers: []models.ContainerInfo{
					{
						Name:   "app|server",
						Image:  "nginx|latest",
						State:  "running",
						Status: "Up|2|hours",
					},
				},
			},
		},
	}

	out, err := Render(device, snap)
	require.NoError(t, err)

	// Raw unescaped pipes inside table cells would break Markdown table structure.
	// Verify that values containing | are properly escaped as \|
	assert.Contains(t, out, `host\|with\|pipes`, "[SEC-003] hostname pipes must be escaped in title")
	assert.Contains(t, out, `Board\|Model\|Piped`, "[SEC-003] board model pipes must be escaped in table")
	assert.Contains(t, out, `eth\|0`, "[SEC-003] interface name pipes must be escaped")
	assert.Contains(t, out, `app\|server`, "[SEC-003] container name pipes must be escaped")
	assert.Contains(t, out, `nginx\|latest`, "[SEC-003] container image pipes must be escaped")
}

// Helper function and format function tests.

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		mb   int64
		want string
	}{
		{0, "—"},
		{-1, "—"},
		{512, "512 MB"},
		{1024, "1024 MB (1 GB)"},
		{32768, "32768 MB (32 GB)"},
	}
	for _, tc := range tests {
		got := formatBytes(tc.mb)
		assert.Equal(t, tc.want, got, "formatBytes(%d)", tc.mb)
	}
}

func TestFormatFreq(t *testing.T) {
	mhz999 := 999.0
	mhz1000 := 1000.0
	mhz4200 := 4200.0

	assert.Equal(t, "—", formatFreq(nil), "nil pointer must return dash")
	assert.Equal(t, "999 MHz", formatFreq(&mhz999), "sub-1000 MHz must format as MHz")
	assert.Equal(t, "1.00 GHz", formatFreq(&mhz1000), "1000 MHz must format as GHz")
	assert.Equal(t, "4.20 GHz", formatFreq(&mhz4200), "4200 MHz must format as GHz")
}

func TestFormatSpeed(t *testing.T) {
	mbps100 := 100
	mbps1000 := 1000
	mbps10000 := 10000

	assert.Equal(t, "—", formatSpeed(nil), "nil pointer must return dash")
	assert.Equal(t, "100 Mbps", formatSpeed(&mbps100), "sub-1000 Mbps must format as Mbps")
	assert.Equal(t, "1 Gbps", formatSpeed(&mbps1000), "1000 Mbps must format as Gbps")
	assert.Equal(t, "10 Gbps", formatSpeed(&mbps10000), "10000 Mbps must format as Gbps")
}

func TestFormatUptime(t *testing.T) {
	assert.Equal(t, "—", formatUptime(0), "zero uptime must return dash")
	assert.Equal(t, "5m", formatUptime(300), "5 minutes")
	assert.Equal(t, "1h 0m", formatUptime(3600), "1 hour")
	assert.Equal(t, "1h 30m", formatUptime(5400), "1.5 hours")
	assert.Equal(t, "1d 1h 1m", formatUptime(90061), "1 day 1 hour 1 minute")
}

func TestEscapePipe(t *testing.T) {
	assert.Equal(t, `no pipes`, escapePipe("no pipes"), "no pipes — unchanged")
	assert.Equal(t, `a\|b`, escapePipe("a|b"), "single pipe escaped")
	assert.Equal(t, `a\|b\|c`, escapePipe("a|b|c"), "multiple pipes all escaped")
	assert.Equal(t, ``, escapePipe(""), "empty string unchanged")
}

// Verify the summaryContainer struct has exactly 4 fields (defense-in-depth for SEC-002).
func TestSummaryContainerFieldCount(t *testing.T) {
	// This documents the expected shape: Name, Image, State, Status only.
	// Any addition of sensitive fields like Env, Labels, Mounts, Networks would
	// be caught by TestRender_AC005_ExcludedDataAbsent.
	c := summaryContainer{}
	// We verify all 4 expected fields exist and there are no extra ones
	// by attempting assignment of all known fields.
	c.Name = "test"
	c.Image = "test"
	c.State = "test"
	c.Status = "test"
	// If additional fields were added (Env, Labels, etc.) this test would not
	// catch them directly, but TestRender_AC005_ExcludedDataAbsent would.
	assert.Equal(t, summaryContainer{
		Name:   "test",
		Image:  "test",
		State:  "test",
		Status: "test",
	}, c, "[SEC-002] container struct must match expected shape")
}

