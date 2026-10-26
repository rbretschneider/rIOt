package commands

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingObserver is a CommandObserver that keeps every row it is told about.
type recordingObserver struct {
	mu   sync.Mutex
	rows []models.Command
}

func (o *recordingObserver) CommandChanged(c models.Command) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.rows = append(o.rows, c)
}

func (o *recordingObserver) statuses() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, len(o.rows))
	for i, c := range o.rows {
		out[i] = c.Status
	}
	return out
}

// newObservedHarness is newHarness with a recording observer on the service.
func newObservedHarness(t *testing.T, connected ...string) (*harness, *recordingObserver) {
	t.Helper()
	h := newHarness(t, connected...)
	obs := &recordingObserver{}
	h.svc = New(Options{
		Commands: h.repo, Policies: h.pol, Telemetry: h.tel, Agents: h.agents,
		Broadcaster: h.bc, Observer: obs, Events: h.ev, Now: h.clock.Now, Logger: nil,
	})
	return h, obs
}

func broadcastStatuses(h *harness) []string {
	var out []string
	for _, u := range h.bc.Updates() {
		out = append(out, u.Status)
	}
	return out
}

func TestEX_Observer_FiresOnAcceptedTransitionsOnly(t *testing.T) {
	t.Run("create, claim, agent reports and sweep each notify once, matching the broadcaster", func(t *testing.T) {
		h, obs := newObservedHarness(t, devA)
		ctx := context.Background()

		res, err := h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: "reboot", ActorName: "ryan"})
		require.NoError(t, err)
		id := res.Command.ID
		h.report(ack(id))
		h.report(progress(id, "running", "wait_dpkg_lock", nil, ""))
		h.report(result(id, models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded, Message: "initiated"}))

		h.seed(uid(9), "docker_update", "running", nil)
		h.clock.Advance(2 * time.Hour)
		h.svc.Sweep(ctx)

		assert.Equal(t, broadcastStatuses(h), obs.statuses(),
			"[AD-004] the observer sees exactly the accepted transitions the dashboard sees, in the same order")
		assert.Contains(t, obs.statuses(), "queued", "creation is observed")
		assert.Contains(t, obs.statuses(), "dispatched", "the delivery claim is observed")
		assert.Contains(t, obs.statuses(), "succeeded", "the result is observed")
		assert.Contains(t, obs.statuses(), "timed_out", "a sweep is observed")
		for _, c := range obs.rows {
			assert.NotNil(t, c.TargetKeys, "the observer receives the full row")
		}
	})

	t.Run("a heartbeat claim, a revert and a cancel are observed", func(t *testing.T) {
		h, obs := newObservedHarness(t)
		ctx := context.Background()
		first, err := h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		_, err = h.svc.ClaimForHeartbeat(ctx, devA)
		require.NoError(t, err)
		_ = first
		second, err := h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: "fetch_logs"})
		require.NoError(t, err)
		_, out, err := h.svc.Cancel(ctx, devA, second.Command.ID)
		require.NoError(t, err)
		require.Equal(t, CancelApplied, out)

		assert.Equal(t, broadcastStatuses(h), obs.statuses(), "[AD-004] claim and cancel are observed like they are broadcast")
		assert.Equal(t, []string{"queued", "dispatched", "queued", "cancelled"}, obs.statuses())
	})

	t.Run("rejected work never reaches the observer", func(t *testing.T) {
		h, obs := newObservedHarness(t)
		ctx := context.Background()
		_, err := h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		require.Len(t, obs.statuses(), 1)

		conflict, err := h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		require.NotNil(t, conflict.Conflict)
		_, err = h.svc.Dispatch(ctx, DispatchRequest{DeviceID: devA, Action: "reboot", Origin: "robot"})
		require.Error(t, err)
		out, err := h.svc.HandleAgentReport(ctx, devA, ack(uid(404)))
		require.NoError(t, err)
		assert.Equal(t, models.ReportNotFound, out.Disposition)

		done := h.seed(uid(5), "docker_update", "running", nil)
		h.report(result(done.ID, models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded}))
		before := len(obs.statuses())
		dup := h.report(result(done.ID, models.CommandResult{Status: "success", Outcome: models.OutcomeSucceeded}))
		assert.Equal(t, models.ReportDuplicate, dup.Disposition)

		assert.Len(t, obs.statuses(), before, "[AD-004] a conflict, an invalid request, a not-found report and a duplicate result notify nothing")
		assert.Equal(t, broadcastStatuses(h), obs.statuses())
	})

	t.Run("the observer is called even when no broadcaster is wired", func(t *testing.T) {
		repo := testutil.NewMockCommandRepo()
		obs := &recordingObserver{}
		svc := New(Options{Commands: repo, Observer: obs, Now: newFakeClock().Now})
		_, err := svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		assert.Equal(t, []string{"queued"}, obs.statuses(), "[AD-004] observation does not depend on the broadcaster")
	})

	t.Run("a service without an observer still works", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "reboot"})
		require.NoError(t, err)
		assert.Len(t, h.bc.Updates(), 1, "the broadcaster path is unchanged")
	})
}
