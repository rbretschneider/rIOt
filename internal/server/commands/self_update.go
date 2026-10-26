package commands

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-002: agent self-update records. Recognition and attribution of the
// agent_update_* events, the registration hook and the resolver the expected-
// activity tracker calls at a record's bound.
//
// Everything here goes through ActorSelfUpdate transitions followed by a
// broadcast (observer plus dashboards). It never calls completed() (no
// command_completed event), the policy recorder or the progress gate, and it
// never sets result_bytes, steps, target_results, stderr_tail, exit_code,
// duration_ms or command output (AD-003, C-6). Every failure fails open: the
// agent's event is stored as today and the device keeps alerting.
//
// Bounds (AD-013): per agent_update_* event at most one short transaction (a
// start) or one to two statements (completed, failed); per registration one
// select and at most one update; every Warn goes through the per-device log
// throttle; the service keeps no per-event state.

// AgentUpdateEventInput is one agent_update_started, _completed or _failed
// event of the authenticated device. At is the server receive time.
type AgentUpdateEventInput struct {
	DeviceID, Hostname string
	Type               models.EventType
	Message            string
	At                 time.Time
}

// AgentUpdateEventLink is what the handler stores with the agent's event: the
// command or record it links to ("" when none) and the server-written events
// to broadcast (at most the one rate-cap warning).
type AgentUpdateEventLink struct {
	CommandID, CommandAttr string
	Events                 []models.Event
}

// resultMsg bounds a server-rendered result_msg (AD-003).
func resultMsg(s string) *string {
	m := models.TruncateHead(models.SanitizeText(s), models.SelfUpdateResultMsgBytes)
	return &m
}

func strPtr(s string) *string { return &s }

// NoteAgentUpdateEvent recognises and attributes one agent_update_* event
// (AD-002, AD-004). It never returns an error: every failure yields an empty
// link and one throttled Warn, so the event is stored as it is today (NFR-009).
func (s *Service) NoteAgentUpdateEvent(ctx context.Context, in AgentUpdateEventInput) AgentUpdateEventLink {
	if s.selfUpdates == nil || in.DeviceID == "" {
		return AgentUpdateEventLink{}
	}
	switch in.Type {
	case models.EventAgentUpdateStarted:
		return s.noteStart(ctx, in)
	case models.EventAgentUpdateCompleted, models.EventAgentUpdateFailed:
		return s.noteOutcomeEvent(ctx, in)
	}
	return AgentUpdateEventLink{}
}

func (s *Service) noteStart(ctx context.Context, in AgentUpdateEventInput) AgentUpdateEventLink {
	from, to, ok := models.ParseSelfUpdateStart(in.Message)
	if !ok {
		s.log.Debug("agent update start message not recognised", "device_id", in.DeviceID, "bytes", len(in.Message))
		return AgentUpdateEventLink{}
	}
	res, err := s.selfUpdates.CreateSelfUpdate(ctx, models.SelfUpdateStart{
		DeviceID: in.DeviceID, Hostname: in.Hostname, From: from, To: to, At: in.At,
	})
	if err != nil {
		s.warnThrottled(in.DeviceID, "agent self-update start could not be recorded; offline alerts are not suppressed", "error", err.Error())
		return AgentUpdateEventLink{}
	}
	// A retired stale record, then the new one: each fires the tracker hook and
	// the dashboard command_update (AD-002 step 3).
	s.broadcast(res.Retired)
	s.broadcast(res.Command)
	s.log.Debug("agent self-update start", "device_id", in.DeviceID, "disposition", string(res.Disposition))

	link := AgentUpdateEventLink{}
	if res.LinkID != "" {
		link.CommandID, link.CommandAttr = res.LinkID, models.CommandAttrInferred
	}
	if res.CapEvent != nil {
		link.Events = append(link.Events, *res.CapEvent)
	}
	return link
}

