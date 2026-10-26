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

// PostgreSQL-gated tests of migration 000034, the self-update repository
// methods and the origin-guarded transition SQL (EXPECT-002, ADD section 8,
// "R"). They skip unless RIOT_TEST_PG_URL names a test database (pgTestURL).

// suT0 is the receive time of the first start in these tests (17:51 UTC on the
// story date, the time of the 2026-10-26 release).
var suT0 = time.Date(2026, 10, 26, 17, 51, 0, 0, time.UTC)

func suStart(device string, at time.Time) models.SelfUpdateStart {
	return models.SelfUpdateStart{DeviceID: device, Hostname: "hawaii", From: "2.78.0", To: "2.80.0", At: at}
}

func (e *pgEnv) suRepo() *CommandRepo {
	e.T.Helper()
	return &CommandRepo{db: &DB{Pool: e.Pool}}
}

// suRow is a self-update row inserted directly, to probe the CHECK.
type suRow struct {
	ID, Device, Action, Status, Origin string
	Dispatched, Deadline               *time.Time
	ExecS                              int
	Keys                               []string
}

func (e *pgEnv) tryInsertSU(r suRow) error {
	e.T.Helper()
	if r.Action == "" {
		r.Action = "agent_update"
	}
	if r.Origin == "" {
		r.Origin = models.OriginAgentSelfUpdate
	}
	if r.ExecS == 0 {
		r.ExecS = 600
	}
	if r.Keys == nil {
		r.Keys = []string{}
	}
	_, err := e.Pool.Exec(context.Background(),
		`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s,
			deadline_at, dispatched_at, target_keys, origin)
		 VALUES ($1, $2, $3, '{}'::jsonb, $4, $5, $5, $5, $6, $7, $8, $9, $10)`,
		r.ID, r.Device, r.Action, r.Status, suT0, r.ExecS, r.Deadline, r.Dispatched, r.Keys, r.Origin)
	return err
}

func tpg(t time.Time) *time.Time { return &t }

func (e *pgEnv) countWhere(where string, args ...interface{}) int {
	e.T.Helper()
	return e.scalarInt(`SELECT count(*) FROM commands WHERE `+where, args...)
}

func (e *pgEnv) suInsertEvent(device, typ, attr, commandID string, at time.Time, msg string) {
	e.T.Helper()
	e.Exec(`INSERT INTO events (device_id, type, severity, message, container_id, container_name, created_at, command_id, command_attr)
		VALUES ($1, $2, 'info', $3, '', '', $4, $5, $6)`, device, typ, msg, at, commandID, attr)
}

func TestEX2_PG_Migration000034UpDownUp(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	has := func(name string) bool {
		return env.scalarInt(`SELECT count(*) FROM pg_constraint WHERE conname=$1 AND conrelid='commands'::regclass`, name) == 1
	}
	hasIdx := func(name string) bool {
		return env.scalarInt(`SELECT count(*) FROM pg_indexes WHERE schemaname=$1 AND indexname=$2`, env.Schema, name) == 1
	}
	require.True(t, has("commands_self_update_shape") && has("commands_origin_check"), "up adds the shape CHECK and keeps the origin CHECK")
	require.True(t, hasIdx("idx_commands_self_update_open") && hasIdx("idx_commands_self_update_recent"), "up adds both partial indexes")

	env.seedDevice("dev-1")
	res, err := repo.CreateSelfUpdate(withDeadline(t, 10*time.Second), suStart("dev-1", suT0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res.Disposition)
	env.Exec(`INSERT INTO command_escalations (command_id, target, device_id, created_at) VALUES ($1, 'outcome', 'dev-1', $2)`, res.LinkID, suT0)
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s)
		VALUES ('ordinary', 'dev-1', 'reboot', '{}'::jsonb, 'queued', $1, $1, $1, 600)`, suT0)

	require.NoError(t, env.Mig.Migrate(33), "down")
	assert.False(t, has("commands_self_update_shape"), "down drops the shape CHECK")
	assert.False(t, hasIdx("idx_commands_self_update_open") || hasIdx("idx_commands_self_update_recent"), "down drops both indexes")
	assert.Equal(t, 0, env.countWhere(`origin='agent_self_update'`), "[R-5] down deletes the self-update rows")
	assert.Equal(t, 1, env.countWhere(`id='ordinary'`), "down keeps every other command")
	assert.Equal(t, 0, env.scalarInt(`SELECT count(*) FROM command_escalations`), "markers cascade with their rows")
	require.True(t, has("commands_origin_check"), "the narrower origin CHECK is back")
	_, err = env.Pool.Exec(context.Background(), `UPDATE commands SET origin='agent_self_update' WHERE id='ordinary'`)
	require.Error(t, err, "after down, the self-update origin is rejected by the origin CHECK")

	require.NoError(t, env.Mig.Up(), "up again")
	assert.True(t, has("commands_self_update_shape") && hasIdx("idx_commands_self_update_open") && hasIdx("idx_commands_self_update_recent"),
		"second up recreates every object")
	res, err = repo.CreateSelfUpdate(withDeadline(t, 10*time.Second), suStart("dev-1", suT0.Add(time.Hour)))
	require.NoError(t, err, "a record can be created after down and up")
	assert.Equal(t, models.SelfUpdateCreated, res.Disposition)
}

func TestEX2_PG_OriginCheck(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	d, dl := suT0, suT0.Add(10*time.Minute)
	require.NoError(t, env.tryInsertSU(suRow{ID: "ok", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}),
		"a well-formed self-update row is accepted")
	err := env.tryInsertSU(suRow{ID: "bad", Device: "dev-1", Status: "succeeded", Dispatched: &d, Deadline: &dl, Origin: "bogus"})
	require.Error(t, err, "an unknown origin is rejected")
	assert.Contains(t, err.Error(), "commands_origin_check", "by the origin CHECK")
	for _, o := range []string{"user", "auto_update", "auto_patch"} {
		env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, origin)
			VALUES ($1, 'dev-1', 'reboot', '{}'::jsonb, 'queued', $2, $2, $2, 600, $1)`, o, suT0)
	}
	assert.Equal(t, 4, env.countWhere(`device_id='dev-1'`), "the three old origins still insert")
}

