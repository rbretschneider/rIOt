package collectors

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

//go:embed data/usb.ids
var embeddedUSBIDs []byte

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

	db := getUSBIDDB()

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

		// Fall back to usb.ids database when sysfs doesn't have names
		if db != nil {
			if dev.Vendor == "" {
				if name, ok := db.vendors[vendorID]; ok {
					dev.Vendor = name
				}
			}
			if dev.Product == "" {
				if name, ok := db.products[vendorID+":"+productID]; ok {
					dev.Product = name
				}
			}
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
		} else if dev.Vendor != "" {
			dev.Description = dev.Vendor + " (" + vendorID + ":" + productID + ")"
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

// usbIDDB is a parsed usb.ids database for looking up vendor/product names.
type usbIDDB struct {
	vendors  map[string]string // vendorID → vendor name
	products map[string]string // "vendorID:productID" → product name
}

var (
	usbIDDBOnce     sync.Once
	usbIDDBInstance *usbIDDB
)

// usb.ids file locations in order of preference.
var usbIDPaths = []string{
	"/usr/share/hwdata/usb.ids",
	"/usr/share/misc/usb.ids",
	"/usr/share/usb.ids",
	"/var/lib/usbutils/usb.ids",
}

// getUSBIDDB lazily loads and caches the usb.ids database.
// Prefers a system-installed usb.ids file, falls back to the embedded copy.
func getUSBIDDB() *usbIDDB {
	usbIDDBOnce.Do(func() {
		for _, path := range usbIDPaths {
			if f, err := os.Open(path); err == nil {
				if db, err := parseUSBIDReader(f); err == nil {
					usbIDDBInstance = db
				}
				f.Close()
				if usbIDDBInstance != nil {
					return
				}
			}
		}
		// Fall back to embedded database
		if db, err := parseUSBIDReader(bytes.NewReader(embeddedUSBIDs)); err == nil {
			usbIDDBInstance = db
		}
	})
	return usbIDDBInstance
}

// parseUSBIDReader parses the standard usb.ids file format from any reader.
// Vendor lines start at column 0: "1a6e  Global Unichip Corp."
// Product lines start with a tab: "\t089a  Coral USB Accelerator"
func parseUSBIDReader(r io.Reader) (*usbIDDB, error) {
	db := &usbIDDB{
		vendors:  make(map[string]string),
		products: make(map[string]string),
	}

	scanner := bufio.NewScanner(r)
	var currentVendor string

	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and empty lines
		if line == "" || line[0] == '#' {
			continue
		}

		// Stop at device class section (starts with "C ")
		if len(line) >= 2 && line[0] == 'C' && line[1] == ' ' {
			break
		}

		if line[0] == '\t' {
			// Product line: "\tPPPP  Product Name"
			if currentVendor == "" {
				continue
			}
			line = line[1:] // strip leading tab
			// Skip sub-devices (double tab)
			if len(line) > 0 && line[0] == '\t' {
				continue
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
			// Vendor line: "VVVV  Vendor Name"
			if len(line) < 6 {
				continue
			}
			vendorID := line[:4]
			// Validate it looks like a hex ID
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

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