// noteOutcomeEvent attributes a completed or failed event to the device's most
// recent record started at most 10 minutes earlier, and moves an open record to
// its outcome (AD-004). These events never create a record and are never linked
// to commands (X-16).
func (s *Service) noteOutcomeEvent(ctx context.Context, in AgentUpdateEventInput) AgentUpdateEventLink {
	rec, err := s.selfUpdates.LatestSelfUpdateSince(ctx, in.DeviceID, in.At.Add(-models.SelfUpdateAttributionWindow))
	if err != nil {
		s.warnThrottled(in.DeviceID, "agent self-update record could not be read; the event is stored unlinked", "error", err.Error())
		return AgentUpdateEventLink{}
	}
	if rec == nil {
		return AgentUpdateEventLink{}
	}
	link := AgentUpdateEventLink{CommandID: rec.ID, CommandAttr: models.CommandAttrInferred}
	if !rec.State().IsActive() {
		return link // FR-022: the outcome is set; link only
	}

	tr := models.CommandTransition{
		ID: rec.ID, DeviceID: in.DeviceID, Actor: models.ActorSelfUpdate,
		From: []models.CommandState{models.CommandRunning, models.CommandVerifying}, Now: in.At,
	}
	from, to, haveVersions := models.SelfUpdateVersions(*rec)
	if in.Type == models.EventAgentUpdateCompleted {
		tr.To = models.CommandSucceeded
		tr.Step = strPtr(models.SelfUpdateStepConfirmed)
		tr.ResultMsg = resultMsg(models.SelfUpdateCompletedMessage(in.Message))
	} else {
		tr.To = models.CommandFailed
		tr.FailureStep = strPtr(models.SelfUpdateFailureAgentFailed)
		if haveVersions {
			tr.ResultMsg = resultMsg(models.SelfUpdateFailedMessage(in.Hostname, from, to, in.Message))
		} else {
			tr.ResultMsg = resultMsg("failed: " + models.SafeText(in.Message, models.SelfUpdateReasonBytes))
		}
	}
	row, applied, err := s.repo.Transition(ctx, tr)
	if err != nil {
		s.warnThrottled(in.DeviceID, "agent self-update outcome could not be recorded; the record ends at its bound", "error", err.Error())
		return link
	}
	if applied {
		s.broadcast(row)
	}
	return link
}

// NoteSelfUpdateRegistration is the registration hook (AD-008, FR-011 c and d):
// an existing device registered, so its open record (and only a running one,
// received before its bound) moves to verifying when the device reports <to>,
// or to failed (wrong_version) when it reports any other version (X-15). It
// returns nothing: a failure leaves the record to its bound.
func (s *Service) NoteSelfUpdateRegistration(ctx context.Context, deviceID, hostname, reportedVersion string, at time.Time) {
	if s.selfUpdates == nil || deviceID == "" {
		return
	}
	rec, err := s.selfUpdates.OpenSelfUpdate(ctx, deviceID)
	if err != nil {
		s.warnThrottled(deviceID, "agent self-update record could not be read at registration", "error", err.Error())
		return
	}
	if rec == nil || rec.State() != models.CommandRunning || rec.DispatchedAt == nil || rec.DeadlineAt == nil {
		return
	}
	hard := rec.DispatchedAt.Add(models.ExpectSelfUpdateBound)
	if rec.DeadlineAt.Before(hard) {
		hard = *rec.DeadlineAt
	}
	if !at.Before(hard) {
		return // Q-1: a registration after the bound belongs to no window
	}
	from, to, ok := models.SelfUpdateVersions(*rec)
	if !ok {
		return
	}
	tr := models.CommandTransition{
		ID: rec.ID, DeviceID: deviceID, Actor: models.ActorSelfUpdate,
		From: []models.CommandState{models.CommandRunning}, Now: at,
	}
	if models.AgentVersionsMatch(reportedVersion, to) {
		tr.To = models.CommandVerifying
		tr.Step = strPtr(models.SelfUpdateStepWaitingForConfirmation)
	} else {
		tr.To = models.CommandFailed
		tr.FailureStep = strPtr(models.SelfUpdateFailureWrongVersion)
		tr.ResultMsg = resultMsg(models.SelfUpdateWrongVersionMessage(hostname, reportedVersion, from, to))
	}
	row, applied, err := s.repo.Transition(ctx, tr)
	if err != nil {
		s.warnThrottled(deviceID, "agent self-update record could not be updated at registration; it ends at its bound", "error", err.Error())
		return
	}
	if applied {
		s.broadcast(row)
	}
}

// ResolveSelfUpdate ends a record at its bound (AD-006 step 2). It is the
// expected-activity tracker's SelfUpdateResolver. applied is false, with a nil
// row, when the record is gone, foreign or already terminal; the caller then
// re-reads.
func (s *Service) ResolveSelfUpdate(ctx context.Context, commandID, deviceID string, outcome models.SelfUpdateOutcome, message string, at time.Time) (*models.Command, bool, error) {
	to, ok := outcome.State()
	if !ok {
		return nil, false, ErrInvalidReport
	}
	tr := models.CommandTransition{
		ID: commandID, DeviceID: deviceID, Actor: models.ActorSelfUpdate, To: to,
		From:      []models.CommandState{models.CommandRunning, models.CommandVerifying},
		ResultMsg: resultMsg(message), Now: at,
	}
	if outcome == models.SelfUpdateConfirmed {
		tr.Step = strPtr(models.SelfUpdateStepConfirmed)
	} else {
		tr.FailureStep = strPtr(outcome.FailureStep())
	}
	row, applied, err := s.repo.Transition(ctx, tr)
	if err != nil || !applied {
		return nil, false, err
	}
	s.broadcast(row)
	return row, true, nil
}
