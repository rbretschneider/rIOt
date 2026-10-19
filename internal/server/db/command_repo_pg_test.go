package db

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PostgreSQL-gated tests. They skip unless RIOT_TEST_PG_URL names a test
// database (pgTestURL), and each one works in its own schema.

var pgNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func (e *pgEnv) repo() *CommandRepo { return &CommandRepo{db: &DB{Pool: e.Pool}} }

// rawCommand is a row inserted directly, bypassing the repository, so tests can
// create legacy shapes and arbitrary states.
type rawCommand struct {
	ID, DeviceID, Action, Status string
	Params                       string
	CreatedAt                    time.Time
	ExpiresAt                    time.Time
	ExecDeadlineS                int
	DeadlineAt                   *time.Time
	TargetKeys                   []string
}

func (e *pgEnv) insertRaw(c rawCommand) {
	e.T.Helper()
	if c.Params == "" {
		c.Params = "{}"
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = pgNow
	}
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = c.CreatedAt.Add(time.Hour)
	}
	if c.ExecDeadlineS == 0 {
		c.ExecDeadlineS = 900
	}
	if c.TargetKeys == nil {
		c.TargetKeys = []string{}
	}
	e.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, target_keys)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $6, $7, $8, $9, $10)`,
		c.ID, c.DeviceID, c.Action, c.Params, c.Status, c.CreatedAt, c.ExpiresAt, c.ExecDeadlineS, c.DeadlineAt, c.TargetKeys)
}

func (e *pgEnv) statusOf(id string) string {
	e.T.Helper()
	var s string
	require.NoError(e.T, e.Pool.QueryRow(context.Background(), `SELECT status FROM commands WHERE id=$1`, id).Scan(&s))
	return s
}

func newQueued(id, device, action string, keys ...string) *models.Command {
	return &models.Command{
		ID: id, DeviceID: device, Action: action, Params: map[string]interface{}{},
		Status: "queued", CreatedAt: pgNow, UpdatedAt: pgNow,
		ExpiresAt: pgNow.Add(time.Hour), ExecDeadlineS: 900, TargetKeys: keys,
	}
}

func TestCL_AC011_PG_MigrationResetsContainerPoliciesOnly(t *testing.T) {
	env := newPGEnv(t, 22)
	env.seedDevice("dev-1")
	env.Exec(`INSERT INTO auto_update_policies (device_id, target, is_stack, last_triggered_at) VALUES
		('dev-1', 'web', false, $1), ('dev-1', 'media', true, $1)`, pgNow)

	require.NoError(t, env.Mig.Migrate(23))

	rows, err := env.Pool.Query(context.Background(),
		`SELECT target, last_triggered_at IS NULL, consecutive_failures FROM auto_update_policies ORDER BY target`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var target string
		var cleared bool
		var failures int
		require.NoError(t, rows.Scan(&target, &cleared, &failures))
		got[target] = cleared
		assert.Zero(t, failures, "new column defaults to 0")
	}
	require.NoError(t, rows.Err())
	assert.True(t, got["web"], "[AC-011] the container-level policy's last-triggered must be cleared")
	assert.False(t, got["media"], "[AC-011] the stack policy's last-triggered must be unchanged")
}

func TestCL_AC073_PG_ClaimRace(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)

	t.Run("ClaimByID", func(t *testing.T) {
		conflict, err := repo.CreateIfNoConflict(ctx, newQueued("c-by-id", "dev-1", "reboot", "host"))
		require.NoError(t, err)
		require.Nil(t, conflict)

		var wins atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, claimed, err := repo.ClaimByID(ctx, "c-by-id", pgNow)
				assert.NoError(t, err)
				if claimed {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, int32(1), wins.Load(), "[AC-073] exactly one of 8 concurrent claims may succeed")
	})

	t.Run("ClaimQueued", func(t *testing.T) {
		conflict, err := repo.CreateIfNoConflict(ctx, newQueued("c-hb", "dev-1", "fetch_logs"))
		require.NoError(t, err)
		require.Nil(t, conflict)

		var total atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := repo.ClaimQueued(ctx, "dev-1", pgNow, 10)
				assert.NoError(t, err)
				for _, c := range got {
					if c.ID == "c-hb" {
						total.Add(1)
					}
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, int32(1), total.Load(), "[AC-073] a command may be delivered by exactly one heartbeat claim")
	})
}

func TestCL_AC107_PG_CancelClaimRace(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 60*time.Second)

	for i := 0; i < 25; i++ {
		id := "race-" + string(rune('a'+i))
		_, err := repo.CreateIfNoConflict(ctx, newQueued(id, "dev-1", "fetch_logs"))
		require.NoError(t, err)

		var cancelled, claimed bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, ok, err := repo.Transition(ctx, models.CommandTransition{
				ID: id, DeviceID: "dev-1", Actor: models.ActorOperator, To: models.CommandCancelled, Now: pgNow})
			assert.NoError(t, err)
			cancelled = ok
		}()
		go func() {
			defer wg.Done()
			_, ok, err := repo.ClaimByID(ctx, id, pgNow)
			assert.NoError(t, err)
			claimed = ok
		}()
		wg.Wait()

		assert.NotEqual(t, cancelled, claimed, "[AC-107] iteration %d: exactly one of cancel and claim may win", i)
		want := "cancelled"
		if claimed {
			want = "dispatched"
		}
		assert.Equal(t, want, env.statusOf(id), "[AC-107] the stored state must match the winner")
	}
}

func TestCL_AC023_PG_DuplicateKeepsUpdatedAt(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)

	_, err := repo.CreateIfNoConflict(ctx, newQueued("c-1", "dev-1", "fetch_logs"))
	require.NoError(t, err)
	msg := "done"
	_, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded, ResultMsg: &msg, Now: pgNow})
	require.NoError(t, err)
	require.True(t, applied)
	before, err := repo.GetByID(ctx, "c-1")
	require.NoError(t, err)

	for _, to := range []models.CommandState{models.CommandSucceeded, models.CommandFailed} {
		late := "late"
		got, applied, err := repo.Transition(ctx, models.CommandTransition{
			ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: to, ResultMsg: &late, Now: pgNow.Add(time.Hour)})
		require.NoError(t, err)
		assert.False(t, applied, "[AC-023] a result on an immutable row (%s) must not apply", to)
		assert.Nil(t, got)
	}

	after, err := repo.GetByID(ctx, "c-1")
	require.NoError(t, err)
	assert.True(t, before.UpdatedAt.Equal(after.UpdatedAt), "[AC-023] updated_at must be unchanged by a duplicate")
	assert.Equal(t, "done", after.ResultMsg)
	assert.Equal(t, "succeeded", after.Status)
}

func TestCL_AC101_PG_LegacyRowsReadCanonical(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)

	legacy := map[string]string{"c-pending": "pending", "c-sent": "sent", "c-success": "success", "c-error": "error", "c-queued": "queued"}
	want := map[string]string{"c-pending": "queued", "c-sent": "dispatched", "c-success": "succeeded", "c-error": "failed", "c-queued": "queued"}
	for id, status := range legacy {
		env.insertRaw(rawCommand{ID: id, DeviceID: "dev-1", Action: "fetch_logs", Status: status})
	}
	for id, canonical := range want {
		got, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, canonical, got.Status, "[AC-101] stored %q must read as %q", legacy[id], canonical)
	}
	list, err := repo.ListByDevice(ctx, "dev-1", 50)
	require.NoError(t, err)
	for _, c := range list {
		assert.Equal(t, want[c.ID], c.Status, "[AC-101] ListByDevice must normalize too")
	}
}

func TestCL_AC103_PG_MigratedLegacyQueuedExpires(t *testing.T) {
	env := newPGEnv(t, 22)
	env.seedDevice("dev-1")
	created := pgNow.Add(-48 * time.Hour)
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at)
		VALUES ('old-reboot', 'dev-1', 'reboot', '{}', 'queued', $1, $1)`, created)
	require.NoError(t, env.Mig.Migrate(23))
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)

	claimed, err := repo.ClaimQueued(ctx, "dev-1", pgNow, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed, "[AC-103] a 2-day-old queued reboot must not be delivered")

	due, err := repo.ListExpiredQueued(ctx, "", pgNow, 500)
	require.NoError(t, err)
	require.Len(t, due, 1, "[AC-103] the sweeper must find the backfilled-expired reboot")
	assert.Equal(t, "old-reboot", due[0].ID)
	assert.True(t, due[0].PreLifecycle, "[SEC-010] pre-existing rows are flagged")

	msg := "expired: not delivered within 15m"
	_, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "old-reboot", Actor: models.ActorServer, To: models.CommandExpired, ResultMsg: &msg, Now: pgNow})
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, "expired", env.statusOf("old-reboot"))
}