func TestEX2_AC024_PG_CheckForbidsQueued(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	d, dl := suT0, suT0.Add(10*time.Minute)
	for _, status := range []string{"queued", "pending", "dispatched", "sent", "acknowledged", "cancelled", "expired", "success", "error"} {
		err := env.tryInsertSU(suRow{ID: "su-" + status, Device: "dev-1", Status: status, Dispatched: &d, Deadline: &dl})
		require.Error(t, err, "[AC-024] status %q must be rejected for a self-update row", status)
		assert.Contains(t, err.Error(), "commands_self_update_shape", "status %q is rejected by the shape CHECK", status)
	}
	for i, status := range []string{"running", "verifying", "succeeded", "failed", "timed_out"} {
		dev := fmt.Sprintf("dev-ok-%d", i)
		env.seedDevice(dev)
		require.NoError(t, env.tryInsertSU(suRow{ID: "ok-" + status, Device: dev, Status: status, Dispatched: &d, Deadline: &dl}),
			"[AC-024] status %q is a legal self-update state", status)
	}

	// An existing row cannot be moved back to queued either (the revert path).
	_, err := env.Pool.Exec(context.Background(), `UPDATE commands SET status='queued' WHERE id='ok-running'`)
	require.Error(t, err, "[AC-024] a self-update row cannot return to queued")
	_, err = env.Pool.Exec(context.Background(), `UPDATE commands SET dispatched_at=NULL WHERE id='ok-running'`)
	require.Error(t, err, "[AC-024] a self-update row cannot lose its dispatch time")
}

