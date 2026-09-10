package partition

import (
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// --- PartitionName / ParsePartitionDay ---

func Test_AC010_PartitionName_formats_table_and_UTC_day(t *testing.T) {
	got := PartitionName("container_logs", day(2026, 9, 10))
	want := "container_logs_p20260910"
	if got != want {
		t.Fatalf("PartitionName() = %q, want %q", got, want)
	}
}

func Test_AC010_PartitionName_uses_UTC_day_not_local_time(t *testing.T) {
	// A time with a non-UTC offset that would fall on a different calendar
	// day in UTC than it appears in its own zone.
	loc := time.FixedZone("UTC-5", -5*3600)
	local := time.Date(2026, 9, 10, 23, 30, 0, 0, loc) // 2026-09-11 04:30 UTC
	got := PartitionName("heartbeats", local)
	want := "heartbeats_p20260911"
	if got != want {
		t.Fatalf("PartitionName() = %q, want %q (must use UTC day)", got, want)
	}
}

func Test_AC010_ParsePartitionDay_round_trips_daily_names(t *testing.T) {
	d := day(2026, 12, 31)
	name := PartitionName("device_logs", d)
	got, ok := ParsePartitionDay(name)
	if !ok {
		t.Fatalf("ParsePartitionDay(%q) ok = false, want true", name)
	}
	if !got.Equal(d) {
		t.Fatalf("ParsePartitionDay(%q) = %v, want %v", name, got, d)
	}
}

func Test_AC010_ParsePartitionDay_rejects_legacy_and_default_names(t *testing.T) {
	for _, name := range []string{
		"container_logs_legacy",
		"container_logs_default",
		"heartbeats",
		"heartbeats_pk_new",
		"device_logs_p2026091", // one digit short
		"device_logs_p202609100", // one digit long
	} {
		if _, ok := ParsePartitionDay(name); ok {
			t.Errorf("ParsePartitionDay(%q) ok = true, want false", name)
		}
	}
}

// --- PartitionsToCreate (AC-010, AC-021a) ---

func Test_AC010_PartitionsToCreate_returns_dayStarts_in_behind_to_ahead_window(t *testing.T) {
	now := day(2026, 9, 10) // exact midnight, simplest case
	cutoff := day(2020, 1, 1)

	got := PartitionsToCreate(now, cutoff, 3, 1)

	want := []time.Time{
		day(2026, 9, 9),  // behind=1
		day(2026, 9, 10), // today
		day(2026, 9, 11),
		day(2026, 9, 12),
		day(2026, 9, 13), // ahead=3
	}
	if len(got) != len(want) {
		t.Fatalf("PartitionsToCreate() returned %d days, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("PartitionsToCreate()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func Test_AC010_PartitionsToCreate_near_midnight_still_includes_tomorrow(t *testing.T) {
	// 23:59:59 UTC on 2026-09-10 must day-floor to 2026-09-10, and with
	// ahead=3 must still include 2026-09-11..13 — time-of-day must not
	// perturb the day-boundary math (NFR-002 coverage margin).
	now := time.Date(2026, 9, 10, 23, 59, 59, 0, time.UTC)
	cutoff := day(2020, 1, 1)

	got := PartitionsToCreate(now, cutoff, 3, 1)

	tomorrow := day(2026, 9, 11)
	found := false
	for _, d := range got {
		if d.Equal(tomorrow) {
			found = true
		}
	}
	if !found {
		t.Fatalf("PartitionsToCreate() at 23:59:59 = %v, want to include tomorrow %v", got, tomorrow)
	}
	last := got[len(got)-1]
	wantLast := day(2026, 9, 13)
	if !last.Equal(wantLast) {
		t.Fatalf("PartitionsToCreate() last day = %v, want %v", last, wantLast)
	}
}

func Test_AC021a_PartitionsToCreate_never_emits_a_day_fully_below_cutoff(t *testing.T) {
	// Freshly-converted table: cutoff is two days in the future of "now".
	// Without cutoff-awareness the "behind" window (now-1) would try to
	// create today-1, today, today+1 — all of which fall inside the legacy
	// partition's [MINVALUE, cutoff) range and would raise "partition would
	// overlap" (SEC-PART-001-003). No returned day may be < cutoffDay.
	now := day(2026, 9, 10)
	cutoff := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) // cutoffDay = 2026-09-12

	got := PartitionsToCreate(now, cutoff, 3, 1)

	cutoffDay := day(2026, 9, 12)
	for _, d := range got {
		if d.Before(cutoffDay) {
			t.Fatalf("PartitionsToCreate() emitted day %v which is before cutoffDay %v (would overlap the legacy partition)", d, cutoffDay)
		}
	}
	// And it must still cover through today+3.
	last := got[len(got)-1]
	wantLast := day(2026, 9, 13)
	if !last.Equal(wantLast) {
		t.Fatalf("PartitionsToCreate() last day = %v, want %v", last, wantLast)
	}
	first := got[0]
	if !first.Equal(cutoffDay) {
		t.Fatalf("PartitionsToCreate() first day = %v, want cutoffDay %v (create-ahead must start at cutoff)", first, cutoffDay)
	}
}

func Test_AC021a_PartitionsToCreate_empty_when_cutoff_is_beyond_ahead_window(t *testing.T) {
	// Degenerate case: cutoff so far in the future that even today+ahead is
	// still below it — no day should be emitted (nothing to create yet).
	now := day(2026, 9, 10)
	cutoff := day(2026, 10, 1)

	got := PartitionsToCreate(now, cutoff, 3, 1)
	if len(got) != 0 {
		t.Fatalf("PartitionsToCreate() = %v, want empty (cutoff beyond the ahead window)", got)
	}
}

func Test_AC010_PartitionsToCreate_with_far_past_cutoff_matches_uncapped_window(t *testing.T) {
	// An already-converted table whose cutoff long passed: create-ahead is
	// bounded purely by behind/ahead, never by cutoff.
	now := day(2026, 9, 10)
	cutoff := day(2020, 1, 1)

	got := PartitionsToCreate(now, cutoff, 3, 1)
	if len(got) != 5 {
		t.Fatalf("PartitionsToCreate() len = %d, want 5", len(got))
	}
}

// --- PartitionsToDrop (AC-011, AC-012) ---

func spec(retention time.Duration) TableSpec {
	return TableSpec{Name: "container_logs", KeyColumn: "timestamp", Retention: retention}
}

func Test_AC011_PartitionsToDrop_drops_partition_whose_upper_bound_is_at_or_before_the_window(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := spec(7 * 24 * time.Hour) // window = now - 7d = 2026-09-03 12:00 UTC

	existing := []PartitionInfo{
		{Name: "container_logs_p20260901", UpperBound: day(2026, 9, 2)}, // well before threshold
	}
	got := PartitionsToDrop(now, s, existing)
	if len(got) != 1 || got[0].Name != "container_logs_p20260901" {
		t.Fatalf("PartitionsToDrop() = %v, want the aged partition dropped", got)
	}
}

func Test_AC011_PartitionsToDrop_does_not_drop_a_partition_still_intersecting_the_window(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := spec(7 * 24 * time.Hour) // threshold = 2026-09-03 12:00 UTC

	existing := []PartitionInfo{
		{Name: "container_logs_p20260905", UpperBound: day(2026, 9, 6)}, // upper bound after threshold: still in window
	}
	got := PartitionsToDrop(now, s, existing)
	if len(got) != 0 {
		t.Fatalf("PartitionsToDrop() = %v, want none dropped (partition still intersects the retention window)", got)
	}
}

func Test_AC011_PartitionsToDrop_boundary_exactly_at_threshold_is_droppable(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := spec(7 * 24 * time.Hour)
	threshold := now.Add(-s.Retention) // 2026-09-03 12:00:00 UTC

	existing := []PartitionInfo{
		{Name: "container_logs_p20260903", UpperBound: threshold}, // upper bound == threshold exactly
	}
	got := PartitionsToDrop(now, s, existing)
	if len(got) != 1 {
		t.Fatalf("PartitionsToDrop() = %v, want the boundary partition dropped (upper bound == now-window)", got)
	}
}

func Test_AC011_PartitionsToDrop_one_second_within_window_is_not_droppable(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := spec(7 * 24 * time.Hour)
	threshold := now.Add(-s.Retention)

	existing := []PartitionInfo{
		{Name: "container_logs_p20260903", UpperBound: threshold.Add(1 * time.Second)}, // 1s later than the boundary — still within window
	}
	got := PartitionsToDrop(now, s, existing)
	if len(got) != 0 {
		t.Fatalf("PartitionsToDrop() = %v, want none dropped (1s within the retention window)", got)
	}
}

func Test_AC011_PartitionsToDrop_never_returns_the_DEFAULT_partition(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := spec(7 * 24 * time.Hour)

	existing := []PartitionInfo{
		// Deliberately given an ancient zero-value UpperBound to prove the
		// DEFAULT exclusion is unconditional, not a side effect of its bound.
		{Name: "container_logs_default", IsDefault: true, UpperBound: time.Time{}},
	}
	got := PartitionsToDrop(now, s, existing)
	if len(got) != 0 {
		t.Fatalf("PartitionsToDrop() = %v, want the DEFAULT partition never dropped (AD-008, NFR-002)", got)
	}
}

// --- Legacy partition drop eligibility (AC-012) ---

func Test_AC012_PartitionsToDrop_drops_legacy_once_cutoff_is_older_than_window(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	s := spec(7 * 24 * time.Hour) // threshold = 2026-09-03

	existing := []PartitionInfo{
		{Name: "container_logs_legacy", IsLegacy: true, UpperBound: day(2026, 9, 1)}, // cutoff well before threshold
	}
	got := PartitionsToDrop(now, s, existing)
	if len(got) != 1 || !got[0].IsLegacy {
		t.Fatalf("PartitionsToDrop() = %v, want the legacy partition dropped once its cutoff ages out (BR-004)", got)
	}
}

func Test_AC012_PartitionsToDrop_retains_legacy_while_cutoff_still_within_window(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	s := spec(7 * 24 * time.Hour) // threshold = 2026-09-03

	existing := []PartitionInfo{
		{Name: "container_logs_legacy", IsLegacy: true, UpperBound: day(2026, 9, 5)}, // cutoff still inside the window
	}
	got := PartitionsToDrop(now, s, existing)
	if len(got) != 0 {
		t.Fatalf("PartitionsToDrop() = %v, want the legacy partition retained while any of its range is within the window", got)
	}
}

// --- Retention window sourcing (AC-013) ---

func Test_AC013_BuildSpecs_maps_configurable_tables_to_RetentionDays(t *testing.T) {
	specs := BuildSpecs(45)
	byName := map[string]TableSpec{}
	for _, s := range specs {
		byName[s.Name] = s
	}

	for _, name := range []string{"telemetry_snapshots", "device_logs", "probe_results"} {
		got := byName[name].Retention
		want := 45 * 24 * time.Hour
		if got != want {
			t.Errorf("BuildSpecs(45)[%s].Retention = %v, want %v (RIOT_RETENTION_DAYS)", name, got, want)
		}
	}
}

func Test_AC013_BuildSpecs_maps_fixed_tables_to_seven_days_regardless_of_RetentionDays(t *testing.T) {
	specs := BuildSpecs(45)
	byName := map[string]TableSpec{}
	for _, s := range specs {
		byName[s.Name] = s
	}

	for _, name := range []string{"heartbeats", "container_logs", "container_metrics"} {
		got := byName[name].Retention
		want := 7 * 24 * time.Hour
		if got != want {
			t.Errorf("BuildSpecs(45)[%s].Retention = %v, want %v (fixed 7 days, unaffected by RetentionDays)", name, got, want)
		}
	}
}

func Test_AC013_BuildSpecs_changing_RetentionDays_only_changes_configurable_tables(t *testing.T) {
	low := BuildSpecs(10)
	high := BuildSpecs(90)

	byName := func(specs []TableSpec) map[string]TableSpec {
		m := map[string]TableSpec{}
		for _, s := range specs {
			m[s.Name] = s
		}
		return m
	}
	l, h := byName(low), byName(high)

	for _, name := range []string{"telemetry_snapshots", "device_logs", "probe_results"} {
		if l[name].Retention == h[name].Retention {
			t.Errorf("%s: retention did not change between RetentionDays=10 and 90", name)
		}
	}
	for _, name := range []string{"heartbeats", "container_logs", "container_metrics"} {
		if l[name].Retention != h[name].Retention {
			t.Errorf("%s: retention changed with RetentionDays (should be a fixed 7-day constant)", name)
		}
	}
}

func Test_AC013_BuildSpecs_covers_exactly_the_six_in_scope_tables(t *testing.T) {
	specs := BuildSpecs(30)
	if len(specs) != 6 {
		t.Fatalf("BuildSpecs() returned %d specs, want 6", len(specs))
	}
	want := map[string]bool{
		"heartbeats": true, "container_logs": true, "container_metrics": true,
		"telemetry_snapshots": true, "device_logs": true, "probe_results": true,
	}
	for _, s := range specs {
		if !want[s.Name] {
			t.Errorf("BuildSpecs() included unexpected table %q", s.Name)
		}
		delete(want, s.Name)
	}
	if len(want) != 0 {
		t.Errorf("BuildSpecs() missing tables: %v", want)
	}
}

func Test_AC002_BuildSpecs_probe_results_keys_on_created_at(t *testing.T) {
	specs := BuildSpecs(30)
	for _, s := range specs {
		if s.Name == "probe_results" {
			if s.KeyColumn != "created_at" {
				t.Fatalf("probe_results KeyColumn = %q, want %q", s.KeyColumn, "created_at")
			}
			return
		}
	}
	t.Fatal("probe_results not found in BuildSpecs()")
}

func Test_AC001_BuildSpecs_other_five_tables_key_on_timestamp(t *testing.T) {
	specs := BuildSpecs(30)
	for _, s := range specs {
		if s.Name == "probe_results" {
			continue
		}
		if s.KeyColumn != "timestamp" {
			t.Errorf("%s KeyColumn = %q, want %q", s.Name, s.KeyColumn, "timestamp")
		}
	}
}
