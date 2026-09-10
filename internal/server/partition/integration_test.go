//go:build integration

// PART-001 integration tests. These exercise the real migration SQL
// (000001..000028) and the Manager against a LIVE PostgreSQL 16 instance —
// none of this can be verified with pure Go unit tests, and this dev host
// has no local Postgres. Build-tag gated so `go test ./...` (no tags) stays
// green with no DB configured (per the story instructions and security
// re-review residual R1).
//
// Run with:
//
//	RIOT_TEST_DB_URL=postgres://riot:riot@localhost:5432/riot_test?sslmode=disable \
//	  go test -tags=integration ./internal/server/partition/...
//
// If RIOT_TEST_DB_URL (or RIOT_DB_URL as a fallback) is not set, every test
// in this file calls t.Skip and the suite exits 0 — it never fails for lack
// of a database. See the PART-001 implementation report for which ACs are
// covered here and the residual R1 obligation that CI actually runs this
// file against a live Postgres.
package partition_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- harness ---

func testDBBaseURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("RIOT_TEST_DB_URL"); v != "" {
		return v
	}
	if v := os.Getenv("RIOT_DB_URL"); v != "" {
		return v
	}
	t.Skip("PART-001 integration test skipped: set RIOT_TEST_DB_URL (or RIOT_DB_URL) to a live PostgreSQL 16 instance to run")
	return ""
}

func migrationsSourceURL(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "cmd", "riot-server", "migrations")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	return "file://" + filepath.ToSlash(abs)
}

func withSearchPath(base, schema string) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%ssearch_path=%s", base, sep, schema)
}

// isolatedSchema creates a throwaway schema on the target database, returns
// a connection string scoped to it via search_path, and registers cleanup
// to drop the schema. Each test gets its own schema so migration state
// (including partition_conversion, golang-migrate's schema_migrations, and
// the six tables) never bleeds between tests.
func isolatedSchema(t *testing.T) (schemaURL string, pool *pgxpool.Pool) {
	t.Helper()
	base := testDBBaseURL(t)
	schema := fmt.Sprintf("part001_it_%d", time.Now().UnixNano())

	admin, err := pgxpool.New(context.Background(), base)
	if err != nil {
		t.Fatalf("connect (admin): %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(context.Background(), fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := pgxpool.New(context.Background(), base)
		if err != nil {
			return
		}
		defer cleanup.Close()
		cleanup.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA "%s" CASCADE`, schema))
	})

	schemaURL = withSearchPath(base, schema)
	pool, err = pgxpool.New(context.Background(), schemaURL)
	if err != nil {
		t.Fatalf("connect (scoped): %v", err)
	}
	t.Cleanup(pool.Close)
	return schemaURL, pool
}

// migrateTo applies migrations up to and including `version` (use ^uint(0)
// via migrateAll for "all the way").
func migrateTo(t *testing.T, dbURL string, version uint) {
	t.Helper()
	m, err := migrate.New(migrationsSourceURL(t), dbURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	defer m.Close()
	if err := m.Migrate(version); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to version %d: %v", version, err)
	}
}

func migrateAll(t *testing.T, dbURL string) {
	t.Helper()
	m, err := migrate.New(migrationsSourceURL(t), dbURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}
}

const versionBeforePartitioning = 23 // last version before PART-001 (000023_autovacuum_tuning)

func seedDevice(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO devices (id, short_id, hostname) VALUES ($1, $1, $1) ON CONFLICT (id) DO NOTHING`, id)
	if err != nil {
		t.Fatalf("seed device %s: %v", id, err)
	}
}

func seedProbe(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO probes (name, type, config) VALUES ('it-probe', 'http', '{}') RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("seed probe: %v", err)
	}
	return id
}

