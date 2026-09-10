package partition

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// --- test doubles ---

// fakeExecutor is an in-memory stand-in for the DB-backed executor,
// letting the manager's scheduling/classification/health logic be
// unit-tested with no live PostgreSQL (per docs/architecture/PART-001-add.md
// §8 AC-017/AC-020 test strategy).
type fakeExecutor struct {
	mu sync.Mutex

	cutoff    map[string]time.Time
	cutoffErr map[string]error

	defaultRows    map[string]int64
	defaultRowsErr map[string]error

	childNames map[string][]string
	listErr    map[string]error

	createErr map[string]error // keyed by table; applied to every create for that table
	dropErr   map[string]error // keyed by partition name

	createCalls []createCall
	dropCalls   []string
}

type createCall struct {
	table    string
	from, to time.Time
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		cutoff:         map[string]time.Time{},
		cutoffErr:      map[string]error{},
		defaultRows:    map[string]int64{},
		defaultRowsErr: map[string]error{},
		childNames:     map[string][]string{},
		listErr:        map[string]error{},
		createErr:      map[string]error{},
		dropErr:        map[string]error{},
	}
}

func (f *fakeExecutor) createPartition(_ context.Context, table string, from, to time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls = append(f.createCalls, createCall{table, from, to})
	return f.createErr[table]
}

func (f *fakeExecutor) dropPartition(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropCalls = append(f.dropCalls, name)
	return f.dropErr[name]
}

func (f *fakeExecutor) readCutoff(_ context.Context, table string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.cutoffErr[table]; ok {
		return time.Time{}, err
	}
	return f.cutoff[table], nil
}

func (f *fakeExecutor) countDefaultRows(_ context.Context, table string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.defaultRowsErr[table]; ok {
		return 0, err
	}
	return f.defaultRows[table], nil
}

func (f *fakeExecutor) listPartitionNames(_ context.Context, table string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.listErr[table]; ok {
		return nil, err
	}
	return f.childNames[table], nil
}

// capturedRecord is a flattened slog.Record captured for assertions.
type capturedRecord struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

// captureHandler is a minimal slog.Handler that records every emitted
// record, letting tests assert on level/message/attributes without stdout
// parsing.
type captureHandler struct {
	mu      sync.Mutex
	records []capturedRecord
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, capturedRecord{level: r.Level, msg: r.Message, attrs: attrs})
	h.mu.Unlock()
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) all() []capturedRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]capturedRecord, len(h.records))
	copy(out, h.records)
	return out
}

func (h *captureHandler) count(level slog.Level, msg string) int {
	n := 0
	for _, r := range h.all() {
		if r.level == level && r.msg == msg {
			n++
		}
	}
	return n
}

var farPast = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// --- AC-017: create/drop/failure observability ---

func Test_AC017_EnsureAll_logs_INFO_with_table_and_partition_on_successful_create(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoff["container_logs"] = farPast
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll() error = %v, want nil", err)
	}

	found := false
	for _, r := range h.all() {
		if r.level == slog.LevelInfo && r.msg == "partition created" && r.attrs["table"] == "container_logs" && r.attrs["partition"] != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an INFO 'partition created' record with table+partition attrs, got: %+v", h.all())
	}
	if len(fx.createCalls) == 0 {
		t.Fatal("expected at least one createPartition call")
	}
}

func Test_AC017_DropAged_logs_INFO_with_table_and_partition_on_successful_drop(t *testing.T) {
	fx := newFakeExecutor()
	fx.childNames["container_logs"] = []string{"container_logs_p20200101"}
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	if err := mgr.DropAged(context.Background()); err != nil {
		t.Fatalf("DropAged() error = %v, want nil", err)
	}

	found := false
	for _, r := range h.all() {
		if r.level == slog.LevelInfo && r.msg == "partition dropped" &&
			r.attrs["table"] == "container_logs" && r.attrs["partition"] == "container_logs_p20200101" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an INFO 'partition dropped' record, got: %+v", h.all())
	}
	if len(fx.dropCalls) != 1 || fx.dropCalls[0] != "container_logs_p20200101" {
		t.Fatalf("dropCalls = %v, want exactly one call for the aged partition", fx.dropCalls)
	}
}

