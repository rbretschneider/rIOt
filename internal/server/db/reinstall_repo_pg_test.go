package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PostgreSQL-gated tests of migration 000035, the reinstall repository methods
// and the origin-guarded transition SQL (EXPECT-008, ADD section 8, "R8"). They
// skip unless RIOT_TEST_PG_URL names a test database (pgTestURL).

// reT0 is the receive time of the first notice in these tests (13:15 UTC on the
// story date, the time of the PiCUPS and PiVPN pages).
var reT0 = time.Date(2026, 10, 27, 13, 15, 0, 0, time.UTC)

func reNotice(device string, at time.Time) models.ReinstallNotice {
	return models.ReinstallNotice{DeviceID: device, Hostname: "PiVPN", At: at}
}

func (e *pgEnv) reRepo() *CommandRepo {
	e.T.Helper()
	return &CommandRepo{db: &DB{Pool: e.Pool}}
}

// seedOnline inserts an online device (commands and the status read need it).
func (e *pgEnv) seedOnline(id string) {
	e.T.Helper()
	e.seedDevice(id)
	e.Exec(`UPDATE devices SET status='online' WHERE id=$1`, id)
}

// tryInsertRE inserts an installer row directly, to probe the CHECK.
func (e *pgEnv) tryInsertRE(r reRow) error {
	e.T.Helper()
	r.Origin = models.OriginInstaller
	if r.Action == "" {
		r.Action = "agent_update"
	}
	if r.ExecS == 0 {
		r.ExecS = 600
	}
	if r.Keys == nil {
		r.Keys = []string{}
	}
	params := r.Params
	if params == "" {
		params = "{}"
	}
	_, err := e.Pool.Exec(context.Background(),
		`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s,
			deadline_at, dispatched_at, target_keys, origin)
		 VALUES ($1, $2, $3, $4::jsonb, $5, $6, $6, $6, $7, $8, $9, $10, $11)`,
		r.ID, r.Device, r.Action, params, r.Status, reT0, r.ExecS, r.Deadline, r.Dispatched, r.Keys, r.Origin)
	return err
}

func TestEX8_PG_Migration000035UpDownUp(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	has := func(name string) bool {
		return env.scalarInt(`SELECT count(*) FROM pg_constraint WHERE conname=$1 AND conrelid='commands'::regclass`, name) == 1
	}
	hasIdx := func(name string) bool {
		return env.scalarInt(`SELECT count(*) FROM pg_indexes WHERE schemaname=$1 AND indexname=$2`, env.Schema, name) == 1
	}
	require.True(t, has("commands_reinstall_shape") && has("commands_origin_check") && has("commands_self_update_shape"),
		"up adds the reinstall shape CHECK and keeps the other CHECKs")
	require.True(t, hasIdx("idx_commands_reinstall_open") && hasIdx("idx_commands_reinstall_recent"), "up adds both partial indexes")

	env.seedOnline("dev-1")
	res, err := repo.CreateReinstall(withDeadline(t, 10*time.Second), reNotice("dev-1", reT0))
	require.NoError(t, err)
	require.Equal(t, models.ReinstallCreated, res.Disposition)
	env.Exec(`INSERT INTO command_escalations (command_id, target, device_id, created_at) VALUES ($1, 'outcome', 'dev-1', $2)`, res.Command.ID, reT0)
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s)
		VALUES ('ordinary', 'dev-1', 'reboot', '{}'::jsonb, 'queued', $1, $1, $1, 600)`, reT0)

	require.NoError(t, env.Mig.Migrate(34), "down")
	assert.False(t, has("commands_reinstall_shape"), "down drops the shape CHECK")
	assert.False(t, hasIdx("idx_commands_reinstall_open") || hasIdx("idx_commands_reinstall_recent"), "down drops both indexes")
	assert.Equal(t, 0, env.countWhere(`origin='installer'`), "down deletes the reinstall rows")
	assert.Equal(t, 1, env.countWhere(`id='ordinary'`), "down keeps every other command")
	assert.Equal(t, 0, env.scalarInt(`SELECT count(*) FROM command_escalations`), "markers cascade with their rows")
	require.True(t, has("commands_origin_check") && has("commands_self_update_shape"), "the EXPECT-002 constraints stay")
	_, err = env.Pool.Exec(context.Background(), `UPDATE commands SET origin='installer' WHERE id='ordinary'`)
	require.Error(t, err, "after down, the installer origin is rejected by the origin CHECK")

	require.NoError(t, env.Mig.Up(), "up again")
	assert.True(t, has("commands_reinstall_shape") && hasIdx("idx_commands_reinstall_open") && hasIdx("idx_commands_reinstall_recent"),
		"second up recreates every object")
	res, err = repo.CreateReinstall(withDeadline(t, 10*time.Second), reNotice("dev-1", reT0.Add(time.Hour)))
	require.NoError(t, err, "a record can be created after down and up")
	assert.Equal(t, models.ReinstallCreated, res.Disposition)
}

func TestEX8_PG_OriginCheck(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedOnline("dev-1")
	d, dl := reT0, reT0.Add(10*time.Minute)
	require.NoError(t, env.tryInsertRE(reRow{ID: "ok", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}),
		"a well-formed installer row is accepted")
	err := env.tryInsertSU(suRow{ID: "bad", Device: "dev-1", Status: "succeeded", Dispatched: &d, Deadline: &dl, Origin: "bogus"})
	require.Error(t, err, "an unknown origin is rejected")
	assert.Contains(t, err.Error(), "commands_origin_check", "by the origin CHECK")
	require.NoError(t, env.tryInsertSU(suRow{ID: "su", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}),
		"a self-update row still inserts next to the installer row")
	for _, o := range []string{"user", "auto_update", "auto_patch"} {
		env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, origin)
			VALUES ($1, 'dev-1', 'reboot', '{}'::jsonb, 'queued', $2, $2, $2, 600, $1)`, o, reT0)
	}
	assert.Equal(t, 5, env.countWhere(`device_id='dev-1'`), "the old origins still insert")
}