func TestEX2_AC024_PG_CheckBoundsDeadlineAndKeys(t *testing.T) {
	env := newPGEnv(t, 0)
	d := suT0
	cases := []struct {
		name    string
		row     suRow
		wantErr bool
	}{
		{"deadline exactly D+10m", suRow{Dispatched: &d, Deadline: tpg(d.Add(10 * time.Minute))}, false},
		{"deadline D+10m+1s", suRow{Dispatched: &d, Deadline: tpg(d.Add(10*time.Minute + time.Second))}, true},
		{"deadline D+15m (a command's deadline)", suRow{Dispatched: &d, Deadline: tpg(d.Add(15 * time.Minute)), ExecS: 900}, true},
		{"exec_deadline_s 600", suRow{Dispatched: &d, Deadline: tpg(d.Add(10 * time.Minute)), ExecS: 600}, false},
		{"exec_deadline_s 601", suRow{Dispatched: &d, Deadline: tpg(d.Add(10 * time.Minute)), ExecS: 601}, true},
		{"a target key", suRow{Dispatched: &d, Deadline: tpg(d.Add(10 * time.Minute)), Keys: []string{"host"}}, true},
		{"no dispatched_at", suRow{Deadline: tpg(d.Add(10 * time.Minute))}, true},
		{"no deadline_at", suRow{Dispatched: &d}, true},
		{"another action", suRow{Dispatched: &d, Deadline: tpg(d.Add(10 * time.Minute)), Action: "reboot"}, true},
	}
	for i, tc := range cases {
		dev := fmt.Sprintf("dev-%d", i)
		env.seedDevice(dev)
		tc.row.ID, tc.row.Device, tc.row.Status = fmt.Sprintf("row-%d", i), dev, "running"
		err := env.tryInsertSU(tc.row)
		if tc.wantErr {
			require.Error(t, err, "[C-2] %s must be rejected", tc.name)
			assert.Contains(t, err.Error(), "commands_self_update_shape", "[C-2] %s: rejected by the shape CHECK", tc.name)
		} else {
			require.NoError(t, err, "[C-2] %s must be accepted", tc.name)
		}
	}

	// The bound cannot be moved by an UPDATE either.
	_, err := env.Pool.Exec(context.Background(), `UPDATE commands SET deadline_at = deadline_at + interval '1 minute' WHERE id='row-0'`)
	require.Error(t, err, "[C-2] deadline_at cannot be extended past D+10m")
	_, err = env.Pool.Exec(context.Background(), `UPDATE commands SET target_keys = ARRAY['host'] WHERE id='row-0'`)
	require.Error(t, err, "[C-2] a self-update row cannot take a target key")
	// A command with the same shape but another origin is unaffected.
	env.seedDevice("dev-user")
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, dispatched_at, target_keys)
		VALUES ('cmd', 'dev-user', 'agent_update', '{}'::jsonb, 'running', $1, $1, $1, 900, $2, $1, ARRAY['host'])`, suT0, suT0.Add(15*time.Minute))
}

func TestEX2_PG_UniqueOpenRecord(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	d, dl := suT0, suT0.Add(10*time.Minute)
	require.NoError(t, env.tryInsertSU(suRow{ID: "a", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}))
	for _, status := range []string{"running", "verifying"} {
		err := env.tryInsertSU(suRow{ID: "dup-" + status, Device: "dev-1", Status: status, Dispatched: &d, Deadline: &dl})
		require.Error(t, err, "a second %s record of the same device is rejected", status)
		assert.Contains(t, err.Error(), "idx_commands_self_update_open", "by the unique index")
	}
	require.NoError(t, env.tryInsertSU(suRow{ID: "done", Device: "dev-1", Status: "succeeded", Dispatched: &d, Deadline: &dl}), "terminal rows are not limited")
	require.NoError(t, env.tryInsertSU(suRow{ID: "other", Device: "dev-2", Status: "running", Dispatched: &d, Deadline: &dl}), "another device may have its own open record")
	env.Exec(`UPDATE commands SET status='succeeded', finished_at=$1 WHERE id='a'`, suT0.Add(time.Minute))
	require.NoError(t, env.tryInsertSU(suRow{ID: "b", Device: "dev-1", Status: "running", Dispatched: &d, Deadline: &dl}), "after the open record ended, a new one may open")
}

func TestEX2_AC024_PG_AgentOperatorServerGuards(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res.Disposition)
	id := res.LinkID
	now := suT0.Add(time.Minute)

	rowState := func() (status, step, failure string) {
		require.NoError(t, env.Pool.QueryRow(context.Background(), `SELECT status, step, failure_step FROM commands WHERE id=$1`, id).Scan(&status, &step, &failure))
		return
	}
	for _, tc := range []struct {
		name string
		t    models.CommandTransition
	}{
		{"agent result succeeded", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded}},
		{"agent result failed", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandFailed}},
		{"agent progress running", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandRunning}},
		{"agent progress verifying", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandVerifying}},
		{"agent ack", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandAcknowledged}},
		{"operator cancel", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorOperator, To: models.CommandCancelled}},
		{"server timed_out (the sweeper edge)", models.CommandTransition{ID: id, Actor: models.ActorServer, To: models.CommandTimedOut}},
		{"server timed_out scoped to the device", models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorServer, To: models.CommandTimedOut}},
		{"server expire", models.CommandTransition{ID: id, Actor: models.ActorServer, To: models.CommandExpired}},
		{"server revert", models.CommandTransition{ID: id, Actor: models.ActorServer, To: models.CommandQueued}},
		{"self-update actor without a device", models.CommandTransition{ID: id, Actor: models.ActorSelfUpdate, To: models.CommandVerifying}},
		{"self-update actor for another device", models.CommandTransition{ID: id, DeviceID: "dev-2", Actor: models.ActorSelfUpdate, To: models.CommandVerifying}},
	} {
		tc.t.Now = now
		got, applied, err := repo.Transition(ctx, tc.t)
		require.NoError(t, err, tc.name)
		assert.False(t, applied, "[C-2] %s must not apply to a self-update row", tc.name)
		assert.Nil(t, got, tc.name)
	}
	status, step, failure := rowState()
	assert.Equal(t, "running", status, "[C-2] the row is unchanged after every refused edge")
	assert.Equal(t, models.SelfUpdateStepWaitingForRestart, step)
	assert.Empty(t, failure)

	// The self-update actor applies, and only to its own rows.
	stepV := models.SelfUpdateStepWaitingForConfirmation
	got, applied, err := repo.Transition(ctx, models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorSelfUpdate,
		To: models.CommandVerifying, Step: &stepV, Now: now})
	require.NoError(t, err)
	require.True(t, applied, "[AC-024] ActorSelfUpdate running -> verifying applies")
	require.NotNil(t, got.VerifyingAt, "verifying_at is stamped")
	assert.True(t, got.VerifyingAt.Equal(now))
	assert.Equal(t, stepV, got.Step)
	assert.Nil(t, got.ExitCode, "[C-6] no result columns are written")
	assert.Equal(t, 0, env.scalarInt(`SELECT result_bytes FROM commands WHERE id=$1`, id), "[C-6] result_bytes stays 0")

	msg, fs := "confirmed: the agent registered on 2.80.0", ""
	_, applied, err = repo.Transition(ctx, models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorSelfUpdate,
		To: models.CommandSucceeded, ResultMsg: &msg, FailureStep: &fs, Now: now.Add(time.Minute)})
	require.NoError(t, err)
	assert.True(t, applied, "verifying -> succeeded applies")
	_, applied, err = repo.Transition(ctx, models.CommandTransition{ID: id, DeviceID: "dev-1", Actor: models.ActorSelfUpdate,
		To: models.CommandFailed, Now: now.Add(2 * time.Minute)})
	require.NoError(t, err)
	assert.False(t, applied, "a terminal record is immutable for the self-update actor too")

	// The self-update actor never matches an ordinary command (the origin guard).
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, dispatched_at)
		VALUES ('ordinary', 'dev-1', 'agent_update', '{}'::jsonb, 'running', $1, $1, $1, 900, $2, $1)`, suT0, suT0.Add(15*time.Minute))
	for _, to := range []models.CommandState{models.CommandVerifying, models.CommandSucceeded, models.CommandFailed, models.CommandTimedOut} {
		_, applied, err = repo.Transition(ctx, models.CommandTransition{ID: "ordinary", DeviceID: "dev-1", Actor: models.ActorSelfUpdate, To: to, Now: now})
		require.NoError(t, err)
		assert.False(t, applied, "[C-2] ActorSelfUpdate -> %s must not apply to an ordinary command", to)
	}
	assert.Equal(t, "running", env.statusOf("ordinary"))
	_, applied, err = repo.Transition(ctx, models.CommandTransition{ID: "ordinary", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded, Now: now})
	require.NoError(t, err)
	assert.True(t, applied, "[AC-028] the agent and server edges are unchanged for ordinary commands")
}

