// Package commands owns every server-side command write (CMD-LIFECYCLE-001,
// AD-002): creation with the target conflict check, TTL and deadline stamping,
// the atomic delivery claim, guarded state transitions, agent report ingestion,
// cancellation and the sweeper. Every status change is one
// db.CommandRepository.Transition, so the database enforces the state machine
// in internal/models.
package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/google/uuid"
)

// PolicyRecorder records the outcome of a command a policy originated
// (db.AutoUpdateRepository satisfies it).
type PolicyRecorder interface {
	RecordSuccess(ctx context.Context, id int, at time.Time) error
	RecordFailure(ctx context.Context, id int, at time.Time) error
}

// TelemetryReader gives target resolution the device's latest container list.
type TelemetryReader interface {
	GetLatestSnapshot(ctx context.Context, deviceID string) (*models.TelemetrySnapshot, error)
}

// AgentSender delivers a command over the device's agent WebSocket.
//
// SendCommand returns sent=false with a nil error when the device is not
// connected, and an error when a write was attempted and failed (the bytes may
// still have reached the agent: the send is then ambiguous, SEC-016).
type AgentSender interface {
	IsConnected(deviceID string) bool
	SendCommand(deviceID string, payload models.CommandPayload) (sent bool, err error)
}

// Broadcaster pushes accepted transitions to dashboard clients.
type Broadcaster interface {
	BroadcastCommandUpdate(u models.CommandUpdate)
}

// CompletionEvents raises the "command completed" event. status is the legacy
// vocabulary the event generator expects ("success" or "error").
type CompletionEvents interface {
	CommandCompleted(ctx context.Context, deviceID, action, status, message string)
}

// Options configures a Service. Commands is required; every other collaborator
// is optional and nil-checked. Never assign a nil *websocket.Hub or
// *events.Generator to an interface field (typed-nil trap): leave it unset.
type Options struct {
	Commands    db.CommandRepository
	Policies    PolicyRecorder
	Telemetry   TelemetryReader
	Agents      AgentSender
	Broadcaster Broadcaster
	Events      CompletionEvents
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Logger receives the service's records; nil means slog.Default().
	Logger *slog.Logger
}

// Service is the single writer of command state.
type Service struct {
	repo        db.CommandRepository
	policies    PolicyRecorder
	telemetry   TelemetryReader
	agents      AgentSender
	broadcaster Broadcaster
	events      CompletionEvents
	now         func() time.Time
	log         *slog.Logger

	limiter     *DeviceLimiter
	logThrottle *LogThrottle
	gate        *progressGate

	ambiguousMu     sync.Mutex
	ambiguousLogged map[string]struct{}
}

// New builds a Service. It starts no goroutine.
func New(o Options) *Service {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo: o.Commands, policies: o.Policies, telemetry: o.Telemetry, agents: o.Agents,
		broadcaster: o.Broadcaster, events: o.Events, now: now, log: logger,
		limiter:         NewDeviceLimiter(now),
		logThrottle:     NewLogThrottle(now, logger),
		gate:            newProgressGate(),
		ambiguousLogged: make(map[string]struct{}),
	}
}

// ---------------------------------------------------------------------------
// Creation and delivery
// ---------------------------------------------------------------------------

// DispatchRequest describes one command to create.
type DispatchRequest struct {
	DeviceID string
	Action   string
	Params   map[string]interface{}
	// Origin is models.OriginUser (default), OriginAutoUpdate or OriginAutoPatch.
	Origin   string
	PolicyID *int
	// WindowEnd caps expires_at for auto-dispatched commands under a window-mode
	// maintenance window (FR-025). Zero means no cap.
	WindowEnd time.Time
	// TargetKeys, when non-nil, replaces the keys resolved from Action and
	// Params (bulk dispatch).
	TargetKeys []string
}