func TestEX8_PG_CheckShape(t *testing.T) {
	env := newPGEnv(t, 0)
	d := reT0
	bound := reT0.Add(10 * time.Minute)
	for i, status := range []string{"queued", "pending", "dispatched", "sent", "acknowledged", "verifying", "failed", "cancelled", "expired", "success", "error"} {
		dev := fmt.Sprintf("dev-s%d", i)
		env.seedOnline(dev)
		err := env.tryInsertRE(reRow{ID: "st-" + status, Device: dev, Status: status, Dispatched: &d, Deadline: &bound})
		require.Error(t, err, "status %q must be rejected for a reinstall row", status)
		assert.Contains(t, err.Error(), "commands_reinstall_shape", "status %q is rejected by the shape CHECK", status)
	}
	for i, status := range []string{"running", "succeeded", "timed_out"} {
		dev := fmt.Sprintf("dev-ok-%d", i)
		env.seedOnline(dev)
		require.NoError(t, env.tryInsertRE(reRow{ID: "ok-" + status, Device: dev, Status: status, Dispatched: &d, Deadline: &bound}),
			"status %q is a legal reinstall state", status)
	}

	cases := []struct {
		name    string
		row     reRow
		wantErr bool
	}{
		{"deadline exactly D+10m", reRow{Dispatched: &d, Deadline: tpg(d.Add(10 * time.Minute))}, false},
		{"deadline D+10m+1s", reRow{Dispatched: &d, Deadline: tpg(d.Add(10*time.Minute + time.Second))}, true},
		{"deadline D+15m (a command's deadline)", reRow{Dispatched: &d, Deadline: tpg(d.Add(15 * time.Minute)), ExecS: 900}, true},
		{"exec_deadline_s 601", reRow{Dispatched: &d, Deadline: &bound, ExecS: 601}, true},
		{"a target key", reRow{Dispatched: &d, Deadline: &bound, Keys: []string{"host"}}, true},
		{"non-empty params", reRow{Dispatched: &d, Deadline: &bound, Params: `{"from_version":"2.82.0"}`}, true},
		{"no dispatched_at", reRow{Deadline: &bound}, true},
		{"no deadline_at", reRow{Dispatched: &d}, true},
		{"another action", reRow{Dispatched: &d, Deadline: &bound, Action: "reboot"}, true},
	}
	for i, tc := range cases {
		dev := fmt.Sprintf("dev-c%d", i)
		env.seedOnline(dev)
		tc.row.ID, tc.row.Device, tc.row.Status = fmt.Sprintf("row-%d", i), dev, "running"
		err := env.tryInsertRE(tc.row)
		if tc.wantErr {
			require.Error(t, err, "%s must be rejected", tc.name)
			assert.Contains(t, err.Error(), "commands_reinstall_shape", "%s: rejected by the shape CHECK", tc.name)
		} else {
			require.NoError(t, err, "%s must be accepted", tc.name)
		}
	}

	_, err := env.Pool.Exec(context.Background(), `UPDATE commands SET deadline_at = deadline_at + interval '1 minute' WHERE id='row-0'`)
	require.Error(t, err, "deadline_at cannot be extended past D+10m")
	_, err = env.Pool.Exec(context.Background(), `UPDATE commands SET target_keys = ARRAY['host'] WHERE id='row-0'`)
	require.Error(t, err, "a reinstall row cannot take a target key")
	_, err = env.Pool.Exec(context.Background(), `UPDATE commands SET status='queued' WHERE id='row-0'`)
	require.Error(t, err, "a reinstall row cannot return to queued")
	_, err = env.Pool.Exec(context.Background(), `UPDATE commands SET params='{"x":1}'::jsonb WHERE id='row-0'`)
	require.Error(t, err, "a reinstall row cannot take params")
}