func TestCL_NFR007_PG_SweepOver100kRowsUnder1s(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	ctx := withDeadline(t, 120*time.Second)

	// 100,000 historical terminal rows plus a handful of due ones.
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, finished_at)
		SELECT 'hist-' || g, 'dev-1', 'fetch_logs', '{}', 'succeeded', $1::timestamptz, $1::timestamptz, $1::timestamptz + INTERVAL '30 minutes', 900, $1::timestamptz
		FROM generate_series(1, 100000) AS g`, pgNow.Add(-24*time.Hour))
	for i := 0; i < 5; i++ {
		id := "due-" + string(rune('a'+i))
		env.insertRaw(rawCommand{ID: id, DeviceID: "dev-1", Action: "reboot", Status: "queued",
			CreatedAt: pgNow.Add(-time.Hour), ExpiresAt: pgNow.Add(-45 * time.Minute)})
		dl := pgNow.Add(-time.Minute)
		env.insertRaw(rawCommand{ID: "over-" + id, DeviceID: "dev-1", Action: "docker_update", Status: "running",
			CreatedAt: pgNow.Add(-time.Hour), DeadlineAt: &dl})
	}
	env.Exec(`ANALYZE commands`)
	repo := env.repo()

	start := time.Now()
	expired, err := repo.ListExpiredQueued(ctx, "", pgNow, 500)
	require.NoError(t, err)
	overdue, err := repo.ListOverdue(ctx, "", pgNow, 500)
	require.NoError(t, err)
	elapsed := time.Since(start)

	assert.Len(t, expired, 5)
	assert.Len(t, overdue, 5)
	assert.Less(t, elapsed, time.Second, "[NFR-007] one sweep pass over 100k historical rows must finish in 1s, took %s", elapsed)
}

func TestCL_NFR002_PG_DownMigrationRestoresLegacyValues(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	// The down migration compares against the database clock (NOW()), so these rows are relative to the real time.
	now := time.Now().UTC()
	dl := now.Add(time.Hour)
	statuses := []string{"queued", "dispatched", "acknowledged", "running", "verifying", "succeeded", "failed", "timed_out", "cancelled", "expired"}
	for _, s := range statuses {
		c := rawCommand{ID: "c-" + s, DeviceID: "dev-1", Action: "fetch_logs", Status: s, CreatedAt: now}
		if s == "dispatched" || s == "acknowledged" || s == "running" || s == "verifying" {
			c.DeadlineAt = &dl
		}
		env.insertRaw(c)
	}

	require.NoError(t, env.Mig.Steps(-1), "down migration")

	want := map[string]string{
		"queued": "queued", "dispatched": "sent", "acknowledged": "sent", "running": "sent", "verifying": "sent",
		"succeeded": "success", "failed": "error", "timed_out": "error", "cancelled": "error", "expired": "error",
	}
	for _, s := range statuses {
		var got string
		require.NoError(t, env.Pool.QueryRow(context.Background(), `SELECT status FROM commands WHERE id=$1`, "c-"+s).Scan(&got))
		assert.Equal(t, want[s], got, "[NFR-002] canonical %q must read as legacy %q after the down migration", s, want[s])
	}

	var cols int
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.columns WHERE table_schema=$1 AND table_name='commands' AND column_name IN ('expires_at','target_keys','pre_lifecycle')`,
		env.Schema).Scan(&cols))
	assert.Zero(t, cols, "the down migration must drop the lifecycle columns")

	require.NoError(t, env.Mig.Up(), "up, down, up must be clean")
	assert.Equal(t, "dispatched", env.statusOf("c-running"), "the second up maps sent back to dispatched")
}

