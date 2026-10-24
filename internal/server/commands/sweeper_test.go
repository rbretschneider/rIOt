package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedDispatchedAgo stores a running docker_update dispatched `ago` ago.
func seedDispatchedAgo(h *harness, id string, ago time.Duration, mod func(*models.Command)) {
	h.t.Helper()
	h.seed(id, "docker_update", "running", func(c *models.Command) {
		at := h.clock.Now().Add(-ago)
		dl := at.Add(time.Duration(c.ExecDeadlineS) * time.Second)
		c.DispatchedAt, c.DeadlineAt = &at, &dl
		if mod != nil {
			mod(c)
		}
	})
}

func TestCL_AC026_SweeperTimesOutOverdueRunning(t *testing.T) {
	h := newHarness(t)
	seedDispatchedAgo(h, uid(1), 36*time.Minute, nil)

	res := h.svc.Sweep(context.Background())

	got := h.get(uid(1))
	assert.Equal(t, "timed_out", got.Status, "[AC-026] a running docker_update dispatched 36m ago must time out")
	assert.Equal(t, "no result from agent within 35m", got.ResultMsg)
	assert.Equal(t, 1, res.TimedOut)
	require.NotNil(t, got.FinishedAt)
	require.Len(t, h.bc.Updates(), 1)
	assert.Equal(t, "timed_out", h.bc.Updates()[0].Status)
	require.Len(t, h.ev.Calls(), 1)
	assert.Equal(t, "error", h.ev.Calls()[0].Status, "the completion event uses the legacy vocabulary")
	assert.Contains(t, h.logs.String(), "command swept")
	assert.Contains(t, h.logs.String(), "from=running")
	assert.Contains(t, h.logs.String(), "to=timed_out")
}

func TestCL_AC027_SweeperLeavesInDeadlineRunning(t *testing.T) {
	h := newHarness(t)
	seedDispatchedAgo(h, uid(1), 20*time.Minute, nil)

	res := h.svc.Sweep(context.Background())

	assert.Equal(t, "running", h.get(uid(1)).Status, "[AC-027] a command inside its deadline must be unchanged")
	assert.Zero(t, res.TimedOut)
	assert.Empty(t, h.bc.Updates())
}

func TestSweeper_DeadlineBoundaryIsInclusive(t *testing.T) {
	h := newHarness(t)
	seedDispatchedAgo(h, uid(1), 35*time.Minute, nil) // deadline == now

	h.svc.Sweep(context.Background())

	assert.Equal(t, "timed_out", h.get(uid(1)).Status, "deadline_at <= now is overdue")
}

func TestSweeper_TimedOutPolicyCommandRecordsFailure(t *testing.T) {
	h := newHarness(t)
	polID := h.seedPolicy(0)
	seedDispatchedAgo(h, uid(1), time.Hour, func(c *models.Command) { c.PolicyID = &polID })

	h.svc.Sweep(context.Background())

	assert.Equal(t, 1, h.pol.Policies[devA][0].ConsecutiveFailures)
	assert.Nil(t, h.pol.Policies[devA][0].LastTriggeredAt)
}

func TestSweeper_ExpiresQueuedPastTTL(t *testing.T) {
	h := newHarness(t)
	h.seed(uid(1), "reboot", "queued", func(c *models.Command) {
		c.ExpiresAt = h.clock.Now().Add(-time.Second)
		c.CreatedAt = h.clock.Now().Add(-15*time.Minute - time.Second)
	})
	h.seed(uid(2), "reboot", "queued", nil) // not due

	res := h.svc.Sweep(context.Background())

	assert.Equal(t, 1, res.Expired)
	assert.Equal(t, "expired", h.get(uid(1)).Status)
	assert.Equal(t, "expired: not delivered within 15m", h.get(uid(1)).ResultMsg)
	assert.Equal(t, "queued", h.get(uid(2)).Status)
}

