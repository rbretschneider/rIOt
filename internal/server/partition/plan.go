// Package partition implements the hand-rolled background partition manager
// for PART-001: daily range partitioning with drop-partition retention for
// rIOt's six high-churn tables (heartbeats, telemetry_snapshots, device_logs,
// container_metrics, container_logs, probe_results).
//
// This file contains only PURE functions: date math and partition-name
// parsing, with no database access and no direct time.Now() reads. All
// "now"/"cutoff" values are injected by the caller (manager.go supplies
// time.Now().UTC() at the edges only), which is what makes this file fully
// unit-testable without a live PostgreSQL instance.
package partition

import (
	"fmt"
	"strings"
	"time"
)

// TableSpec is the immutable description of one partitioned table.
type TableSpec struct {
	Name      string        // e.g. "container_logs"
	KeyColumn string        // "timestamp" | "created_at"
	Retention time.Duration // 7*24h or RetentionDays*24h
}

// PartitionInfo describes one existing partition of a table, as reported by
// the executor after inspecting the catalog (pg_inherits/pg_class).
type PartitionInfo struct {
	Name       string
	UpperBound time.Time // parsed from the daily name, or the persisted cutoff for the legacy partition
	IsLegacy   bool
	IsDefault  bool // the forward catch partition; never droppable (AD-008)
}

// Health is the manager's mutex-protected status snapshot, surfaced via
// Manager.Health() for a future status endpoint (AD-005, SEC-PART-001-005).
type Health struct {
	LastRunAt     time.Time
	LastSuccessAt time.Time
	LastError     string
	Tables        map[string]TableHealth
}

// TableHealth is the per-table slice of the Health snapshot.
type TableHealth struct {
	FurthestFuturePartition time.Time
	DefaultRowCount         int64
}

// dailyCreateAheadDays / dailyCreateBehindDays are the manager's fixed
// create-ahead margins (AD-005): 3 days ahead, 1 day behind "now".
const (
	DefaultAheadDays  = 3
	DefaultBehindDays = 1
)

// BuildSpecs constructs the six in-scope TableSpecs from configuration. The
// three tables with a configurable retention window use retentionDays
// (RIOT_RETENTION_DAYS, Config.RetentionDays); the other three use the
// existing hardcoded 7-day window that runRetention used before this story
// (FR-014, BR-005) — no new hardcoded retention duration is introduced.
func BuildSpecs(retentionDays int) []TableSpec {
	sevenDays := 7 * 24 * time.Hour
	configured := time.Duration(retentionDays) * 24 * time.Hour
	return []TableSpec{
		{Name: "heartbeats", KeyColumn: "timestamp", Retention: sevenDays},
		{Name: "container_logs", KeyColumn: "timestamp", Retention: sevenDays},
		{Name: "container_metrics", KeyColumn: "timestamp", Retention: sevenDays},
		{Name: "telemetry_snapshots", KeyColumn: "timestamp", Retention: configured},
		{Name: "device_logs", KeyColumn: "timestamp", Retention: configured},
		{Name: "probe_results", KeyColumn: "created_at", Retention: configured},
	}
}

// PartitionName returns the deterministic daily partition name for a table
// and a day (only the UTC calendar day of `day` is used): "<table>_p<YYYYMMDD>".
func PartitionName(table string, day time.Time) string {
	return fmt.Sprintf("%s_p%s", table, day.UTC().Format("20060102"))
}

// ParsePartitionDay is the inverse of PartitionName: it extracts the UTC day
// a daily partition name encodes. ok is false for the legacy partition
// ("<table>_legacy"), the DEFAULT catch ("<table>_default"), or any name that
// does not end in "_p" followed by exactly 8 digits.
func ParsePartitionDay(name string) (day time.Time, ok bool) {
	idx := strings.LastIndex(name, "_p")
	if idx == -1 || idx+2+8 != len(name) {
		return time.Time{}, false
	}
	datePart := name[idx+2:]
	t, err := time.Parse("20060102", datePart)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// dayFloor truncates t to its UTC calendar-day start (00:00:00 UTC).
func dayFloor(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// PartitionsToCreate returns the UTC day-starts that should have a daily
// partition, given the current time and the table's legacy-partition cutoff.
// The range is [max(today-behindDays, cutoffDay) .. today+aheadDays],
// inclusive.
//
// It is cutoff-aware (SEC-PART-001-003 / AC-021a): it never emits a day
// whose entire range lies below cutoff, so a freshly-converted table's
// create-ahead never overlaps the legacy partition [MINVALUE, cutoff) — this
// is what eliminates the post-conversion "partition would overlap" error
// storm the pre-review design produced.
func PartitionsToCreate(now, cutoff time.Time, aheadDays, behindDays int) []time.Time {
	today := dayFloor(now)
	cutoffDay := dayFloor(cutoff)

	start := today.AddDate(0, 0, -behindDays)
	if start.Before(cutoffDay) {
		start = cutoffDay
	}
	end := today.AddDate(0, 0, aheadDays)

	if start.After(end) {
		return nil
	}

	days := make([]time.Time, 0, int(end.Sub(start).Hours()/24)+1)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	return days
}

// PartitionsToDrop returns the partitions in `existing` that are eligible to
// drop: those whose entire range is older than the table's retention window,
// i.e. UpperBound <= now - spec.Retention (FR-012, BR-008). The legacy
// partition is treated identically via its PartitionInfo.UpperBound (the
// persisted cutoff, FR-013/BR-004). The DEFAULT catch partition is NEVER
// returned — it has no finite upper bound and dropping it would break
// NFR-002 (AD-008).
func PartitionsToDrop(now time.Time, spec TableSpec, existing []PartitionInfo) []PartitionInfo {
	threshold := now.Add(-spec.Retention)
	var drop []PartitionInfo
	for _, p := range existing {
		if p.IsDefault {
			continue
		}
		if !p.UpperBound.After(threshold) {
			drop = append(drop, p)
		}
	}
	return drop
}
