package db

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PostgreSQL-gated tests of migration 000033 and ExpectRepo (EXPECT-001). They
// skip unless RIOT_TEST_PG_URL names a test database (see pgTestURL).

func (e *pgEnv) expectRepo() *ExpectRepo {
	e.T.Helper()
	return &ExpectRepo{db: &DB{Pool: e.Pool}}
}

// finish marks a raw command terminal at the given time.
func (e *pgEnv) finish(id, status string, at time.Time) {
	e.T.Helper()
	e.Exec(`UPDATE commands SET status=$2, finished_at=$3, updated_at=$3, dispatched_at=$4 WHERE id=$1`,
		id, status, at, at.Add(-10*time.Minute))
}

func (e *pgEnv) scalarInt(sql string, args ...interface{}) int {
	e.T.Helper()
	var n int
	require.NoError(e.T, e.Pool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func TestEX_Repo_PG_MigrationUpDownUp(t *testing.T) {
	env := newPGEnv(t, 0)
	has := func(table string) bool {
		return env.scalarInt(`SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name=$2`, env.Schema, table) == 1
	}
	hasCol := func() bool {
		return env.scalarInt(`SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='commands' AND column_name='window_closed_at'`, env.Schema) == 1
	}
	require.True(t, has("command_escalations") && has("device_powered_off") && hasCol(), "up creates the objects")

	var def string
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname=$1 AND indexname='idx_commands_window_pending'`, env.Schema).Scan(&def))
	for _, a := range models.ExpectActions() {
		assert.Contains(t, def, "'"+a+"'", "[AD-012] index predicate lists %s", a)
	}
	assert.Equal(t, len(models.ExpectActions()), strings.Count(def, "::text"), "the predicate has exactly the ExpectActions() entries")

	env.Exec(`INSERT INTO admin_config (key, value) VALUES ('expect_restart_series', '{}')`)
	// Down to 32 (not Steps(-1): 000034 is the latest migration since EXPECT-002).
	require.NoError(t, env.Mig.Migrate(32), "down")
	assert.False(t, has("command_escalations") || has("device_powered_off") || hasCol(), "down removes the objects")
	assert.Equal(t, 0, env.scalarInt(`SELECT count(*) FROM admin_config WHERE key='expect_restart_series'`), "down removes the series row")
	require.NoError(t, env.Mig.Up(), "up again")
	assert.True(t, has("command_escalations") && has("device_powered_off") && hasCol(), "second up recreates the objects")
}

func TestEX_Repo_PG_MigrationBackfillLapsesOldTerminalCommands(t *testing.T) {
	env := newPGEnv(t, 32)
	env.seedDevice("dev-1")
	now := time.Now().UTC()
	env.insertRaw(rawCommand{ID: "old", DeviceID: "dev-1", Action: "reboot", Status: "succeeded", CreatedAt: now.Add(-3 * time.Hour)})
	env.finish("old", "succeeded", now.Add(-2*time.Hour))
	env.insertRaw(rawCommand{ID: "recent", DeviceID: "dev-1", Action: "reboot", Status: "succeeded", CreatedAt: now.Add(-30 * time.Minute)})
	env.finish("recent", "succeeded", now.Add(-10*time.Minute))
	env.insertRaw(rawCommand{ID: "active", DeviceID: "dev-1", Action: "reboot", Status: "running", CreatedAt: now.Add(-3 * time.Hour)})
	require.NoError(t, env.Mig.Migrate(33))

	closed := func(id string) (bool, string) {
		var at *time.Time
		var outcome string
		require.NoError(t, env.Pool.QueryRow(context.Background(),
			`SELECT window_closed_at, window_outcome FROM commands WHERE id=$1`, id).Scan(&at, &outcome))
		return at != nil, outcome
	}
	ok, outcome := closed("old")
	assert.True(t, ok, "[X-12] a terminal command older than 1 h is lapsed by the migration")
	assert.Equal(t, "lapsed", outcome)
	ok, _ = closed("recent")
	assert.False(t, ok, "a terminal command from the last hour stays pending")
	ok, _ = closed("active")
	assert.False(t, ok, "an active command stays pending")
}

func TestEX_Repo_PG_ListsAndCloseWindow(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.expectRepo()
	ctx := withDeadline(t, 30*time.Second)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	env.insertRaw(rawCommand{ID: "act", DeviceID: "dev-1", Action: "docker_update", Status: "running", CreatedAt: now.Add(-1 * time.Hour)})
	env.insertRaw(rawCommand{ID: "queued", DeviceID: "dev-1", Action: "reboot", Status: "queued", CreatedAt: now.Add(-1 * time.Minute)})
	env.insertRaw(rawCommand{ID: "recent", DeviceID: "dev-1", Action: "reboot", Status: "succeeded", CreatedAt: now.Add(-3 * time.Hour)})
	env.finish("recent", "succeeded", now.Add(-2*time.Hour))
	env.insertRaw(rawCommand{ID: "old", DeviceID: "dev-1", Action: "os_update", Status: "failed", CreatedAt: now.Add(-50 * time.Hour)})
	env.finish("old", "failed", now.Add(-49*time.Hour))
	env.insertRaw(rawCommand{ID: "nodispatch", DeviceID: "dev-1", Action: "reboot", Status: "expired", CreatedAt: now.Add(-4 * time.Hour)})
	env.Exec(`UPDATE commands SET finished_at=$2, updated_at=$2 WHERE id=$1`, "nodispatch", now.Add(-150*time.Minute))
	env.insertRaw(rawCommand{ID: "outofscope", DeviceID: "dev-1", Action: "docker_start", Status: "running", CreatedAt: now.Add(-1 * time.Hour)})

	ids := func(cmds []models.Command) []string {
		out := make([]string, len(cmds))
		for i, c := range cmds {
			out[i] = c.ID
		}
		return out
	}

	open, err := repo.ListOpenWindows(ctx, now, 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"queued", "act", "recent", "nodispatch"}, ids(open),
		"[AD-009] active, queued and recently finished windows, newest first; out-of-scope and >24h old rows excluded")

	limited, err := repo.ListOpenWindows(ctx, now, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"queued", "act"}, ids(limited), "[SEC-002 c] the LIMIT keeps the newest rows")

	backlog, err := repo.ListDueBacklog(ctx, now, 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"old", "nodispatch", "recent"}, ids(backlog), "terminal pending rows, oldest finish first")

	lapse, err := repo.ListLapseCandidates(ctx, now.Add(-24*time.Hour), 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"old", "nodispatch"}, ids(lapse), "terminal before the cutoff, or terminal with no dispatch time")

	applied, err := repo.CloseWindow(ctx, "recent", now, models.WindowOutcomeClosed)
	require.NoError(t, err)
	assert.True(t, applied, "the first close applies")
	applied, err = repo.CloseWindow(ctx, "recent", now.Add(time.Hour), models.WindowOutcomeEscalated)
	require.NoError(t, err)
	assert.False(t, applied, "the guard makes a second close a no-op")
	var outcome string
	require.NoError(t, env.Pool.QueryRow(ctx, `SELECT window_outcome FROM commands WHERE id='recent'`).Scan(&outcome))
	assert.Equal(t, "closed", outcome, "the first outcome is kept")
	open, err = repo.ListOpenWindows(ctx, now, 100)
	require.NoError(t, err)
	assert.NotContains(t, ids(open), "recent", "a processed window leaves the open set")
}

func TestEX_Repo_PG_RecordEscalationExactlyOnceUnderConcurrency(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.insertRaw(rawCommand{ID: "cmd-1", DeviceID: "dev-1", Action: "reboot", Status: "succeeded"})
	repo := env.expectRepo()
	ctx := withDeadline(t, 30*time.Second)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	var inserted int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			evt := &models.Event{DeviceID: "dev-1", Type: models.EventDeviceOffline, Severity: models.SeverityWarning,
				Message: "vermont still offline", CreatedAt: now, CommandID: "cmd-1", CommandAttr: models.CommandAttrEscalation}
			ok, err := repo.RecordEscalation(ctx, &models.Escalation{CommandID: "cmd-1", Target: "device", DeviceID: "dev-1", CreatedAt: now}, evt)
			assert.NoError(t, err)
			if ok {
				atomic.AddInt32(&inserted, 1)
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, inserted, "[FR-024] exactly one caller inserts the marker")
	assert.Equal(t, 1, env.scalarInt(`SELECT count(*) FROM events WHERE command_id='cmd-1' AND command_attr='escalation'`), "exactly one event is written")
	assert.Equal(t, 1, env.scalarInt(`SELECT count(*) FROM command_escalations WHERE command_id='cmd-1' AND event_id IS NOT NULL`), "the marker links its event")

	_, err := repo.RecordEscalation(ctx, &models.Escalation{CommandID: "nope", Target: "device", DeviceID: "dev-1", CreatedAt: now},
		&models.Event{DeviceID: "dev-1", Type: models.EventDeviceOffline, Severity: models.SeverityWarning, CreatedAt: now})
	assert.Error(t, err, "a marker for a missing command violates the foreign key")
	assert.Equal(t, 1, env.scalarInt(`SELECT count(*) FROM events WHERE device_id='dev-1'`), "a failed transaction leaves no orphan event")
}

func TestEX_Repo_PG_EscalationTargetsOpenAndClear(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.insertRaw(rawCommand{ID: "cmd-1", DeviceID: "dev-1", Action: "reboot", Status: "succeeded"})
	repo := env.expectRepo()
	ctx := withDeadline(t, 30*time.Second)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	for _, target := range []string{"device", "summary", models.EscalationTarget(models.EscContainer, "plex")} {
		ok, err := repo.RecordEscalation(ctx, &models.Escalation{CommandID: "cmd-1", Target: target, DeviceID: "dev-1", CreatedAt: now},
			&models.Event{DeviceID: "dev-1", Type: models.EventDeviceOffline, Severity: models.SeverityWarning, CreatedAt: now})
		require.NoError(t, err)
		require.True(t, ok)
	}
	targets, err := repo.ListEscalationTargets(ctx, "cmd-1")
	require.NoError(t, err)
	assert.Len(t, targets, 3)
	assert.True(t, models.IsContainerEscalationTarget(targets[0]), "sorted: container: first")

	open, err := repo.ListOpenDeviceEscalations(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, open, 1, "only the device marker is a device escalation")
	assert.Equal(t, "cmd-1", open[0].CommandID)
	open, err = repo.ListOpenDeviceEscalations(ctx, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, open, "[FR-037] older than the display window")

	n, err := repo.ClearDeviceEscalations(ctx, "dev-1", now.Add(time.Minute))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	open, err = repo.ListOpenDeviceEscalations(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Empty(t, open, "a cleared escalation is not open")
	n, err = repo.ClearDeviceEscalations(ctx, "dev-1", now.Add(2*time.Minute))
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "clearing again is a no-op")
}

func TestEX_Repo_PG_Cascades(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	env.insertRaw(rawCommand{ID: "cmd-1", DeviceID: "dev-1", Action: "reboot", Status: "succeeded"})
	repo := env.expectRepo()
	ctx := withDeadline(t, 30*time.Second)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	_, err := repo.RecordEscalation(ctx, &models.Escalation{CommandID: "cmd-1", Target: "device", DeviceID: "dev-1", CreatedAt: now},
		&models.Event{DeviceID: "dev-1", Type: models.EventDeviceOffline, Severity: models.SeverityWarning, CreatedAt: now})
	require.NoError(t, err)
	_, err = repo.UpsertPoweredOff(ctx, &models.PoweredOffState{DeviceID: "dev-2", CommandID: "c", ShutdownAt: now, OfflineAt: now})
	require.NoError(t, err)

	env.Exec(`DELETE FROM commands WHERE id='cmd-1'`)
	assert.Equal(t, 0, env.scalarInt(`SELECT count(*) FROM command_escalations`), "[AD-012] markers are removed with their command")
	env.Exec(`DELETE FROM devices WHERE id='dev-2'`)
	assert.Equal(t, 0, env.scalarInt(`SELECT count(*) FROM device_powered_off`), "[AD-012] the powered-off row is removed with its device")
}

func TestEX_Repo_PG_SuppressedDeathsAndStarts(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	repo := env.expectRepo()
	ctx := withDeadline(t, 30*time.Second)
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ev := func(device, typ, name, cmd, attr string, at time.Time) {
		env.Exec(`INSERT INTO events (device_id, type, severity, message, container_name, created_at, command_id, command_attr)
			VALUES ($1,$2,'info','m',$3,$4,$5,$6)`, device, typ, name, at, cmd, attr)
	}
	ev("dev-1", "container_died", "plex", "cmd-1", "expected", now)
	ev("dev-1", "container_died", "plex", "cmd-1", "expected", now.Add(time.Minute))
	ev("dev-1", "container_died", "sonarr", "cmd-1", "expected", now)
	ev("dev-1", "container_died", "radarr", "cmd-1", "inferred", now)
	ev("dev-1", "container_died", "lidarr", "cmd-2", "expected", now)
	ev("dev-2", "container_died", "foreign", "cmd-1", "expected", now)
	ev("dev-1", "container_started", "plex", "", "", now.Add(2*time.Minute))
	ev("dev-1", "container_started", "old", "", "", now.Add(-time.Hour))
	ev("dev-2", "container_started", "foreign", "", "", now.Add(2*time.Minute))

	deaths, err := repo.SuppressedDeaths(ctx, "dev-1", "cmd-1", 10)
	require.NoError(t, err)
	require.Len(t, deaths, 2, "only expected-attributed deaths of this command on this device")
	assert.Equal(t, "plex", deaths[0].ContainerName)
	assert.True(t, deaths[0].At.Equal(now.Add(time.Minute)), "the last death per container")
	assert.Equal(t, "sonarr", deaths[1].ContainerName)

	starts, err := repo.ContainerStartsSince(ctx, "dev-1", now, 10)
	require.NoError(t, err)
	assert.Len(t, starts, 1, "only starts since the given time on this device")
	assert.True(t, starts["plex"].Equal(now.Add(2*time.Minute)))
}

func TestEX_SEC001_PG_UpsertNoRestamp(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.expectRepo()
	ctx := withDeadline(t, 30*time.Second)
	t0 := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	first := &models.PoweredOffState{DeviceID: "dev-1", CommandID: "cmd-1", ActionLabel: "Shutdown", ActorPhrase: "by ryan",
		ShutdownAt: t0, OfflineAt: t0.Add(time.Minute), CreatedAt: t0.Add(time.Minute)}
	stored, err := repo.UpsertPoweredOff(ctx, first)
	require.NoError(t, err)
	assert.True(t, stored, "the first upsert inserts")

	again := *first
	again.OfflineAt = t0.Add(48 * time.Hour)
	again.CreatedAt = again.OfflineAt
	stored, err = repo.UpsertPoweredOff(ctx, &again)
	require.NoError(t, err)
	assert.False(t, stored, "[C-1 d] the same command never refreshes its row")

	older := &models.PoweredOffState{DeviceID: "dev-1", CommandID: "cmd-0", ShutdownAt: t0.Add(-time.Hour), OfflineAt: t0}
	stored, err = repo.UpsertPoweredOff(ctx, older)
	require.NoError(t, err)
	assert.False(t, stored, "an older shutdown never replaces a newer row")

	newer := &models.PoweredOffState{DeviceID: "dev-1", CommandID: "cmd-2", ActorPhrase: "by ryan", ShutdownAt: t0.Add(72 * time.Hour), OfflineAt: t0.Add(72 * time.Hour)}
	stored, err = repo.UpsertPoweredOff(ctx, newer)
	require.NoError(t, err)
	assert.True(t, stored, "[FR-032] a newer shutdown replaces the older row")

	var cmd string
	var offline time.Time
	require.NoError(t, env.Pool.QueryRow(ctx, `SELECT command_id, offline_at FROM device_powered_off WHERE device_id='dev-1'`).Scan(&cmd, &offline))
	assert.Equal(t, "cmd-2", cmd)
	assert.True(t, offline.Equal(newer.OfflineAt), "offline_at is the newer shutdown's, never a later re-stamp")
	assert.Equal(t, 1, env.scalarInt(`SELECT count(*) FROM device_powered_off`), "at most one row per device")

	got, err := repo.DeletePoweredOff(ctx, "dev-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "cmd-2", got.CommandID, "delete returns the deleted row")
	got, err = repo.DeletePoweredOff(ctx, "dev-1")
	require.NoError(t, err)
	assert.Nil(t, got, "deleting a missing row returns nil")
}

func TestEX_SEC001_PG_PurgeInvalidPoweredOff(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.expectRepo()
	ctx := withDeadline(t, 30*time.Second)
	t0 := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	mk := func(id, status string, heartbeat *time.Time) {
		env.Exec(`INSERT INTO devices (id, short_id, hostname, status, last_heartbeat) VALUES ($1,$1,$1,$2,$3)`, id, status, heartbeat)
		_, err := repo.UpsertPoweredOff(ctx, &models.PoweredOffState{DeviceID: id, CommandID: "c-" + id, ShutdownAt: t0, OfflineAt: t0.Add(time.Minute)})
		require.NoError(t, err)
	}
	before, after := t0.Add(30*time.Second), t0.Add(time.Hour)
	mk("valid", "offline", &before)
	mk("nohb", "offline", nil)
	mk("online", "online", &before)
	mk("warning", "warning", &before)
	mk("heartbeat-after", "offline", &after)

	list, err := repo.ListPoweredOff(ctx)
	require.NoError(t, err)
	var ids []string
	for _, p := range list {
		ids = append(ids, p.DeviceID)
	}
	assert.Equal(t, []string{"nohb", "valid"}, ids, "[C-1 b] ListPoweredOff returns valid rows only, before any purge ran")

	n, err := repo.PurgeInvalidPoweredOff(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n, "online, warning and heartbeat-after-offline rows are purged")
	assert.Equal(t, 2, env.scalarInt(`SELECT count(*) FROM device_powered_off`), "valid rows survive")
	n, err = repo.PurgeInvalidPoweredOff(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 0, n, "the purge is idempotent")
}

func TestEX_SEC002_PG_RetentionSkipsPendingWindows(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	now := time.Now().UTC()
	old := now.Add(-200 * 24 * time.Hour)
	add := func(id, action string, closed bool) {
		env.insertRaw(rawCommand{ID: id, DeviceID: "dev-1", Action: action, Status: "succeeded", CreatedAt: old})
		env.Exec(`UPDATE commands SET finished_at=$2, updated_at=$2 WHERE id=$1`, id, old)
		if closed {
			env.Exec(`UPDATE commands SET window_closed_at=$2 WHERE id=$1`, id, old)
		}
	}
	add("pending-reboot", "reboot", false)
	add("closed-reboot", "reboot", true)
	add("pending-out-of-scope", "docker_start", false)

	r := &DockerRepo{db: &DB{Pool: env.Pool}}
	require.NoError(t, r.Purge(withDeadline(t, 60*time.Second), now))

	exists := func(id string) bool {
		return env.scalarInt(`SELECT count(*) FROM commands WHERE id=$1`, id) == 1
	}
	assert.True(t, exists("pending-reboot"), "[C-2 b] a command whose window is not processed is exempt from retention")
	assert.False(t, exists("closed-reboot"), "a processed window is deleted by retention as before")
	assert.False(t, exists("pending-out-of-scope"), "out-of-scope actions are deleted as before")
}