func TestEX2_PG_ListOverdueExcludesSelfUpdate(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res.Disposition)
	past := suT0.Add(-time.Hour)
	env.insertRaw(rawCommand{ID: "ordinary", DeviceID: "dev-2", Action: "agent_update", Status: "running", DeadlineAt: &past})

	got, err := repo.ListOverdue(ctx, "", suT0.Add(2*time.Hour), 100)
	require.NoError(t, err)
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"ordinary"}, ids, "[AD-007 row 7] ListOverdue never returns a self-update row, even long past its bound")
	got, err = repo.ListOverdue(ctx, "dev-1", suT0.Add(2*time.Hour), 100)
	require.NoError(t, err)
	assert.Empty(t, got, "also when scoped to the device")
}

func TestEX2_AC007_PG_AttributionPredicate(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	ctx := withDeadline(t, 30*time.Second)
	S := suT0
	insert := func(dev, id, action, status string, dispatched, finished *time.Time, origin string) {
		env.seedDevice(dev)
		env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s,
				deadline_at, dispatched_at, finished_at, origin)
			VALUES ($1, $2, $3, '{}'::jsonb, $4, $5, $5, $5, 900, $6, $7, $8, $9)`,
			id, dev, action, status, S.Add(-time.Hour), dispatched, dispatched, finished, origin)
	}
	disp := S.Add(-5 * time.Minute)
	cases := []struct {
		name  string
		setup func(dev string)
		want  models.SelfUpdateDisposition
	}{
		{"dispatched command in flight", func(d string) { insert(d, "c-"+d, "agent_update", "dispatched", &disp, nil, "user") }, models.SelfUpdateAttributedCommand},
		{"legacy sent status in flight", func(d string) { insert(d, "c-"+d, "agent_update", "sent", &disp, nil, "user") }, models.SelfUpdateAttributedCommand},
		{"acknowledged", func(d string) { insert(d, "c-"+d, "agent_update", "acknowledged", &disp, nil, "user") }, models.SelfUpdateAttributedCommand},
		{"running", func(d string) { insert(d, "c-"+d, "agent_update", "running", &disp, nil, "user") }, models.SelfUpdateAttributedCommand},
		{"verifying", func(d string) { insert(d, "c-"+d, "agent_update", "verifying", &disp, nil, "user") }, models.SelfUpdateAttributedCommand},
		{"succeeded 9 minutes ago", func(d string) {
			f := S.Add(-9 * time.Minute)
			insert(d, "c-"+d, "agent_update", "succeeded", &disp, &f, "user")
		}, models.SelfUpdateAttributedCommand},
		{"failed exactly 10 minutes ago", func(d string) {
			f := S.Add(-10 * time.Minute)
			insert(d, "c-"+d, "agent_update", "failed", &disp, &f, "user")
		}, models.SelfUpdateAttributedCommand},
		{"auto_update origin counts too", func(d string) { insert(d, "c-"+d, "agent_update", "running", &disp, nil, "auto_update") }, models.SelfUpdateAttributedCommand},
		{"succeeded 11 minutes ago", func(d string) {
			f := S.Add(-11 * time.Minute)
			insert(d, "c-"+d, "agent_update", "succeeded", &disp, &f, "user")
		}, models.SelfUpdateCreated},
		{"queued, never dispatched", func(d string) { insert(d, "c-"+d, "agent_update", "queued", nil, nil, "user") }, models.SelfUpdateCreated},
		{"expired while queued", func(d string) {
			f := S.Add(-time.Minute)
			insert(d, "c-"+d, "agent_update", "expired", nil, &f, "user")
		}, models.SelfUpdateCreated},
		{"cancelled while queued", func(d string) {
			f := S.Add(-time.Minute)
			insert(d, "c-"+d, "agent_update", "cancelled", nil, &f, "user")
		}, models.SelfUpdateCreated},
		{"another action in flight", func(d string) { insert(d, "c-"+d, "reboot", "running", &disp, nil, "user") }, models.SelfUpdateCreated},
		{"no command at all", func(d string) { env.seedDevice(d) }, models.SelfUpdateCreated},
	}
	for i, tc := range cases {
		dev := fmt.Sprintf("dev-%02d", i)
		tc.setup(dev)
		res, err := repo.CreateSelfUpdate(ctx, suStart(dev, S))
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, res.Disposition, "[AC-007] %s", tc.name)
		if tc.want == models.SelfUpdateAttributedCommand {
			assert.Equal(t, "c-"+dev, res.LinkID, "%s: the start links to the command", tc.name)
			assert.Nil(t, res.Command, "%s: no record is created", tc.name)
			assert.Equal(t, 0, env.countWhere(`device_id=$1 AND origin='agent_self_update'`, dev), "%s: no row", tc.name)
		} else {
			require.NotNil(t, res.Command, "%s: a record is created", tc.name)
			assert.Equal(t, res.Command.ID, res.LinkID)
		}
	}

	// A command of another device never captures this device's start.
	env.seedDevice("dev-mine")
	insert("dev-other", "c-other", "agent_update", "running", &disp, nil, "user")
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-mine", S))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateCreated, res.Disposition, "[NFR-010] a command of another device does not attribute")

	// The newest qualifying command wins.
	insert("dev-two", "c-old", "agent_update", "running", &disp, nil, "user")
	newer := S.Add(-2 * time.Minute)
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, dispatched_at)
		VALUES ('c-new', 'dev-two', 'agent_update', '{}'::jsonb, 'dispatched', $1, $1, $1, 900, $2, $1)`, newer, newer.Add(15*time.Minute))
	res, err = repo.CreateSelfUpdate(ctx, suStart("dev-two", S))
	require.NoError(t, err)
	assert.Equal(t, "c-new", res.LinkID, "the newest qualifying command is the link")
}

