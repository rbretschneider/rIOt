package commands

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-008: installer reinstall records. Recognition of an authenticated
// notice, the registration hook and the resolver the expected-activity tracker
// calls at a record's bound.
//
// Everything here goes through ActorReinstall transitions followed by a
// broadcast (observer plus dashboards). It never calls completed() (no
// command_completed event), the policy recorder or the progress gate, and it
// never sets result_bytes, steps, target_results, stderr_tail, exit_code,
// duration_ms or command output (AD-005, EXPECT-002 C-6). Every failure fails
// open: no record is created and the device keeps alerting (NFR-006).
//
// Bounds (AD-012): per notice one short transaction; per registration one select
// and at most one update; every Warn goes through the per-device log throttle;
// the service keeps no per-notice state, and an accepted, refused or rejected
// notice logs at Debug only.

// ReinstallNoticeInput is one authenticated reinstall notice. At is the server
// receive time. Nothing from the request body is part of it (FR-012).
type ReinstallNoticeInput struct {
	DeviceID, Hostname string
	At                 time.Time
}

// ReinstallNoticeResult is what the handler answers and broadcasts: the
// disposition and the server-written events (at most the rate-cap warning).
type ReinstallNoticeResult struct {
	Disposition models.ReinstallDisposition
	Events      []models.Event
}

// NoteReinstallNotice records one notice (AD-004). It never returns an error:
// every failure yields ReinstallUnavailable and one throttled Warn.
func (s *Service) NoteReinstallNotice(ctx context.Context, in ReinstallNoticeInput) ReinstallNoticeResult {
	if s.reinstalls == nil || in.DeviceID == "" {
		return ReinstallNoticeResult{Disposition: models.ReinstallUnavailable}
	}
	res, err := s.reinstalls.CreateReinstall(ctx, models.ReinstallNotice{DeviceID: in.DeviceID, Hostname: in.Hostname, At: in.At})
	if err != nil {
		s.warnThrottled(in.DeviceID, "agent reinstall notice could not be recorded; offline alerts are not suppressed", "error", err.Error())
		return ReinstallNoticeResult{Disposition: models.ReinstallUnavailable}
	}
	// A retired stale record, then the new one: each fires the tracker hook and
	// the dashboard command_update (AD-004).
	s.broadcast(res.Retired)
	s.broadcast(res.Command)
	s.log.Debug("agent reinstall notice", "device_id", in.DeviceID, "disposition", string(res.Disposition))

	out := ReinstallNoticeResult{Disposition: res.Disposition}
	if out.Disposition == "" {
		out.Disposition = models.ReinstallUnavailable
	}
	if res.CapEvent != nil {
		out.Events = append(out.Events, *res.CapEvent)
	}
	return out
}

// NoteReinstallRegistration is the registration hook (AD-007, FR-015 a): an
// existing device registered, so its open record (and only a running one,
// received before its bound) ends succeeded. It returns nothing: a failure
// leaves the record to its bound.
func (s *Service) NoteReinstallRegistration(ctx context.Context, deviceID string, at time.Time) {
	if s.reinstalls == nil || deviceID == "" {
		return
	}
	rec, err := s.reinstalls.OpenReinstall(ctx, deviceID)
	if err != nil {
		s.warnThrottled(deviceID, "agent reinstall record could not be read at registration", "error", err.Error())
		return
	}
	if rec == nil || rec.State() != models.CommandRunning || rec.DispatchedAt == nil || rec.DeadlineAt == nil {
		return
	}
	hard := rec.DispatchedAt.Add(models.ExpectReinstallBound)
	if rec.DeadlineAt.Before(hard) {
		hard = *rec.DeadlineAt
	}
	if !at.Before(hard) {
		return // a registration at or after the bound belongs to no window
	}
	if at.Before(*rec.DispatchedAt) {
		return // FR-015 a: only a registration received after the start closes it
	}
	row, applied, err := s.repo.Transition(ctx, models.CommandTransition{
		ID: rec.ID, DeviceID: deviceID, Actor: models.ActorReinstall, To: models.CommandSucceeded,
		From:      []models.CommandState{models.CommandRunning},
		Step:      strPtr(models.ReinstallStepConfirmed),
		ResultMsg: resultMsg(models.ReinstallConfirmedResultMessage), Now: at,
	})
	if err != nil {
		s.warnThrottled(deviceID, "agent reinstall record could not be updated at registration; it ends at its bound", "error", err.Error())
		return
	}
	if applied {
		s.broadcast(row)
	}
}

// ResolveReinstall ends a record at its bound (AD-008 step 2). It is the
// expected-activity tracker's ReinstallResolver. applied is false, with a nil
// row, when the record is gone, foreign or already terminal; the caller then
// re-reads.
func (s *Service) ResolveReinstall(ctx context.Context, commandID, deviceID string, outcome models.ReinstallOutcome, message string, at time.Time) (*models.Command, bool, error) {
	to, ok := outcome.State()
	if !ok {
		return nil, false, ErrInvalidReport
	}
	row, applied, err := s.repo.Transition(ctx, models.CommandTransition{
		ID: commandID, DeviceID: deviceID, Actor: models.ActorReinstall, To: to,
		From:        []models.CommandState{models.CommandRunning},
		FailureStep: strPtr(outcome.FailureStep()),
		ResultMsg:   resultMsg(message), Now: at,
	})
	if err != nil || !applied {
		return nil, false, err
	}
	s.broadcast(row)
	return row, true, nil
}