func TestEX8_PG_UniqueOpenRecord(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedOnline("dev-1")
	env.seedOnline("dev-2")
	d, dl := reT0, reT0.Add(10*time.Minute)
	require.NoError(t, env.tryInsertRE(reRow{ID: "a", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}))
	err := env.tryInsertRE(reRow{ID: "dup", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl})
	require.Error(t, err, "a second running record of the same device is rejected")
	assert.Contains(t, err.Error(), "idx_commands_reinstall_open", "by the unique index")
	require.NoError(t, env.tryInsertRE(reRow{ID: "done", Device: "dev-1", Status: "succeeded", Dispatched: &d, Deadline: &dl}), "terminal rows are not limited")
	require.NoError(t, env.tryInsertRE(reRow{ID: "other", Device: "dev-2", Status: "running", Dispatched: &d, Deadline: &dl}), "another device may have its own open record")
	// A self-update record and a reinstall record can be open together (AC-023).
	require.NoError(t, env.tryInsertSU(suRow{ID: "su", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}),
		"[AD-017] a self-update record may be open next to a reinstall record")
	env.Exec(`UPDATE commands SET status='succeeded', finished_at=$1 WHERE id='a'`, reT0.Add(time.Minute))
	require.NoError(t, env.tryInsertRE(reRow{ID: "b", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}), "after the open record ended, a new one may open")
}

func TestEX8_AC015_PG_OfflineDeviceNoRow(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	ctx := withDeadline(t, 20*time.Second)
	env.seedDevice("dev-off")
	env.Exec(`UPDATE devices SET status='offline' WHERE id='dev-off'`)
	res, err := repo.CreateReinstall(ctx, reNotice("dev-off", reT0))
	require.NoError(t, err)
	assert.Equal(t, models.ReinstallDeviceOffline, res.Disposition, "[AC-015] an offline device gets no record")
	assert.Nil(t, res.Command)
	res, err = repo.CreateReinstall(ctx, reNotice("no-such-device", reT0))
	require.NoError(t, err)
	assert.Equal(t, models.ReinstallDeviceOffline, res.Disposition, "an unknown device is the same")
	assert.Equal(t, 0, env.countWhere(`origin='installer'`), "no row exists")
	assert.Equal(t, 0, env.scalarInt(`SELECT count(*) FROM events`), "and no event")

	env.seedDevice("dev-warn")
	env.Exec(`UPDATE devices SET status='warning' WHERE id='dev-warn'`)
	res, err = repo.CreateReinstall(ctx, reNotice("dev-warn", reT0))
	require.NoError(t, err)
	assert.Equal(t, models.ReinstallCreated, res.Disposition, "a warning device is live (SEC-004)")
}