func TestCL_SEC009_PG_DownMigrationFailsQueuedPowerActions(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	// The down migration compares against the database clock (NOW()), so these rows are relative to the real time.
	future := time.Now().UTC().Add(10 * time.Minute)
	env.insertRaw(rawCommand{ID: "reboot", DeviceID: "dev-1", Action: "reboot", Status: "queued", ExpiresAt: future})
	env.insertRaw(rawCommand{ID: "shutdown", DeviceID: "dev-1", Action: "shutdown", Status: "queued", ExpiresAt: future})
	env.insertRaw(rawCommand{ID: "patch-reboot", DeviceID: "dev-1", Action: "os_update", Status: "queued", ExpiresAt: future,
		Params: `{"mode":"full","include_reboot_class":true}`})
	env.insertRaw(rawCommand{ID: "patch-plain", DeviceID: "dev-1", Action: "os_update", Status: "queued", ExpiresAt: future,
		Params: `{"mode":"full"}`})
	env.insertRaw(rawCommand{ID: "docker", DeviceID: "dev-1", Action: "docker_update", Status: "queued", ExpiresAt: future})
	env.insertRaw(rawCommand{ID: "stale", DeviceID: "dev-1", Action: "fetch_logs", Status: "queued", ExpiresAt: time.Now().UTC().Add(-time.Hour)})

	require.NoError(t, env.Mig.Steps(-1))

	for _, id := range []string{"reboot", "shutdown", "patch-reboot"} {
		assert.Equal(t, "error", env.statusOf(id), "[SEC-009] queued power action %s must not survive a downgrade, even unexpired", id)
	}
	var msg string
	require.NoError(t, env.Pool.QueryRow(context.Background(), `SELECT result_msg FROM commands WHERE id='reboot'`).Scan(&msg))
	assert.Equal(t, "not delivered: power action dropped on downgrade", msg)
	assert.Equal(t, "queued", env.statusOf("patch-plain"), "an os_update without include_reboot_class keeps its queue")
	assert.Equal(t, "queued", env.statusOf("docker"), "non-power actions inside their TTL stay queued")
	assert.Equal(t, "error", env.statusOf("stale"), "an overdue queued row must never become deliverable again")
}

