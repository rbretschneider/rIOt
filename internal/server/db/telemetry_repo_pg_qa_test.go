package db

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer (REG-HARDEN-001). Covers AC-026 (SQL half), AC-032,
// AC-033 and security condition C9 (ADD section 8.1) against a real PostgreSQL.
//
// This test runs ONLY when RIOT_QA_DISPOSABLE_DB_URL is set, and refuses any host
// other than localhost / 127.0.0.1 / ::1. It applies the repository migrations to
// that database and inserts rows with device IDs prefixed "qa-". Point it at a
// throwaway container, never at a real deployment:
//
//	docker run --rm -d --name riot-qa-pg -e POSTGRES_PASSWORD=qa -p 127.0.0.1:55499:5432 postgres:16
//	RIOT_QA_DISPOSABLE_DB_URL=postgres://postgres:qa@127.0.0.1:55499/postgres?sslmode=disable \
//	  go test ./internal/server/db -run PGDisposable -v

func disposableDB(t *testing.T) (*DB, context.Context) {
	t.Helper()
	raw := os.Getenv("RIOT_QA_DISPOSABLE_DB_URL")
	if raw == "" {
		t.Skip("RIOT_QA_DISPOSABLE_DB_URL not set; skipping the disposable-database check (C9)")
	}
	u, err := url.Parse(raw)
	require.NoError(t, err)
	host := u.Hostname()
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, host,
		"refusing to run against non-local host %q: this test must only touch a disposable database", host)

	ctx := context.Background()
	d, err := New(ctx, raw)
	require.NoError(t, err)
	t.Cleanup(d.Close)
	require.NoError(t, d.RunMigrations(os.DirFS("../../../cmd/riot-server/migrations"), raw))
	return d, ctx
}

func qaSeedDevice(t *testing.T, ctx context.Context, d *DB, id string) {
	t.Helper()
	_, err := d.Pool.Exec(ctx, `INSERT INTO devices (id, short_id, hostname) VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING`, id, id[:min(8, len(id))], id)
	require.NoError(t, err)
}

// qaSnap inserts a snapshot row at NOW() + offset.
func qaSnap(t *testing.T, ctx context.Context, d *DB, device, offset string, data map[string]any) {
	t.Helper()
	b, err := json.Marshal(data)
	require.NoError(t, err)
	_, err = d.Pool.Exec(ctx,
		`INSERT INTO telemetry_snapshots (device_id, timestamp, data) VALUES ($1, NOW() + $2::interval, $3)`,
		device, offset, b)
	require.NoError(t, err)
}

func qaHB(t *testing.T, ctx context.Context, d *DB, device, offset string, uptime int) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"uptime": uptime})
	_, err := d.Pool.Exec(ctx,
		`INSERT INTO heartbeats (device_id, timestamp, data) VALUES ($1, NOW() + $2::interval, $3)`,
		device, offset, b)
	require.NoError(t, err)
}

