package db_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// PostgreSQL-gated tests of the attempt ID of a self-update record (EXPECT-007,
// ADD section 8, "R"). They skip unless RIOT_TEST_PG_URL names a test database.
// This is an external test package because testutil imports db.

var ex7T0 = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

const ex7AttemptA = "9f1c0123456789abcdef0123456789ab"
const ex7AttemptB = "1111222233334444aaaabbbbccccdddd"

func ex7Start(device, attempt string, at time.Time) models.SelfUpdateStart {
	return models.SelfUpdateStart{DeviceID: device, Hostname: "hawaii", From: "2.86.0", To: "2.87.0", AttemptID: attempt, At: at}
}

func ex7SeedDevices(t *testing.T, pool *pgxpool.Pool, ids ...string) {
	t.Helper()
	for _, id := range ids {
		_, err := pool.Exec(context.Background(), `INSERT INTO devices (id, short_id, hostname) VALUES ($1, $1, 'h')`, id)
		require.NoError(t, err)
	}
}

func ex7Count(t *testing.T, pool *pgxpool.Pool, where string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM commands WHERE `+where, args...).Scan(&n))
	return n
}

func TestEX7_PG_AttemptStoredInParams(t *testing.T) {
	pool := newParityPool(t)
	ex7SeedDevices(t, pool, "dev-1")
	repo := db.NewCommandRepo(&db.DB{Pool: pool})
	ctx := context.Background()

	res, err := repo.CreateSelfUpdate(ctx, ex7Start("dev-1", ex7AttemptA, ex7T0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res.Disposition)
	assert.Equal(t, ex7AttemptA, models.SelfUpdateAttemptID(*res.Command), "[AC-011] the created record carries the attempt ID")

	var stored string
	require.NoError(t, pool.QueryRow(ctx, `SELECT params->>'attempt_id' FROM commands WHERE id=$1`, res.LinkID).Scan(&stored))
	assert.Equal(t, ex7AttemptA, stored, "[AC-011] the attempt ID is stored in params")
	assert.LessOrEqual(t, len(stored), 64, "[NFR-002] at most 64 bytes")

	// A start without an attempt ID stores no key.
	_, err = pool.Exec(ctx, `UPDATE commands SET status='succeeded', finished_at=$2 WHERE id=$1`, res.LinkID, ex7T0.Add(time.Minute))
	require.NoError(t, err)
	res2, err := repo.CreateSelfUpdate(ctx, ex7Start("dev-1", "", ex7T0.Add(time.Hour)))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res2.Disposition)
	var has bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT params ? 'attempt_id' FROM commands WHERE id=$1`, res2.LinkID).Scan(&has))
	assert.False(t, has, "a start without an attempt reference stores no attempt_id key")
}