func TestEX2_PG_CreateSelfUpdateRowShape(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	env.seedDevice("dev-1")
	ctx := withDeadline(t, 20*time.Second)
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res.Disposition)
	c, err := repo.GetByID(ctx, res.LinkID)
	require.NoError(t, err)
	assert.True(t, models.IsCanonicalUUID(c.ID), "the id is a canonical UUID")
	assert.Equal(t, "agent_update", c.Action)
	assert.Equal(t, models.OriginAgentSelfUpdate, c.Origin)
	assert.Equal(t, "running", c.Status)
	assert.Equal(t, models.SelfUpdateStepWaitingForRestart, c.Step)
	assert.Equal(t, 600, c.ExecDeadlineS)
	assert.Equal(t, "system", c.ActorKind)
	assert.Empty(t, c.ActorName+c.ActorAuth+c.ActorSub, "no actor identity")
	assert.Nil(t, c.PolicyID)
	assert.Empty(t, c.TargetKeys, "no target keys")
	assert.Equal(t, "2.78.0", c.Params[models.SelfUpdateParamFrom])
	assert.Equal(t, "2.80.0", c.Params[models.SelfUpdateParamTo])
	for name, ts := range map[string]*time.Time{"dispatched_at": c.DispatchedAt, "started_at": c.StartedAt, "deadline_at": c.DeadlineAt} {
		require.NotNil(t, ts, name)
	}
	assert.True(t, c.CreatedAt.Equal(suT0) && c.UpdatedAt.Equal(suT0) && c.ExpiresAt.Equal(suT0), "created, updated and expires at the receive time")
	assert.True(t, c.DispatchedAt.Equal(suT0) && c.StartedAt.Equal(suT0), "dispatched and started at the receive time")
	assert.True(t, c.DeadlineAt.Equal(suT0.Add(10*time.Minute)), "deadline is start + 10 minutes")
	assert.Nil(t, c.FinishedAt)
	assert.Nil(t, c.WindowClosedAt, "the EXPECT-001 window is pending")
	assert.Equal(t, 0, env.scalarInt(`SELECT count(*) FROM docker_audit`), "[AD-001] no docker_audit row is written")
	assert.Equal(t, 0, env.scalarInt(`SELECT result_bytes FROM commands WHERE id=$1`, c.ID), "[C-6] no result bytes")

	// The row is what the EXPECT-001 loaders read.
	open, err := (&ExpectRepo{db: &DB{Pool: env.Pool}}).ListOpenWindows(ctx, suT0, 100)
	require.NoError(t, err)
	require.Len(t, open, 1, "[AD-001] ListOpenWindows returns the record")
	assert.Equal(t, c.ID, open[0].ID)

	// And OpenSelfUpdate / LatestSelfUpdateSince read it.
	o, err := repo.OpenSelfUpdate(ctx, "dev-1")
	require.NoError(t, err)
	require.NotNil(t, o)
	assert.Equal(t, c.ID, o.ID)
	none, err := repo.OpenSelfUpdate(ctx, "dev-2")
	require.NoError(t, err)
	assert.Nil(t, none, "another device has no open record")
	l, err := repo.LatestSelfUpdateSince(ctx, "dev-1", suT0.Add(-10*time.Minute))
	require.NoError(t, err)
	require.NotNil(t, l)
	assert.Equal(t, c.ID, l.ID)
	l, err = repo.LatestSelfUpdateSince(ctx, "dev-1", suT0.Add(time.Second))
	require.NoError(t, err)
	assert.Nil(t, l, "created before since")
}