func TestCL_SEC002_PG_NULReportPersists(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)
	_, err := repo.CreateIfNoConflict(ctx, newQueued("c-1", "dev-1", "fetch_logs"))
	require.NoError(t, err)

	hostile := "pull failed\x00 layer \xff\xfe broken \x00"
	msg := models.TruncateTail(models.SanitizeText(hostile), models.MaxResultMsgBytes)
	stderr := models.TruncateTail(models.SanitizeText(hostile), models.MaxStderrTailBytes)
	step := models.SanitizeStep("pull\x00")
	_, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandFailed,
		ResultMsg: &msg, StderrTail: &stderr, FailureStep: &step, Now: pgNow})
	require.NoError(t, err, "[SEC-002] sanitized hostile text must never produce a database error")
	require.True(t, applied)

	got, err := repo.GetByID(ctx, "c-1")
	require.NoError(t, err)
	for name, v := range map[string]string{"result_msg": got.ResultMsg, "stderr_tail": got.StderrTail, "failure_step": got.FailureStep} {
		assert.NotContains(t, v, "\x00", "[SEC-002] %s must be NUL-free", name)
		assert.True(t, utf8.ValidString(v), "[SEC-002] %s must be valid UTF-8", name)
	}
	assert.Contains(t, got.ResultMsg, "pull failed")
}