func TestEX7_AC018_PG_ReplayAfterCloseMatchesInTx(t *testing.T) {
	pool := newParityPool(t)
	ex7SeedDevices(t, pool, "dev-1")
	repo := db.NewCommandRepo(&db.DB{Pool: pool})
	ctx := context.Background()

	res, err := repo.CreateSelfUpdate(ctx, ex7Start("dev-1", ex7AttemptA, ex7T0))
	require.NoError(t, err)
	require.Equal(t, models.SelfUpdateCreated, res.Disposition)
	msg := "agent reported the update failed"
	_, applied, err := repo.Transition(ctx, models.CommandTransition{ID: res.LinkID, DeviceID: "dev-1", Actor: models.ActorSelfUpdate,
		To: models.CommandFailed, ResultMsg: &msg, Now: ex7T0.Add(time.Minute)})
	require.NoError(t, err)
	require.True(t, applied, "the record is closed")

	replay, err := repo.CreateSelfUpdate(ctx, ex7Start("dev-1", ex7AttemptA, ex7T0.Add(20*time.Minute)))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateAttemptMatch, replay.Disposition, "[AC-018] a replay after the close is a match")
	assert.Equal(t, res.LinkID, replay.LinkID, "[AC-018] linked to the existing record")
	assert.Nil(t, replay.Command, "[C-3 a] nothing to announce: no Command")
	assert.Nil(t, replay.Retired, "[C-3 a] nothing to announce: no Retired")
	assert.Nil(t, replay.CapEvent, "[AC-018] no cap event")
	assert.Equal(t, 1, ex7Count(t, pool, `origin='agent_self_update'`), "[AC-018] still exactly one record: the replay counts toward nothing")
	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM commands WHERE id=$1`, res.LinkID).Scan(&status))
	assert.Equal(t, string(models.CommandFailed), status, "[AC-018] the replay did not reopen the record")
}

func TestEX7_PG_SelfUpdateByAttemptDeviceScoped(t *testing.T) {
	pool := newParityPool(t)
	ex7SeedDevices(t, pool, "dev-1", "dev-2")
	repo := db.NewCommandRepo(&db.DB{Pool: pool})
	ctx := context.Background()

	res, err := repo.CreateSelfUpdate(ctx, ex7Start("dev-1", ex7AttemptA, ex7T0))
	require.NoError(t, err)

	found, err := repo.SelfUpdateByAttempt(ctx, "dev-1", ex7AttemptA)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, res.LinkID, found.ID, "[FR-025] the owner is found")

	other, err := repo.SelfUpdateByAttempt(ctx, "dev-2", ex7AttemptA)
	require.NoError(t, err)
	assert.Nil(t, other, "[AC-029] another device's attempt ID finds nothing")
	none, err := repo.SelfUpdateByAttempt(ctx, "dev-1", ex7AttemptB)
	require.NoError(t, err)
	assert.Nil(t, none, "[AC-019] an unknown attempt ID finds nothing")

	// The same attempt ID on another device is that device's own attempt.
	res2, err := repo.CreateSelfUpdate(ctx, ex7Start("dev-2", ex7AttemptA, ex7T0))
	require.NoError(t, err)
	assert.Equal(t, models.SelfUpdateCreated, res2.Disposition, "[AC-029] a record of dev-1 never matches dev-2's start")
	assert.NotEqual(t, res.LinkID, res2.LinkID)
}

func TestEX7_PG_ConcurrentSameAttemptOneRecord(t *testing.T) {
	pool := newParityPool(t)
	ex7SeedDevices(t, pool, "dev-1")
	repo := db.NewCommandRepo(&db.DB{Pool: pool})
	ctx := context.Background()

	const n = 50
	var wg sync.WaitGroup
	results := make([]models.SelfUpdateCreateResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = repo.CreateSelfUpdate(ctx, ex7Start("dev-1", ex7AttemptA, ex7T0))
		}(i)
	}
	close(start)
	wg.Wait()

	created, matched := 0, 0
	ids := map[string]bool{}
	for i := range results {
		require.NoError(t, errs[i])
		switch results[i].Disposition {
		case models.SelfUpdateCreated:
			created++
		case models.SelfUpdateAttemptMatch, models.SelfUpdateOpenRecord:
			matched++
		default:
			t.Errorf("unexpected disposition %q", results[i].Disposition)
		}
		ids[results[i].LinkID] = true
	}
	assert.Equal(t, 1, created, "[AC-018] exactly one of 50 concurrent starts with one attempt ID creates a record")
	assert.Equal(t, n-1, matched, "[AC-018] the rest link to it")
	assert.Len(t, ids, 1, "[AC-018] every start links to the same record")
	assert.Equal(t, 1, ex7Count(t, pool, `origin='agent_self_update' AND params->>'attempt_id'=$1`, ex7AttemptA), "[AC-018] one row owns the attempt ID")
}

// ex7Repo is what both the PostgreSQL repository and the mock offer.
type ex7Repo interface {
	db.SelfUpdateRepository
	Transition(ctx context.Context, tr models.CommandTransition) (*models.Command, bool, error)
}

// ex7Script runs one fixed sequence against a repository and returns a
// normalised trace: dispositions, whether a start linked to the first or the
// second record, and how many records exist at the end.
func ex7Script(t *testing.T, repo ex7Repo, closeAt time.Time) []string {
	t.Helper()
	ctx := context.Background()
	var trace []string
	ids := map[string]string{}
	label := func(id string) string {
		for k, v := range ids {
			if v == id {
				return k
			}
		}
		return "?"
	}
	step := func(name string, s models.SelfUpdateStart) {
		res, err := repo.CreateSelfUpdate(ctx, s)
		require.NoError(t, err)
		if res.Disposition == models.SelfUpdateCreated {
			ids[name] = res.LinkID
		}
		trace = append(trace, fmt.Sprintf("%s: %s link=%s command=%t retired=%t cap=%t", name, res.Disposition, label(res.LinkID),
			res.Command != nil, res.Retired != nil, res.CapEvent != nil))
	}
	step("A1", ex7Start("dev-1", ex7AttemptA, ex7T0))
	step("A1 replay", ex7Start("dev-1", ex7AttemptA, ex7T0.Add(time.Second)))
	step("B while A open", ex7Start("dev-1", ex7AttemptB, ex7T0.Add(2*time.Second)))
	step("no attempt while A open", ex7Start("dev-1", "", ex7T0.Add(3*time.Second)))
	msg := "done"
	_, applied, err := repo.Transition(ctx, models.CommandTransition{ID: ids["A1"], DeviceID: "dev-1", Actor: models.ActorSelfUpdate,
		To: models.CommandSucceeded, ResultMsg: &msg, Now: closeAt})
	require.NoError(t, err)
	require.True(t, applied)
	step("A1 replay after close", ex7Start("dev-1", ex7AttemptA, closeAt.Add(20*time.Minute)))
	step("B after close", ex7Start("dev-1", ex7AttemptB, closeAt.Add(21*time.Minute)))
	step("B replay", ex7Start("dev-1", ex7AttemptB, closeAt.Add(22*time.Minute)))
	byA, err := repo.SelfUpdateByAttempt(ctx, "dev-1", ex7AttemptA)
	require.NoError(t, err)
	byB, err := repo.SelfUpdateByAttempt(ctx, "dev-1", ex7AttemptB)
	require.NoError(t, err)
	byX, err := repo.SelfUpdateByAttempt(ctx, "dev-1", "unknown")
	require.NoError(t, err)
	byOther, err := repo.SelfUpdateByAttempt(ctx, "dev-2", ex7AttemptA)
	require.NoError(t, err)
	trace = append(trace, fmt.Sprintf("by attempt: A=%s B=%s unknown=%t other-device=%t", label(idOf(byA)), label(idOf(byB)), byX != nil, byOther != nil))
	return trace
}

func idOf(c *models.Command) string {
	if c == nil {
		return ""
	}
	return c.ID
}

func TestEX7_PG_MockParityAttempt(t *testing.T) {
	pool := newParityPool(t)
	ex7SeedDevices(t, pool, "dev-1", "dev-2")
	pg := db.NewCommandRepo(&db.DB{Pool: pool})
	mock := testutil.NewMockCommandRepo()
	closeAt := ex7T0.Add(time.Minute)

	pgTrace := ex7Script(t, pg, closeAt)
	mockTrace := ex7Script(t, mock, closeAt)

	assert.Equal(t, pgTrace, mockTrace, "[R] the mock must mirror step 0 and SelfUpdateByAttempt of the SQL")
	require.NotEmpty(t, pgTrace)
	assert.Contains(t, strings.Join(pgTrace, "\n"), "attempt_match", "the script exercises step 0")
}

func TestEX7_NFR009_PG_AttemptLookupUsesPartialIndex(t *testing.T) {
	pool := newParityPool(t)
	ex7SeedDevices(t, pool, "dev-1")
	ctx := context.Background()

	// The retention bound: at most about 540 self-update records per device.
	for i := 0; i < 540; i++ {
		created := ex7T0.Add(time.Duration(i) * time.Minute)
		_, err := pool.Exec(ctx,
			`INSERT INTO commands (id, device_id, action, params, status, created_at, updated_at, expires_at, exec_deadline_s,
				deadline_at, dispatched_at, finished_at, origin, target_keys)
			 VALUES ($1, 'dev-1', 'agent_update', $2::jsonb, 'succeeded', $3, $3, $3, 600, $4, $3, $3, 'agent_self_update', '{}')`,
			fmt.Sprintf("00000000-0000-4000-8000-%012d", i), fmt.Sprintf(`{"attempt_id":"attempt-%d"}`, i), created, created.Add(10*time.Minute))
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `ANALYZE commands`)
	require.NoError(t, err)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	// A table this small would be scanned in order; ask the planner for the
	// cheapest plan that does not scan the table, to prove the query can use the
	// partial index.
	_, err = tx.Exec(ctx, `SET LOCAL enable_seqscan = off`)
	require.NoError(t, err)
	rows, err := tx.Query(ctx, `EXPLAIN SELECT id FROM commands
		WHERE device_id=$1 AND origin='agent_self_update' AND params->>'attempt_id' = $2
		ORDER BY created_at DESC LIMIT 1`, "dev-1", "attempt-7")
	require.NoError(t, err)
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	rows.Close()
	assert.Contains(t, strings.Join(plan, "\n"), "idx_commands_self_update_recent",
		"[NFR-009] the attempt lookup must use the partial index; plan:\n%s", strings.Join(plan, "\n"))
}
