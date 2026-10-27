package quota

import (
	"testing"
	"time"
	_ "time/tzdata" // Pacific/Kiritimati loads the same on every machine (TIME-12H-001 AD-002)
)

// BR-003: the daily quota day boundary is the UTC day whatever the process zone
// is. Not parallel: time.Local is process-wide and is restored in t.Cleanup.
func TestTIME12H_AC066_QuotaDayIgnoresProcessZone(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })

	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatalf("load Pacific/Kiritimati: %v", err)
	}

	// 2026-10-27T11:00Z is already 2026-10-28 01:00 in Kiritimati.
	now := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
	want := New(nil).get("host", now).Day
	if want != "2026-10-27" {
		t.Fatalf("UTC quota day = %q, want 2026-10-27", want)
	}

	time.Local = kiritimati
	got := New(nil).get("host", now.In(time.Local)).Day
	if got != want {
		t.Errorf("quota day with process zone Kiritimati = %q, want the UTC day %q", got, want)
	}
}
