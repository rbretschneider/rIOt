package collectors

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DefaultSMARTInterval is how often SMART scans run. SMART reads are harmless
// but there's no reason to pound drives every 60 s — every 4 h is plenty.
const DefaultSMARTInterval = 4 * time.Hour

// HardwareCollector gathers PCI devices, disk drive details, serial ports,
// and GPU information from sysfs. Linux-only.
type HardwareCollector struct {
	smartInterval time.Duration

	mu              sync.Mutex
	lastSMART       time.Time
	cachedDiskDrives []models.DiskDrive
}

func (c *HardwareCollector) Name() string { return "hardware" }

func (c *HardwareCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.HardwareInfo{}
	if runtime.GOOS != "linux" {
		return info, nil
	}

	info.PCIDevices = collectPCIDevices()
	info.DiskDrives = c.collectDiskDrivesThrottled()
	info.SerialPorts = collectSerialPorts()
	info.GPUs = collectGPUs(info.PCIDevices)

	return info, nil
}

// collectDiskDrivesThrottled returns disk drives with SMART data, but only
// re-runs smartctl when the SMART interval has elapsed. In between, it reuses
// cached SMART fields overlaid onto fresh sysfs data.
func (c *HardwareCollector) collectDiskDrivesThrottled() []models.DiskDrive {
	drives := collectDiskDrives()

	c.mu.Lock()
	defer c.mu.Unlock()

	interval := c.smartInterval
	if interval == 0 {
		interval = DefaultSMARTInterval
	}

	if time.Since(c.lastSMART) >= interval || c.cachedDiskDrives == nil {
		enrichDrivesSMART(drives)
		c.lastSMART = time.Now()
		c.cachedDiskDrives = drives
	} else {
		// Overlay cached SMART data onto fresh sysfs drives
		cached := make(map[string]*models.DiskDrive, len(c.cachedDiskDrives))
		for i := range c.cachedDiskDrives {
			cached[c.cachedDiskDrives[i].Name] = &c.cachedDiskDrives[i]
		}
		for i := range drives {
			if prev, ok := cached[drives[i].Name]; ok && prev.SmartAvailable {
				drives[i].SmartAvailable = prev.SmartAvailable
				drives[i].SmartHealth = prev.SmartHealth
				drives[i].SmartTemp = prev.SmartTemp
				drives[i].SmartPowerOnHours = prev.SmartPowerOnHours
				drives[i].SmartReallocated = prev.SmartReallocated
				drives[i].SmartPendingSector = prev.SmartPendingSector
			}
		}
	}

	return drives
}

// --- PCI Devices ---

func collectPCIDevices() []models.PCIDevice {
	basePath := "/sys/bus/pci/devices"
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil
	}

	db := getPCIIDDB()
	var devices []models.PCIDevice

	for _, entry := range entries {
		devPath := filepath.Join(basePath, entry.Name())

		vendorID := readSysfsFile(devPath, "vendor")   // e.g. "0x8086"
		deviceID := readSysfsFile(devPath, "device")    // e.g. "0x1533"
		classCode := readSysfsFile(devPath, "class")    // e.g. "0x020000"
		driver := readSysfsLink(devPath, "driver")
		subsysVendor := readSysfsFile(devPath, "subsystem_vendor")
		subsysDevice := readSysfsFile(devPath, "subsystem_device")

		// Strip "0x" prefix
		vendorID = strings.TrimPrefix(vendorID, "0x")
		deviceID = strings.TrimPrefix(deviceID, "0x")
		classCode = strings.TrimPrefix(classCode, "0x")
		subsysVendor = strings.TrimPrefix(subsysVendor, "0x")
		subsysDevice = strings.TrimPrefix(subsysDevice, "0x")

		if vendorID == "" || deviceID == "" {
			continue
		}

		dev := models.PCIDevice{
			Slot:           entry.Name(),
			VendorID:       vendorID,
			DeviceID:       deviceID,
			ClassCode:      classCode,
			Driver:         driver,
			SubsysVendorID: subsysVendor,
			SubsysDeviceID: subsysDevice,
		}

		// Look up names from pci.ids database
		if db != nil {
			if name, ok := db.vendors[vendorID]; ok {
				dev.Vendor = name
			}
			if name, ok := db.products[vendorID+":"+deviceID]; ok {
				dev.Device = name
			}
		}

		// Classify by PCI class code
		if len(classCode) >= 4 {
			dev.ClassName = pciClassName(classCode[:4])
		}

		// Build description
		if dev.Vendor != "" && dev.Device != "" {
			dev.Description = dev.Vendor + " " + dev.Device
		} else if dev.Device != "" {
			dev.Description = dev.Device
		} else if dev.Vendor != "" {
			dev.Description = dev.Vendor + " (" + vendorID + ":" + deviceID + ")"
		} else {
			dev.Description = vendorID + ":" + deviceID
		}

		// Read NUMA node if available
		if numa := readSysfsFile(devPath, "numa_node"); numa != "" && numa != "-1" {
			dev.NUMANode = numa
		}

		// Read IRQ
		dev.IRQ = readSysfsFile(devPath, "irq")

		devices = append(devices, dev)
	}

	return devices
}

