package storage

import "testing"

func TestDUX_AC167_AbsoluteFirstDiskThresholds(t *testing.T) {
	if mode := Mode(320*GiB, 8*1024*GiB, 0, 0); mode != "normal" {
		t.Fatalf("large disk incorrectly emergency: %s", mode)
	}
	if mode := Mode(400<<20, 100*GiB, 0, 0); mode != "critical" {
		t.Fatal(mode)
	}
	low, emergency := Thresholds(100*GiB, 0, 0)
	if low != 15*GiB || emergency != 5*GiB {
		t.Fatalf("%d %d", low, emergency)
	}
}
func TestDUX_SEC045_ProbeMismatchByGrowthDelta(t *testing.T) {
	if ProbeMatches(100<<20, 200<<20, 80<<20, 199<<20) {
		t.Fatal("accepted unrelated filesystem")
	}
	if !ProbeMatches(100<<20, 200<<20, 80<<20, 180<<20) {
		t.Fatal("rejected matching growth")
	}
	if ProbeMatches(200<<20, 100<<20, 0, 0) {
		t.Fatal("accepted impossible source")
	}
}