func TestEX2_AC017_PG_ConcurrentStartsOneRecordOneCapEvent(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	env.seedDevice("dev-1")
	ctx := withDeadline(t, 60*time.Second)

	run := func(n int, at time.Time) map[models.SelfUpdateDisposition]int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		counts := map[models.SelfUpdateDisposition]int{}
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", at))
				assert.NoError(t, err, "a concurrent start must not fail")
				mu.Lock()
				counts[res.Disposition]++
				mu.Unlock()
			}()
		}
		wg.Wait()
		return counts
	}

	counts := run(50, suT0)
	assert.Equal(t, 1, counts[models.SelfUpdateCreated], "[AC-017] 50 concurrent starts create exactly one record")
	assert.Equal(t, 49, counts[models.SelfUpdateOpenRecord], "[AC-017] the other 49 are absorbed by the open record")
	assert.Equal(t, 1, env.countWhere(`origin='agent_self_update'`), "one row")

	// Fill the 24 h cap: close the open record, start the next one hour later.
	closeOpen := func(at time.Time) {
		env.Exec(`UPDATE commands SET status='succeeded', finished_at=$1, updated_at=$1 WHERE origin='agent_self_update' AND status IN ('running','verifying')`, at)
	}
	for i := 1; i < models.SelfUpdateRateCap; i++ {
		closeOpen(suT0.Add(time.Duration(i) * time.Hour))
		res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0.Add(time.Duration(i)*time.Hour)))
		require.NoError(t, err)
		require.Equal(t, models.SelfUpdateCreated, res.Disposition, "record %d of the day is created", i+1)
	}
	closeOpen(suT0.Add(6 * time.Hour))
	assert.Equal(t, 6, env.countWhere(`origin='agent_self_update'`), "six records")

	capAt := suT0.Add(7 * time.Hour)
	counts = run(20, capAt)
	assert.Equal(t, 20, counts[models.SelfUpdateCapped], "[AC-017] every start beyond the cap is capped")
	assert.Equal(t, 6, env.countWhere(`origin='agent_self_update'`), "no seventh record")
	assert.Equal(t, 1, env.scalarInt(`SELECT count(*) FROM events WHERE device_id='dev-1' AND type='agent_self_update_paused'`),
		"[AC-017] exactly one rate-cap warning event, whatever the concurrency")
	var sev, msg, cmdID string
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT severity, message, command_id FROM events WHERE type='agent_self_update_paused'`).Scan(&sev, &msg, &cmdID))
	assert.Equal(t, "warning", sev)
	assert.Equal(t, models.SelfUpdateRateCapMessage("hawaii"), msg, "[FR-007] the exact text")
	assert.Empty(t, cmdID, "the rate-cap event is not linked to a record")

	// A later capped start in the same 24 h adds no second event.
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", capAt.Add(time.Hour)))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateCapped, res.Disposition)
	assert.Nil(t, res.CapEvent, "no second cap event inside 24 hours")
	assert.Equal(t, 1, env.scalarInt(`SELECT count(*) FROM events WHERE type='agent_self_update_paused'`))

	// Once the oldest records age out of the 24 h window, a new record is created.
	res, err = repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0.Add(24*time.Hour+30*time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateCreated, res.Disposition, "the cap is a rolling window")
}

func TestEX2_PG_StaleRetireInTx(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	env.seedDevice("dev-1")
	ctx := withDeadline(t, 30*time.Second)
	first, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, first.Disposition)
	bound := suT0.Add(10 * time.Minute)

	// Exactly one hour after the bound is not yet stale (the test is strict).
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", bound.Add(time.Hour)))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateOpenRecord, res.Disposition, "[C-4] exactly bound + 1h still absorbs the start")
	assert.Nil(t, res.Retired)
	assert.Equal(t, first.LinkID, res.LinkID)

	// One second past it, the open record is retired and a new one is created.
	at := bound.Add(time.Hour + time.Second)
	res, err = repo.CreateSelfUpdate(ctx, suStart("dev-1", at))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res.Disposition, "[C-4] a stale open record never blocks a new one")
	require.NotNil(t, res.Retired, "[C-4] the retired row is returned for broadcast")
	assert.Equal(t, first.LinkID, res.Retired.ID)
	assert.Equal(t, "timed_out", res.Retired.Status)
	assert.Equal(t, models.SelfUpdateFailureUnresolved, res.Retired.FailureStep)
	assert.Equal(t, models.SelfUpdateStaleResultMessage, res.Retired.ResultMsg)
	require.NotNil(t, res.Retired.FinishedAt)
	assert.True(t, res.Retired.FinishedAt.Equal(at), "finished at the start time")
	assert.NotEqual(t, first.LinkID, res.LinkID)
	assert.Equal(t, 1, env.countWhere(`origin='agent_self_update' AND status IN ('running','verifying')`), "exactly one open record")
	assert.Equal(t, 2, env.countWhere(`origin='agent_self_update'`))

	// A retire and a cap in the same call: the retired row is still returned.
	env.seedDevice("dev-2")
	for i := 0; i < models.SelfUpdateRateCap-1; i++ {
		env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, dispatched_at, finished_at, origin)
			VALUES ($1, 'dev-2', 'agent_update', '{}'::jsonb, 'succeeded', $2, $2, $2, 600, $3, $2, $3, 'agent_self_update')`,
			fmt.Sprintf("old-%d", i), suT0.Add(time.Duration(i)*time.Minute), suT0.Add(time.Duration(i)*time.Minute+time.Minute))
	}
	open2, err := repo.CreateSelfUpdate(ctx, suStart("dev-2", suT0.Add(6*time.Minute)))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, open2.Disposition, "the sixth record is created")
	at2 := suT0.Add(6*time.Minute + 10*time.Minute + time.Hour + time.Minute)
	res, err = repo.CreateSelfUpdate(ctx, suStart("dev-2", at2))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateCapped, res.Disposition, "the seventh record in 24h is capped")
	require.NotNil(t, res.Retired, "[C-4] the stale row is retired in the same transaction even when the start is capped")
	assert.Equal(t, open2.LinkID, res.Retired.ID)
}

