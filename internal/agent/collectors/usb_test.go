package collectors

import (
	"bytes"
	"testing"
)

func TestEmbeddedUSBIDDB(t *testing.T) {
	if len(embeddedUSBIDs) == 0 {
		t.Fatal("embedded usb.ids is empty")
	}

	db, err := parseUSBIDReader(bytes.NewReader(embeddedUSBIDs))
	if err != nil {
		t.Fatalf("failed to parse embedded usb.ids: %v", err)
	}

	if len(db.vendors) < 1000 {
		t.Errorf("expected at least 1000 vendors, got %d", len(db.vendors))
	}
	if len(db.products) < 1000 {
		t.Errorf("expected at least 1000 products, got %d", len(db.products))
	}

	// Verify well-known entries
	tests := []struct {
		vendorID, productID string
		wantVendor          string
		wantProduct         string
	}{
		{"1a6e", "089a", "Global Unichip Corp.", "Coral USB Accelerator"},
		{"8087", "", "Intel Corp.", ""},
		{"0bda", "", "Realtek Semiconductor Corp.", ""},
	}
	for _, tt := range tests {
		if got := db.vendors[tt.vendorID]; tt.wantVendor != "" && got != tt.wantVendor {
			t.Errorf("vendor %s: got %q, want %q", tt.vendorID, got, tt.wantVendor)
		}
		if tt.productID != "" {
			key := tt.vendorID + ":" + tt.productID
			if got := db.products[key]; got != tt.wantProduct {
				t.Errorf("product %s: got %q, want %q", key, got, tt.wantProduct)
			}
		}
	}
}
