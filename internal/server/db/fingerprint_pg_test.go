package db

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDUX_SEC030_FingerprintReadOnlySession(t *testing.T) {
	e := newPGEnv(t, 0)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, e.BaseURL+"&search_path="+e.Schema)
	require.NoError(t, err)
	defer conn.Close(ctx)
	f, err := ReadMigrationFingerprint(ctx, conn)
	require.NoError(t, err)
	require.True(t, f.Lifecycle)
	require.True(t, f.Partitioned)
	require.Equal(t, "upgrade", f.Action)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `CREATE TABLE must_not_exist(id int)`)
	require.Error(t, err)
	tx.Rollback(ctx)
}

func TestDUX_SEC030_FingerprintCreatesNothing(t *testing.T) {
	e := newPGEnv(t, 22)
	e.Exec(`DROP TABLE schema_migrations`)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, e.BaseURL+"&search_path="+e.Schema)
	require.NoError(t, err)
	defer conn.Close(ctx)
	_, err = ReadMigrationFingerprint(ctx, conn)
	require.NoError(t, err)
	var exists bool
	require.NoError(t, e.Pool.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists))
	require.False(t, exists)
}

func TestDUX_SEC044_MigrateOutputHasNoDSN(t *testing.T) {
	for _, mode := range []string{"migrate", "db-fingerprint"} {
		var out bytes.Buffer
		code := Maintenance(context.Background(), mode, "postgres://secret_user:secret_password@bad%host/secret_database", os.DirFS("../../../cmd/riot-server/migrations"), &out)
		require.NotZero(t, code)
		for _, secret := range []string{"secret_user", "secret_password", "bad%host", "secret_database"} {
			require.NotContains(t, out.String(), secret)
		}
	}
}

func TestDUX_SEC022_RenumberedPart001NoOpOnConvertedDB(t *testing.T) {
	e := newPGEnv(t, 22)
	ctx := context.Background()
	e.seedDevice("migration-fixture")
	e.Exec(`INSERT INTO container_logs(device_id,container_id,container_name,timestamp,stream,line) VALUES('migration-fixture','0123456789ab','web',now(),'stdout','preserve this log')`)
	fixtures := fstest.MapFS{}
	for _, root := range []string{"../../../cmd/riot-server/migrations", "testdata/part001_branch"} {
		files, err := fs.ReadDir(os.DirFS(root), ".")
		require.NoError(t, err)
		for _, file := range files {
			if root == "../../../cmd/riot-server/migrations" && file.Name() >= "000023" {
				continue
			}
			body, err := os.ReadFile(root + "/" + file.Name())
			require.NoError(t, err)
			fixtures[file.Name()] = &fstest.MapFile{Data: body}
		}
	}
	source, err := iofs.New(fixtures, ".")
	require.NoError(t, err)
	branch, err := migrate.NewWithSourceInstance("iofs", source, e.BaseURL+"&search_path="+e.Schema)
	require.NoError(t, err)
	defer branch.Close()
	require.NoError(t, branch.Up())
	var before string
	require.NoError(t, e.Pool.QueryRow(ctx, `SELECT string_agg(table_name||cutoff::text,',' ORDER BY table_name) FROM partition_conversion`).Scan(&before))
	require.NoError(t, e.Mig.Force(22))
	require.NoError(t, e.Mig.Up())
	var after, line string
	require.NoError(t, e.Pool.QueryRow(ctx, `SELECT string_agg(table_name||cutoff::text,',' ORDER BY table_name) FROM partition_conversion`).Scan(&after))
	require.Equal(t, before, after)
	require.NoError(t, e.Pool.QueryRow(ctx, `SELECT line FROM container_logs WHERE device_id='migration-fixture'`).Scan(&line))
	require.Equal(t, "preserve this log", line)
	version, dirty, err := e.Mig.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	require.GreaterOrEqual(t, version, uint(32))
}

func TestDUX_SEC043_TrimmedSnapshotSectionsReadAsUnknown(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("trim-fixture")
	e.Exec(`INSERT INTO telemetry_snapshots(device_id,timestamp,data) VALUES('trim-fixture',now()-interval '1 minute','{"security":{"firewall_active":true},"updates":{"pending_count":3}}'),('trim-fixture',now(),'{"trimmed":true,"dropped_sections":["security","updates"],"cpu":{"usage_percent":12}}')`)
	repo := NewTelemetryRepo(&DB{Pool: e.Pool})
	snap, err := repo.GetLatestSnapshot(context.Background(), "trim-fixture")
	require.NoError(t, err)
	require.True(t, snap.Data.Trimmed)
	require.NotNil(t, snap.Data.Security)
	require.NotNil(t, snap.Data.Updates)
	summaries, err := repo.GetAllLatestSummaries(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, summaries)
	require.NotNil(t, summaries[0].Security)
	require.NotNil(t, summaries[0].Updates)
}