// DispatchResult is the outcome of Dispatch. Exactly one of Command and
// Conflict is set.
type DispatchResult struct {
	// Command is the created command in its state after delivery was attempted:
	// dispatched when it went out over the agent WebSocket, queued otherwise.
	Command *models.Command
	// Conflict is the existing non-terminal command that holds one of the
	// request's target keys; no row was created.
	Conflict *models.Command
	// ConflictKey is the first of the request's target keys that the holder also
	// holds (the key the 409 reports).
	ConflictKey string
}

// Dispatch is the single creation path for all seven command-creating sites
// (AD-002): it resolves the target keys, stamps expires_at and the execution
// deadline, creates the row unless a target conflict exists, and delivers it
// through the atomic claim when the agent is connected.
func (s *Service) Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error) {
	origin := req.Origin
	if origin == "" {
		origin = models.OriginUser
	}
	if !models.IsValidOrigin(origin) {
		return DispatchResult{}, fmt.Errorf("invalid command origin %q", origin)
	}
	params := req.Params
	if params == nil {
		params = map[string]interface{}{}
	}
	keys := req.TargetKeys
	if keys == nil {
		keys = s.ResolveTargetKeys(ctx, req.DeviceID, req.Action, params)
	}

	now := s.now().UTC()
	cmd := &models.Command{
		ID:            uuid.New().String(),
		DeviceID:      req.DeviceID,
		Action:        req.Action,
		Params:        params,
		Status:        string(models.CommandQueued),
		CreatedAt:     now,
		UpdatedAt:     now,
		ExpiresAt:     models.ExpiresAt(req.Action, now, req.WindowEnd),
		ExecDeadlineS: models.ExecDeadlineSeconds(req.Action, len(containerIDParams(params))),
		Origin:        origin,
		PolicyID:      req.PolicyID,
		TargetKeys:    models.NormalizeTargetKeys(keys...),
	}
	conflict, err := s.repo.CreateIfNoConflict(ctx, cmd)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("create command: %w", err)
	}
	if conflict != nil {
		s.log.Info("command conflict", "device_id", req.DeviceID, "action", req.Action,
			"existing_command_id", conflict.ID, "existing_status", conflict.Status)
		return DispatchResult{Conflict: conflict, ConflictKey: overlappingKey(cmd.TargetKeys, conflict.TargetKeys)}, nil
	}
	cmd.Normalize()
	s.broadcast(cmd)
	return DispatchResult{Command: s.deliver(ctx, cmd)}, nil
}

// deliver sends a freshly created command over the WebSocket when the agent is
// connected. The claim comes first, so a cancelled or expired command is never
// sent (NFR-005); a failed send reverts the claim (FR-085). It returns the
// command in its final state.
func (s *Service) deliver(ctx context.Context, cmd *models.Command) *models.Command {
	if s.agents == nil || !s.agents.IsConnected(cmd.DeviceID) {
		return cmd
	}
	claimed, ok, err := s.repo.ClaimByID(ctx, cmd.ID, s.now().UTC())
	if err != nil {
		s.log.Error("claim command for ws delivery", "command_id", cmd.ID, "device_id", cmd.DeviceID, "error", err.Error())
		return cmd
	}
	if !ok {
		// Cancelled, expired or claimed by a heartbeat in the meantime.
		return s.current(ctx, cmd)
	}
	s.broadcast(claimed)

	sent, sendErr := s.agents.SendCommand(cmd.DeviceID, models.CommandPayload{
		CommandID: claimed.ID, Action: claimed.Action, Params: claimed.Params,
	})
	if sent && sendErr == nil {
		return claimed
	}
	msg := "agent not connected; re-queued for heartbeat delivery"
	if sendErr != nil {
		msg = "ws send failed; delivery uncertain; re-queued for heartbeat delivery"
		s.log.Warn("send command via ws failed, re-queued for heartbeat delivery",
			"command_id", claimed.ID, "device_id", claimed.DeviceID, "error", sendErr.Error())
	}
	return s.revertDispatch(ctx, claimed, msg)
}