func isPartitioned(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_partitioned_table pt
			JOIN pg_class c ON c.oid = pt.partrelid
			WHERE c.relname = $1
		)`, table).Scan(&exists)
	if err != nil {
		t.Fatalf("check partitioned(%s): %v", table, err)
	}
	return exists
}

func relfilenode(t *testing.T, pool *pgxpool.Pool, relname string) string {
	t.Helper()
	var node string
	err := pool.QueryRow(context.Background(),
		`SELECT pg_relation_filenode(c.oid)::text FROM pg_class c WHERE c.relname = $1`, relname).Scan(&node)
	if err != nil {
		t.Fatalf("relfilenode(%s): %v", relname, err)
	}
	return node
}

// --- AC-008: fresh/empty install converts cleanly ---

func TestIntegration_AC008_FreshEmptyDatabase_ConvertsCleanly(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)

	tables := []string{"heartbeats", "telemetry_snapshots", "device_logs", "container_metrics", "container_logs", "probe_results"}
	for _, tbl := range tables {
		if !isPartitioned(t, pool, tbl) {
			t.Errorf("%s: not partitioned after conversion on a fresh/empty database", tbl)
		}
	}

	// The legacy and default partitions must exist, empty, with no error.
	for _, tbl := range tables {
		var legacyCount, defaultCount int64
		if err := pool.QueryRow(context.Background(), fmt.Sprintf(`SELECT count(*) FROM %s`, pgx.Identifier{tbl + "_legacy"}.Sanitize())).Scan(&legacyCount); err != nil {
			t.Errorf("%s_legacy: query failed: %v", tbl, err)
		}
		if err := pool.QueryRow(context.Background(), fmt.Sprintf(`SELECT count(*) FROM %s`, pgx.Identifier{tbl + "_default"}.Sanitize())).Scan(&defaultCount); err != nil {
			t.Errorf("%s_default: query failed: %v", tbl, err)
		}
		if legacyCount != 0 || defaultCount != 0 {
			t.Errorf("%s: expected empty legacy/default partitions on fresh install, got legacy=%d default=%d", tbl, legacyCount, defaultCount)
		}
	}
}

// --- AC-001 / AC-002: partition key column per table ---

func TestIntegration_AC001_AC002_TablesArePartitionedOnCorrectKey(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)

	cases := map[string]string{
		"heartbeats":          "timestamp",
		"telemetry_snapshots": "timestamp",
		"device_logs":         "timestamp",
		"container_metrics":   "timestamp",
		"container_logs":      "timestamp",
		"probe_results":       "created_at",
	}
	for tbl, wantKey := range cases {
		var key string
		err := pool.QueryRow(context.Background(), `
			SELECT a.attname
			FROM pg_partitioned_table pt
			JOIN pg_class c ON c.oid = pt.partrelid
			JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = pt.partattrs[0]
			WHERE c.relname = $1`, tbl).Scan(&key)
		if err != nil {
			t.Errorf("%s: partition key lookup failed: %v", tbl, err)
			continue
		}
		if key != wantKey {
			t.Errorf("%s: partition key = %q, want %q", tbl, key, wantKey)
		}
	}
}

// --- AC-003: composite primary key includes the partition key, id stays unique ---

func TestIntegration_AC003_CompositePrimaryKeyIncludesPartitionKey(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)
	seedDevice(t, pool, "dev-ac003")

	var cols []string
	rows, err := pool.Query(context.Background(), `
		SELECT a.attname
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
		WHERE c.relname = 'heartbeats' AND con.contype = 'p'
		ORDER BY k.ord`)
	if err != nil {
		t.Fatalf("query PK columns: %v", err)
	}
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, col)
	}
	rows.Close()

	if len(cols) != 2 || cols[0] != "id" || cols[1] != "timestamp" {
		t.Fatalf("heartbeats PK columns = %v, want [id timestamp]", cols)
	}

	// id remains unique in practice across two inserts.
	var id1, id2 int64
	now := time.Now().UTC()
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1,$2,'{}') RETURNING id`, "dev-ac003", now).Scan(&id1); err != nil {
		t.Fatalf("insert 1: %v", err)
	}
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1,$2,'{}') RETURNING id`, "dev-ac003", now).Scan(&id2); err != nil {
		t.Fatalf("insert 2: %v", err)
	}
	if id1 == id2 {
		t.Fatalf("id sequence did not yield unique values: %d == %d", id1, id2)
	}
}

// --- AC-004: every pre-existing index is preserved on the parent ---

func TestIntegration_AC004_AllPreExistingIndexesPreserved(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)

	want := []struct{ table, index string }{
		{"heartbeats", "idx_heartbeats_device_time"},
		{"telemetry_snapshots", "idx_telemetry_device_time"},
		{"device_logs", "idx_device_logs_device_ts"},
		{"device_logs", "idx_device_logs_priority"},
		{"device_logs", "idx_device_logs_dedup"},
		{"container_metrics", "idx_container_metrics_lookup"},
		{"container_metrics", "idx_container_metrics_time"},
		{"container_logs", "idx_container_logs_lookup"},
		{"container_logs", "idx_container_logs_time"},
		{"probe_results", "idx_probe_results_probe_time"},
	}
	for _, w := range want {
		var exists bool
		err := pool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_index i
				JOIN pg_class ic ON ic.oid = i.indexrelid
				JOIN pg_class tc ON tc.oid = i.indrelid
				WHERE tc.relname = $1 AND ic.relname = $2
			)`, w.table, w.index).Scan(&exists)
		if err != nil {
			t.Errorf("%s.%s: lookup failed: %v", w.table, w.index, err)
			continue
		}
		if !exists {
			t.Errorf("%s: expected index %s on the partitioned parent, not found", w.table, w.index)
		}
	}
}