// readSysfsLink reads a symlink basename (e.g. driver name).
func readSysfsLink(dir, name string) string {
	target, err := os.Readlink(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// pciClassName maps the first 4 hex digits of PCI class to a human-readable category.
func pciClassName(classPrefix string) string {
	switch strings.ToLower(classPrefix) {
	case "0000":
		return "Unclassified"
	case "0100":
		return "SCSI Controller"
	case "0101":
		return "IDE Controller"
	case "0102":
		return "Floppy Controller"
	case "0104":
		return "RAID Controller"
	case "0105":
		return "ATA Controller"
	case "0106":
		return "SATA Controller"
	case "0107":
		return "SAS Controller"
	case "0108":
		return "NVMe Controller"
	case "0200":
		return "Ethernet Controller"
	case "0280":
		return "Network Controller"
	case "0300":
		return "VGA Controller"
	case "0302":
		return "3D Controller"
	case "0380":
		return "Display Controller"
	case "0400":
		return "Video Device"
	case "0401":
		return "Audio Device"
	case "0403":
		return "Audio Device"
	case "0480":
		return "Multimedia Controller"
	case "0500":
		return "RAM Controller"
	case "0501":
		return "Flash Controller"
	case "0600":
		return "Host Bridge"
	case "0601":
		return "ISA Bridge"
	case "0604":
		return "PCI Bridge"
	case "0680":
		return "Bridge Device"
	case "0700":
		return "Serial Controller"
	case "0800":
		return "System Peripheral"
	case "0801":
		return "DMA Controller"
	case "0803":
		return "System Timer"
	case "0805":
		return "SD Host Controller"
	case "0880":
		return "System Peripheral"
	case "0c00":
		return "FireWire Controller"
	case "0c03":
		return "USB Controller"
	case "0c04":
		return "Fibre Channel"
	case "0c05":
		return "SMBus Controller"
	case "0d00":
		return "IRDA Controller"
	case "0d11":
		return "Bluetooth Controller"
	case "1000":
		return "Network Encryption"
	case "1080":
		return "Encryption Controller"
	case "1100":
		return "Signal Processing"
	case "1101":
		return "DPIO Module"
	case "ff00":
		return "Unassigned Class"
	default:
		// Try matching just class (first 2 chars)
		switch strings.ToLower(classPrefix[:2]) {
		case "01":
			return "Storage Controller"
		case "02":
			return "Network Controller"
		case "03":
			return "Display Controller"
		case "04":
			return "Multimedia Controller"
		case "05":
			return "Memory Controller"
		case "06":
			return "Bridge"
		case "07":
			return "Communication Controller"
		case "08":
			return "System Peripheral"
		case "09":
			return "Input Device"
		case "0a":
			return "Docking Station"
		case "0b":
			return "Processor"
		case "0c":
			return "Serial Bus Controller"
		case "0d":
			return "Wireless Controller"
		case "0e":
			return "Intelligent Controller"
		case "0f":
			return "Satellite Controller"
		case "10":
			return "Encryption Controller"
		case "11":
			return "Signal Processing"
		case "12":
			return "Processing Accelerator"
		case "13":
			return "Non-Essential Instrumentation"
		default:
			return classPrefix
		}
	}
}

// --- Disk Drives ---

func collectDiskDrives() []models.DiskDrive {
	basePath := "/sys/block"
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil
	}

	var drives []models.DiskDrive

	for _, entry := range entries {
		name := entry.Name()
		// Skip virtual devices (loop, dm-, ram, nbd)
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "dm-") ||
			strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "nbd") ||
			strings.HasPrefix(name, "zram") {
			continue
		}

		devPath := filepath.Join(basePath, name)
		devicePath := filepath.Join(devPath, "device")

		// Check if device subdirectory exists (physical devices have it)
		if _, err := os.Stat(devicePath); err != nil {
			continue
		}

		drive := models.DiskDrive{
			Name:   name,
			Model:  readSysfsFile(devicePath, "model"),
			Serial: readSysfsFile(devicePath, "serial"),
			Rev:    readSysfsFile(devicePath, "rev"),
		}

		// Read size in 512-byte sectors
		if sizeStr := readSysfsFile(devPath, "size"); sizeStr != "" {
			if sectors, err := strconv.ParseUint(sizeStr, 10, 64); err == nil {
				drive.SizeBytes = int64(sectors * 512)
				drive.SizeGB = float64(sectors*512) / 1024 / 1024 / 1024
			}
		}

		// Determine drive type (SSD vs HDD vs NVMe)
		if strings.HasPrefix(name, "nvme") {
			drive.Type = "NVMe"
			// NVMe devices have model in a different location
			if drive.Model == "" {
				drive.Model = readSysfsFile(devPath, "device/model")
			}
			if drive.Serial == "" {
				drive.Serial = readSysfsFile(devPath, "device/serial")
			}
			drive.Rev = readSysfsFile(devPath, "device/firmware_rev")
		} else if strings.HasPrefix(name, "mmcblk") {
			drive.Type = "SD/eMMC"
		} else {
			// Check rotational flag (0 = SSD, 1 = HDD)
			if rot := readSysfsFile(filepath.Join(devPath, "queue"), "rotational"); rot == "0" {
				drive.Type = "SSD"
			} else if rot == "1" {
				drive.Type = "HDD"
			}
		}

		// Read transport type if available
		drive.Transport = readSysfsFile(devPath, "device/transport")

		// Read removable flag
		if rem := readSysfsFile(devPath, "removable"); rem == "1" {
			drive.Removable = true
		}

		// Read scheduler
		drive.Scheduler = readSysfsFile(filepath.Join(devPath, "queue"), "scheduler")
		// Extract active scheduler from bracket format: "none [mq-deadline] bfq"
		if idx := strings.Index(drive.Scheduler, "["); idx >= 0 {
			if end := strings.Index(drive.Scheduler[idx:], "]"); end >= 0 {
				drive.Scheduler = drive.Scheduler[idx+1 : idx+end]
			}
		}

		drives = append(drives, drive)
	}

	// Enrich drives with SMART data if smartctl is available
	enrichDrivesSMART(drives)

	return drives
}