func TestTelemetryRepo_PGDisposable_AC032_AC033_C9(t *testing.T) {
	d, ctx := disposableDB(t)
	repo := NewTelemetryRepo(d)

	// Clean slate for the qa- devices only (disposable DB; the cascade removes their rows).
	_, err := d.Pool.Exec(ctx, `DELETE FROM devices WHERE id LIKE 'qa-%'`)
	require.NoError(t, err)

	const (
		devD  = "qa-dev-d"      // future(+1y) + past(-1h)
		devF  = "qa-dev-future" // ONLY future rows (C9a)
		devW  = "qa-dev-window" // +6m, +4m, -1h
		devG  = "qa-dev-gpu"    // past has a GPU, future row has none (fallback)
		devGF = "qa-dev-gpufut" // future row has a GPU, past row has none
		devC  = "qa-dev-cont"   // past and future container lists differ
	)
	for _, id := range []string{devD, devF, devW, devG, devGF, devC} {
		qaSeedDevice(t, ctx, d, id)
	}

	sys := func(name string) map[string]any { return map[string]any{"system": map[string]any{"hostname": name}} }
	sec := func(n int) map[string]any {
		m := sys("h")
		m["security"] = map[string]any{"failed_logins_24h": n}
		return m
	}

	// ADD 8.1 step 1.
	qaSnap(t, ctx, d, devD, "1 year", sec(999))
	qaSnap(t, ctx, d, devD, "-1 hour", sec(1))
	qaHB(t, ctx, d, devD, "1 day", 999)
	qaHB(t, ctx, d, devD, "-5 minutes", 1)

	// C9(a): only future rows.
	qaSnap(t, ctx, d, devF, "1 year", sec(999))
	qaSnap(t, ctx, d, devF, "2 days", sec(998))
	qaHB(t, ctx, d, devF, "1 day", 999)

	// Allowance boundary through real SQL: +6m excluded, +4m included.
	qaSnap(t, ctx, d, devW, "6 minutes", sec(666))
	qaSnap(t, ctx, d, devW, "4 minutes", sec(444))
	qaSnap(t, ctx, d, devW, "-1 hour", sec(1))

	// GPU: fallback to the newest VALID row (guard must be inside the subquery).
	gpu := func(n int) map[string]any {
		m := sys("g")
		g := make([]any, n)
		for i := range g {
			g[i] = map[string]any{"name": "gpu"}
		}
		m["gpu_telemetry"] = map[string]any{"gpus": g}
		return m
	}
	qaSnap(t, ctx, d, devG, "-1 hour", gpu(1))
	qaSnap(t, ctx, d, devG, "1 year", gpu(0))
	qaSnap(t, ctx, d, devGF, "-1 hour", gpu(0))
	qaSnap(t, ctx, d, devGF, "1 year", gpu(1))

	// Container leaderboard.
	cont := func(name string) map[string]any {
		m := sys("c")
		m["docker"] = map[string]any{"containers": []any{map[string]any{"id": name, "name": name, "state": "running"}}}
		return m
	}
	qaSnap(t, ctx, d, devC, "-1 hour", cont("past-container"))
	qaSnap(t, ctx, d, devC, "1 year", cont("future-container"))

	t.Run("[AC-032] GetLatestSnapshot uses the past row", func(t *testing.T) {
		s, err := repo.GetLatestSnapshot(ctx, devD)
		require.NoError(t, err)
		require.NotNil(t, s.Data.Security)
		assert.Equal(t, 1, s.Data.Security.FailedLogins24h, "the +1y row must not be returned as latest")
		assert.True(t, s.Timestamp.Before(time.Now()), "latest timestamp must be in the past")
	})

	t.Run("[C9a] a device whose only rows are future-dated has no latest snapshot", func(t *testing.T) {
		_, err := repo.GetLatestSnapshot(ctx, devF)
		assert.Error(t, err, "no valid row: the device must have no latest snapshot")
	})

	t.Run("[AC-032] boundary: +6m excluded, +4m included", func(t *testing.T) {
		s, err := repo.GetLatestSnapshot(ctx, devW)
		require.NoError(t, err)
		assert.Equal(t, 444, s.Data.Security.FailedLogins24h, "+4m is within the allowance and is latest; +6m is excluded")
	})

	t.Run("[AC-032] GetAllLatestSnapshots", func(t *testing.T) {
		all, err := repo.GetAllLatestSnapshots(ctx)
		require.NoError(t, err)
		byDev := map[string]models.TelemetrySnapshot{}
		for _, s := range all {
			byDev[s.DeviceID] = s
		}
		require.Contains(t, byDev, devD)
		assert.Equal(t, 1, byDev[devD].Data.Security.FailedLogins24h)
		assert.NotContains(t, byDev, devF, "C9a: future-only device drops out of the fleet-wide latest view")
		require.Contains(t, byDev, devW)
		assert.Equal(t, 444, byDev[devW].Data.Security.FailedLogins24h)
	})

	t.Run("[AC-032] GetAllLatestSummaries", func(t *testing.T) {
		sums, err := repo.GetAllLatestSummaries(ctx)
		require.NoError(t, err)
		byDev := map[string]SnapshotSummary{}
		for _, s := range sums {
			byDev[s.DeviceID] = s
		}
		require.Contains(t, byDev, devD)
		require.NotNil(t, byDev[devD].Security)
		assert.Equal(t, 1, byDev[devD].Security.FailedLogins24h)
		assert.NotContains(t, byDev, devF, "C9a")
		// C9b: if the guard were an outer WHERE, D would vanish instead of falling back.
		require.Contains(t, byDev, devW, "C9b: devices with a valid older row must remain (guard inside the subquery)")
		assert.Equal(t, 444, byDev[devW].Security.FailedLogins24h)
	})

	t.Run("[AC-032] GetGPUDeviceIDs falls back to the newest valid row", func(t *testing.T) {
		ids, err := repo.GetGPUDeviceIDs(ctx)
		require.NoError(t, err)
		assert.Contains(t, ids, devG, "C9b: valid older GPU row must still count (guard inside the subquery)")
		assert.NotContains(t, ids, devGF, "a GPU only in a future-dated row must not count")
	})

	t.Run("[AC-032] GetFleetContainerLeaderboard falls back to the newest valid row", func(t *testing.T) {
		lb, err := repo.GetFleetContainerLeaderboard(ctx)
		require.NoError(t, err)
		var got *FleetContainerProjection
		for i := range lb {
			if lb[i].DeviceID == devC {
				got = &lb[i]
			}
		}
		require.NotNil(t, got, "C9b: device with a valid older row must remain in the leaderboard")
		require.Len(t, got.Containers, 1)
		assert.Equal(t, "past-container", got.Containers[0].Name)
	})

	t.Run("[AC-033] GetHistory excludes future rows", func(t *testing.T) {
		h, err := repo.GetHistory(ctx, devD, 100, 0)
		require.NoError(t, err)
		require.Len(t, h, 1)
		assert.Equal(t, 1, h[0].Data.Security.FailedLogins24h)
		hf, err := repo.GetHistory(ctx, devF, 100, 0)
		require.NoError(t, err)
		assert.Empty(t, hf)
	})

	t.Run("[AC-033] GetHeartbeatHistory and GetFleetHeartbeats exclude future rows", func(t *testing.T) {
		since := time.Now().Add(-2 * time.Hour)
		hh, err := repo.GetHeartbeatHistory(ctx, devD, since)
		require.NoError(t, err)
		require.Len(t, hh, 1)
		assert.EqualValues(t, 1, hh[0].Data.Uptime)
		hf, err := repo.GetHeartbeatHistory(ctx, devF, since)
		require.NoError(t, err)
		assert.Empty(t, hf)

		fleet, err := repo.GetFleetHeartbeats(ctx, since)
		require.NoError(t, err)
		require.Len(t, fleet[devD], 1)
		assert.EqualValues(t, 1, fleet[devD][0].Data.Uptime)
		assert.Empty(t, fleet[devF])
	})

	t.Run("[C9a] device reappears after one clamped post", func(t *testing.T) {
		// The handler clamps a far-future timestamp to the receive time; store exactly that.
		snap := &models.TelemetrySnapshot{DeviceID: devF, Timestamp: time.Now().UTC(),
			Data: models.FullTelemetryData{Security: &models.SecurityInfo{FailedLogins24h: 7}}}
		require.NoError(t, repo.StoreSnapshot(ctx, snap))
		s, err := repo.GetLatestSnapshot(ctx, devF)
		require.NoError(t, err)
		assert.Equal(t, 7, s.Data.Security.FailedLogins24h, "the clamped row becomes latest; the 1y row never does")
		sums, err := repo.GetAllLatestSummaries(ctx)
		require.NoError(t, err)
		found := false
		for _, x := range sums {
			if x.DeviceID == devF {
				found = true
				assert.Equal(t, 7, x.Security.FailedLogins24h)
			}
		}
		assert.True(t, found, "device must be back in the summaries view")
	})

	t.Run("[AC-034] the guard did not delete or rewrite the future rows", func(t *testing.T) {
		var n int
		require.NoError(t, d.Pool.QueryRow(ctx,
			`SELECT count(*) FROM telemetry_snapshots WHERE device_id LIKE 'qa-%' AND timestamp > NOW() + interval '1 day'`).Scan(&n))
		assert.Equal(t, 6, n, "all six far-future rows (D, F x2, G, GF, C) must still exist: the guard is read-side only")
	})
}

