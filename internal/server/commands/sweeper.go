package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

const (
	// SweepInterval is how often RunSweeper's caller should tick (FR-027).
	SweepInterval = 60 * time.Second
	// sweepBatch bounds each query of one pass (NFR-007).
	sweepBatch = 500
)

// SweepResult counts what one pass changed.
type SweepResult struct {
	// Expired and TimedOut count rows with the full side effects (broadcast,
	// completion event, policy outcome).
	Expired  int
	TimedOut int
	// LegacyExpired and LegacyTimedOut count pre-lifecycle rows, which are
	// relabelled silently (SEC-010).
	LegacyExpired  int
	LegacyTimedOut int
}

// Sweep runs one pass: queued commands past expires_at become expired, and
// in-flight commands past deadline_at become timed_out (FR-023, FR-024).
// Errors are logged and retried on the next pass.
func (s *Service) Sweep(ctx context.Context) SweepResult {
	var res SweepResult
	s.expireQueued(ctx, "", &res)
	s.timeOutOverdue(ctx, &res)
	if res.LegacyExpired > 0 || res.LegacyTimedOut > 0 {
		s.log.Info("legacy commands relabelled", "expired", res.LegacyExpired, "timed_out", res.LegacyTimedOut)
	}
	return res
}

// RunSweeper sweeps once immediately (so a stale row is resolved at server
// start, before the first tick, FR-027) and then on every tick until ctx ends.
func (s *Service) RunSweeper(ctx context.Context, tick <-chan time.Time) {
	s.Sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			s.Sweep(ctx)
		}
	}
}

// expireQueued moves due queued commands (of deviceID, or of every device when
// empty) to expired. The heartbeat path uses it for one device.
func (s *Service) expireQueued(ctx context.Context, deviceID string, res *SweepResult) {
	now := s.now().UTC()
	due, err := s.repo.ListExpiredQueued(ctx, deviceID, now, sweepBatch)
	if err != nil {
		s.log.Error("list expired queued commands", "device_id", deviceID, "error", err.Error())
		return
	}
	for i := range due {
		before := due[i]
		msg := expiredMessage(before)
		after, applied, err := s.repo.Transition(ctx, models.CommandTransition{
			ID: before.ID, Actor: models.ActorServer, To: models.CommandExpired,
			From: []models.CommandState{models.CommandQueued}, ResultMsg: &msg, Now: now,
		})
		if err != nil {
			s.log.Error("expire command", "command_id", before.ID, "device_id", before.DeviceID, "error", err.Error())
			continue
		}
		if !applied {
			continue // claimed or cancelled between the list and the update
		}
		if s.sweptSilently(after) {
			res.LegacyExpired++
			continue
		}
		res.Expired++
		s.afterSweep(ctx, after, models.CommandQueued, models.CommandExpired, now)
	}
}

// timeOutOverdue moves in-flight commands past their deadline to timed_out.
func (s *Service) timeOutOverdue(ctx context.Context, res *SweepResult) {
	now := s.now().UTC()
	due, err := s.repo.ListOverdue(ctx, "", now, sweepBatch)
	if err != nil {
		s.log.Error("list overdue commands", "error", err.Error())
		return
	}
	for i := range due {
		before := due[i]
		if before.Origin == models.OriginAgentSelfUpdate || before.Origin == models.OriginInstaller {
			// Defensive (EXPECT-002 AD-007 row 7, EXPECT-008 AD-009): ListOverdue and
			// the server edge already exclude these rows; the tracker resolves their
			// bound.
			continue
		}
		msg := fmt.Sprintf("no result from agent within %s", models.FormatDurationShort(time.Duration(before.ExecDeadlineS)*time.Second))
		after, applied, err := s.repo.Transition(ctx, models.CommandTransition{
			ID: before.ID, Actor: models.ActorServer, To: models.CommandTimedOut,
			From: []models.CommandState{
				models.CommandDispatched, models.CommandAcknowledged, models.CommandRunning, models.CommandVerifying,
			},
			ResultMsg: &msg, Now: now,
		})
		if err != nil {
			s.log.Error("time out command", "command_id", before.ID, "device_id", before.DeviceID, "error", err.Error())
			continue
		}
		if !applied {
			continue // the agent reported between the list and the update
		}
		if s.sweptSilently(after) {
			res.LegacyTimedOut++
			continue
		}
		res.TimedOut++
		s.afterSweep(ctx, after, before.State(), models.CommandTimedOut, now)
	}
}

// sweptSilently reports whether a swept row is a pre-lifecycle one: those are
// relabelled without a broadcast, a completion event or a policy outcome, so the
// first sweep after the migration cannot flood the hub or the notification
// channels (SEC-010).
func (s *Service) sweptSilently(cmd *models.Command) bool {
	if !cmd.PreLifecycle {
		return false
	}
	s.gate.forget(cmd.ID)
	return true
}

// afterSweep runs the side effects of a sweeper transition.
func (s *Service) afterSweep(ctx context.Context, cmd *models.Command, from, to models.CommandState, now time.Time) {
	s.afterTransition(ctx, cmd, to, now)
	s.broadcast(cmd)
	s.completed(ctx, cmd, to)
	s.log.Info("command swept", "command_id", cmd.ID, "device_id", cmd.DeviceID, "from", string(from), "to", string(to))
}

// expiredMessage explains an expiry. After an ambiguous send it must not claim
// the command was never delivered (SEC-016).
func expiredMessage(cmd models.Command) string {
	ttl := cmd.ExpiresAt.Sub(cmd.CreatedAt)
	if ttl <= 0 {
		ttl = models.QueuedTTL(cmd.Action)
	}
	if cmd.SendAmbiguous {
		return fmt.Sprintf("expired: delivery unconfirmed within %s; an earlier send attempt may have reached the agent",
			models.FormatDurationShort(ttl))
	}
	return fmt.Sprintf("expired: not delivered within %s", models.FormatDurationShort(ttl))
}