func TestEX8_PG_CreateShapeAndSecondNoticeAbsorbed(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	env.seedOnline("dev-1")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateReinstall(ctx, reNotice("dev-1", reT0))
	require.NoError(t, err)
	require.Equal(t, models.ReinstallCreated, res.Disposition)
	c := res.Command
	require.NotNil(t, c)
	assert.Equal(t, "running", c.Status)
	assert.Equal(t, models.OriginInstaller, c.Origin)
	assert.Equal(t, models.ActionAgentUpdate, c.Action)
	assert.Equal(t, models.ReinstallStepWaitingForRestart, c.Step)
	assert.Empty(t, c.Params, "params stay empty")
	assert.Empty(t, c.TargetKeys)
	assert.Equal(t, "system", c.ActorKind)
	assert.True(t, c.DispatchedAt.Equal(reT0) && c.DeadlineAt.Equal(reT0.Add(10*time.Minute)), "D = the notice, deadline = D + 10m")
	assert.Equal(t, 600, c.ExecDeadlineS)

	res, err = repo.CreateReinstall(ctx, reNotice("dev-1", reT0.Add(5*time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, models.ReinstallOpenRecord, res.Disposition, "an open record absorbs the notice")
	assert.Equal(t, 1, env.countWhere(`origin='installer'`))
	open, err := repo.OpenReinstall(ctx, "dev-1")
	require.NoError(t, err)
	require.NotNil(t, open)
	assert.Equal(t, c.ID, open.ID)
	none, err := repo.OpenReinstall(ctx, "dev-2")
	require.NoError(t, err)
	assert.Nil(t, none)
	assert.Zero(t, env.scalarInt(`SELECT count(*) FROM docker_audit`), "no docker_audit row is written")
}

func TestEX8_AC016_PG_ConcurrentNoticesOneRecordOneCapEvent(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	env.seedOnline("dev-1")
	ctx := withDeadline(t, 60*time.Second)

	run := func(n int, at func(i int) time.Time) (created, capped, open int, capEvents int) {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				res, err := repo.CreateReinstall(ctx, reNotice("dev-1", at(i)))
				if !assert.NoError(t, err) {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				switch res.Disposition {
				case models.ReinstallCreated:
					created++
				case models.ReinstallCapped:
					capped++
				case models.ReinstallOpenRecord:
					open++
				}
				if res.CapEvent != nil {
					capEvents++
				}
			}(i)
		}
		wg.Wait()
		return
	}

	created, _, open, _ := run(30, func(int) time.Time { return reT0 })
	assert.Equal(t, 1, created, "[AC-016] 30 concurrent notices create one record")
	assert.Equal(t, 29, open, "and 29 are absorbed")
	assert.Equal(t, 1, env.countWhere(`origin='installer'`))

	// Fill the cap with closed records, then race 30 more.
	for i := 1; i < models.ReinstallRateCap; i++ {
		env.Exec(`UPDATE commands SET status='succeeded', finished_at=$1 WHERE origin='installer' AND status='running'`, reT0.Add(time.Duration(i)*time.Hour))
		res, err := repo.CreateReinstall(ctx, reNotice("dev-1", reT0.Add(time.Duration(i)*time.Hour+time.Minute)))
		require.NoError(t, err)
		require.Equal(t, models.ReinstallCreated, res.Disposition)
	}
	env.Exec(`UPDATE commands SET status='succeeded', finished_at=$1 WHERE origin='installer' AND status='running'`, reT0.Add(7*time.Hour))
	require.Equal(t, models.ReinstallRateCap, env.countWhere(`origin='installer'`))

	created, capped, _, capEvents := run(30, func(int) time.Time { return reT0.Add(8 * time.Hour) })
	assert.Zero(t, created, "[AC-016] over the cap nothing is created")
	assert.Equal(t, 30, capped)
	assert.Equal(t, 1, capEvents, "exactly one writer wrote the cap event")
	assert.Equal(t, 1, env.scalarInt(`SELECT count(*) FROM events WHERE type='agent_reinstall_paused' AND command_id=''`), "and one row exists")
	assert.Equal(t, models.ReinstallRateCap, env.countWhere(`origin='installer'`), "the record count did not move")

	// Rolling window: 24h after the oldest record a new one is created again.
	res, err := repo.CreateReinstall(ctx, reNotice("dev-1", reT0.Add(24*time.Hour+time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, models.ReinstallCreated, res.Disposition, "once the oldest record is more than 24h old, a notice creates a record again")
}

func TestEX8_AC020_PG_StaleRetireInTx(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	env.seedOnline("dev-1")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateReinstall(ctx, reNotice("dev-1", reT0))
	require.NoError(t, err)
	stale := res.Command

	// Exactly bound + 1h: the open record still absorbs the notice.
	res, err = repo.CreateReinstall(ctx, reNotice("dev-1", reT0.Add(10*time.Minute+time.Hour)))
	require.NoError(t, err)
	assert.Equal(t, models.ReinstallOpenRecord, res.Disposition, "bound + 1h exactly: still open")
	assert.Nil(t, res.Retired)

	res, err = repo.CreateReinstall(ctx, reNotice("dev-1", reT0.Add(10*time.Minute+time.Hour+time.Second)))
	require.NoError(t, err)
	require.Equal(t, models.ReinstallCreated, res.Disposition, "[NFR-004] a stale open record never blocks a new one")
	require.NotNil(t, res.Retired)
	assert.Equal(t, stale.ID, res.Retired.ID)
	assert.Equal(t, "timed_out", res.Retired.Status)
	assert.Equal(t, models.ReinstallFailureUnresolved, res.Retired.FailureStep)
	assert.Equal(t, models.ReinstallStaleResultMessage, res.Retired.ResultMsg)
	assert.Equal(t, 2, env.countWhere(`origin='installer'`))
	assert.Equal(t, 1, env.countWhere(`origin='installer' AND status='running'`), "exactly one open record remains")
}

func TestEX8_PG_Guards(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	env.seedOnline("dev-1")
	env.seedOnline("dev-2")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateReinstall(ctx, reNotice("dev-1", reT0))
	require.NoError(t, err)
	id := res.Command.ID
	now := reT0.Add(time.Minute)

	for _, tc := range []struct {
		name string
		t    models.CommandTransition
	}{
		{"agent result succeeded", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded}},
		{"agent result failed", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandFailed}},
		{"agent progress running", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandRunning}},
		{"agent ack", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandAcknowledged}},
		{"operator cancel", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorOperator, To: models.CommandCancelled}},
		{"server timed_out (the sweeper edge)", models.CommandTransition{ID: id, Actor: models.ActorServer, To: models.CommandTimedOut}},
		{"server timed_out scoped to the device", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorServer, To: models.CommandTimedOut}},
		{"server revert", models.CommandTransition{ID: id, Actor: models.ActorServer, To: models.CommandQueued}},
		{"self-update actor", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorSelfUpdate, To: models.CommandTimedOut}},
		{"reinstall actor without a device", models.CommandTransition{ID: id, Actor: models.ActorReinstall, To: models.CommandSucceeded}},
		{"reinstall actor for another device", models.CommandTransition{ID: id, DeviceID: "dev-2", Actor: models.ActorReinstall, To: models.CommandSucceeded}},
		{"reinstall actor to verifying (not an edge)", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorReinstall, To: models.CommandVerifying}},
		{"reinstall actor to failed (not an edge)", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorReinstall, To: models.CommandFailed}},
	} {
		tc.t.Now = now
		got, applied, err := repo.Transition(ctx, tc.t)
		require.NoError(t, err, tc.name)
		assert.False(t, applied, "[C-2] %s must not apply to a reinstall row", tc.name)
		assert.Nil(t, got, tc.name)
	}
	var status, step, failure string
	require.NoError(t, env.Pool.QueryRow(context.Background(), `SELECT status, step, failure_step FROM commands WHERE id=$1`, id).Scan(&status, &step, &failure))
	assert.Equal(t, "running", status, "the row is unchanged after every refused edge")
	assert.Equal(t, models.ReinstallStepWaitingForRestart, step)
	assert.Empty(t, failure)

	// ActorReinstall cannot touch an ordinary or a self-update row.
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, dispatched_at)
		VALUES ('ordinary', 'dev-1', 'agent_update', '{}'::jsonb, 'running', $1, $1, $1, 600, $2, $1)`, reT0, reT0.Add(10*time.Minute))
	d, dl := reT0, reT0.Add(10*time.Minute)
	require.NoError(t, env.tryInsertSU(suRow{ID: "self", Device: "dev-2", Status: "running", Dispatched: &d, Deadline: &dl}))
	for _, target := range []struct{ id, dev string }{{"ordinary", "dev-1"}, {"self", "dev-2"}} {
		for _, to := range []models.CommandState{models.CommandSucceeded, models.CommandTimedOut} {
			_, applied, err := repo.Transition(ctx, models.CommandTransition{ID: target.id, DeviceID: target.dev, Actor: models.ActorReinstall, To: to, Now: now})
			require.NoError(t, err)
			assert.False(t, applied, "[C-2] ActorReinstall -> %s must not apply to %s", to, target.id)
		}
	}

	// The reinstall actor applies, and only to its own rows.
	stepC := models.ReinstallStepConfirmed
	msg := models.ReinstallConfirmedResultMessage
	got, applied, err := repo.Transition(ctx, models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorReinstall,
		To: models.CommandSucceeded, Step: &stepC, ResultMsg: &msg, Now: now})
	require.NoError(t, err)
	require.True(t, applied, "ActorReinstall running -> succeeded applies")
	assert.Equal(t, "succeeded", got.Status)
	require.NotNil(t, got.FinishedAt)
	assert.True(t, got.FinishedAt.Equal(now), "finished_at is the transition time")
	_, applied, err = repo.Transition(ctx, models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorReinstall, To: models.CommandTimedOut, Now: now})
	require.NoError(t, err)
	assert.False(t, applied, "a terminal record is not moved again")
}

func TestEX8_PG_ListOverdueExcludesReinstall(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	env.seedOnline("dev-1")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateReinstall(ctx, reNotice("dev-1", reT0))
	require.NoError(t, err)
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, dispatched_at)
		VALUES ('overdue', 'dev-1', 'docker_update', '{}'::jsonb, 'running', $1, $1, $1, 600, $2, $1)`, reT0, reT0.Add(10*time.Minute))
	due, err := repo.ListOverdue(ctx, "", reT0.Add(3*time.Hour), 100)
	require.NoError(t, err)
	var ids []string
	for _, c := range due {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"overdue"}, ids, "[AD-009] the sweeper's list holds the ordinary command and never the reinstall record %s", res.Command.ID)
}