func TestDeviceRepo_PGDisposable_HasAPIKey_AndHostileIDs(t *testing.T) {
	d, ctx := disposableDB(t)
	repo := NewDeviceRepo(d)
	_, err := d.Pool.Exec(ctx, `DELETE FROM devices WHERE id LIKE 'qa-%'`)
	require.NoError(t, err)
	qaSeedDevice(t, ctx, d, "qa-keyed")
	qaSeedDevice(t, ctx, d, "qa-keyless")

	has, err := repo.HasAPIKey(ctx, "qa-keyed")
	require.NoError(t, err)
	assert.False(t, has, "no key yet")

	require.NoError(t, repo.StoreAPIKey(ctx, "riot_qa_key_1", "qa-keyed"))
	has, err = repo.HasAPIKey(ctx, "qa-keyed")
	require.NoError(t, err)
	assert.True(t, has)
	has, err = repo.HasAPIKey(ctx, "qa-keyless")
	require.NoError(t, err)
	assert.False(t, has, "another device's key must not count")
	has, err = repo.HasAPIKey(ctx, "no-such-device")
	require.NoError(t, err)
	assert.False(t, has)

	id, err := repo.LookupAPIKey(ctx, "riot_qa_key_1")
	require.NoError(t, err)
	assert.Equal(t, "qa-keyed", id)

	require.NoError(t, repo.DeleteAPIKeysByDevice(ctx, "qa-keyed"))
	has, err = repo.HasAPIKey(ctx, "qa-keyed")
	require.NoError(t, err)
	assert.False(t, has, "rotate-key style deletion returns the device to keyless")

	// SEC-001 premise: a NUL byte in device_id errors in PostgreSQL (so the request falls
	// through to the new-device path), and non-exact strings never match an existing ID.
	for name, hostile := range map[string]string{
		"NUL":          "qa-keyed\x00",
		"trailing sp":  "qa-keyed ",
		"upper-cased":  strings.ToUpper("qa-keyed"),
		"SQL metachar": "qa-keyed' OR '1'='1",
	} {
		dev, err := repo.FindByDeviceUUID(ctx, hostile)
		assert.True(t, err != nil || dev == nil, "%s: hostile id %q must not resolve to the existing device", name, hostile)
	}
	_, err = repo.HasAPIKey(ctx, "qa-keyed\x00")
	assert.Error(t, err, "HasAPIKey with a NUL byte errors; the handler treats that as fail-closed")
}