// revertDispatch undoes a claim after a failed send (FR-085). The guard only
// matches a still-dispatched row, so a revert after the agent already reported
// is rejected and the row keeps the agent's state.
func (s *Service) revertDispatch(ctx context.Context, claimed *models.Command, msg string) *models.Command {
	reverted, applied, err := s.repo.Transition(ctx, models.CommandTransition{
		ID: claimed.ID, Actor: models.ActorServer, To: models.CommandQueued,
		From: []models.CommandState{models.CommandDispatched}, ResultMsg: &msg, Now: s.now().UTC(),
	})
	if err != nil {
		s.log.Error("revert command dispatch", "command_id", claimed.ID, "device_id", claimed.DeviceID, "error", err.Error())
		return claimed
	}
	if !applied {
		s.warnThrottled(claimed.DeviceID, "command transition rejected",
			"command_id", claimed.ID, "from", string(models.CommandDispatched), "to", string(models.CommandQueued))
		return s.current(ctx, claimed)
	}
	s.broadcast(reverted)
	return reverted
}

// current re-reads a command, falling back to fallback when the read fails.
func (s *Service) current(ctx context.Context, fallback *models.Command) *models.Command {
	row, err := s.repo.GetByID(ctx, fallback.ID)
	if err != nil {
		s.log.Error("read command", "command_id", fallback.ID, "error", err.Error())
		return fallback
	}
	return row
}

// ClaimForHeartbeat is the heartbeat delivery path (FR-062, FR-026): it expires
// the device's overdue queued commands, then atomically claims up to
// models.HeartbeatClaimLimit queued commands. Only commands claimed by this call
// are returned. The claim predicate itself excludes expired commands, so a
// failure of the expire step cannot deliver one (NFR-005).
func (s *Service) ClaimForHeartbeat(ctx context.Context, deviceID string) ([]models.CommandPayload, error) {
	var swept SweepResult
	s.expireQueued(ctx, deviceID, &swept)

	claimed, err := s.repo.ClaimQueued(ctx, deviceID, s.now().UTC(), models.HeartbeatClaimLimit)
	if err != nil {
		return nil, fmt.Errorf("claim queued commands: %w", err)
	}
	payloads := make([]models.CommandPayload, 0, len(claimed))
	for i := range claimed {
		s.broadcast(&claimed[i])
		payloads = append(payloads, models.CommandPayload{
			CommandID: claimed[i].ID, Action: claimed[i].Action, Params: claimed[i].Params,
		})
	}
	return payloads, nil
}

// BulkResult is the outcome of DispatchBulkDocker.
type BulkResult struct {
	// Command is the created docker_bulk_update, or nil when every target
	// conflicted.
	Command *models.Command
	// Conflicts lists the skipped targets with the command that holds each.
	Conflicts []models.BulkConflict
}

// DispatchBulkDocker creates one docker_bulk_update for the targets that do not
// conflict with an in-flight command and reports the others as conflicts
// (FR-060). A conflict that appears between the pre-check and the insert is
// reported for every target.
func (s *Service) DispatchBulkDocker(ctx context.Context, deviceID string, containerIDs []string) (BulkResult, error) {
	containers := s.containers(ctx, deviceID)

	held := map[string]string{} // target key -> id of the oldest active holder
	active, err := s.repo.ListActive(ctx, deviceID, "")
	if err != nil {
		return BulkResult{}, fmt.Errorf("list active commands: %w", err)
	}
	for _, c := range active {
		for _, k := range c.TargetKeys {
			if _, ok := held[k]; !ok {
				held[k] = c.ID
			}
		}
	}

	seen := map[string]bool{}
	var kept []string
	var keptKeys []string
	conflicts := []models.BulkConflict{}
	for _, id := range containerIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		key := containerTargetKey(containers, id)
		if holder, busy := held[key]; busy {
			conflicts = append(conflicts, models.BulkConflict{ContainerID: id, TargetKey: key, ExistingCommandID: holder})
			continue
		}
		kept = append(kept, id)
		keptKeys = append(keptKeys, key)
	}
	if len(kept) == 0 {
		return BulkResult{Conflicts: conflicts}, nil
	}

	res, err := s.Dispatch(ctx, DispatchRequest{
		DeviceID:   deviceID,
		Action:     models.ActionDockerBulkUpdate,
		Params:     map[string]interface{}{"container_ids": kept},
		TargetKeys: models.NormalizeTargetKeys(keptKeys...),
	})
	if err != nil {
		return BulkResult{}, err
	}
	if res.Conflict != nil {
		for i, id := range kept {
			conflicts = append(conflicts, models.BulkConflict{ContainerID: id, TargetKey: keptKeys[i], ExistingCommandID: res.Conflict.ID})
		}
		return BulkResult{Conflicts: conflicts}, nil
	}
	return BulkResult{Command: res.Command, Conflicts: conflicts}, nil
}