// --- AC-005: foreign keys preserved; container_logs stays FK-free ---

func TestIntegration_AC005_ForeignKeysPreservedContainerLogsFKFree(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)

	fkTables := []string{"heartbeats", "telemetry_snapshots", "device_logs", "container_metrics", "probe_results"}
	for _, tbl := range fkTables {
		var validated bool
		err := pool.QueryRow(context.Background(), `
			SELECT con.convalidated
			FROM pg_constraint con
			JOIN pg_class c ON c.oid = con.conrelid
			WHERE c.relname = $1 AND con.contype = 'f'`, tbl).Scan(&validated)
		if err != nil {
			t.Errorf("%s: FK lookup failed (expected one FK): %v", tbl, err)
			continue
		}
		if !validated {
			t.Errorf("%s: FK exists but is not validated after 000028", tbl)
		}
	}

	var fkCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		WHERE c.relname = 'container_logs' AND con.contype = 'f'`).Scan(&fkCount); err != nil {
		t.Fatalf("container_logs FK count: %v", err)
	}
	if fkCount != 0 {
		t.Fatalf("container_logs has %d foreign key(s), want 0 (FR-004/A-006)", fkCount)
	}
}

// --- AC-006 / AC-016 / AC-019: metadata swap, no row copy, relfilenode unchanged, AEL row-count-independent ---

func TestIntegration_AC006_AC016_AC019_SwapIsMetadataOnly_NoRewrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows int
	}{
		{"tiny_fixture", 10},
		{"large_fixture", 5000},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			dbURL, pool := isolatedSchema(t)
			migrateTo(t, dbURL, versionBeforePartitioning)
			seedDevice(t, pool, "dev-relfilenode")

			// Populate container_logs (representative of the 28 GB production
			// case) before the swap runs.
			batch := &pgx.Batch{}
			now := time.Now().UTC()
			for i := 0; i < tc.rows; i++ {
				batch.Queue(`INSERT INTO container_logs (device_id, container_id, container_name, timestamp, stream, line)
					VALUES ($1,$2,$3,$4,'stdout',$5)`,
					"dev-relfilenode", "c1", "web", now.Add(-time.Duration(i)*time.Minute), fmt.Sprintf("line %d", i))
			}
			br := pool.SendBatch(context.Background(), batch)
			if err := br.Close(); err != nil {
				t.Fatalf("seed container_logs: %v", err)
			}

			// Capture the pre-swap heap's relfilenode (still named
			// container_logs before 000027 renames it).
			preNode := relfilenode(t, pool, "container_logs")
			preIndexNode := relfilenode(t, pool, "idx_container_logs_lookup")

			migrateAll(t, dbURL) // runs 000024..000028, including the swap in 000027

			var rowCount int64
			if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM container_logs_legacy`).Scan(&rowCount); err != nil {
				t.Fatalf("count container_logs_legacy: %v", err)
			}
			if rowCount != int64(tc.rows) {
				t.Fatalf("container_logs_legacy row count = %d, want %d (rows must not be lost or duplicated by the swap)", rowCount, tc.rows)
			}

			// The legacy child's original lookup index is renamed out of the
			// way (to free the canonical name for the parent's own index
			// shell — see 000027's comments); check the RENAMED child index,
			// not the post-swap parent name (which is a physical-storage-less
			// partitioned-index shell with no relfilenode of its own).
			postNode := relfilenode(t, pool, "container_logs_legacy")
			postIndexNode := relfilenode(t, pool, "container_logs_legacy_lookup")

			if postNode != preNode {
				t.Fatalf("container_logs heap relfilenode changed across the swap (%s -> %s): the conversion rewrote the table, violating NFR-001", preNode, postNode)
			}
			if postIndexNode != preIndexNode {
				t.Fatalf("idx_container_logs_lookup relfilenode changed across the swap (%s -> %s): the index was rebuilt instead of adopted, violating NFR-001/AD-004", preIndexNode, postIndexNode)
			}
		})
	}
}