// smartctlJSON is the subset of smartctl --json output we care about.
type smartctlJSON struct {
	SmartStatus struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature struct {
		Current int `json:"current"`
	} `json:"temperature"`
	ATASmartAttributes struct {
		Table []struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Value int    `json:"value"`
			Raw   struct {
				Value int64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	PowerOnTime struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	NVMeSmartHealthLog struct {
		Temperature     int   `json:"temperature"`
		PowerOnHours    int64 `json:"power_on_hours"`
		MediaErrors     int64 `json:"media_errors"`
		CriticalWarning int   `json:"critical_warning"`
	} `json:"nvme_smart_health_information_log"`
	Smartctl struct {
		ExitStatus int `json:"exit_status"`
	} `json:"smartctl"`
}

// enrichDrivesSMART runs smartctl for each drive and populates SMART fields.
func enrichDrivesSMART(drives []models.DiskDrive) {
	// Check if smartctl is available
	smartctlPath, err := exec.LookPath("smartctl")
	if err != nil {
		// Try with sudo — agent may need privilege escalation
		if _, err := exec.LookPath("sudo"); err != nil {
			return
		}
		smartctlPath = "smartctl"
	}

	for i := range drives {
		d := &drives[i]
		devPath := "/dev/" + d.Name

		// Run smartctl with JSON output; use sudo since drives typically need root
		out, err := exec.Command("sudo", "-n", smartctlPath, "--json=c", "--all", devPath).Output()
		if err != nil {
			// smartctl returns non-zero for various reasons (drive doesn't support SMART, etc.)
			// but still outputs valid JSON. If we got output, try to parse it.
			if len(out) == 0 {
				continue
			}
		}

		var result smartctlJSON
		if err := json.Unmarshal(out, &result); err != nil {
			continue
		}

		d.SmartAvailable = true

		// Health status — smartctl exit bit 3 means SMART status failed
		if result.Smartctl.ExitStatus&(1<<3) != 0 {
			d.SmartHealth = "FAILED"
		} else {
			if result.SmartStatus.Passed {
				d.SmartHealth = "PASSED"
			} else {
				// If we got data but passed is false and bit 3 isn't set,
				// it might just be unavailable
				d.SmartHealth = "UNKNOWN"
			}
		}

		// Temperature — try NVMe log first, then generic
		if result.NVMeSmartHealthLog.Temperature > 0 {
			temp := result.NVMeSmartHealthLog.Temperature
			d.SmartTemp = &temp
		} else if result.Temperature.Current > 0 {
			temp := result.Temperature.Current
			d.SmartTemp = &temp
		}

		// Power-on hours — try NVMe log first, then generic
		if result.NVMeSmartHealthLog.PowerOnHours > 0 {
			h := result.NVMeSmartHealthLog.PowerOnHours
			d.SmartPowerOnHours = &h
		} else if result.PowerOnTime.Hours > 0 {
			h := result.PowerOnTime.Hours
			d.SmartPowerOnHours = &h
		}

		// NVMe media errors count as reallocated equivalent
		if result.NVMeSmartHealthLog.MediaErrors > 0 {
			e := result.NVMeSmartHealthLog.MediaErrors
			d.SmartReallocated = &e
		}

		// ATA SMART attributes
		for _, attr := range result.ATASmartAttributes.Table {
			switch attr.ID {
			case 5: // Reallocated Sectors Count
				v := attr.Raw.Value
				d.SmartReallocated = &v
			case 197: // Current Pending Sector Count
				v := attr.Raw.Value
				d.SmartPendingSector = &v
			}
		}
	}
}

// --- Serial Ports ---

func collectSerialPorts() []models.SerialPort {
	var ports []models.SerialPort

	// Check /sys/class/tty for serial ports
	basePath := "/sys/class/tty"
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		name := entry.Name()
		// Only include serial ports (ttyS*, ttyUSB*, ttyACM*, ttyAMA*)
		if !strings.HasPrefix(name, "ttyS") && !strings.HasPrefix(name, "ttyUSB") &&
			!strings.HasPrefix(name, "ttyACM") && !strings.HasPrefix(name, "ttyAMA") {
			continue
		}

		devPath := filepath.Join(basePath, name)

		// For ttyS* ports, check if they're real hardware (type != 0)
		if strings.HasPrefix(name, "ttyS") {
			portType := readSysfsFile(devPath, "type")
			if portType == "" || portType == "0" {
				continue // Not a real serial port
			}
		}

		port := models.SerialPort{
			Name: name,
			Path: "/dev/" + name,
		}

		// Try to get port type
		if strings.HasPrefix(name, "ttyUSB") {
			port.Type = "USB-Serial"
		} else if strings.HasPrefix(name, "ttyACM") {
			port.Type = "USB-ACM"
		} else if strings.HasPrefix(name, "ttyAMA") {
			port.Type = "ARM-UART"
		} else {
			port.Type = "UART"
		}

		// Read driver
		port.Driver = readSysfsLink(devPath, "device/driver")

		ports = append(ports, port)
	}

	return ports
}

// --- GPUs ---

func collectGPUs(pciDevices []models.PCIDevice) []models.GPUInfo {
	var gpus []models.GPUInfo

	for _, dev := range pciDevices {
		if len(dev.ClassCode) < 4 {
			continue
		}
		classPrefix := strings.ToLower(dev.ClassCode[:4])
		if classPrefix != "0300" && classPrefix != "0302" && classPrefix != "0380" {
			continue
		}

		gpu := models.GPUInfo{
			Vendor:      dev.Vendor,
			Model:       dev.Device,
			PCISlot:     dev.Slot,
			Driver:      dev.Driver,
			Description: dev.Description,
		}

		// Try reading VRAM from DRM subsystem
		drmPath := filepath.Join("/sys/bus/pci/devices", dev.Slot, "drm")
		if drmEntries, err := os.ReadDir(drmPath); err == nil {
			for _, de := range drmEntries {
				if !strings.HasPrefix(de.Name(), "card") {
					continue
				}
				cardPath := filepath.Join(drmPath, de.Name())

				// Try various VRAM info paths
				if vram := readSysfsFile(filepath.Join(cardPath, "device"), "mem_info_vram_total"); vram != "" {
					if v, err := strconv.ParseInt(vram, 10, 64); err == nil {
						gpu.VRAMMB = int(v / 1024 / 1024)
					}
				}
				break
			}
		}

		gpus = append(gpus, gpu)
	}

	return gpus
}

// --- PCI ID Database ---

type pciIDDB struct {
	vendors  map[string]string // vendorID → vendor name
	products map[string]string // "vendorID:productID" → product name
}

var (
	pciIDDBOnce     sync.Once
	pciIDDBInstance *pciIDDB
)

var pciIDPaths = []string{
	"/usr/share/hwdata/pci.ids",
	"/usr/share/misc/pci.ids",
	"/usr/share/pci.ids",
}

func getPCIIDDB() *pciIDDB {
	pciIDDBOnce.Do(func() {
		for _, path := range pciIDPaths {
			if f, err := os.Open(path); err == nil {
				if db, err := parsePCIIDReader(f); err == nil {
					pciIDDBInstance = db
				}
				f.Close()
				if pciIDDBInstance != nil {
					return
				}
			}
		}
	})
	return pciIDDBInstance
}

func parsePCIIDReader(r io.Reader) (*pciIDDB, error) {
	db := &pciIDDB{
		vendors:  make(map[string]string),
		products: make(map[string]string),
	}

	scanner := bufio.NewScanner(r)
	var currentVendor string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		// Stop at class section
		if len(line) >= 2 && line[0] == 'C' && line[1] == ' ' {
			break
		}

		if line[0] == '\t' {
			if currentVendor == "" {
				continue
			}
			line = line[1:]
			if len(line) > 0 && line[0] == '\t' {
				continue // sub-device
			}
			if len(line) < 6 {
				continue
			}
			productID := line[:4]
			productName := strings.TrimSpace(line[4:])
			if productName != "" {
				db.products[currentVendor+":"+productID] = productName
			}
		} else {
			if len(line) < 6 {
				continue
			}
			vendorID := line[:4]
			if !isHex4(vendorID) {
				currentVendor = ""
				continue
			}
			vendorName := strings.TrimSpace(line[4:])
			if vendorName != "" {
				db.vendors[vendorID] = vendorName
				currentVendor = vendorID
			}
		}
	}

	return db, nil
}
