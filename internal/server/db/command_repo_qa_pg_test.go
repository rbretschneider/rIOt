package db

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Added by QA Engineer. PG-gated like command_repo_pg_test.go: these skip unless
// RIOT_TEST_PG_URL names a test database.

func (e *pgEnv) seedDispatched(id, device string) {
	e.T.Helper()
	repo := e.repo()
	ctx := withDeadline(e.T, 30*time.Second)
	_, err := repo.CreateIfNoConflict(ctx, newQueued(id, device, "fetch_logs"))
	require.NoError(e.T, err)
	_, ok, err := repo.ClaimByID(ctx, id, pgNow)
	require.NoError(e.T, err)
	require.True(e.T, ok)
}

// Covers AC-086 (FR-072/FR-073 immutability under concurrency): two agent
// results racing on one in-flight row. Exactly one applies and the stored state
// is the winner's; the loser can never overwrite it.
func TestCL_AC086_QA_PG_ConcurrentConflictingResultsExactlyOneWins(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 90*time.Second)

	for i := 0; i < 20; i++ {
		id := "qa-res-" + string(rune('a'+i))
		env.seedDispatched(id, "dev-1")

		var okSucceeded, okFailed bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, ok, err := repo.Transition(ctx, models.CommandTransition{
				ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded, Now: pgNow.Add(time.Minute)})
			assert.NoError(t, err)
			okSucceeded = ok
		}()
		go func() {
			defer wg.Done()
			_, ok, err := repo.Transition(ctx, models.CommandTransition{
				ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandFailed, Now: pgNow.Add(time.Minute)})
			assert.NoError(t, err)
			okFailed = ok
		}()
		wg.Wait()

		assert.NotEqual(t, okSucceeded, okFailed, "[AC-086] iteration %d: exactly one terminal result may win", i)
		want := "failed"
		if okSucceeded {
			want = "succeeded"
		}
		assert.Equal(t, want, env.statusOf(id), "[AC-086] the stored state must be the winner's")
	}
}

// Covers AC-087 (FR-073): a timed_out row accepts exactly one late result even
// when many duplicate results arrive at the same time.
func TestCL_AC087_QA_PG_TimedOutLateResultAppliesExactlyOnce(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 60*time.Second)

	env.seedDispatched("qa-late", "dev-1")
	_, applied, err := repo.Transition(ctx, models.CommandTransition{
		ID: "qa-late", Actor: models.ActorServer, To: models.CommandTimedOut, Now: pgNow.Add(time.Hour)})
	require.NoError(t, err)
	require.True(t, applied)

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		to := models.CommandSucceeded
		if i%2 == 1 {
			to = models.CommandFailed
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := repo.Transition(ctx, models.CommandTransition{
				ID: "qa-late", DeviceID: "dev-1", Actor: models.ActorAgent, To: to, Now: pgNow.Add(2 * time.Hour)})
			assert.NoError(t, err)
			if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), wins.Load(), "[AC-087] exactly one late result may be accepted")
	assert.Contains(t, []string{"succeeded", "failed"}, env.statusOf("qa-late"))
}

// Covers AC-026 / AC-087 (FR-070, FR-073): an agent result racing the sweeper's
// timed_out must never be lost. Whichever order the two UPDATEs run in, the
// agent's result applies (a timed_out row accepts a late result) and the final
// state is the agent's outcome, never a timed_out that overwrote it.
func TestCL_AC026_QA_PG_AgentResultNeverLostToSweeperTimeout(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	repo := env.repo()
	ctx := withDeadline(t, 90*time.Second)

	for i := 0; i < 20; i++ {
		id := "qa-sweep-" + string(rune('a'+i))
		env.seedDispatched(id, "dev-1")

		var agentOK bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			// The sweeper's exact guard (service.timeOutOverdue).
			_, _, err := repo.Transition(ctx, models.CommandTransition{
				ID: id, Actor: models.ActorServer, To: models.CommandTimedOut,
				From: []models.CommandState{
					models.CommandDispatched, models.CommandAcknowledged, models.CommandRunning, models.CommandVerifying},
				Now: pgNow.Add(time.Hour)})
			assert.NoError(t, err)
		}()
		go func() {
			defer wg.Done()
			_, ok, err := repo.Transition(ctx, models.CommandTransition{
				ID: id, DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandSucceeded, Now: pgNow.Add(time.Hour)})
			assert.NoError(t, err)
			agentOK = ok
		}()
		wg.Wait()

		assert.True(t, agentOK, "[AC-087] iteration %d: the agent's result must apply in either order", i)
		assert.Equal(t, "succeeded", env.statusOf(id), "[AC-026] iteration %d: a timeout must never overwrite a result", i)
	}
}