func Test_AC017_EnsureAll_genuine_error_logs_ERROR_isolates_per_table_and_retries_next_cycle(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoff["heartbeats"] = farPast
	fx.cutoff["container_logs"] = farPast
	fx.createErr["heartbeats"] = errors.New("disk full")

	h := &captureHandler{}
	specs := []TableSpec{
		{Name: "heartbeats", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour},
		{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour},
	}
	mgr := newManager(fx, specs, slog.New(h))

	err := mgr.EnsureAll(context.Background())
	if err == nil {
		t.Fatal("EnsureAll() error = nil, want non-nil (heartbeats create failures)")
	}

	sawContainerLogs := false
	for _, c := range fx.createCalls {
		if c.table == "container_logs" {
			sawContainerLogs = true
		}
	}
	if !sawContainerLogs {
		t.Fatal("container_logs was never attempted after heartbeats failed — per-table isolation broken (FR-021, NFR-006)")
	}

	if h.count(slog.LevelError, "partition manager: create failed") == 0 {
		t.Fatal("expected an ERROR 'partition manager: create failed' record for heartbeats")
	}

	callsBefore := len(fx.createCalls)
	mgr.EnsureAll(context.Background())
	if len(fx.createCalls) <= callsBefore {
		t.Fatal("expected the failing table to be retried on the next EnsureAll cycle")
	}
}

func Test_AC017_EnsureAll_no_partition_conversion_row_logs_WARN_not_ERROR(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoffErr["heartbeats"] = errors.New("no rows in result set")
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "heartbeats", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll() error = %v, want nil (missing bookkeeping row is fail-safe, not a hard failure)", err)
	}
	if h.count(slog.LevelWarn, "partition manager: no partition_conversion row; skipping create-ahead") != 1 {
		t.Fatalf("expected exactly one WARN for the missing cutoff row, got: %+v", h.all())
	}
	if h.count(slog.LevelError, "partition manager: create failed") != 0 {
		t.Fatal("a missing partition_conversion row must not be logged as an ERROR")
	}
}

// --- AC-020: health surface + persistent-stall escalation ---

func Test_AC020_Health_reports_LastError_and_does_not_advance_LastSuccessAt_on_failure(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoff["container_logs"] = farPast
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("initial EnsureAll() error = %v, want nil", err)
	}
	successAt := mgr.Health().LastSuccessAt
	if successAt.IsZero() {
		t.Fatal("LastSuccessAt not set after a successful run")
	}

	fx.createErr["container_logs"] = errors.New("boom")
	time.Sleep(2 * time.Millisecond) // ensure a distinguishable LastRunAt if it were (wrongly) reused as success

	if err := mgr.EnsureAll(context.Background()); err == nil {
		t.Fatal("expected EnsureAll() to return an error")
	}

	got := mgr.Health()
	if got.LastError == "" {
		t.Fatal("Health().LastError is empty after a failed run")
	}
	if !got.LastSuccessAt.Equal(successAt) {
		t.Fatalf("Health().LastSuccessAt changed after a failed run: got %v, want unchanged %v", got.LastSuccessAt, successAt)
	}
}

func Test_AC020_stalled_escalation_emits_a_single_ERROR_after_three_consecutive_failures(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoff["container_logs"] = farPast
	fx.createErr["container_logs"] = errors.New("boom")
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	for i := 0; i < 5; i++ {
		mgr.EnsureAll(context.Background())
	}

	if got := h.count(slog.LevelError, "partition manager stalled"); got != 1 {
		t.Fatalf("expected exactly one escalated 'partition manager stalled' ERROR after 5 consecutive failures, got %d", got)
	}
}