// --- AC-007: idempotent / resumable re-run ---

func TestIntegration_AC007_ConversionIsIdempotentOnReRun(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)

	// Re-running the full migration path is a no-op and produces no error.
	migrateAll(t, dbURL)

	if !isPartitioned(t, pool, "container_logs") {
		t.Fatal("container_logs not partitioned after a repeated migrate-up")
	}

	var partCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'container_logs'`).Scan(&partCount); err != nil {
		t.Fatalf("count partitions: %v", err)
	}
	// legacy + default, at minimum, and no duplicates from the re-run.
	if partCount < 2 {
		t.Fatalf("container_logs has %d partitions after a repeated migrate-up, want at least legacy+default", partCount)
	}
}

// --- AC-009: existing insert/read paths continue to work unchanged ---

func TestIntegration_AC009_ExistingQueriesContinueToWork(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)
	seedDevice(t, pool, "dev-ac009")

	now := time.Now().UTC()

	// Plain INSERT (heartbeats).
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1,$2,'{}')`, "dev-ac009", now); err != nil {
		t.Fatalf("insert heartbeat: %v", err)
	}

	// INSERT ... RETURNING id (probe_results).
	probeID := seedProbe(t, pool)
	var resultID int64
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO probe_results (probe_id, success, latency_ms, metadata, created_at) VALUES ($1,true,1.5,'{}',$2) RETURNING id`,
		probeID, now).Scan(&resultID); err != nil {
		t.Fatalf("insert probe_result: %v", err)
	}

	// pgx.CopyFrom (container_logs, container_metrics).
	if _, err := pool.CopyFrom(context.Background(),
		pgx.Identifier{"container_logs"},
		[]string{"device_id", "container_id", "container_name", "timestamp", "stream", "line"},
		pgx.CopyFromRows([][]interface{}{{"dev-ac009", "c1", "web", now, "stdout", "hello"}})); err != nil {
		t.Fatalf("CopyFrom container_logs: %v", err)
	}

	// device_logs staged INSERT ... SELECT ... ON CONFLICT dedup path.
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `CREATE TEMP TABLE device_logs_stage (
		device_id TEXT, timestamp TIMESTAMPTZ, priority INT, unit TEXT, message TEXT) ON COMMIT DROP`); err != nil {
		t.Fatalf("create stage: %v", err)
	}
	if _, err := tx.CopyFrom(context.Background(), pgx.Identifier{"device_logs_stage"},
		[]string{"device_id", "timestamp", "priority", "unit", "message"},
		pgx.CopyFromRows([][]interface{}{{"dev-ac009", now, 4, "sshd", "test message"}})); err != nil {
		t.Fatalf("copy into stage: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO device_logs (device_id, timestamp, priority, unit, message)
		SELECT device_id, timestamp, priority, unit, message FROM device_logs_stage
		ON CONFLICT (device_id, timestamp, priority, unit, (md5(message))) DO NOTHING`); err != nil {
		t.Fatalf("dedup insert: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Reads: latest snapshot / per-device lookups / range scans all still work.
	var hbCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM heartbeats WHERE device_id=$1`, "dev-ac009").Scan(&hbCount); err != nil {
		t.Fatalf("read heartbeats: %v", err)
	}
	if hbCount != 1 {
		t.Fatalf("heartbeats count = %d, want 1", hbCount)
	}

	var logCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM device_logs WHERE device_id=$1`, "dev-ac009").Scan(&logCount); err != nil {
		t.Fatalf("read device_logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("device_logs count = %d, want 1 (dedup insert path broken by partitioning)", logCount)
	}
}

// --- AC-007 / AC-018: full up -> down -> up cycle across all six tables ---
//
// AC-018's other test (TestIntegration_AC018_DownMigrationBestEffortReversal)
// exercises the down-migration data-preservation contract in depth on one
// table (heartbeats); this test is a breadth check confirming the identical
// rename/PK/index-restore pattern in every table's down migration is
// self-consistent for all six tables, not just the one exercised in depth.

func TestIntegration_AC007_AC018_FullUpDownUpCycle_AllSixTables(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)

	tables := []string{"heartbeats", "telemetry_snapshots", "device_logs", "container_metrics", "container_logs", "probe_results"}
	for _, tbl := range tables {
		if !isPartitioned(t, pool, tbl) {
			t.Fatalf("%s not partitioned after up", tbl)
		}
	}

	migrateTo(t, dbURL, versionBeforePartitioning)
	for _, tbl := range tables {
		if isPartitioned(t, pool, tbl) {
			t.Fatalf("%s still partitioned after down", tbl)
		}
		var cnt int
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+tbl).Scan(&cnt); err != nil {
			t.Fatalf("%s: query against the restored plain table failed: %v", tbl, err)
		}
	}

	migrateAll(t, dbURL)
	for _, tbl := range tables {
		if !isPartitioned(t, pool, tbl) {
			t.Fatalf("%s not partitioned after re-up (idempotent re-conversion after a down/up cycle)", tbl)
		}
	}
}

