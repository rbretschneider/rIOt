package expect

import (
	"testing"
	"time"
	_ "time/tzdata" // named zones load the same on every machine (TIME-12H-001 AD-002)
)

func TestTIME12H_AC061_HeldSummaryMessage(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	since := time.Date(2026, 10, 27, 17, 5, 9, 0, time.UTC)
	got := heldSummaryMessage(3, "web-01", since, ny)
	want := "3 escalation notifications for web-01 were held since 1:05 PM EDT; see Alerts"
	if got != want {
		t.Errorf("heldSummaryMessage = %q, want %q", got, want)
	}
}

func TestTIME12H_AC061_HeldSummaryMessageWithoutZoneIsUTC(t *testing.T) {
	since := time.Date(2026, 10, 27, 17, 5, 9, 0, time.UTC)
	got := heldSummaryMessage(3, "web-01", since, nil)
	want := "3 escalation notifications for web-01 were held since 5:05 PM UTC; see Alerts"
	if got != want {
		t.Errorf("heldSummaryMessage(nil zone) = %q, want %q", got, want)
	}
}
