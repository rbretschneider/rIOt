package partition

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// stallThreshold is the number of consecutive failed manager operations
// (EnsureAll or DropAged) after which the manager escalates a single ERROR
// log ("partition manager stalled") rather than only logging per-cycle
// (AD-005, SEC-PART-001-005).
const stallThreshold = 3

// coverageWarnMargin is how close the furthest pre-created future partition
// may get to "now" before the manager surfaces a WARN that forward coverage
// is collapsing (AD-005).
const coverageWarnMargin = 24 * time.Hour

// benign PostgreSQL SQLSTATEs for CREATE TABLE ... PARTITION OF: these are
// expected, recoverable conditions and must be logged at WARN, never ERROR,
// so a clean conversion or a rare clock-skewed device cannot trip
// newServerErrorAlertWorker (AD-005, SEC-PART-001-003, residual R6).
const (
	sqlstateDuplicateObject     = "42P07" // partition already exists (IF NOT EXISTS normally prevents this)
	sqlstateCheckViolation      = "23514" // DEFAULT partition already holds a row for the day being created
	sqlstateInvalidObjectDefRel = "42P17" // new partition's range would overlap an existing partition
)

var dailyPartitionRe = regexp.MustCompile(`_p\d{8}$`)

// executor is the narrow set of DDL/query primitives the manager needs
// against PostgreSQL. It exists so the scheduling/classification/health
// logic in this file can be unit-tested with a fake executor and no live
// database (AC-017, AC-020 — see manager_test.go), while NewManager wires up
// the real pgxpool-backed implementation for production use.
type executor interface {
	createPartition(ctx context.Context, table string, from, to time.Time) error
	dropPartition(ctx context.Context, name string) error
	readCutoff(ctx context.Context, table string) (time.Time, error)
	countDefaultRows(ctx context.Context, table string) (int64, error)
	// listPartitionNames returns the raw child relation names currently
	// attached to the given partitioned table (pg_inherits), unclassified.
	listPartitionNames(ctx context.Context, table string) ([]string, error)
}

// pgxExecutor is the real executor backed by a pgxpool.Pool.
type pgxExecutor struct {
	pool *pgxpool.Pool
}

func (e *pgxExecutor) createPartition(ctx context.Context, table string, from, to time.Time) error {
	stmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%s) TO (%s)",
		sqlIdent(PartitionName(table, from)), sqlIdent(table), sqlLiteral(from), sqlLiteral(to))
	_, err := e.pool.Exec(ctx, stmt)
	return err
}

func (e *pgxExecutor) dropPartition(ctx context.Context, name string) error {
	stmt := fmt.Sprintf("DROP TABLE IF EXISTS %s", sqlIdent(name))
	_, err := e.pool.Exec(ctx, stmt)
	return err
}

func (e *pgxExecutor) readCutoff(ctx context.Context, table string) (time.Time, error) {
	var cutoff time.Time
	err := e.pool.QueryRow(ctx, `SELECT cutoff FROM partition_conversion WHERE table_name = $1`, table).Scan(&cutoff)
	return cutoff, err
}

func (e *pgxExecutor) countDefaultRows(ctx context.Context, table string) (int64, error) {
	stmt := fmt.Sprintf("SELECT count(*) FROM %s", sqlIdent(table+"_default"))
	var count int64
	err := e.pool.QueryRow(ctx, stmt).Scan(&count)
	return count, err
}

func (e *pgxExecutor) listPartitionNames(ctx context.Context, table string) ([]string, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// Manager pre-creates daily partitions ahead of need and drops partitions
// (including the legacy partition) once their entire range is older than
// the owning table's retention window. It never drops the DEFAULT catch
// partition. See docs/architecture/PART-001-add.md AD-005.
type Manager struct {
	exec  executor
	specs []TableSpec
	log   *slog.Logger

	ahead  int
	behind int

	mu                  sync.Mutex
	health              Health
	consecutiveFailures int
	stalled             bool
}

// NewManager constructs a Manager for the given table specs, backed by a
// live PostgreSQL connection pool. log may be nil, in which case
// slog.Default() is used.
func NewManager(pool *pgxpool.Pool, specs []TableSpec, log *slog.Logger) *Manager {
	return newManager(&pgxExecutor{pool: pool}, specs, log)
}

// newManager is the executor-injectable constructor used internally by
// NewManager and directly by unit tests (manager_test.go, same package) to
// exercise scheduling/classification/health logic without a live database.
func newManager(exec executor, specs []TableSpec, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		exec:   exec,
		specs:  specs,
		log:    log,
		ahead:  DefaultAheadDays,
		behind: DefaultBehindDays,
		health: Health{Tables: make(map[string]TableHealth, len(specs))},
	}
}

// EnsureAll creates every daily partition each table needs, from that
// table's cutoff-aware create-ahead window. Called synchronously at startup
// (before the server serves traffic) and once per Run() cycle thereafter. A
// failure on one table is logged and does not prevent the others from being
// processed (FR-021, NFR-006).
func (m *Manager) EnsureAll(ctx context.Context) error {
	now := time.Now().UTC()
	var errs error
	for _, spec := range m.specs {
		if err := m.ensureTable(ctx, spec, now); err != nil {
			errs = errors.Join(errs, fmt.Errorf("%s: %w", spec.Name, err))
		}
	}
	m.recordRun(errs)
	return errs
}