func TestEX2_PG_AttributionDoesNotRetire(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	env.seedDevice("dev-1")
	ctx := withDeadline(t, 30*time.Second)
	_, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0))
	require.NoError(t, err)
	late := suT0.Add(3 * time.Hour)
	disp := late.Add(-time.Minute)
	env.Exec(`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s, deadline_at, dispatched_at)
		VALUES ('cmd', 'dev-1', 'agent_update', '{}'::jsonb, 'dispatched', $1, $1, $1, 900, $2, $1)`, disp, disp.Add(15*time.Minute))
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", late))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateAttributedCommand, res.Disposition, "[AD-002] command attribution is checked first")
	assert.Nil(t, res.Retired, "the stale row is left for the next record-creating start")
}

func TestEX2_PG_ListByCommandSplit(t *testing.T) {
	env := newPGEnv(t, 0)
	repo := env.suRepo()
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	ctx := withDeadline(t, 30*time.Second)
	res, err := repo.CreateSelfUpdate(ctx, suStart("dev-1", suT0))
	require.NoError(t, err)
	id := res.LinkID
	er := &EventRepo{db: &DB{Pool: env.Pool}}

	// 200 flooded agent events, then the server's own events, later in time.
	for i := 0; i < 200; i++ {
		env.suInsertEvent("dev-1", "agent_update_started", models.CommandAttrInferred, id, suT0.Add(time.Duration(i)*time.Millisecond), fmt.Sprintf("flood %03d", i))
	}
	env.suInsertEvent("dev-1", "device_offline", models.CommandAttrExpected, id, suT0.Add(time.Minute), "offline")
	env.suInsertEvent("dev-1", "device_online", models.CommandAttrExpected, id, suT0.Add(2*time.Minute), "online")
	env.suInsertEvent("dev-1", "agent_self_update_failed", models.CommandAttrOutcome, id, suT0.Add(3*time.Minute), "outcome")
	env.suInsertEvent("dev-1", "device_offline", models.CommandAttrEscalation, id, suT0.Add(4*time.Minute), "escalation")
	env.suInsertEvent("dev-1", "agent_self_update_paused", "", id, suT0.Add(5*time.Minute), "stuck without an attr")
	env.suInsertEvent("dev-1", "agent_update_completed", models.CommandAttrInferred, id, suT0.Add(6*time.Minute), "completed")
	// Rows that must not appear: another command, another device, an unclassified link.
	env.suInsertEvent("dev-1", "device_offline", models.CommandAttrExpected, "other-command", suT0, "elsewhere")
	env.suInsertEvent("dev-2", "device_offline", models.CommandAttrExpected, id, suT0, "foreign device")
	env.suInsertEvent("dev-1", "container_died", "", id, suT0, "unclassified")

	got, err := er.ListByCommandSplit(ctx, "dev-1", id, models.CommandEventsServerLimit, models.CommandEventsAgentLimit)
	require.NoError(t, err)
	var msgs []string
	for _, e := range got.ServerEvents {
		msgs = append(msgs, e.Message)
	}
	assert.Equal(t, []string{"offline", "online", "outcome", "escalation", "stuck without an attr"}, msgs,
		"[C-3] every server-authored event is returned, oldest first, whatever the agent volume")
	assert.Zero(t, got.ServerEventsOmitted)
	require.Len(t, got.AgentEvents, 50, "[C-3] at most 50 agent-sourced events")
	assert.Equal(t, "flood 000", got.AgentEvents[0].Message, "oldest first")
	assert.Equal(t, 201-50, got.AgentEventsOmitted, "[C-3] the omitted count is reported (200 flood + 1 completed - 50)")
	for _, e := range append(got.ServerEvents, got.AgentEvents...) {
		assert.Equal(t, "dev-1", e.DeviceID)
		assert.Equal(t, id, e.CommandID)
	}

	small, err := er.ListByCommandSplit(ctx, "dev-1", id, 2, 3)
	require.NoError(t, err)
	assert.Len(t, small.ServerEvents, 2)
	assert.Equal(t, 3, small.ServerEventsOmitted)
	assert.Len(t, small.AgentEvents, 3)

	foreign, err := er.ListByCommandSplit(ctx, "dev-2", "no-such-id", 500, 50)
	require.NoError(t, err)
	assert.NotNil(t, foreign.ServerEvents, "empty groups are empty arrays, not null")
	assert.Empty(t, foreign.ServerEvents)
	assert.Empty(t, foreign.AgentEvents)
	assert.Zero(t, foreign.AgentEventsOmitted)
}