// ---------------------------------------------------------------------------
// Cancellation
// ---------------------------------------------------------------------------

// CancelOutcome is the result of Cancel.
type CancelOutcome int

const (
	// CancelApplied: the command was queued and is now cancelled.
	CancelApplied CancelOutcome = iota + 1
	// CancelNotFound: no such command on this device.
	CancelNotFound
	// CancelNotCancellable: the command has been dispatched (or is terminal).
	CancelNotCancellable
)

// Cancel moves a queued command to cancelled (FR-086). It is mutually exclusive
// with the delivery claim: exactly one of the two wins (FR-088). The returned
// command is the cancelled row, or the current row for CancelNotCancellable.
func (s *Service) Cancel(ctx context.Context, deviceID, commandID string) (*models.Command, CancelOutcome, error) {
	if !models.IsCanonicalUUID(commandID) {
		return nil, CancelNotFound, nil
	}
	row, err := s.repo.GetForDevice(ctx, commandID, deviceID)
	if errors.Is(err, db.ErrCommandNotFound) {
		return nil, CancelNotFound, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read command: %w", err)
	}

	msg := "cancelled by operator"
	if row.SendAmbiguous {
		msg = "cancelled by operator; an earlier send attempt may have reached the agent"
	}
	now := s.now().UTC()
	cancelled, applied, err := s.repo.Transition(ctx, models.CommandTransition{
		ID: commandID, DeviceID: deviceID, Actor: models.ActorOperator, To: models.CommandCancelled,
		ResultMsg: &msg, Now: now,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("cancel command: %w", err)
	}
	if applied {
		s.afterTransition(ctx, cancelled, models.CommandCancelled, now)
		s.broadcast(cancelled)
		return cancelled, CancelApplied, nil
	}

	// Lost the race with a claim, or the command is not queued.
	row, err = s.repo.GetForDevice(ctx, commandID, deviceID)
	if errors.Is(err, db.ErrCommandNotFound) {
		return nil, CancelNotFound, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read command: %w", err)
	}
	s.log.Info("command cancel refused", "command_id", commandID, "device_id", deviceID, "status", row.Status)
	return row, CancelNotCancellable, nil
}

// ---------------------------------------------------------------------------
// Agent reports
// ---------------------------------------------------------------------------

// ReportOutcome is what HandleAgentReport did with a report (AD-008).
type ReportOutcome struct {
	Disposition models.ReportDisposition
	// Status is the command's canonical status after the call, "" when the
	// command is not found.
	Status string
}

// AllowReport charges one token of the per-device report budget, shared by the
// HTTP endpoint and the WS handler (SEC C-1). It must run before any decode or
// repository access.
func (s *Service) AllowReport(deviceID string) bool {
	if s.limiter.Allow(deviceID) {
		return true
	}
	s.warnThrottled(deviceID, "agent report rate limited")
	return false
}

// HandleAgentReport applies one agent report (ack, progress or result) on
// behalf of the authenticated device. Every write is scoped to deviceID in SQL,
// so a foreign command is indistinguishable from a missing one. A report whose
// only problem is its content is stored, not rejected (SEC-002).
func (s *Service) HandleAgentReport(ctx context.Context, deviceID string, report models.CommandReport) (ReportOutcome, error) {
	if !models.IsCanonicalUUID(report.CommandID) {
		s.warnThrottled(deviceID, "malformed command_id from agent", "length", len(report.CommandID))
		return ReportOutcome{}, ErrMalformedCommandID
	}
	report = sanitizeReport(report)
	now := s.now().UTC()
	id := report.CommandID

	t := models.CommandTransition{ID: id, DeviceID: deviceID, Actor: models.ActorAgent, Now: now}
	switch report.Kind {
	case models.ReportKindAck:
		t.To = models.CommandAcknowledged
	case models.ReportKindProgress:
		switch report.State {
		case string(models.CommandRunning):
			t.To = models.CommandRunning
		case string(models.CommandVerifying):
			t.To = models.CommandVerifying
		default:
			return ReportOutcome{}, fmt.Errorf("%w: progress state", ErrInvalidReport)
		}
		progress := validProgress(report.Progress)
		step := report.Step
		t.Step = &step
		t.ProgressSet = true
		t.Progress = progress
		if report.Message != "" {
			msg := report.Message
			t.ResultMsg = &msg
		}
		if t.To == models.CommandRunning && s.gate.coalesce(id, t.To, step, progress, now) {
			return ReportOutcome{Disposition: models.ReportCoalesced, Status: string(models.CommandRunning)}, nil
		}
	case models.ReportKindResult:
		if report.Result == nil {
			return ReportOutcome{}, fmt.Errorf("%w: result report without a result", ErrInvalidReport)
		}
		s.fillResultTransition(&t, *report.Result)
	default:
		return ReportOutcome{}, fmt.Errorf("%w: kind", ErrInvalidReport)
	}

	cmd, applied, err := s.repo.Transition(ctx, t)
	if err != nil {
		return ReportOutcome{}, fmt.Errorf("apply agent report: %w", err)
	}
	if applied {
		s.afterAgentTransition(ctx, report, cmd, now)
		return ReportOutcome{Disposition: models.ReportAccepted, Status: cmd.Status}, nil
	}
	return s.classifyRejected(ctx, deviceID, id, t.To, report.Kind)
}

// fillResultTransition maps a final result onto the transition: the outcome,
// message, stderr tail, failing step and exit data.
func (s *Service) fillResultTransition(t *models.CommandTransition, res models.CommandResult) {
	state, recognized := res.ResolveState()
	if !recognized {
		s.log.Warn("command result with unrecognized status treated as failed", "command_id", t.ID, "device_id", t.DeviceID)
	}
	t.To = state
	msg := res.Message
	t.ResultMsg = &msg
	tail := res.Stderr
	t.StderrTail = &tail
	t.ExitCode = res.ExitCode
	t.DurationMs = res.DurationMs
	if res.Step != "" {
		step := res.Step
		t.Step = &step
		if state == models.CommandFailed {
			failing := res.Step
			t.FailureStep = &failing
		}
	}
}

// validProgress returns p when it is a whole percent in 0..100, otherwise nil
// (stored as NULL; the state change still applies, AC-095).
func validProgress(p *int) *int {
	if p == nil || *p < 0 || *p > 100 {
		return nil
	}
	v := *p
	return &v
}

// afterAgentTransition runs the side effects of an accepted agent report:
// progress gate bookkeeping, output storage, policy outcome, completion event
// and the dashboard broadcast.
func (s *Service) afterAgentTransition(ctx context.Context, report models.CommandReport, cmd *models.Command, now time.Time) {
	state := cmd.State()
	if cmd.SendAmbiguous {
		s.logAmbiguousOnce(cmd, "")
	}
	switch {
	case state.IsTerminal():
		s.gate.forget(cmd.ID)
	case state == models.CommandRunning || state == models.CommandVerifying:
		s.gate.remember(cmd.ID, state, cmd.Step, cmd.Progress, now)
	}

	if report.Kind == models.ReportKindResult {
		if out := report.Result.Output; out != "" {
			err := s.repo.SaveCommandOutput(ctx, &models.CommandOutput{CommandID: cmd.ID, Stream: "combined", Content: out})
			if err != nil {
				s.log.Error("save command output", "command_id", cmd.ID, "device_id", cmd.DeviceID, "bytes", len(out), "error", err.Error())
			}
		}
	}
	if state.IsTerminal() {
		s.afterTransition(ctx, cmd, state, now)
		s.completed(ctx, cmd, state)
	}
	s.broadcast(cmd)
}

// classifyRejected explains a report whose guarded update matched no row
// (AD-008): not_found (missing or foreign: one device-scoped read cannot tell
// them apart), duplicate or already_terminal for a result on an immutable row,
// otherwise invalid_transition. The id is a validated canonical UUID, so it is
// safe to log.
func (s *Service) classifyRejected(ctx context.Context, deviceID, id string, target models.CommandState, kind models.CommandReportKind) (ReportOutcome, error) {
	row, err := s.repo.GetForDevice(ctx, id, deviceID)
	if errors.Is(err, db.ErrCommandNotFound) {
		s.warnThrottled(deviceID, "command report for unknown or foreign command", "command_id", id)
		return ReportOutcome{Disposition: models.ReportNotFound}, nil
	}
	if err != nil {
		return ReportOutcome{}, fmt.Errorf("classify rejected report: %w", err)
	}

	from := row.State()
	if row.SendAmbiguous {
		s.logAmbiguousOnce(row, string(from))
	}
	if kind == models.ReportKindResult && from.IsImmutable() {
		if from == target {
			return ReportOutcome{Disposition: models.ReportDuplicate, Status: row.Status}, nil
		}
		s.warnThrottled(deviceID, "command transition rejected", "command_id", id, "from", string(from), "to", string(target))
		return ReportOutcome{Disposition: models.ReportAlreadyTerminal, Status: row.Status}, nil
	}
	s.warnThrottled(deviceID, "command transition rejected", "command_id", id, "from", string(from), "to", string(target))
	return ReportOutcome{Disposition: models.ReportInvalidTransition, Status: row.Status}, nil
}

// logAmbiguousOnce records, once per command, that an agent reported on a
// command whose WS send was reverted as ambiguous (SEC-016): the history may
// show "cancelled" or "expired" for a command that actually ran. The record is
// exempt from the rejection throttle because only the server can set the flag;
// the once-per-command bound keeps a report flood from filling server_logs.
func (s *Service) logAmbiguousOnce(cmd *models.Command, from string) {
	s.ambiguousMu.Lock()
	if _, dup := s.ambiguousLogged[cmd.ID]; dup {
		s.ambiguousMu.Unlock()
		return
	}
	if len(s.ambiguousLogged) >= maxAmbiguousLogged {
		s.ambiguousLogged = make(map[string]struct{})
	}
	s.ambiguousLogged[cmd.ID] = struct{}{}
	s.ambiguousMu.Unlock()
	s.log.Warn("agent reported on command after ambiguous send",
		"command_id", cmd.ID, "device_id", cmd.DeviceID, "from", from, "to", cmd.Status)
}

// maxAmbiguousLogged bounds the once-per-command dedupe set.
const maxAmbiguousLogged = 1024

// ---------------------------------------------------------------------------
// Shared side effects
// ---------------------------------------------------------------------------

// afterTransition records the policy outcome for a command that reached a
// terminal state (AD-007) and forgets its progress gate entry.
func (s *Service) afterTransition(ctx context.Context, cmd *models.Command, state models.CommandState, now time.Time) {
	if !state.IsTerminal() {
		return
	}
	s.gate.forget(cmd.ID)
	if s.policies == nil || cmd.PolicyID == nil {
		return
	}
	at := now
	if cmd.FinishedAt != nil {
		at = *cmd.FinishedAt
	}
	var err error
	switch state {
	case models.CommandSucceeded:
		err = s.policies.RecordSuccess(ctx, *cmd.PolicyID, at)
	default:
		err = s.policies.RecordFailure(ctx, *cmd.PolicyID, at)
	}
	if err != nil {
		s.log.Error("record auto-update outcome", "command_id", cmd.ID, "device_id", cmd.DeviceID,
			"policy_id", *cmd.PolicyID, "to", string(state), "error", err.Error())
	}
}

// completed raises the "command completed" event for a final state.
func (s *Service) completed(ctx context.Context, cmd *models.Command, state models.CommandState) {
	if s.events == nil {
		return
	}
	s.events.CommandCompleted(ctx, cmd.DeviceID, cmd.Action, models.LegacyStatus(state), cmd.ResultMsg)
}

// broadcast pushes one accepted transition to the dashboards (FR-079). Rejected
// transitions never reach it.
func (s *Service) broadcast(cmd *models.Command) {
	if s.broadcaster == nil || cmd == nil {
		return
	}
	s.broadcaster.BroadcastCommandUpdate(models.NewCommandUpdate(*cmd))
}

// warnThrottled writes a Warn through the per-device rejection throttle. Only
// validated fields may be passed: never agent-supplied content (SEC C-1).
func (s *Service) warnThrottled(deviceID, msg string, attrs ...any) {
	if !s.logThrottle.Allow(deviceID) {
		return
	}
	s.log.Warn(msg, append([]any{"device_id", deviceID}, attrs...)...)
}

// ---------------------------------------------------------------------------
// Progress gate (AD-020)
// ---------------------------------------------------------------------------

const (
	// progressGateIdle is how long an idle gate entry is kept.
	progressGateIdle = time.Minute
	// progressGatePruneEvery bounds how often idle entries are scanned.
	progressGatePruneEvery = 64
)

type progressEntry struct {
	state    models.CommandState
	step     string
	progress *int
	last     time.Time
}

// progressGate remembers, per command, the last accepted running/verifying
// write so that identical or too-frequent running -> running reports are
// coalesced before they cost a row rewrite and a broadcast (SEC C-1). Entries
// exist only for commands whose report was accepted, so an unknown or foreign
// id can never allocate one.
type progressGate struct {
	mu      sync.Mutex
	entries map[string]*progressEntry
	writes  int
}

func newProgressGate() *progressGate {
	return &progressGate{entries: make(map[string]*progressEntry)}
}

// coalesce reports whether a running report for a command already in running
// must be dropped: it changes neither step nor progress, or the previous
// accepted write is less than models.ProgressMinInterval old.
func (g *progressGate) coalesce(id string, to models.CommandState, step string, progress *int, now time.Time) bool {
	if to != models.CommandRunning {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	e, ok := g.entries[id]
	if !ok || e.state != models.CommandRunning {
		return false
	}
	if e.step == step && intPtrEqual(e.progress, progress) {
		return true
	}
	return now.Sub(e.last) < models.ProgressMinInterval
}

// remember records an accepted running/verifying write.
func (g *progressGate) remember(id string, state models.CommandState, step string, progress *int, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writes++
	if g.writes%progressGatePruneEvery == 0 {
		for k, e := range g.entries {
			if now.Sub(e.last) > progressGateIdle {
				delete(g.entries, k)
			}
		}
	}
	g.entries[id] = &progressEntry{state: state, step: step, progress: validProgress(progress), last: now}
}

// forget drops a command's entry (terminal transition).
func (g *progressGate) forget(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.entries, id)
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// overlappingKey returns the first key of want that also appears in held, or
// the holder's first key when none matches.
func overlappingKey(want, held []string) string {
	for _, w := range want {
		for _, h := range held {
			if w == h {
				return w
			}
		}
	}
	if len(held) > 0 {
		return held[0]
	}
	return ""
}
