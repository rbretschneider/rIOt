package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MigrationFingerprint is intentionally free of database connection identifiers.
type MigrationFingerprint struct {
	Version        int64  `json:"version"`
	Dirty          bool   `json:"dirty"`
	Lifecycle      bool   `json:"f_lc"`
	Autovacuum     bool   `json:"f_av"`
	Partitioned    bool   `json:"f_pt"`
	Classification string `json:"classification"`
	Action         string `json:"action"`
}

func ClassifyMigrationState(f MigrationFingerprint) (string, string) {
	if f.Dirty {
		return "dirty", "stop"
	}
	if f.Version == 0 && !f.Lifecycle && !f.Autovacuum && !f.Partitioned {
		return "fresh", "upgrade"
	}
	if f.Version == 22 && !f.Lifecycle && !f.Autovacuum && !f.Partitioned {
		return "main_before_lifecycle", "upgrade"
	}
	if f.Version >= 23 && f.Lifecycle {
		if f.Version == 23 && f.Partitioned {
			return "mixed", "stop"
		}
		return "main", "upgrade"
	}
	if f.Version == 23 && f.Autovacuum && !f.Lifecycle && !f.Partitioned {
		return "branch_autovacuum", "operator_reconciliation_required"
	}
	if f.Version >= 24 && f.Version <= 28 && f.Partitioned && !f.Lifecycle {
		return "branch_partitioning", "operator_reconciliation_required"
	}
	return "unexpected", "stop"
}

// ReadMigrationFingerprint creates no schema objects and takes no migration lock.
func ReadMigrationFingerprint(ctx context.Context, conn *pgx.Conn) (MigrationFingerprint, error) {
	f := MigrationFingerprint{}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return f, err
	}
	defer tx.Rollback(ctx)
	var table *string
	if err = tx.QueryRow(ctx, `SELECT to_regclass('schema_migrations')::text`).Scan(&table); err != nil {
		return f, err
	}
	if table != nil {
		if err = tx.QueryRow(ctx, `SELECT version,dirty FROM schema_migrations LIMIT 1`).Scan(&f.Version, &f.Dirty); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return f, err
		}
	}
	err = tx.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='commands' AND column_name='deadline_at') AND
 EXISTS(SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid=c.conrelid JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname=current_schema() AND c.conname='commands_origin_check'),
 EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL unnest(c.reloptions) o WHERE n.nspname=current_schema() AND c.relname IN ('container_logs','container_logs_legacy') AND o LIKE 'autovacuum_vacuum_scale_factor=%'),
 to_regclass('partition_conversion') IS NOT NULL OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='container_logs' AND c.relkind='p')`).Scan(&f.Lifecycle, &f.Autovacuum, &f.Partitioned)
	if err != nil {
		return f, err
	}
	f.Classification, f.Action = ClassifyMigrationState(f)
	return f, tx.Commit(ctx)
}

func maintenanceErrorClass(err error) string {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && (pgerr.Code == "28P01" || pgerr.Code == "28000") {
		return "auth"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return "timeout"
		}
		return "unreachable"
	}
	return "other"
}

// Maintenance runs before server construction. It never serves HTTP and never
// forces a migration version. Operators reconcile branch histories explicitly.
func Maintenance(ctx context.Context, mode, connStr string, migrations fs.FS, output io.Writer) int {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	fail := func(err error) int {
		fmt.Fprintf(output, "{\"error\":\"database maintenance failed\",\"class\":%q}\n", maintenanceErrorClass(err))
		return 1
	}
	cfg, err := pgx.ParseConfig(connStr)
	if err != nil {
		return fail(err)
	}
	cfg.ConnectTimeout = 10 * time.Second
	cfg.RuntimeParams["application_name"] = "riot-server-" + mode
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fail(err)
	}
	defer conn.Close(ctx)
	f, err := ReadMigrationFingerprint(ctx, conn)
	if err != nil {
		return fail(err)
	}
	if f.Action != "upgrade" {
		json.NewEncoder(output).Encode(f)
		return 1
	}
	if mode == "migrate" {
		if err = (&DB{}).RunMigrations(migrations, connStr); err != nil {
			return fail(err)
		}
		f, err = ReadMigrationFingerprint(ctx, conn)
		if err != nil {
			return fail(err)
		}
	}
	if err = json.NewEncoder(output).Encode(f); err != nil {
		return 1
	}
	return 0
}