func TestCL_AC031_SweeperRunsBeforeFirstTick(t *testing.T) {
	h := newHarness(t)
	seedDispatchedAgo(h, uid(1), 2*time.Hour, nil)
	tick := make(chan time.Time) // never fires
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		h.svc.RunSweeper(ctx, tick)
	}()

	require.Eventually(t, func() bool {
		c, _ := h.repo.Get(uid(1))
		return c.Status == "timed_out"
	}, 5*time.Second, 5*time.Millisecond, "[AC-031] a stale row must be timed out before the first interval elapses")
	cancel()
	<-done
}

func TestSweeper_SweepsOnEveryTick(t *testing.T) {
	h := newHarness(t)
	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.svc.RunSweeper(ctx, tick)
	}()

	seedDispatchedAgo(h, uid(1), 2*time.Hour, nil)
	tick <- h.clock.Now() // unbuffered: the loop took the tick, the sweep follows

	require.Eventually(t, func() bool {
		c, _ := h.repo.Get(uid(1))
		return c.Status == "timed_out"
	}, 5*time.Second, 5*time.Millisecond)
	cancel()
	<-done
}

func TestCL_AC103_LegacyQueuedRebootExpires(t *testing.T) {
	h := newHarness(t)
	// Created by a pre-lifecycle server 2 days ago; the migration backfilled expires_at.
	created := h.clock.Now().Add(-48 * time.Hour)
	h.seed(uid(1), "reboot", "queued", func(c *models.Command) {
		c.CreatedAt, c.UpdatedAt = created, created
		c.ExpiresAt = created.Add(15 * time.Minute)
		c.PreLifecycle = true
	})

	h.svc.Sweep(context.Background())
	payloads, err := h.svc.ClaimForHeartbeat(context.Background(), devA)

	require.NoError(t, err)
	assert.Equal(t, "expired", h.get(uid(1)).Status, "[AC-103] the legacy queued reboot must be expired")
	assert.Empty(t, payloads, "[AC-103] ... and must not be delivered on the next heartbeat")
}

func TestCL_SEC010_LegacyRowsSweptSilently(t *testing.T) {
	h := newHarness(t)
	polID := h.seedPolicy(0)
	created := h.clock.Now().Add(-48 * time.Hour)
	// Two pre-lifecycle rows: one expired queued, one timed-out ex-sent.
	h.seed(uid(1), "reboot", "queued", func(c *models.Command) {
		c.CreatedAt, c.ExpiresAt, c.PreLifecycle, c.PolicyID = created, created.Add(15*time.Minute), true, &polID
	})
	seedDispatchedAgo(h, uid(2), 40*time.Hour, func(c *models.Command) { c.PreLifecycle, c.PolicyID = true, &polID })
	// One current-lifecycle row, which is announced normally.
	seedDispatchedAgo(h, uid(3), 3*time.Hour, nil)

	res := h.svc.Sweep(context.Background())

	assert.Equal(t, "expired", h.get(uid(1)).Status)
	assert.Equal(t, "timed_out", h.get(uid(2)).Status)
	assert.Equal(t, 1, res.LegacyExpired)
	assert.Equal(t, 1, res.LegacyTimedOut)
	assert.Equal(t, 1, res.TimedOut)

	updates := h.bc.Updates()
	require.Len(t, updates, 1, "[SEC-010] pre-lifecycle rows are relabelled without a broadcast")
	assert.Equal(t, uid(3), updates[0].CommandID)
	require.Len(t, h.ev.Calls(), 1, "[SEC-010] ... and without a completion event")
	assert.Zero(t, h.pol.Policies[devA][0].ConsecutiveFailures, "[SEC-010] ... and without a policy outcome")
	assert.Equal(t, 1, strings.Count(h.logs.String(), "legacy commands relabelled"), "one summary Info line per pass")
	assert.Contains(t, h.logs.String(), "expired=1")
	assert.Contains(t, h.logs.String(), "timed_out=1")
}