func TestCL_SEC006_PG_TargetKeyFilterIsBound(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)

	hostile := "docker:container:x'; DROP TABLE commands;--"
	percent := "docker:container:100%"
	_, err := repo.CreateIfNoConflict(ctx, newQueued("c-hostile", "dev-1", "docker_update", hostile))
	require.NoError(t, err)
	_, err = repo.CreateIfNoConflict(ctx, newQueued("c-percent", "dev-1", "docker_update", percent))
	require.NoError(t, err)
	_, err = repo.CreateIfNoConflict(ctx, newQueued("c-plain", "dev-1", "docker_update", "docker:container:plain"))
	require.NoError(t, err)

	for key, wantID := range map[string]string{hostile: "c-hostile", percent: "c-percent", "docker:container:%": ""} {
		active, err := repo.ListActive(ctx, "dev-1", key)
		require.NoError(t, err, "[SEC-006] key %q must not break the query", key)
		filtered, err := repo.ListByDeviceFiltered(ctx, "dev-1", 50, 0, nil, "", key)
		require.NoError(t, err)
		if wantID == "" {
			assert.Empty(t, active, "[SEC-006] %q is data, not a LIKE pattern", key)
			assert.Empty(t, filtered)
			continue
		}
		require.Len(t, active, 1)
		assert.Equal(t, wantID, active[0].ID)
		require.Len(t, filtered, 1)
		assert.Equal(t, wantID, filtered[0].ID)
	}

	var n int
	require.NoError(t, env.Pool.QueryRow(context.Background(), `SELECT count(*) FROM commands`).Scan(&n))
	assert.Equal(t, 3, n, "the table must still exist with all rows")
}

