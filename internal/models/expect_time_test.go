package models

import (
	"testing"
	"time"
)

func mustRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return at
}

func TestTIME12H_AC049_PoweredOffIgnoresProcessZone(t *testing.T) {
	// Not parallel: time.Local is process-wide. It is restored in t.Cleanup.
	old := time.Local
	t.Cleanup(func() { time.Local = old })
	time.Local = loadZone(t, "Asia/Tokyo")

	cov := OfflineCoverage{
		Kind: OfflineKindPoweredOff, ActorPhrase: "by ryan", ShutdownAt: exampleInstant,
		Zone: loadZone(t, "America/New_York"),
	}
	if got, want := cov.Message("web-01"), "Device web-01 powered off by ryan at 1:05 PM EDT"; got != want {
		t.Errorf("message = %q, want %q (process zone Asia/Tokyo must not be used)", got, want)
	}
}

func TestTIME12H_AC059_PoweredOffMessage(t *testing.T) {
	cov := OfflineCoverage{
		Kind: OfflineKindPoweredOff, ActorPhrase: "by ryan", ShutdownAt: exampleInstant,
		Zone: loadZone(t, "America/New_York"),
	}
	if got, want := cov.Message("web-01"), "Device web-01 powered off by ryan at 1:05 PM EDT"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestTIME12H_AC059_PoweredOffWithoutZoneIsUTC(t *testing.T) {
	cov := OfflineCoverage{Kind: OfflineKindPoweredOff, ActorPhrase: "by ryan", ShutdownAt: exampleInstant}
	if got, want := cov.Message("web-01"), "Device web-01 powered off by ryan at 5:05 PM UTC"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestTIME12H_AC060_BackByMessage(t *testing.T) {
	backBy := mustRFC3339(t, "2026-10-27T17:20:00Z")
	cov := OfflineCoverage{
		Kind: OfflineKindWindow, ActionLabel: "Reboot", ActorPhrase: "by ryan", BackBy: &backBy,
		Zone: loadZone(t, "America/New_York"),
	}
	want := "Device web-01 went offline during Reboot by ryan (expected, back by 1:20 PM EDT)"
	if got := cov.Message("web-01"); got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}

func TestTIME12H_AC060_NoBackByUnchanged(t *testing.T) {
	cov := OfflineCoverage{
		Kind: OfflineKindWindow, ActionLabel: "Reboot", ActorPhrase: "by ryan",
		Zone: loadZone(t, "America/New_York"),
	}
	want := "Device web-01 went offline during Reboot by ryan (expected)"
	if got := cov.Message("web-01"); got != want {
		t.Errorf("message without BackBy = %q, want the pre-story text %q", got, want)
	}
}

func TestTIME12H_AC062_MidnightNoonAndDST(t *testing.T) {
	cases := []struct {
		at   string
		want string
	}{
		{"2026-10-27T04:00:00Z", "12:00 AM EDT"},
		{"2026-10-27T16:00:00Z", "12:00 PM EDT"},
		{"2026-11-01T05:30:00Z", "1:30 AM EDT"},
		{"2026-11-01T06:30:00Z", "1:30 AM EST"},
		{"2026-03-08T06:59:59Z", "1:59 AM EST"},
		{"2026-03-08T07:00:00Z", "3:00 AM EDT"},
	}
	ny := loadZone(t, "America/New_York")
	for _, tc := range cases {
		cov := OfflineCoverage{Kind: OfflineKindPoweredOff, ActorPhrase: "by ryan", ShutdownAt: mustRFC3339(t, tc.at), Zone: ny}
		want := "Device web-01 powered off by ryan at " + tc.want
		if got := cov.Message("web-01"); got != want {
			t.Errorf("powered-off message for %s = %q, want %q", tc.at, got, want)
		}
	}
}