// --- AC-018: down migration performs the documented best-effort reversal ---

func TestIntegration_AC018_DownMigrationBestEffortReversal(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)
	seedDevice(t, pool, "dev-ac018")

	now := time.Now().UTC()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1,$2,'{}')`, "dev-ac018", now.Add(-30*24*time.Hour)); err != nil {
		t.Fatalf("seed legacy-range heartbeat: %v", err)
	}

	migrateTo(t, dbURL, versionBeforePartitioning) // walks 000028..000024 down in order

	if isPartitioned(t, pool, "heartbeats") {
		t.Fatal("heartbeats still partitioned after down-migrating past 000027")
	}

	var pkCols int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		WHERE c.relname = 'heartbeats' AND con.contype = 'p' AND con.conkey = ARRAY[
			(SELECT attnum FROM pg_attribute WHERE attrelid = c.oid AND attname = 'id')
		]`).Scan(&pkCols); err != nil {
		t.Fatalf("check restored single-column PK: %v", err)
	}
	if pkCols != 1 {
		t.Fatal("heartbeats does not have its original single-column (id) primary key restored after down migration")
	}

	// Pre-existing (legacy-range) data must survive the reversal.
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM heartbeats WHERE device_id=$1`, "dev-ac018").Scan(&count); err != nil {
		t.Fatalf("read heartbeats after down: %v", err)
	}
	if count != 1 {
		t.Fatalf("heartbeats row count after down migration = %d, want 1 (legacy data must survive the best-effort reversal)", count)
	}
}

// --- AC-021b: a CopyFrom batch containing an out-of-margin future row succeeds via the DEFAULT catch ---

func TestIntegration_AC021b_CopyFromWithFutureSkewedRowSucceedsViaDefault(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateAll(t, dbURL)
	seedDevice(t, pool, "dev-ac021b")

	now := time.Now().UTC()
	skewed := now.AddDate(0, 0, 10) // 10 days ahead — beyond the 3-day create-ahead margin

	rows := [][]interface{}{
		{"dev-ac021b", "c1", "web", now, "stdout", "well-dated line"},
		{"dev-ac021b", "c1", "web", skewed, "stdout", "clock-skewed line"},
	}
	if _, err := pool.CopyFrom(context.Background(),
		pgx.Identifier{"container_logs"},
		[]string{"device_id", "container_id", "container_name", "timestamp", "stream", "line"},
		pgx.CopyFromRows(rows)); err != nil {
		t.Fatalf("CopyFrom with a future-skewed row failed the whole batch (NFR-002 violated): %v", err)
	}

	var total int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM container_logs WHERE device_id=$1`, "dev-ac021b").Scan(&total); err != nil {
		t.Fatalf("count container_logs: %v", err)
	}
	if total != 2 {
		t.Fatalf("container_logs count = %d, want 2 (both rows of the batch must be inserted)", total)
	}

	var inDefault int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM container_logs_default WHERE device_id=$1`, "dev-ac021b").Scan(&inDefault); err != nil {
		t.Fatalf("count container_logs_default: %v", err)
	}
	if inDefault != 1 {
		t.Fatalf("container_logs_default count = %d, want 1 (the skewed row must land in the DEFAULT catch)", inDefault)
	}
}

// --- AC-022: cutoff derived from max(key); VALIDATE cannot fail on a future-dated row; empty fallback; dirty-recovery ---

func TestIntegration_AC022a_CutoffDerivedFromMaxKey_ValidatesWithFutureDatedRow(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateTo(t, dbURL, versionBeforePartitioning)
	seedDevice(t, pool, "dev-ac022a")

	future := time.Now().UTC().AddDate(0, 0, 60)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1,$2,'{}')`, "dev-ac022a", future); err != nil {
		t.Fatalf("seed future-dated heartbeat: %v", err)
	}

	migrateAll(t, dbURL) // must NOT fail VALIDATE CONSTRAINT in 000026

	var cutoff time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT cutoff FROM partition_conversion WHERE table_name = 'heartbeats'`).Scan(&cutoff); err != nil {
		t.Fatalf("read persisted cutoff: %v", err)
	}
	if !cutoff.After(future) {
		t.Fatalf("persisted cutoff %v is not strictly after the seeded future row %v", cutoff, future)
	}
}

func TestIntegration_AC022b_EmptyTableCutoffFallsBackToNow(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	before := time.Now().UTC()
	migrateAll(t, dbURL)
	after := time.Now().UTC()

	var cutoff time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT cutoff FROM partition_conversion WHERE table_name = 'probe_results'`).Scan(&cutoff); err != nil {
		t.Fatalf("read persisted cutoff: %v", err)
	}
	// cutoff = date_trunc('day', now()) + 1 day; must fall within (today's
	// floor, today's floor + 1 day] relative to the migration's execution window.
	lowerBound := time.Date(before.Year(), before.Month(), before.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	upperBound := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 2)
	if cutoff.Before(lowerBound) || cutoff.After(upperBound) {
		t.Fatalf("empty-table cutoff = %v, want within [%v, %v] (now-based day-aligned fallback)", cutoff, lowerBound, upperBound)
	}
}

