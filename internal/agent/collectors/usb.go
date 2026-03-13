package collectors

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// USBCollector gathers USB device information from sysfs.
// Linux-only; returns empty USBInfo on other platforms.
type USBCollector struct{}

func (c *USBCollector) Name() string { return "usb" }

func (c *USBCollector) Collect(ctx context.Context) (interface{}, error) {
	info := &models.USBInfo{}
	if runtime.GOOS != "linux" {
		return info, nil
	}

	basePath := "/sys/bus/usb/devices"
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return info, nil
	}

	for _, entry := range entries {
		name := entry.Name()
		// Skip interfaces (contain ':') and usb root hubs (start with "usb")
		if strings.Contains(name, ":") || strings.HasPrefix(name, "usb") {
			continue
		}

		devPath := filepath.Join(basePath, name)

		// Must have idVendor and idProduct to be a real device
		vendorID := readSysfsFile(devPath, "idVendor")
		productID := readSysfsFile(devPath, "idProduct")
		if vendorID == "" || productID == "" {
			continue
		}

		dev := models.USBDevice{
			Bus:       readSysfsFile(devPath, "busnum"),
			Device:    readSysfsFile(devPath, "devnum"),
			VendorID:  vendorID,
			ProductID: productID,
			Vendor:    readSysfsFile(devPath, "manufacturer"),
			Product:   readSysfsFile(devPath, "product"),
			Serial:    readSysfsFile(devPath, "serial"),
			SysPath:   name,
		}

		if cls := readSysfsFile(devPath, "bDeviceClass"); cls != "" {
			dev.DeviceClass = usbClassName(cls)
		}

		if speed := readSysfsFile(devPath, "speed"); speed != "" {
			if v, err := strconv.ParseFloat(speed, 64); err == nil {
				dev.SpeedMbps = v
			}
		}

		// Build a human-readable description
		if dev.Vendor != "" && dev.Product != "" {
			dev.Description = dev.Vendor + " " + dev.Product
		} else if dev.Product != "" {
			dev.Description = dev.Product
		} else {
			dev.Description = vendorID + ":" + productID
		}

		info.Devices = append(info.Devices, dev)
	}

	return info, nil
}

func readSysfsFile(dir, file string) string {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// usbClassName maps bDeviceClass hex codes to human-readable names.
func usbClassName(hex string) string {
	switch strings.ToLower(hex) {
	case "00":
		return "Per Interface"
	case "01":
		return "Audio"
	case "02":
		return "Communications"
	case "03":
		return "HID"
	case "05":
		return "Physical"
	case "06":
		return "Image"
	case "07":
		return "Printer"
	case "08":
		return "Mass Storage"
	case "09":
		return "Hub"
	case "0a":
		return "CDC Data"
	case "0b":
		return "Smart Card"
	case "0d":
		return "Content Security"
	case "0e":
		return "Video"
	case "0f":
		return "Personal Healthcare"
	case "10":
		return "Audio/Video"
	case "dc":
		return "Diagnostic"
	case "e0":
		return "Wireless"
	case "ef":
		return "Miscellaneous"
	case "fe":
		return "Application Specific"
	case "ff":
		return "Vendor Specific"
	default:
		return hex
	}
}