// DropAged drops every partition (including the legacy partition) whose
// entire range is older than its table's retention window. The DEFAULT
// catch partition is never dropped. A failure on one table is logged and
// does not prevent the others from being processed (FR-021, NFR-006).
func (m *Manager) DropAged(ctx context.Context) error {
	var errs error
	for _, spec := range m.specs {
		if err := m.dropAgedTable(ctx, spec); err != nil {
			errs = errors.Join(errs, fmt.Errorf("%s: %w", spec.Name, err))
		}
	}
	m.recordRun(errs)
	return errs
}

// Run performs a synchronous EnsureAll (in case it has not already been
// called) and then loops hourly doing ensure + drop for every table until
// ctx is cancelled. A per-cycle panic is recovered and logged so the loop
// survives (NFR-006); a per-table error never stops the loop.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runCycleSafely(ctx)
		}
	}
}

func (m *Manager) runCycleSafely(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Error("partition manager: cycle panic recovered", "recover", fmt.Sprintf("%v", r))
		}
	}()
	m.EnsureAll(ctx)
	m.DropAged(ctx)
}

// Health returns a copy of the manager's current status snapshot.
func (m *Manager) Health() Health {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := Health{
		LastRunAt:     m.health.LastRunAt,
		LastSuccessAt: m.health.LastSuccessAt,
		LastError:     m.health.LastError,
		Tables:        make(map[string]TableHealth, len(m.health.Tables)),
	}
	for k, v := range m.health.Tables {
		snap.Tables[k] = v
	}
	return snap
}

// --- table-level operations ---

func (m *Manager) ensureTable(ctx context.Context, spec TableSpec, now time.Time) error {
	cutoff, err := m.exec.readCutoff(ctx, spec.Name)
	if err != nil {
		// No partition_conversion row: the table hasn't gone through the
		// PART-001 conversion (or the row was lost). Fail-safe: skip
		// create-ahead for this table rather than guessing a cutoff, and
		// surface it — this is a configuration problem, not routine.
		m.log.Warn("partition manager: no partition_conversion row; skipping create-ahead", "table", spec.Name, "error", err.Error())
		return nil
	}

	days := PartitionsToCreate(now, cutoff, m.ahead, m.behind)
	furthest := cutoff
	var tableErr error

	for _, day := range days {
		end := day.AddDate(0, 0, 1)
		if err := m.exec.createPartition(ctx, spec.Name, day, end); err != nil {
			if code, benign := classifyBenign(err); benign {
				m.log.Warn("partition manager: create skipped (benign)",
					"table", spec.Name, "day", day.Format("2006-01-02"), "sqlstate", code,
					"hint", "a device clock may be skewed; rows are safely held in the DEFAULT partition")
			} else {
				m.log.Error("partition manager: create failed",
					"table", spec.Name, "day", day.Format("2006-01-02"), "error", err.Error())
				tableErr = errors.Join(tableErr, err)
			}
			continue
		}
		m.log.Info("partition created", "table", spec.Name, "partition", PartitionName(spec.Name, day),
			"from", day.Format("2006-01-02"), "to", end.Format("2006-01-02"))
		if end.After(furthest) {
			furthest = end
		}
	}

	defaultCount, err := m.exec.countDefaultRows(ctx, spec.Name)
	if err != nil {
		m.log.Warn("partition manager: could not read DEFAULT partition row count", "table", spec.Name, "error", err.Error())
		defaultCount = 0
	} else if defaultCount > 0 {
		m.log.Warn("partition manager: rows present in DEFAULT catch partition", "table", spec.Name, "count", defaultCount,
			"hint", "a device clock may be skewed beyond the forward create-ahead margin; see DELETE remediation in the operator docs")
	}
	m.updateTableHealth(spec.Name, furthest, defaultCount)

	if furthest.Sub(now) < coverageWarnMargin {
		m.log.Warn("partition manager: forward partition coverage low", "table", spec.Name, "furthest_partition", furthest)
	}

	return tableErr
}

func (m *Manager) dropAgedTable(ctx context.Context, spec TableSpec) error {
	now := time.Now().UTC()
	existing, err := m.listPartitions(ctx, spec)
	if err != nil {
		m.log.Error("partition manager: list partitions failed", "table", spec.Name, "error", err.Error())
		return err
	}

	toDrop := PartitionsToDrop(now, spec, existing)
	var tableErr error
	for _, p := range toDrop {
		if !isDroppableName(spec.Name, p.Name) {
			// Defense in depth (§10): never issue a DROP for a name that
			// doesn't match the recognized daily/legacy pattern.
			m.log.Warn("partition manager: refusing to drop unrecognized partition name", "table", spec.Name, "partition", p.Name)
			continue
		}
		if err := m.exec.dropPartition(ctx, p.Name); err != nil {
			m.log.Error("partition drop failed", "table", spec.Name, "partition", p.Name, "error", err.Error())
			tableErr = errors.Join(tableErr, err)
			continue
		}
		kind := "daily"
		if p.IsLegacy {
			kind = "legacy"
		}
		m.log.Info("partition dropped", "table", spec.Name, "partition", p.Name, "kind", kind, "upper_bound", p.UpperBound)
	}
	return tableErr
}