func Test_AC020_stall_escalation_resets_after_a_success(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoff["container_logs"] = farPast
	fx.createErr["container_logs"] = errors.New("boom")
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	for i := 0; i < 3; i++ {
		mgr.EnsureAll(context.Background())
	}
	if got := h.count(slog.LevelError, "partition manager stalled"); got != 1 {
		t.Fatalf("expected one stalled ERROR after 3 failures, got %d", got)
	}

	delete(fx.createErr, "container_logs")
	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("recovery EnsureAll() error = %v, want nil", err)
	}

	for i := 0; i < 3; i++ {
		fx.createErr["container_logs"] = errors.New("boom again")
		mgr.EnsureAll(context.Background())
	}
	if got := h.count(slog.LevelError, "partition manager stalled"); got != 2 {
		t.Fatalf("expected a second stalled ERROR after recovery + 3 more failures, got %d", got)
	}
}

func Test_AC020_nonzero_DefaultRowCount_is_surfaced_via_health_and_WARN(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoff["container_logs"] = farPast
	fx.defaultRows["container_logs"] = 3
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	if err := mgr.EnsureAll(context.Background()); err != nil {
		t.Fatalf("EnsureAll() error = %v, want nil", err)
	}

	if got := mgr.Health().Tables["container_logs"].DefaultRowCount; got != 3 {
		t.Fatalf("Health().Tables[container_logs].DefaultRowCount = %d, want 3", got)
	}
	if h.count(slog.LevelWarn, "partition manager: rows present in DEFAULT catch partition") == 0 {
		t.Fatal("expected a WARN log for a non-zero DEFAULT row count")
	}
}

func Test_AC020_low_forward_coverage_emits_WARN(t *testing.T) {
	fx := newFakeExecutor()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	// cutoff == now, and every create is benign-skipped, so the furthest
	// tracked partition never advances past cutoff — coverage stays "now",
	// which must trip the <24h WARN.
	fx.cutoff["container_logs"] = now
	fx.createErr["container_logs"] = &pgconn.PgError{Code: "42P07"}
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	if err := mgr.ensureTable(context.Background(), mgr.specs[0], now); err != nil {
		t.Fatalf("ensureTable() error = %v, want nil (benign create errors must not fail the table)", err)
	}

	if h.count(slog.LevelWarn, "partition manager: forward partition coverage low") == 0 {
		t.Fatalf("expected a WARN for collapsing forward coverage, got: %+v", h.all())
	}
}

func Test_AC020_Health_returns_an_independent_copy(t *testing.T) {
	fx := newFakeExecutor()
	fx.cutoff["container_logs"] = farPast
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(&captureHandler{}))
	mgr.EnsureAll(context.Background())

	snap := mgr.Health()
	snap.Tables["container_logs"] = TableHealth{DefaultRowCount: 999}

	again := mgr.Health()
	if again.Tables["container_logs"].DefaultRowCount == 999 {
		t.Fatal("Health() leaked internal state — mutating a returned snapshot affected a later snapshot")
	}
}

// --- AC-021b (unit slice) / R6: benign vs genuine SQLSTATE classification ---

func Test_AC021b_classifyBenign_treats_duplicate_check_and_overlap_as_benign(t *testing.T) {
	cases := []struct {
		name, code string
	}{
		{"duplicate_object", "42P07"},
		{"check_violation_default_capture", "23514"},
		{"invalid_object_definition_overlap", "42P17"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotCode, benign := classifyBenign(&pgconn.PgError{Code: c.code})
			if !benign {
				t.Fatalf("classifyBenign(code=%s) benign = false, want true", c.code)
			}
			if gotCode != c.code {
				t.Fatalf("classifyBenign(code=%s) returned code %q", c.code, gotCode)
			}
		})
	}
}

