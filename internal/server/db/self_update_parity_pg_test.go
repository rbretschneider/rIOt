package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-002 AD-007 / implementation note 4: the in-memory MockCommandRepo must
// emulate the four-actor origin guard and the CHECK of the SQL. This test runs
// the same transition against PostgreSQL and against the mock over a table of
// actor x origin x status x target state and requires identical outcomes. It
// is an external test package because testutil imports db.

func parityPGURL(t *testing.T) string {
	t.Helper()
	raw := os.Getenv("RIOT_TEST_PG_URL")
	if raw == "" {
		t.Skip("RIOT_TEST_PG_URL not set; skipping PostgreSQL-gated test")
	}
	u, err := url.Parse(raw)
	if err != nil || (os.Getenv("RIOT_TEST_PG_ALLOW") != "1" && !strings.Contains(strings.ToLower(strings.TrimPrefix(u.Path, "/")), "test")) {
		t.Skip("RIOT_TEST_PG_URL does not name a test database (name must contain \"test\"); set RIOT_TEST_PG_ALLOW=1 to override")
	}
	return raw
}

func newParityPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := parityPGURL(t)
	ctx := context.Background()
	buf := make([]byte, 6)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	schema := "cl_" + hex.EncodeToString(buf)

	admin, err := pgxpool.New(ctx, raw)
	require.NoError(t, err)
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	cfg, err := pgxpool.ParseConfig(raw)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	source, err := iofs.New(os.DirFS("../../../cmd/riot-server/migrations"), ".")
	require.NoError(t, err)
	mig, err := migrate.NewWithSourceInstance("iofs", source, u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Close()
		_, _ = mig.Close()
		_, dropErr := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		assert.NoError(t, dropErr)
		admin.Close()
	})
	require.NoError(t, mig.Up())
	return pool
}

func TestEX2_AC024_MockGuardParity(t *testing.T) {
	pool := newParityPool(t)
	ctx := context.Background()
	repo := db.NewCommandRepo(&db.DB{Pool: pool})
	mock := testutil.NewMockCommandRepo()
	t0 := time.Date(2026, 10, 26, 17, 51, 0, 0, time.UTC)
	now := t0.Add(time.Minute)
	_, err := pool.Exec(ctx, `INSERT INTO devices (id, short_id, hostname) VALUES ('dev-1', 'dev-1', 'h')`)
	require.NoError(t, err)

	actors := []models.CommandActor{models.ActorServer, models.ActorAgent, models.ActorOperator, models.ActorSelfUpdate}
	origins := []string{models.OriginUser, models.OriginAutoUpdate, models.OriginAutoPatch, models.OriginAgentSelfUpdate}
	selfStates := map[models.CommandState]bool{models.CommandRunning: true, models.CommandVerifying: true,
		models.CommandSucceeded: true, models.CommandFailed: true, models.CommandTimedOut: true}

	compared := 0
	for _, origin := range origins {
		id := "row-" + origin
		exec := 900
		deadline := t0.Add(15 * time.Minute)
		if origin == models.OriginAgentSelfUpdate {
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
				continue // the CHECK forbids the row; TestEX2_AC024_PG_CheckForbidsQueued covers that
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
					require.Equal(t, pgApplied, mockApplied, "[AC-024] parity: origin %s, actor %s, %s -> %s (pg applied=%v, mock applied=%v)",
						origin, actor, from, to, pgApplied, mockApplied)
					if pgApplied {
						assert.Equal(t, pgRow.Status, mockRow.Status, "parity: resulting status %s/%s %s->%s", origin, actor, from, to)
					}
					// The two guards agree with the design, not only with each other.
					wantApplied := models.CanTransition(from, to, actor) && (actor == models.ActorSelfUpdate) == (origin == models.OriginAgentSelfUpdate)
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
		c := &models.Command{ID: "mock-su", DeviceID: "dev-9", Action: "agent_update", Params: map[string]interface{}{}, Status: "running",
			Origin: models.OriginAgentSelfUpdate, CreatedAt: t1, UpdatedAt: t1, ExpiresAt: t1, ExecDeadlineS: 600,
			DeadlineAt: &dl, DispatchedAt: &t1, TargetKeys: []string{}}
		mut(c)
		return c
	}
	for name, mut := range map[string]func(c *models.Command){
		"queued":            func(c *models.Command) { c.Status = "queued" },
		"deadline D+10m+1s": func(c *models.Command) { d := t1.Add(10*time.Minute + time.Second); c.DeadlineAt = &d },
		"exec 601":          func(c *models.Command) { c.ExecDeadlineS = 601 },
		"target key":        func(c *models.Command) { c.TargetKeys = []string{"host"} },
		"no dispatched_at":  func(c *models.Command) { c.DispatchedAt = nil },
		"another action":    func(c *models.Command) { c.Action = "reboot" },
	} {
		_, err := mock.CreateIfNoConflict(ctx, badShape(mut))
		assert.Error(t, err, "[C-2] the mock emulates the shape CHECK: %s", name)
	}
	_, err = mock.CreateIfNoConflict(ctx, badShape(func(c *models.Command) {}))
	require.NoError(t, err, "a well-formed self-update row is accepted")
	_, err = mock.CreateIfNoConflict(ctx, badShape(func(c *models.Command) { c.ID = "mock-su-2" }))
	assert.Error(t, err, "the mock emulates the unique open-record index")
}