// The SQL builder needs no database: the four-actor origin guard is part of the
// statement text (C-2), so this test runs everywhere.
func TestEX2_AC024_TransitionSQLOriginGuards(t *testing.T) {
	now := suT0
	for _, tc := range []struct {
		name string
		tr   models.CommandTransition
	}{
		{"server timed_out", models.CommandTransition{ID: "x", Actor: models.ActorServer, To: models.CommandTimedOut}},
		{"server expire", models.CommandTransition{ID: "x", Actor: models.ActorServer, To: models.CommandExpired}},
		{"agent result", models.CommandTransition{ID: "x", DeviceID: "d", Actor: models.ActorAgent, To: models.CommandSucceeded}},
		{"agent progress", models.CommandTransition{ID: "x", DeviceID: "d", Actor: models.ActorAgent, To: models.CommandRunning}},
		{"operator cancel", models.CommandTransition{ID: "x", DeviceID: "d", Actor: models.ActorOperator, To: models.CommandCancelled}},
	} {
		tc.tr.Now = now
		q, _, ok := buildTransitionSQL(tc.tr)
		require.True(t, ok, tc.name)
		assert.Contains(t, q, "AND origin <> 'agent_self_update'", "[C-2] %s: every other actor excludes self-update rows", tc.name)
		assert.NotContains(t, q, "AND origin = 'agent_self_update'", tc.name)
	}
	for _, to := range []models.CommandState{models.CommandVerifying, models.CommandSucceeded, models.CommandFailed, models.CommandTimedOut} {
		q, args, ok := buildTransitionSQL(models.CommandTransition{ID: "x", DeviceID: "d", Actor: models.ActorSelfUpdate, To: to, Now: now})
		require.True(t, ok, "self-update -> %s", to)
		assert.Contains(t, q, "AND origin = 'agent_self_update'", "[C-2] the self-update actor matches only self-update rows")
		assert.NotContains(t, q, "origin <>", "self-update -> %s", to)
		assert.Contains(t, q, "device_id=$", "the device scope is in the statement")
		assert.Contains(t, args, "d")
	}
	_, _, ok := buildTransitionSQL(models.CommandTransition{ID: "x", Actor: models.ActorSelfUpdate, To: models.CommandVerifying, Now: now})
	assert.False(t, ok, "[AD-003] DeviceID is mandatory for the self-update actor")
	_, _, ok = buildTransitionSQL(models.CommandTransition{ID: "x", DeviceID: "d", Actor: models.ActorSelfUpdate, To: models.CommandQueued, Now: now})
	assert.False(t, ok, "the self-update actor has no edge to queued")
	_, _, ok = buildTransitionSQL(models.CommandTransition{ID: "x", DeviceID: "d", Actor: models.ActorSelfUpdate, To: models.CommandCancelled, Now: now})
	assert.False(t, ok, "the self-update actor has no edge to cancelled")
}
