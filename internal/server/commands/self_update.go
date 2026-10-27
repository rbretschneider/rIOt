package commands

import (
	"context"
	"errors"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
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
//
// CommandRef, AttemptRef, FromVersion and ToVersion are the raw, untrusted
// EXPECT-007 fields of the agent's payload (AD-004). The Service validates every
// one of them before use; the handler passes them only when the device that
// authenticated the request is the device the request names (FR-020).
type AgentUpdateEventInput struct {
	DeviceID, Hostname string
	Type               models.EventType
	Message            string
	CommandRef         string
	AttemptRef         string
	FromVersion        string
	ToVersion          string
	At                 time.Time
}

// AgentUpdateEventLink is what the handler stores with the agent's event: the
// command or record it links to ("" when none) and the server-written events
// to broadcast (at most the one rate-cap warning).
//
// Dedupe is true only when CommandID is non-empty and the link came from a
// validated reference the agent supplied: a valid command reference, a record
// that owns the attempt ID, or the record this start just created for it. Only
// then may the handler drop a repeat (AD-009, security condition C-3 b). A link
// found by the timing rules is never de-duplicated.
type AgentUpdateEventLink struct {
	CommandID, CommandAttr string
	Events                 []models.Event
	Dedupe                 bool
}

// resultMsg bounds a server-rendered result_msg (AD-003).
func resultMsg(s string) *string {
	m := models.TruncateHead(models.SanitizeText(s), models.SelfUpdateResultMsgBytes)
	return &m
}

func strPtr(s string) *string { return &s }

// NoteAgentUpdateEvent recognises and attributes one agent_update_* event
// (AD-002, AD-004, EXPECT-007 AD-006). It never returns an error: every failure
// yields an empty link and one throttled Warn, so the event is stored as today
// (NFR-009).
//
// Evidence beats inference (BR-001). A valid command reference links the event
// to that command and does nothing else. An attempt reference is matched to the
// record that owns it. Anything missing, invalid or unreadable is processed
// exactly as EXPECT-002 processes an event without a reference (BR-002,
// NFR-008).
func (s *Service) NoteAgentUpdateEvent(ctx context.Context, in AgentUpdateEventInput) AgentUpdateEventLink {
	if s.selfUpdates == nil || in.DeviceID == "" {
		return AgentUpdateEventLink{}
	}
	switch in.Type {
	case models.EventAgentUpdateStarted, models.EventAgentUpdateCompleted, models.EventAgentUpdateFailed:
	default:
		return AgentUpdateEventLink{}
	}
	// "Both present" is decided before either value is validated (FR-027).
	ref := models.ParseAgentUpdateRef(in.CommandRef, in.AttemptRef)
	if ref.Kind == models.RefCommand {
		if link, ok := s.linkCommandRef(ctx, in, ref.Value); ok {
			return link
		}
		ref = models.AgentUpdateRef{}
	}
	if in.Type == models.EventAgentUpdateStarted {
		return s.noteStart(ctx, in, ref)
	}
	return s.noteOutcomeEvent(ctx, in, ref)
}

// linkCommandRef checks a command reference (AD-006 step 2): the device's own
// agent_update command of an operator or schedule origin, dispatched, and not
// terminal or finished at most 10 minutes earlier. The read is device-scoped in
// SQL, so another device's command reads as not found, exactly like an unknown
// one (no oracle). ok is false for every invalid form and for a read error; the
// caller then processes the event as if it carried no reference.
//
// A valid reference only links. It creates no record, counts toward no cap and
// changes no command, record, window or outcome, whatever the event type.
func (s *Service) linkCommandRef(ctx context.Context, in AgentUpdateEventInput, id string) (AgentUpdateEventLink, bool) {
	c, err := s.repo.GetForDevice(ctx, id, in.DeviceID)
	if err != nil {
		if !errors.Is(err, db.ErrCommandNotFound) {
			s.log.Debug("agent update command reference could not be read", "device_id", in.DeviceID)
		}
		return AgentUpdateEventLink{}, false
	}
	if !models.ValidAgentUpdateCommandRef(c, in.At) {
		s.log.Debug("agent update command reference not valid", "device_id", in.DeviceID)
		return AgentUpdateEventLink{}, false
	}
	return AgentUpdateEventLink{CommandID: c.ID, CommandAttr: models.CommandAttrAgent, Dedupe: true}, true
}

// startVersions picks the versions of a start (AD-007): valid, different
// structured versions win; otherwise the message is parsed as in EXPECT-002.
// When both are valid and disagree, the structured versions are used and one
// Debug line records it. The agent's message is stored unchanged either way.
func (s *Service) startVersions(in AgentUpdateEventInput) (from, to string, ok bool) {
	sFrom, sTo, structured := models.StructuredSelfUpdateVersions(in.FromVersion, in.ToVersion)
	mFrom, mTo, parsed := models.ParseSelfUpdateStart(in.Message)
	switch {
	case structured:
		if parsed && (!models.AgentVersionsMatch(sFrom, mFrom) || !models.AgentVersionsMatch(sTo, mTo)) {
			s.log.Debug("agent update start versions differ from the message", "device_id", in.DeviceID)
		}
		return sFrom, sTo, true
	case parsed:
		return mFrom, mTo, true
	}
	return "", "", false
}

func (s *Service) noteStart(ctx context.Context, in AgentUpdateEventInput, ref models.AgentUpdateRef) AgentUpdateEventLink {
	from, to, ok := s.startVersions(in)
	if !ok {
		s.log.Debug("agent update start message not recognised", "device_id", in.DeviceID, "bytes", len(in.Message))
		return AgentUpdateEventLink{}
	}
	attempt := ""
	if ref.Kind == models.RefAttempt {
		attempt = ref.Value
	}
	res, err := s.selfUpdates.CreateSelfUpdate(ctx, models.SelfUpdateStart{
		DeviceID: in.DeviceID, Hostname: in.Hostname, From: from, To: to, AttemptID: attempt, At: in.At,
	})
	if err != nil {
		s.warnThrottled(in.DeviceID, "agent self-update start could not be recorded; offline alerts are not suppressed", "error", err.Error())
		return AgentUpdateEventLink{}
	}
	// A retired stale record, then the new one: each fires the tracker hook and
	// the dashboard command_update (AD-002 step 3). An attempt_match result
	// carries neither, so a replay announces nothing (security condition C-3 a).
	if res.Disposition != models.SelfUpdateAttemptMatch {
		s.broadcast(res.Retired)
		s.broadcast(res.Command)
	}
	s.log.Debug("agent self-update start", "device_id", in.DeviceID, "disposition", string(res.Disposition))

	link := AgentUpdateEventLink{}
	if res.LinkID != "" {
		link.CommandID, link.CommandAttr = res.LinkID, models.CommandAttrInferred
		// The record that owns the attempt ID (found, or created just now for it)
		// is the exact target of this reference.
		if attempt != "" && (res.Disposition == models.SelfUpdateAttemptMatch || res.Disposition == models.SelfUpdateCreated) {
			link.CommandAttr, link.Dedupe = models.CommandAttrAgent, true
		}
	}
	if res.CapEvent != nil {
		link.Events = append(link.Events, *res.CapEvent)
	}
	return link
}

// noteOutcomeEvent attributes a completed or failed event and moves an open
// record to its outcome (AD-004, EXPECT-007 AD-006 step 4). These events never
// create a record and are never linked to commands (X-16).
//
// With an attempt reference the record that owns the ID is used, whatever its
// age (FR-025). An attempt ID no record owns falls back to the timing rule, but
// only to a record without an attempt ID, so one attempt's completion can never
// close another attempt's record (FR-026). Without a usable reference the
// device's most recent record started at most 10 minutes earlier is used, as in
// EXPECT-002.
func (s *Service) noteOutcomeEvent(ctx context.Context, in AgentUpdateEventInput, ref models.AgentUpdateRef) AgentUpdateEventLink {
	var rec *models.Command
	attr, dedupe := models.CommandAttrInferred, false

	if ref.Kind == models.RefAttempt {
		owner, err := s.selfUpdates.SelfUpdateByAttempt(ctx, in.DeviceID, ref.Value)
		switch {
		case err != nil:
			// NFR-008: unreadable means no reference.
			s.warnThrottled(in.DeviceID, "agent self-update record could not be read by attempt; falling back to timing", "error", err.Error())
			ref = models.AgentUpdateRef{}
		case owner != nil:
			rec, attr, dedupe = owner, models.CommandAttrAgent, true
		}
	}
	if rec == nil {
		latest, err := s.selfUpdates.LatestSelfUpdateSince(ctx, in.DeviceID, in.At.Add(-models.SelfUpdateAttributionWindow))
		if err != nil {
			s.warnThrottled(in.DeviceID, "agent self-update record could not be read; the event is stored unlinked", "error", err.Error())
			return AgentUpdateEventLink{}
		}
		if latest == nil {
			return AgentUpdateEventLink{}
		}
		if ref.Kind == models.RefAttempt && models.SelfUpdateAttemptID(*latest) != "" {
			return AgentUpdateEventLink{} // FR-026: that record belongs to another attempt
		}
		rec = latest
	}

	link := AgentUpdateEventLink{CommandID: rec.ID, CommandAttr: attr, Dedupe: dedupe && rec.ID != ""}
	if !rec.State().IsActive() {
		return link // FR-022: the outcome is set; link only
	}
	s.applyRecordOutcome(ctx, in, rec)
	return link
}

// applyRecordOutcome moves an open record to the outcome the agent reported. It
// is the EXPECT-002 transition code, unchanged (implementation note 7).
func (s *Service) applyRecordOutcome(ctx context.Context, in AgentUpdateEventInput, rec *models.Command) {
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
		return
	}
	if applied {
		s.broadcast(row)
	}
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
