package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pgTestAllowed decides whether the PG-gated tests may run against rawURL
// (SEC C-10): the database name must contain "test" (case-insensitive), or the
// explicit override RIOT_TEST_PG_ALLOW=1 must be set. The tests create schemas
// and run up/down migrations, so a developer who exported a production URL by
// mistake must get a skip, not a migration run against production.
func pgTestAllowed(rawURL, allowEnv string) bool {
	if allowEnv == "1" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	name := strings.TrimPrefix(u.Path, "/")
	return strings.Contains(strings.ToLower(name), "test")
}

// pgTestURL returns RIOT_TEST_PG_URL, or skips the test when it is unset or
// does not point at a test database. Every PG-gated test calls it first.
func pgTestURL(t *testing.T) string {
	t.Helper()
	raw := os.Getenv("RIOT_TEST_PG_URL")
	if raw == "" {
		t.Skip("RIOT_TEST_PG_URL not set; skipping PostgreSQL-gated test")
	}
	if !pgTestAllowed(raw, os.Getenv("RIOT_TEST_PG_ALLOW")) {
		t.Skip("RIOT_TEST_PG_URL does not name a test database (name must contain \"test\"); set RIOT_TEST_PG_ALLOW=1 to override")
	}
	return raw
}

func TestCL_SEC013_PGTestsSkipOnNonTestDatabase(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		allow string
		want  bool
	}{
		{"test database name", "postgres://u:p@localhost:5432/riot_test?sslmode=disable", "", true},
		{"upper case test", "postgres://u:p@localhost:5432/RIOT_TEST_2", "", true},
		{"production-looking name", "postgres://u:p@prod-db.example.com:5432/riot?sslmode=disable", "", false},
		{"test only in host", "postgres://u:p@testhost:5432/riot", "", false},
		{"test only in user", "postgres://test:p@db:5432/riot", "", false},
		{"test only in query", "postgres://u:p@db:5432/riot?application_name=test", "", false},
		{"override allows any name", "postgres://u:p@prod-db.example.com:5432/riot", "1", true},
		{"override must be exactly 1", "postgres://u:p@prod-db.example.com:5432/riot", "true", false},
		{"no database name", "postgres://u:p@db:5432", "", false},
		{"unparseable url", "://not a url", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pgTestAllowed(tc.url, tc.allow),
				"[SEC-013] pgTestAllowed(%q, %q)", tc.url, tc.allow)
		})
	}
}

// pgEnv is a throwaway schema with the migrations applied up to some version.
type pgEnv struct {
	T       *testing.T
	Pool    *pgxpool.Pool
	Mig     *migrate.Migrate
	Schema  string
	BaseURL string
}

// newPGEnv creates a unique schema cl_<random>, migrates it to `version`
// (0 means the latest) and drops the schema in cleanup.
func newPGEnv(t *testing.T, version uint) *pgEnv {
	t.Helper()
	raw := pgTestURL(t)
	ctx := context.Background()

	buf := make([]byte, 6)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	schema := "cl_" + hex.EncodeToString(buf)

	admin, err := pgxpool.New(ctx, raw)
	require.NoError(t, err, "connect admin pool")
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)

	cfg, err := pgxpool.ParseConfig(raw)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err, "connect schema pool")

	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	source, err := iofs.New(os.DirFS("../../../cmd/riot-server/migrations"), ".")
	require.NoError(t, err, "open migrations")
	mig, err := migrate.NewWithSourceInstance("iofs", source, u.String())
	require.NoError(t, err, "create migrator")

	t.Cleanup(func() {
		pool.Close()
		srcErr, dbErr := mig.Close()
		assert.NoError(t, srcErr)
		assert.NoError(t, dbErr)
		_, dropErr := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		assert.NoError(t, dropErr, "drop schema %s", schema)
		admin.Close()
	})

	env := &pgEnv{T: t, Pool: pool, Mig: mig, Schema: schema, BaseURL: raw}
	if version == 0 {
		require.NoError(t, mig.Up(), "migrate up")
	} else {
		require.NoError(t, mig.Migrate(version), "migrate to %d", version)
	}
	return env
}

// Exec runs a statement in the schema and fails the test on error.
func (e *pgEnv) Exec(sql string, args ...interface{}) {
	e.T.Helper()
	_, err := e.Pool.Exec(context.Background(), sql, args...)
	require.NoError(e.T, err, sql)
}

// seedDevice inserts a device row (commands reference devices).
func (e *pgEnv) seedDevice(id string) {
	e.T.Helper()
	e.Exec(`INSERT INTO devices (id, short_id, hostname) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		id, id[:min(len(id), 8)], "host-"+id)
}

// withDeadline gives a test a bounded context.
func withDeadline(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