// listPartitions inspects the catalog for the current children of a
// partitioned table and classifies each as a daily partition, the legacy
// partition, or the DEFAULT catch. Unrecognized child names are ignored.
// If the legacy partition exists but its bound cannot be read from
// partition_conversion, it is fail-safe excluded from the result (never
// dropped without a known bound) and a WARN is logged (§9 error table).
func (m *Manager) listPartitions(ctx context.Context, spec TableSpec) ([]PartitionInfo, error) {
	names, err := m.exec.listPartitionNames(ctx, spec.Name)
	if err != nil {
		return nil, err
	}

	legacyName := spec.Name + "_legacy"
	defaultName := spec.Name + "_default"

	var cutoff time.Time
	haveCutoff := false
	for _, n := range names {
		if n == legacyName {
			c, err := m.exec.readCutoff(ctx, spec.Name)
			if err != nil {
				m.log.Warn("partition manager: cannot read cutoff for legacy partition; will not drop it this cycle", "table", spec.Name, "error", err.Error())
			} else {
				cutoff = c
				haveCutoff = true
			}
			break
		}
	}

	infos := make([]PartitionInfo, 0, len(names))
	for _, n := range names {
		switch {
		case n == defaultName:
			infos = append(infos, PartitionInfo{Name: n, IsDefault: true})
		case n == legacyName:
			if !haveCutoff {
				continue
			}
			infos = append(infos, PartitionInfo{Name: n, IsLegacy: true, UpperBound: cutoff})
		default:
			day, ok := ParsePartitionDay(n)
			if !ok || !strings.HasPrefix(n, spec.Name+"_p") {
				continue
			}
			infos = append(infos, PartitionInfo{Name: n, UpperBound: day.AddDate(0, 0, 1)})
		}
	}
	return infos, nil
}

// --- health bookkeeping ---

func (m *Manager) updateTableHealth(table string, furthest time.Time, defaultCount int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.health.Tables == nil {
		m.health.Tables = make(map[string]TableHealth)
	}
	m.health.Tables[table] = TableHealth{FurthestFuturePartition: furthest, DefaultRowCount: defaultCount}
}

func (m *Manager) recordRun(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	m.health.LastRunAt = now

	if err == nil {
		m.health.LastSuccessAt = now
		m.health.LastError = ""
		m.consecutiveFailures = 0
		m.stalled = false
		return
	}

	m.health.LastError = err.Error()
	m.consecutiveFailures++
	if m.consecutiveFailures >= stallThreshold && !m.stalled {
		m.stalled = true
		// Escalated once per stall episode via the default slog handler,
		// which server.go wires through the DB log handler — visible to the
		// operator without tailing stdout (AD-005, SEC-PART-001-005).
		m.log.Error("partition manager stalled", "consecutive_failures", m.consecutiveFailures, "error", err.Error())
	}
}

// --- helpers ---

// classifyBenign reports whether err is one of the expected, recoverable
// PostgreSQL errors that CREATE TABLE ... PARTITION OF can raise under
// normal operation (duplicate object, DEFAULT-capture, or range overlap).
// Classification is by SQLSTATE code only, never by message text (§12).
func classifyBenign(err error) (code string, benign bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}
	switch pgErr.Code {
	case sqlstateDuplicateObject, sqlstateCheckViolation, sqlstateInvalidObjectDefRel:
		return pgErr.Code, true
	default:
		return pgErr.Code, false
	}
}

// isDroppableName is a defense-in-depth gate (§10): DROP is only ever issued
// for a name matching the recognized "<table>_p<YYYYMMDD>" daily pattern or
// exactly "<table>_legacy". The DEFAULT partition ("<table>_default") is
// never droppable (AD-008).
func isDroppableName(table, name string) bool {
	if name == table+"_legacy" {
		return true
	}
	if name == table+"_default" {
		return false
	}
	return strings.HasPrefix(name, table+"_p") && dailyPartitionRe.MatchString(name)
}

// sqlIdent quotes a table/partition identifier for use in dynamically built
// DDL, mirroring the SQL-side format('%I', …) convention used in the
// migrations (SEC-PART-001-006). All identifiers passed here are built from
// the fixed in-code TableSpec list and the server's own wall clock — never
// from request or agent-supplied data (§10).
func sqlIdent(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// sqlLiteral formats a timestamp as a quoted SQL literal, mirroring the
// SQL-side format('%L', …) convention (SEC-PART-001-006). The value always
// originates from day-boundary date math, never from request or agent
// data.
func sqlLiteral(t time.Time) string {
	return "'" + t.UTC().Format(time.RFC3339Nano) + "'"
}
