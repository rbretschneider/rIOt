package partition

import (
	"reflect"
	"testing"
	"time"
	_ "time/tzdata" // Pacific/Kiritimati loads the same on every machine (TIME-12H-001 AD-002)
)

// BR-003: partition boundaries are UTC whatever the process zone is. The test
// sets time.Local to UTC+14 and feeds instants located there; the plan must
// equal the one computed from the same instants in UTC.
func TestTIME12H_AC066_PartitionPlanIgnoresProcessZone(t *testing.T) {
	// Not parallel: time.Local is process-wide. It is restored in t.Cleanup.
	old := time.Local
	t.Cleanup(func() { time.Local = old })

	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatalf("load Pacific/Kiritimati: %v", err)
	}

	// 2026-10-27T11:00Z is already 2026-10-28 01:00 in Kiritimati.
	now := time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	wantCreate := PartitionsToCreate(now, cutoff, 3, 2)
	wantName := PartitionName("heartbeats", now)

	time.Local = kiritimati
	localNow := now.In(time.Local)
	localCutoff := cutoff.In(time.Local)

	if got := PartitionsToCreate(localNow, localCutoff, 3, 2); !reflect.DeepEqual(got, wantCreate) {
		t.Errorf("PartitionsToCreate under process zone Kiritimati = %v, want the UTC result %v", got, wantCreate)
	}
	if got := PartitionName("heartbeats", localNow); got != wantName {
		t.Errorf("PartitionName under process zone Kiritimati = %q, want %q", got, wantName)
	}
	if wantName != "heartbeats_p20261027" {
		t.Errorf("PartitionName = %q, want the UTC day heartbeats_p20261027", wantName)
	}
}