func Test_R6_classifyBenign_does_not_swallow_disk_full_or_permission_errors(t *testing.T) {
	cases := []string{"53100", "42501"} // disk_full, insufficient_privilege
	for _, code := range cases {
		_, benign := classifyBenign(&pgconn.PgError{Code: code})
		if benign {
			t.Fatalf("classifyBenign(code=%s) benign = true, want false — must not swallow a genuine, page-worthy failure", code)
		}
	}
}

func Test_AC021b_classifyBenign_non_pg_error_is_not_benign(t *testing.T) {
	if _, benign := classifyBenign(errors.New("generic error")); benign {
		t.Fatal("classifyBenign(generic error) benign = true, want false")
	}
}

// --- §10 defense-in-depth: DROP target pattern gating (AC-011/AC-012) ---

func Test_AC011_isDroppableName_allows_daily_and_legacy_rejects_default_and_unknown(t *testing.T) {
	cases := []struct {
		name, table, partition string
		want                   bool
	}{
		{"daily", "container_logs", "container_logs_p20260910", true},
		{"legacy", "container_logs", "container_logs_legacy", true},
		{"default_never_droppable", "container_logs", "container_logs_default", false},
		{"wrong_table_prefix", "container_logs", "heartbeats_p20260910", false},
		{"unrecognized_suffix", "container_logs", "container_logs_backup", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDroppableName(c.table, c.partition); got != c.want {
				t.Errorf("isDroppableName(%q, %q) = %v, want %v", c.table, c.partition, got, c.want)
			}
		})
	}
}

// --- SEC-PART-001-006: %I/%L-equivalent quoting in the Go executor ---

func Test_SEC006_sqlIdent_quotes_identifiers(t *testing.T) {
	got := sqlIdent("container_logs_p20260910")
	want := `"container_logs_p20260910"`
	if got != want {
		t.Fatalf("sqlIdent() = %s, want %s", got, want)
	}
}

func Test_SEC006_sqlLiteral_produces_a_quoted_RFC3339_literal(t *testing.T) {
	ts := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	got := sqlLiteral(ts)
	want := "'2026-09-10T00:00:00Z'"
	if got != want {
		t.Fatalf("sqlLiteral() = %s, want %s", got, want)
	}
}

// --- listPartitions classification (AC-012 fail-safe: missing cutoff excludes legacy from drop eligibility) ---

func Test_AC012_listPartitions_excludes_legacy_when_cutoff_unreadable(t *testing.T) {
	fx := newFakeExecutor()
	fx.childNames["container_logs"] = []string{"container_logs_legacy", "container_logs_default", "container_logs_p20260101"}
	fx.cutoffErr["container_logs"] = errors.New("no rows in result set")
	h := &captureHandler{}
	mgr := newManager(fx, []TableSpec{{Name: "container_logs", KeyColumn: "timestamp", Retention: 7 * 24 * time.Hour}}, slog.New(h))

	infos, err := mgr.listPartitions(context.Background(), mgr.specs[0])
	if err != nil {
		t.Fatalf("listPartitions() error = %v, want nil", err)
	}
	for _, p := range infos {
		if p.IsLegacy {
			t.Fatal("legacy partition must be excluded from the result when its cutoff cannot be read (fail-safe: never drop data we can't bound)")
		}
	}
	if h.count(slog.LevelWarn, "partition manager: cannot read cutoff for legacy partition; will not drop it this cycle") == 0 {
		t.Fatal("expected a WARN when the legacy partition's cutoff cannot be read")
	}
	// The daily and DEFAULT partitions must still be classified normally.
	foundDaily, foundDefault := false, false
	for _, p := range infos {
		if p.Name == "container_logs_p20260101" {
			foundDaily = true
		}
		if p.Name == "container_logs_default" && p.IsDefault {
			foundDefault = true
		}
	}
	if !foundDaily || !foundDefault {
		t.Fatalf("expected the daily and DEFAULT partitions still classified, got: %+v", infos)
	}
}