func TestIntegration_AC022c_DirtyPreSwapReRunRecomputesCutoff(t *testing.T) {
	dbURL, pool := isolatedSchema(t)
	migrateTo(t, dbURL, versionBeforePartitioning)
	seedDevice(t, pool, "dev-ac022c")

	early := time.Now().UTC().AddDate(0, 0, -5)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1,$2,'{}')`, "dev-ac022c", early); err != nil {
		t.Fatalf("seed early heartbeat: %v", err)
	}
	migrateTo(t, dbURL, versionBeforePartitioning+1) // 000024 only

	// Directly corrupt the persisted cutoff to an obviously stale/wrong
	// value, simulating exactly the failure mode the security review
	// (SEC-PART-001-001) found in the pre-revision ADD: a bad/stale cutoff
	// surviving in partition_conversion across a re-run because the old
	// design used `ON CONFLICT DO NOTHING`. If file 000024's guarded DO
	// block still uses `DO NOTHING` (the bug), this stale value would
	// survive the re-run below; the fixed `DO UPDATE` must overwrite it.
	staleCutoff := time.Now().UTC().AddDate(0, 0, -100)
	if _, err := pool.Exec(context.Background(),
		`UPDATE partition_conversion SET cutoff = $1 WHERE table_name = 'heartbeats'`, staleCutoff); err != nil {
		t.Fatalf("inject stale cutoff: %v", err)
	}

	// Documented recovery procedure (§12): force back to the last-good
	// version and re-run. This re-executes file 000024's guarded DO block.
	m, err := migrate.New(migrationsSourceURL(t), dbURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	defer m.Close()
	if err := m.Force(int(versionBeforePartitioning)); err != nil {
		t.Fatalf("force back to %d: %v", versionBeforePartitioning, err)
	}
	if err := m.Migrate(versionBeforePartitioning + 1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-run 000024: %v", err)
	}

	var recomputed time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT cutoff FROM partition_conversion WHERE table_name = 'heartbeats'`).Scan(&recomputed); err != nil {
		t.Fatalf("read recomputed cutoff: %v", err)
	}
	if recomputed.Equal(staleCutoff) {
		t.Fatalf("recomputed cutoff %v equals the injected stale value — file 000024 did not recompute from live data (the sticky-cutoff recovery bug, SEC-PART-001-001, is present)", recomputed)
	}
	if !recomputed.After(early) {
		t.Fatalf("recomputed cutoff %v is not strictly after the live seeded row %v", recomputed, early)
	}

	// Recovery completes: the rest of the conversion applies cleanly with
	// the recomputed bound (VALIDATE in 000026 must not fail — it would
	// have, had the stale/too-early bound survived, since a row at `early`
	// existed and the stale bound predates it).
	migrateAll(t, dbURL)
	if !isPartitioned(t, pool, "heartbeats") {
		t.Fatal("heartbeats not partitioned after completing the dirty-recovery re-run")
	}
}