func TestSweeper_RepositoryErrorsAreLoggedAndRetried(t *testing.T) {
	h := newHarness(t)
	seedDispatchedAgo(h, uid(1), 2*time.Hour, nil)
	h.repo.FailOn("ListOverdue", assert.AnError)

	h.svc.Sweep(context.Background())
	assert.Equal(t, "running", h.get(uid(1)).Status)
	assert.Contains(t, h.logs.String(), "list overdue commands")

	h.repo.FailOn("ListOverdue", nil)
	h.svc.Sweep(context.Background())
	assert.Equal(t, "timed_out", h.get(uid(1)).Status, "the next pass retries")
}

func TestSweeper_NeverSweepsTerminalRows(t *testing.T) {
	h := newHarness(t)
	seedDispatchedAgo(h, uid(1), 2*time.Hour, nil)
	h.report(result(uid(1), models.CommandResult{Status: "success"}))

	res := h.svc.Sweep(context.Background())

	assert.Equal(t, "succeeded", h.get(uid(1)).Status, "a terminal row is never swept")
	assert.Zero(t, res.TimedOut)
}

// seedRebootDispatchedAgo stores a running reboot dispatched `ago` ago, with the
// execution deadline the server computes for new reboot rows.
func seedRebootDispatchedAgo(h *harness, id string, ago time.Duration) {
	h.t.Helper()
	h.seed(id, "reboot", "running", func(c *models.Command) {
		at := h.clock.Now().Add(-ago)
		dl := at.Add(time.Duration(c.ExecDeadlineS) * time.Second)
		c.DispatchedAt, c.DeadlineAt = &at, &dl
	})
}

func TestSweep_RD_AC017_RebootNotTimedOutBefore20m(t *testing.T) {
	t.Run("a reboot waiting 19m for the dpkg lock is still running", func(t *testing.T) {
		h := newHarness(t)
		seedRebootDispatchedAgo(h, uid(1), 19*time.Minute)

		res := h.svc.Sweep(context.Background())

		assert.Equal(t, "running", h.get(uid(1)).Status, "[AC-017] a reboot inside its 20m deadline must not be timed out")
		assert.Zero(t, res.TimedOut, "[AC-017] nothing may be swept before the 20m deadline")
	})
	t.Run("the old 10m deadline no longer times a reboot out", func(t *testing.T) {
		h := newHarness(t)
		seedRebootDispatchedAgo(h, uid(1), 11*time.Minute)

		h.svc.Sweep(context.Background())

		assert.Equal(t, "running", h.get(uid(1)).Status, "[AC-017] 11m is past the old 600s deadline but inside the new 1200s one")
	})
	t.Run("a reboot with no result after 20m times out with the 20m text", func(t *testing.T) {
		h := newHarness(t)
		seedRebootDispatchedAgo(h, uid(1), 20*time.Minute)

		res := h.svc.Sweep(context.Background())

		got := h.get(uid(1))
		assert.Equal(t, "timed_out", got.Status, "[AC-017] a lost reboot result must still be swept at the new deadline")
		assert.Equal(t, "no result from agent within 20m", got.ResultMsg)
		assert.Equal(t, 1, res.TimedOut)
	})
	t.Run("shutdown keeps the 10m deadline", func(t *testing.T) {
		h := newHarness(t)
		h.seed(uid(1), "shutdown", "running", func(c *models.Command) {
			at := h.clock.Now().Add(-10 * time.Minute)
			dl := at.Add(time.Duration(c.ExecDeadlineS) * time.Second)
			c.DispatchedAt, c.DeadlineAt = &at, &dl
		})

		h.svc.Sweep(context.Background())

		assert.Equal(t, "timed_out", h.get(uid(1)).Status, "[AC-020] shutdown must still time out at 10m")
		assert.Equal(t, "no result from agent within 10m", h.get(uid(1)).ResultMsg)
	})
}
