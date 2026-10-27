package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-008 AD-005 / ADD section 4.5 (C-7): the in-memory MockCommandRepo must
// emulate the five-actor origin guard, the CHECK and the AD-017 attribution
// predicate of the SQL. This test runs the same transition against PostgreSQL
// and against the mock over a table of actor x origin x status x target state
// and requires identical outcomes. It is an external test package because
// testutil imports db.

func TestEX8_MockGuardParity(t *testing.T) {
	pool := newParityPool(t)
	ctx := context.Background()
	repo := db.NewCommandRepo(&db.DB{Pool: pool})
	mock := testutil.NewMockCommandRepo()
	t0 := time.Date(2026, 10, 27, 13, 15, 0, 0, time.UTC)
	now := t0.Add(time.Minute)
	_, err := pool.Exec(ctx, `INSERT INTO devices (id, short_id, hostname, status) VALUES ('dev-1', 'dev-1', 'h', 'online')`)
	require.NoError(t, err)

	actors := []models.CommandActor{models.ActorServer, models.ActorAgent, models.ActorOperator, models.ActorSelfUpdate, models.ActorReinstall}
	origins := []string{models.OriginUser, models.OriginAutoUpdate, models.OriginAutoPatch, models.OriginAgentSelfUpdate, models.OriginInstaller}
	selfStates := map[models.CommandState]bool{models.CommandRunning: true, models.CommandVerifying: true,
		models.CommandSucceeded: true, models.CommandFailed: true, models.CommandTimedOut: true}
	reStates := map[models.CommandState]bool{models.CommandRunning: true, models.CommandSucceeded: true, models.CommandTimedOut: true}

	compared := 0
	for _, origin := range origins {
		id := "row-" + origin
		exec := 900
		deadline := t0.Add(15 * time.Minute)
		if origin == models.OriginAgentSelfUpdate || origin == models.OriginInstaller {
			exec, deadline = 600, t0.Add(10*time.Minute)
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s,
				deadline_at, dispatched_at, origin, target_keys)
			 VALUES ($1, 'dev-1', 'agent_update', '{}'::jsonb, 'running', $2, $2, $2, $3, $4, $2, $5, '{}')`,
			id, t0, exec, deadline, origin)
		require.NoError(t, err, "insert %s row", origin)

		for _, from := range models.AllCommandStates() {
			if origin == models.OriginAgentSelfUpdate && !selfStates[from] {
				continue // the CHECK forbids the row
			}
			if origin == models.OriginInstaller && !reStates[from] {
				continue // the CHECK forbids the row
			}
			for _, to := range models.AllCommandStates() {
				if to == models.CommandDispatched {
					continue // the claim edge is ClaimQueued/ClaimByID, never Transition
				}
				for _, actor := range actors {
					tr := models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: actor, To: to, Now: now}
					_, err := pool.Exec(ctx, `UPDATE commands SET status=$2, finished_at=NULL, updated_at=$3, step='' WHERE id=$1`, id, string(from), t0)
					require.NoError(t, err)
					seeded := models.Command{ID: id, DeviceID: "dev-1", Action: "agent_update", Params: map[string]interface{}{},
						Status: string(from), Origin: origin, CreatedAt: t0, UpdatedAt: t0, ExpiresAt: t0, ExecDeadlineS: exec,
						DeadlineAt: &deadline, DispatchedAt: &t0, TargetKeys: []string{}}
					mock.Seed(&seeded)

					pgRow, pgApplied, pgErr := repo.Transition(ctx, tr)
					mockRow, mockApplied, mockErr := mock.Transition(ctx, tr)
					require.NoError(t, pgErr, "pg %s/%s %s->%s", origin, actor, from, to)
					require.NoError(t, mockErr)
					require.Equal(t, pgApplied, mockApplied, "parity: origin %s, actor %s, %s -> %s (pg applied=%v, mock applied=%v)",
						origin, actor, from, to, pgApplied, mockApplied)
					if pgApplied {
						assert.Equal(t, pgRow.Status, mockRow.Status, "parity: resulting status %s/%s %s->%s", origin, actor, from, to)
					}
					wantApplied := models.CanTransition(from, to, actor) &&
						(actor == models.ActorSelfUpdate) == (origin == models.OriginAgentSelfUpdate) &&
						(actor == models.ActorReinstall) == (origin == models.OriginInstaller)
					assert.Equal(t, wantApplied, pgApplied, "[C-2] origin %s, actor %s, %s -> %s", origin, actor, from, to)
					compared++
				}
			}
		}
	}
	assert.Greater(t, compared, 1000, "the table is large enough to mean something (%d comparisons)", compared)

	// The CHECK and the unique index: the mock refuses what PostgreSQL refuses.
	t1 := t0.Add(30 * time.Minute)
	badShape := func(mut func(c *models.Command)) *models.Command {
		dl := t1.Add(10 * time.Minute)
		c := &models.Command{ID: "mock-re", DeviceID: "dev-9", Action: "agent_update", Params: map[string]interface{}{}, Status: "running",
			Origin: models.OriginInstaller, CreatedAt: t1, UpdatedAt: t1, ExpiresAt: t1, ExecDeadlineS: 600,
			DeadlineAt: &dl, DispatchedAt: &t1, TargetKeys: []string{}}
		mut(c)
		return c
	}
	for name, mut := range map[string]func(c *models.Command){
		"queued":            func(c *models.Command) { c.Status = "queued" },
		"verifying":         func(c *models.Command) { c.Status = "verifying" },
		"failed":            func(c *models.Command) { c.Status = "failed" },
		"deadline D+10m+1s": func(c *models.Command) { d := t1.Add(10*time.Minute + time.Second); c.DeadlineAt = &d },
		"exec 601":          func(c *models.Command) { c.ExecDeadlineS = 601 },
		"target key":        func(c *models.Command) { c.TargetKeys = []string{"host"} },
		"params":            func(c *models.Command) { c.Params = map[string]interface{}{"from_version": "2.82.0"} },
		"no dispatched_at":  func(c *models.Command) { c.DispatchedAt = nil },
		"another action":    func(c *models.Command) { c.Action = "reboot" },
	} {
		_, err := mock.CreateIfNoConflict(ctx, badShape(mut))
		assert.Error(t, err, "[C-2] the mock emulates the reinstall shape CHECK: %s", name)
	}
	_, err = mock.CreateIfNoConflict(ctx, badShape(func(c *models.Command) {}))
	require.NoError(t, err, "a well-formed reinstall row is accepted")
	_, err = mock.CreateIfNoConflict(ctx, badShape(func(c *models.Command) { c.ID = "mock-re-2" }))
	assert.Error(t, err, "the mock emulates the unique open-record index")

	// ADD section 4.5 / AD-017: a running reinstall row never captures a
	// self-update start, in PostgreSQL and in the mock alike.
	_, err = pool.Exec(ctx, `INSERT INTO devices (id, short_id, hostname, status) VALUES ('dev-2', 'dev-2', 'h2', 'online')`)
	require.NoError(t, err)
	pgRe, err := repo.CreateReinstall(ctx, models.ReinstallNotice{DeviceID: "dev-2", Hostname: "h2", At: t0})
	require.NoError(t, err)
	require.Equal(t, models.ReinstallCreated, pgRe.Disposition)
	pgSu, err := repo.CreateSelfUpdate(ctx, models.SelfUpdateStart{DeviceID: "dev-2", Hostname: "h2", From: "2.82.0", To: "2.83.0", At: t0.Add(time.Minute)})
	require.NoError(t, err)

	m2 := testutil.NewMockCommandRepo()
	devs := testutil.NewMockDeviceRepo()
	devs.Devices["dev-2"] = &models.Device{ID: "dev-2", Hostname: "h2", Status: models.DeviceStatusOnline}
	m2.LinkDevices(devs)
	mockRe, err := m2.CreateReinstall(ctx, models.ReinstallNotice{DeviceID: "dev-2", Hostname: "h2", At: t0})
	require.NoError(t, err)
	require.Equal(t, models.ReinstallCreated, mockRe.Disposition)
	mockSu, err := m2.CreateSelfUpdate(ctx, models.SelfUpdateStart{DeviceID: "dev-2", Hostname: "h2", From: "2.82.0", To: "2.83.0", At: t0.Add(time.Minute)})
	require.NoError(t, err)
	assert.Equal(t, pgSu.Disposition, mockSu.Disposition, "parity: the self-update disposition next to a running reinstall row")
	assert.Equal(t, models.SelfUpdateCreated, pgSu.Disposition, "[AD-017] PostgreSQL creates a record")
	assert.NotEqual(t, pgRe.Command.ID, pgSu.LinkID, "and does not link the reinstall row")
	assert.NotEqual(t, mockRe.Command.ID, mockSu.LinkID, "the mock does not link the reinstall row either")
}