func TestEX8_AC023_PG_SelfUpdateAttributionIgnoresReinstall(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.reRepo()
	env.seedOnline("dev-1")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateReinstall(ctx, reNotice("dev-1", reT0))
	require.NoError(t, err)
	require.Equal(t, models.ReinstallCreated, res.Disposition)

	su, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", reT0.Add(2*time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateCreated, su.Disposition,
		"[AD-017] a running reinstall row never captures a self-update start: a record is created")
	assert.NotEqual(t, res.Command.ID, su.LinkID)

	// Also after the reinstall record finished within the attribution window.
	env.Exec(`UPDATE commands SET status='succeeded', finished_at=$1 WHERE id=$2`, reT0.Add(3*time.Minute), res.Command.ID)
	env.Exec(`UPDATE commands SET status='succeeded', finished_at=$1 WHERE id=$2`, reT0.Add(3*time.Minute), su.LinkID)
	su2, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", reT0.Add(5*time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateCreated, su2.Disposition, "a recently finished reinstall row does not attribute either")

	// The reverse: a self-update record does not block a reinstall record.
	env.seedOnline("dev-2")
	su3, err := repo.CreateSelfUpdate(ctx, suStart("dev-2", reT0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, su3.Disposition)
	re, err := repo.CreateReinstall(ctx, reNotice("dev-2", reT0.Add(time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, models.ReinstallCreated, re.Disposition, "[AC-023] both records can be open on one device")
}

// reRow is a reinstall row inserted directly, to probe the CHECK.
type reRow struct {
	ID, Device, Action, Status, Origin, Params string
	Dispatched, Deadline                       *time.Time
	ExecS                                      int
	Keys                                       []string
}
