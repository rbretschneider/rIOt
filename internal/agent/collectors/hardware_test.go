package collectors

import (
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func TestHardwareCollectorName(t *testing.T) {
	c := &HardwareCollector{}
	if c.Name() != "hardware" {
		t.Errorf("expected Name() = %q, got %q", "hardware", c.Name())
	}
}

func TestParsePCIIDReader(t *testing.T) {
	input := `# PCI ID Database
#
0001  SafeNet (different from different SafeNet different different)
	0001  SafeNet HSM
8086  Intel Corporation
	1533  I210 Gigabit Network Connection
	2723  Wi-Fi 6 AX200
10de  NVIDIA Corporation
	2504  GA106 [GeForce RTX 3060 Lite Hash Rate]
C 00  Unclassified device
`

	db, err := parsePCIIDReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Vendor count
	if len(db.vendors) != 3 {
		t.Errorf("expected 3 vendors, got %d", len(db.vendors))
	}

	// Verify vendor lookups
	tests := []struct {
		vendorID    string
		wantVendor  string
		productID   string
		wantProduct string
	}{
		{"8086", "Intel Corporation", "1533", "I210 Gigabit Network Connection"},
		{"8086", "Intel Corporation", "2723", "Wi-Fi 6 AX200"},
		{"10de", "NVIDIA Corporation", "2504", "GA106 [GeForce RTX 3060 Lite Hash Rate]"},
		{"0001", "SafeNet (different from different SafeNet different different)", "0001", "SafeNet HSM"},
	}

	for _, tt := range tests {
		if got := db.vendors[tt.vendorID]; got != tt.wantVendor {
			t.Errorf("vendor %s: got %q, want %q", tt.vendorID, got, tt.wantVendor)
		}
		key := tt.vendorID + ":" + tt.productID
		if got := db.products[key]; got != tt.wantProduct {
			t.Errorf("product %s: got %q, want %q", key, got, tt.wantProduct)
		}
	}
}

func TestParsePCIIDReader_SkipsSubDevicesAndComments(t *testing.T) {
	input := `# Comment line
8086  Intel Corporation
	1533  I210 Gigabit Network Connection
		8086 0001  Ethernet Server Adapter I210-T1
	2723  Wi-Fi 6 AX200

# Another comment
10de  NVIDIA Corporation
`

	db, err := parsePCIIDReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(db.vendors) != 2 {
		t.Errorf("expected 2 vendors, got %d", len(db.vendors))
	}
	if len(db.products) != 2 {
		t.Errorf("expected 2 products (sub-device skipped), got %d", len(db.products))
	}
}

func TestParsePCIIDReader_StopsAtClassSection(t *testing.T) {
	input := `8086  Intel Corporation
	1533  I210 Gigabit Network Connection
C 00  Unclassified device
	00  Non-VGA unclassified device
ffff  FakeVendor
`

	db, err := parsePCIIDReader(strings.NewReader(input))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(db.vendors) != 1 {
		t.Errorf("expected 1 vendor (parsing stops at C section), got %d", len(db.vendors))
	}
	if _, ok := db.vendors["ffff"]; ok {
		t.Error("vendor after C section should not be parsed")
	}
}

func TestParsePCIIDReader_EmptyInput(t *testing.T) {
	db, err := parsePCIIDReader(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.vendors) != 0 {
		t.Errorf("expected 0 vendors, got %d", len(db.vendors))
	}
}

func TestPCIClassName(t *testing.T) {
	tests := []struct {
		classPrefix string
		want        string
	}{
		{"0200", "Ethernet Controller"},
		{"0300", "VGA Controller"},
		{"0302", "3D Controller"},
		{"0108", "NVMe Controller"},
		{"0106", "SATA Controller"},
		{"0c03", "USB Controller"},
		{"0403", "Audio Device"},
		{"0600", "Host Bridge"},
		{"0604", "PCI Bridge"},
		{"0d11", "Bluetooth Controller"},
		// Fallback to 2-digit class
		{"0199", "Storage Controller"},
		{"0399", "Display Controller"},
		{"0c99", "Serial Bus Controller"},
		// Unknown class
		{"ff00", "Unassigned Class"},
	}

	for _, tt := range tests {
		t.Run(tt.classPrefix, func(t *testing.T) {
			got := pciClassName(tt.classPrefix)
			if got != tt.want {
				t.Errorf("pciClassName(%q) = %q, want %q", tt.classPrefix, got, tt.want)
			}
		})
	}
}

func TestCollectGPUs_FiltersDisplayClass(t *testing.T) {
	// GPUs should be extracted from PCI devices with class 0300, 0302, 0380
	pciDevices := []models.PCIDevice{
		{Slot: "0000:00:02.0", VendorID: "8086", DeviceID: "9a49", ClassCode: "030000", Vendor: "Intel", Device: "UHD Graphics", Description: "Intel UHD Graphics"},
		{Slot: "0000:01:00.0", VendorID: "10de", DeviceID: "2504", ClassCode: "030200", Vendor: "NVIDIA", Device: "RTX 3060", Description: "NVIDIA RTX 3060"},
		{Slot: "0000:00:1f.3", VendorID: "8086", DeviceID: "a0c8", ClassCode: "040300", Vendor: "Intel", Device: "Audio", Description: "Intel Audio"},
		{Slot: "0000:00:14.0", VendorID: "8086", DeviceID: "a0ed", ClassCode: "0c0330", Vendor: "Intel", Device: "USB", Description: "Intel USB"},
	}

	gpus := collectGPUs(pciDevices)

	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs (display class only), got %d", len(gpus))
	}

	if gpus[0].Model != "UHD Graphics" {
		t.Errorf("expected first GPU model %q, got %q", "UHD Graphics", gpus[0].Model)
	}
	if gpus[0].Vendor != "Intel" {
		t.Errorf("expected first GPU vendor %q, got %q", "Intel", gpus[0].Vendor)
	}
	if gpus[1].Model != "RTX 3060" {
		t.Errorf("expected second GPU model %q, got %q", "RTX 3060", gpus[1].Model)
	}
}

func TestCollectGPUs_NoGPUs(t *testing.T) {
	pciDevices := []models.PCIDevice{
		{Slot: "0000:00:1f.3", VendorID: "8086", DeviceID: "a0c8", ClassCode: "040300"},
		{Slot: "0000:00:14.0", VendorID: "8086", DeviceID: "a0ed", ClassCode: "0c0330"},
	}

	gpus := collectGPUs(pciDevices)

	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs, got %d", len(gpus))
	}
}

func TestCollectGPUs_ShortClassCode(t *testing.T) {
	pciDevices := []models.PCIDevice{
		{Slot: "0000:00:02.0", VendorID: "8086", DeviceID: "9a49", ClassCode: "03"},
	}

	gpus := collectGPUs(pciDevices)
	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs for short class code, got %d", len(gpus))
	}
}

func TestIsHex4(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"8086", true},
		{"10de", true},
		{"abcd", true},
		{"ABCD", true},
		{"0000", true},
		{"ffff", true},
		{"xyz1", false},
		{"80", false},
		{"80860", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isHex4(tt.input)
		if got != tt.want {
			t.Errorf("isHex4(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