func TestCL_SEC_PG_ActiveNonQueuedRowsAlwaysHaveDeadline(t *testing.T) {
	env := newPGEnv(t, 22)
	env.seedDevice("dev-1")
	// Migrated legacy 'sent' rows.
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at)
		VALUES ('legacy-sent', 'dev-1', 'docker_update', '{}', 'sent', $1, $1)`, pgNow.Add(-time.Hour))
	require.NoError(t, env.Mig.Migrate(23))

	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)
	// Agent edges out of queued, without any claim.
	for i, to := range []models.CommandState{models.CommandAcknowledged, models.CommandRunning, models.CommandVerifying} {
		id := "agent-" + string(to)
		_, err := repo.CreateIfNoConflict(ctx, newQueued(id, "dev-1", "fetch_logs"))
		require.NoError(t, err)
		_, applied, err := repo.Transition(ctx, models.CommandTransition{
			ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: to, Now: pgNow.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
		require.True(t, applied)
	}
	// Claimed rows.
	_, err := repo.CreateIfNoConflict(ctx, newQueued("claimed", "dev-1", "fetch_logs"))
	require.NoError(t, err)
	_, claimed, err := repo.ClaimByID(ctx, "claimed", pgNow)
	require.NoError(t, err)
	require.True(t, claimed)

	var missing int
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM commands
		 WHERE status IN ('dispatched','sent','acknowledged','running','verifying') AND deadline_at IS NULL`).Scan(&missing))
	assert.Zero(t, missing, "[SEC] every active non-queued row must carry a deadline_at or the sweeper cannot time it out")

	var total int
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM commands WHERE status IN ('dispatched','acknowledged','running','verifying')`).Scan(&total))
	assert.Equal(t, 5, total, "the legacy sent row, three agent edges and the claim")
}

func TestCL_SEC_PG_ClaimSkipsExpiredEvenIfExpireStepFails(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.insertRaw(rawCommand{ID: "stale-reboot", DeviceID: "dev-1", Action: "reboot", Status: "queued",
		CreatedAt: pgNow.Add(-time.Hour), ExpiresAt: pgNow.Add(-45 * time.Minute)})
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)

	got, err := repo.ClaimQueued(ctx, "dev-1", pgNow, 10)
	require.NoError(t, err)
	assert.Empty(t, got, "[SEC] ClaimQueued must exclude an expired queued reboot even if nothing expired it first")

	_, claimed, err := repo.ClaimByID(ctx, "stale-reboot", pgNow)
	require.NoError(t, err)
	assert.False(t, claimed, "[SEC] ClaimByID must exclude an expired queued reboot")
	assert.Equal(t, "queued", env.statusOf("stale-reboot"), "a refused claim must not change the row")

	exactly := pgNow
	env.insertRaw(rawCommand{ID: "boundary", DeviceID: "dev-1", Action: "reboot", Status: "queued", ExpiresAt: exactly})
	_, claimed, err = repo.ClaimByID(ctx, "boundary", pgNow)
	require.NoError(t, err)
	assert.False(t, claimed, "expires_at == now is expired: the claim predicate is a strict expires_at > now")
}

func TestCL_PG_CreateIfNoConflictSerializesOverlappingKeys(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	repo := env.repo()
	ctx := withDeadline(t, 60*time.Second)

	var created atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := newQueued("race-"+string(rune('a'+i)), "dev-1", "docker_bulk_update", "docker:compose:/srv/a", "docker:container:x"+string(rune('a'+i)))
			conflict, err := repo.CreateIfNoConflict(ctx, cmd)
			assert.NoError(t, err)
			if conflict == nil {
				created.Add(1)
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, int32(1), created.Load(), "[BR-008] overlapping multi-key creates must admit exactly one")

	conflict, err := repo.CreateIfNoConflict(ctx, newQueued("other-device", "dev-2", "docker_update", "docker:compose:/srv/a"))
	require.NoError(t, err)
	assert.Nil(t, conflict, "the same key on another device never conflicts")

	conflict, err = repo.CreateIfNoConflict(ctx, newQueued("host-key", "dev-1", "reboot", "host"))
	require.NoError(t, err)
	assert.Nil(t, conflict, "[D-013] a host key does not conflict with Docker keys")

	holder, err := repo.CreateIfNoConflict(ctx, newQueued("second-reboot", "dev-1", "reboot", "host"))
	require.NoError(t, err)
	require.NotNil(t, holder, "[AC-070] a second reboot while the first is non-terminal conflicts")
	assert.Equal(t, "host-key", holder.ID)
	var n int
	require.NoError(t, env.Pool.QueryRow(context.Background(), `SELECT count(*) FROM commands WHERE id='second-reboot'`).Scan(&n))
	assert.Zero(t, n, "[AC-068] a conflicting create must not leave a row")

	// A terminal holder frees the key (timed_out counts as terminal, D-004).
	msg := "no result"
	_, applied, err := repo.Transition(ctx, models.CommandTransition{ID: "host-key", Actor: models.ActorServer, To: models.CommandTimedOut, From: []models.CommandState{models.CommandQueued}, Now: pgNow})
	require.NoError(t, err)
	assert.False(t, applied, "queued -> timed_out is not an edge")
	_, applied, err = repo.Transition(ctx, models.CommandTransition{ID: "host-key", Actor: models.ActorOperator, To: models.CommandCancelled, ResultMsg: &msg, Now: pgNow})
	require.NoError(t, err)
	require.True(t, applied)
	holder, err = repo.CreateIfNoConflict(ctx, newQueued("third-reboot", "dev-1", "reboot", "host"))
	require.NoError(t, err)
	assert.Nil(t, holder, "[AC-069] a terminal previous command allows a new one")
}

func TestCL_PG_TransitionWritesTimestampsProgressAndLateResult(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)
	_, err := repo.CreateIfNoConflict(ctx, newQueued("c-1", "dev-1", "docker_update", "docker:container:web"))
	require.NoError(t, err)

	t0 := pgNow
	claimed, ok, err := repo.ClaimByID(ctx, "c-1", t0)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, claimed.DeadlineAt)
	deadline := *claimed.DeadlineAt
	assert.True(t, deadline.Equal(t0.Add(900*time.Second)), "deadline_at = claim time + exec_deadline_s")

	step := "pull"
	pct := 40
	running, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandRunning,
		Step: &step, ProgressSet: true, Progress: &pct, Now: t0.Add(time.Minute)})
	require.NoError(t, err)
	require.True(t, applied)
	require.NotNil(t, running.StartedAt)
	assert.Equal(t, "pull", running.Step)
	require.NotNil(t, running.Progress)
	assert.Equal(t, 40, *running.Progress)
	assert.True(t, running.DeadlineAt.Equal(deadline), "[SEC] progress must not extend deadline_at")

	again, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandRunning,
		ProgressSet: true, Progress: nil, Now: t0.Add(2 * time.Minute)})
	require.NoError(t, err)
	require.True(t, applied, "running -> running is legal")
	assert.Nil(t, again.Progress, "a nil progress with ProgressSet writes NULL (AC-095)")
	assert.Equal(t, "pull", again.Step, "an unset step leaves the column unchanged")
	assert.True(t, again.StartedAt.Equal(*running.StartedAt), "started_at is COALESCEd, never moved")

	verifying, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandVerifying, Now: t0.Add(3 * time.Minute)})
	require.NoError(t, err)
	require.True(t, applied)
	require.NotNil(t, verifying.VerifyingAt)

	_, applied, err = repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandVerifying, Now: t0.Add(4 * time.Minute)})
	require.NoError(t, err)
	assert.False(t, applied, "verifying -> verifying is not an edge")

	_, applied, err = repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-2", Actor: models.ActorAgent, To: models.CommandSucceeded, Now: t0.Add(5 * time.Minute)})
	require.NoError(t, err)
	assert.False(t, applied, "[AC-024] a transition scoped to another device must not apply")

	timedOut, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", Actor: models.ActorServer, To: models.CommandTimedOut, Now: t0.Add(40 * time.Minute)})
	require.NoError(t, err)
	require.True(t, applied)
	require.NotNil(t, timedOut.FinishedAt)

	msg, tail, fstep := "boom", "line1\nline2", "recreate"
	exit, dur := 1, int64(1234)
	late, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandFailed,
		ResultMsg: &msg, StderrTail: &tail, FailureStep: &fstep, ExitCode: &exit, DurationMs: &dur, Now: t0.Add(41 * time.Minute)})
	require.NoError(t, err)
	require.True(t, applied, "[AC-087] timed_out accepts one late result")
	assert.Equal(t, "failed", late.Status)
	assert.Equal(t, "recreate", late.FailureStep)
	assert.Equal(t, "line1\nline2", late.StderrTail)
	require.NotNil(t, late.ExitCode)
	assert.Equal(t, 1, *late.ExitCode)
	require.NotNil(t, late.DurationMs)
	assert.Equal(t, int64(1234), *late.DurationMs)

	_, applied, err = repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded, Now: t0.Add(42 * time.Minute)})
	require.NoError(t, err)
	assert.False(t, applied, "[AC-087] only one late result is accepted")
}

func TestCL_PG_RevertClearsClaimAndMarksAmbiguous(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)
	_, err := repo.CreateIfNoConflict(ctx, newQueued("c-1", "dev-1", "reboot", "host"))
	require.NoError(t, err)
	_, ok, err := repo.ClaimByID(ctx, "c-1", pgNow)
	require.NoError(t, err)
	require.True(t, ok)

	msg := "ws send failed; delivery uncertain; re-queued for heartbeat delivery"
	reverted, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", Actor: models.ActorServer, To: models.CommandQueued,
		From: []models.CommandState{models.CommandDispatched}, ResultMsg: &msg, Now: pgNow.Add(time.Second)})
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, "queued", reverted.Status)
	assert.Nil(t, reverted.DispatchedAt)
	assert.Nil(t, reverted.DeadlineAt)
	assert.True(t, reverted.SendAmbiguous, "[SEC-016] a revert flags the send as ambiguous")

	_, applied, err = repo.Transition(ctx, models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandAcknowledged, Now: pgNow.Add(2 * time.Second)})
	require.NoError(t, err)
	assert.True(t, applied, "an agent report is proof of delivery even after a revert")

	other := newQueued("c-2", "dev-1", "fetch_logs")
	_, err = repo.CreateIfNoConflict(ctx, other)
	require.NoError(t, err)
	_, applied, err = repo.Transition(ctx, models.CommandTransition{
		ID: "c-2", Actor: models.ActorServer, To: models.CommandQueued,
		From: []models.CommandState{models.CommandDispatched}, Now: pgNow})
	require.NoError(t, err)
	assert.False(t, applied, "[AC-104] only a dispatched row can be reverted")
}

func TestCL_PG_ListFiltersUseAliasExpansion(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)
	env.insertRaw(rawCommand{ID: "a", DeviceID: "dev-1", Action: "reboot", Status: "pending", CreatedAt: pgNow.Add(-3 * time.Minute)})
	env.insertRaw(rawCommand{ID: "b", DeviceID: "dev-1", Action: "reboot", Status: "queued", CreatedAt: pgNow.Add(-2 * time.Minute)})
	env.insertRaw(rawCommand{ID: "c", DeviceID: "dev-1", Action: "reboot", Status: "success", CreatedAt: pgNow.Add(-time.Minute)})
	env.insertRaw(rawCommand{ID: "d", DeviceID: "dev-2", Action: "reboot", Status: "queued", CreatedAt: pgNow})

	stored, ok := models.ParseStatusFilter("queued")
	require.True(t, ok)
	got, err := repo.ListByDeviceFiltered(ctx, "dev-1", 50, 0, stored, "", "")
	require.NoError(t, err)
	require.Len(t, got, 2, "filtering by queued must also match the legacy pending alias")
	assert.Equal(t, []string{"b", "a"}, []string{got[0].ID, got[1].ID}, "newest first")

	active, err := repo.ListActive(ctx, "dev-1", "")
	require.NoError(t, err)
	assert.Len(t, active, 2, "ListActive is device scoped and excludes terminal rows")
}

func TestCL_SEC003_PG_LatestQueriesIgnoreFutureRows(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	ctx := withDeadline(t, 30*time.Second)
	repo := &TelemetryRepo{db: &DB{Pool: env.Pool}}

	now := time.Now().UTC()
	data := `{"docker":{"available":true,"containers":[{"id":"c1","name":"%s","state":"running"}]},
		"gpu_telemetry":{"gpus":[{"name":"%s"}]},"updates":{"pending_updates":%d}}`
	insert := func(device string, ts time.Time, label string, pending int) {
		env.Exec(`INSERT INTO telemetry_snapshots (device_id, timestamp, data) VALUES ($1, $2, $3::jsonb)`,
			device, ts, fmt.Sprintf(data, label, label, pending))
	}
	insert("dev-1", now.Add(-time.Minute), "real", 3)
	insert("dev-1", now.Add(365*24*time.Hour), "future", 99) // a hostile or skewed clock
	insert("dev-2", now.Add(365*24*time.Hour), "only-future", 99)

	latest, err := repo.GetLatestSnapshot(ctx, "dev-1")
	require.NoError(t, err)
	assert.Equal(t, "real", latest.Data.Docker.Containers[0].Name, "[SEC-003] GetLatestSnapshot must skip the future row")

	ts, ok, err := repo.LatestSnapshotTime(ctx, "dev-1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, ts.Before(now), "[SEC-003] LatestSnapshotTime must be the real snapshot, got %s", ts)
	_, ok, err = repo.LatestSnapshotTime(ctx, "dev-2")
	require.NoError(t, err)
	assert.False(t, ok, "a device with only future rows has no current state")

	all, err := repo.GetAllLatestSnapshots(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "[SEC-003] GetAllLatestSnapshots must skip dev-2's only (future) row")
	assert.Equal(t, "dev-1", all[0].DeviceID)
	assert.Equal(t, "real", all[0].Data.Docker.Containers[0].Name)

	summaries, err := repo.GetAllLatestSummaries(ctx)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, 3, summaries[0].Updates.PendingUpdates, "[SEC-003] the summary must come from the real row")

	board, err := repo.GetFleetContainerLeaderboard(ctx)
	require.NoError(t, err)
	require.Len(t, board, 1)
	assert.Equal(t, "real", board[0].Containers[0].Name)

	gpus, err := repo.GetGPUDeviceIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"dev-1"}, gpus)

	history, err := repo.GetHistory(ctx, "dev-1", 10, 0)
	require.NoError(t, err)
	assert.Len(t, history, 2, "history keeps every snapshot")

	exact := now.Add(models.TelemetryFutureTolerance - 5*time.Second)
	insert("dev-2", exact, "within-tolerance", 1)
	ts, ok, err = repo.LatestSnapshotTime(ctx, "dev-2")
	require.NoError(t, err)
	assert.True(t, ok, "a snapshot inside the 2m tolerance is current")
	assert.WithinDuration(t, exact, ts, time.Second)
}
