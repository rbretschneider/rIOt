package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// restartedMessage is the failure text of a command that was executing when the
// agent stopped (FR-019).
const restartedMessage = "agent restarted during execution; outcome unknown"

// cmdTransport is how the command lifecycle reaches the server: the outbox
// transport plus the fire-and-forget lifecycle events. The production
// implementation is agentTransport; tests inject a fake.
type cmdTransport interface {
	outboxTransport
	SendDockerEvent(ctx context.Context, evt *models.DockerEvent) error
	ReportEvent(ctx context.Context, evt *models.AgentEvent) error
}

// errNoServerClient is returned by the production transport before the HTTP
// client exists.
var errNoServerClient = fmt.Errorf("agent: server client not configured")

// agentTransport is the production cmdTransport over the agent's WebSocket and
// HTTP clients.
type agentTransport struct{ a *Agent }

func (t agentTransport) WSConnected() bool {
	return t.a.wsClient != nil && t.a.wsClient.connected()
}

func (t agentTransport) SendWS(msg AgentWSMessage) error { return t.a.sendWS(msg) }

func (t agentTransport) PostReport(ctx context.Context, commandID string, rep models.CommandReport) (*ReportReply, error) {
	if t.a.client == nil {
		return nil, errNoServerClient
	}
	return t.a.client.PostCommandReport(ctx, t.a.config.Agent.DeviceID, commandID, rep)
}

func (t agentTransport) SendDockerEvent(ctx context.Context, evt *models.DockerEvent) error {
	deviceID := t.a.config.Agent.DeviceID
	if t.a.client == nil || deviceID == "" {
		return errNoServerClient
	}
	return t.a.client.SendDockerEvent(ctx, deviceID, evt)
}

func (t agentTransport) ReportEvent(ctx context.Context, evt *models.AgentEvent) error {
	deviceID := t.a.config.Agent.DeviceID
	if t.a.client == nil || deviceID == "" {
		return errNoServerClient
	}
	return t.a.client.ReportEvent(ctx, deviceID, evt)
}

// commandSystemDeps are the injectable parts of the command system.
type commandSystemDeps struct {
	clk          clock
	fs           stateFS
	tr           cmdTransport
	outboxPath   string
	seenPath     string
	inflightPath string
}

// defaultCommandDeps is the composition-root wiring used by Run.
func (a *Agent) defaultCommandDeps() commandSystemDeps {
	return commandSystemDeps{
		clk:          realClock{},
		fs:           osStateFS{},
		tr:           agentTransport{a: a},
		outboxPath:   OutboxPath(),
		seenPath:     SeenCommandsPath(),
		inflightPath: InflightPath(),
	}
}

// setupCommandSystem builds the outbox, the seen and in-flight stores, the
// target locks and the notifier, and loads the persisted state. A store that
// cannot be read starts empty: the agent keeps working, and the fail-closed
// rules of C-8 apply because writes to it will fail too. ctx bounds the
// notifier goroutine, which must outlive the SIGTERM cancellation of the loops
// so that results of commands finishing inside the shutdown grace are still
// announced; Run passes the command context for that reason.
func (a *Agent) setupCommandSystem(ctx context.Context, d commandSystemDeps) {
	a.clk = d.clk
	a.tr = d.tr
	a.out = newOutbox(d.outboxPath, d.fs, d.clk, d.tr)
	a.seen = newSeenStore(d.seenPath, d.fs, d.clk)
	a.inflight = newInflightStore(d.inflightPath, d.fs)
	a.locks = newTargetLocks()
	a.notify = newNotifier(d.clk)
	if a.cmdCtx == nil {
		a.cmdCtx, a.cmdCancel = context.WithCancel(context.Background())
	}

	if err := a.out.Load(); err != nil {
		slog.Error("outbox: cannot read persisted entries, starting empty", "error", err)
	}
	if err := a.seen.Load(); err != nil {
		slog.Error("cmdstate: cannot read seen-command IDs, starting empty", "error", err)
	}
	if err := a.inflight.Load(); err != nil {
		slog.Error("cmdstate: cannot read in-flight markers, starting empty", "error", err)
	}
	go a.notify.Run(ctx)
}

// clock returns the agent's clock; Agents built without the command system
// (older tests) fall back to the wall clock.
func (a *Agent) clock() clock {
	if a.clk != nil {
		return a.clk
	}
	return realClock{}
}

// commandContext is the context commands execute under. It is cancelled only
// after the shutdown grace period, not by the SIGTERM cancel of the loops.
func (a *Agent) commandContext() context.Context {
	if a.cmdCtx != nil {
		return a.cmdCtx
	}
	return context.Background()
}

// ---------------------------------------------------------------------------
// Intake
// ---------------------------------------------------------------------------

// handleCommandMessage decodes a WS `command` message and submits it.
func (a *Agent) handleCommandMessage(msg AgentWSMessage, via string) {
	var payload models.CommandPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		slog.Warn("command: invalid payload", "via", via, "error", err)
		return
	}
	a.submitCommand(payload, via)
}

// submitCommand is the only entry point for commands, from both the WebSocket
// and the heartbeat. It validates, deduplicates, persists a marker, acknowledges
// and then starts execution in its own goroutine (AD-011). Everything before the
// goroutine is synchronous so the ack is on the wire before the first step.
func (a *Agent) submitCommand(payload models.CommandPayload, via string) {
	// C-7: a server-supplied ID that is not a canonical UUID is never executed,
	// never stored and never reported. Only its length is logged.
	if !models.IsCanonicalUUID(payload.CommandID) {
		slog.Warn("invalid command_id from server", "length", len(payload.CommandID), "via", via, "action", payload.Action)
		return
	}
	id := payload.CommandID

	// C-4(d): after SIGTERM nothing new starts. The refusal is delivered after
	// the restart, so the server learns the command did not run.
	if a.shuttingDown.Load() {
		if dup, err := a.seen.CheckAndAdd(id); dup {
			return
		} else if err != nil {
			slog.Error("cmdstate: cannot persist seen ID while shutting down", "command_id", id, "error", err)
		}
		slog.Info("command: refused, agent shutting down", "id", id, "action", payload.Action)
		a.failBeforeStart(id, payload.Action, models.ReasonShutdown, "agent shutting down")
		return
	}

	// FR-057 / C-8: durable dedupe before anything runs.
	dup, err := a.seen.CheckAndAdd(id)
	if dup {
		slog.Debug("command: duplicate delivery ignored", "id", id, "action", payload.Action, "via", via)
		return
	}
	if err != nil {
		if models.IsDedupeCriticalAction(payload.Action) {
			slog.Error("command: seen-ID store unavailable, refusing to run", "id", id, "action", payload.Action, "error", err)
			a.failBeforeStart(id, payload.Action, models.ReasonPersistence,
				fmt.Sprintf("command dedupe store unavailable; refusing to run %s", payload.Action))
			return
		}
		slog.Error("command: seen-ID store unavailable, running without a durable dedupe record",
			"id", id, "action", payload.Action, "error", err)
	}

	slog.Info("command: received", "id", id, "action", payload.Action, "via", via)

	// FR-019 / C-4: the marker is durable before the command starts.
	marker := inflightMarker{Action: payload.Action, Target: markerTarget(payload), StartedAt: a.clock().Now()}
	if err := a.inflight.Put(id, marker); err != nil {
		if isDockerMutation(payload.Action) {
			slog.Error("command: in-flight marker unavailable, refusing to run", "id", id, "action", payload.Action, "error", err)
			if rerr := a.inflight.Remove(id); rerr != nil {
				slog.Warn("command: could not discard unwritable marker", "id", id, "error", rerr)
			}
			a.failBeforeStart(id, payload.Action, models.ReasonPersistence,
				fmt.Sprintf("command marker store unavailable; refusing to run %s", payload.Action))
			return
		}
		slog.Error("command: in-flight marker not durable, continuing", "id", id, "action", payload.Action, "error", err)
	}

	a.sendAck(id)

	cr := a.newCmdRun(payload)
	a.cmdWG.Add(1)
	go a.executeCommand(cr)
}

// failBeforeStart queues a failed result for a command that was refused at
// intake. The step is always precheck.
func (a *Agent) failBeforeStart(id, action, reason, message string) {
	res := models.CommandResult{
		CommandID: id,
		Status:    models.ResultStatusError,
		Outcome:   models.OutcomeFailed,
		Message:   message,
		Step:      models.StepPrecheck,
		Reason:    reason,
	}
	if err := a.out.Add(res); err != nil {
		slog.Error("command: refusal result not persisted", "id", id, "action", action, "error", err)
	}
}

func isDockerMutation(action string) bool {
	return action == models.ActionDockerUpdate || action == models.ActionDockerBulkUpdate
}

// markerTarget is the human-readable target recorded in the in-flight marker.
func markerTarget(p models.CommandPayload) string {
	for _, k := range []string{"container", "container_id", "compose_work_dir"} {
		if v, _ := p.Params[k].(string); v != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Acknowledgement and progress
// ---------------------------------------------------------------------------

// sendAck acknowledges a command within AckBudget, over the WebSocket or HTTP.
// It is skipped against a legacy server, which ignores it (FR-075, AC-092).
func (a *Agent) sendAck(id string) {
	if a.out.Legacy() {
		return
	}
	ctx, cancel := context.WithTimeout(a.commandContext(), models.AckBudget)
	defer cancel()

	ack := models.CommandAck{CommandID: id, At: a.clock().Now()}
	data, err := json.Marshal(ack)
	if err != nil {
		slog.Error("command: cannot encode ack", "id", id, "error", err)
		return
	}
	if a.tr.WSConnected() {
		if err := a.tr.SendWS(AgentWSMessage{Type: "command_ack", Data: data}); err == nil {
			return
		}
	}
	rep, err := a.tr.PostReport(ctx, id, ack.Report())
	if err != nil {
		slog.Debug("command: ack not delivered", "id", id, "error", err)
		return
	}
	a.out.ObserveHTTPStatus(rep)
}

// sendProgress delivers one progress report, best effort (D-016): over the
// WebSocket when it is up, over HTTP otherwise, never against a legacy server.
func (a *Agent) sendProgress(ctx context.Context, p models.CommandProgress) {
	if a.out.Legacy() {
		return
	}
	data, err := json.Marshal(p)
	if err != nil {
		slog.Error("command: cannot encode progress", "id", p.CommandID, "error", err)
		return
	}
	if a.tr.WSConnected() {
		if err := a.tr.SendWS(AgentWSMessage{Type: "command_progress", Data: data}); err == nil {
			return
		}
	}
	rep, err := a.tr.PostReport(ctx, p.CommandID, p.Report())
	if err != nil {
		slog.Debug("command: progress not delivered", "id", p.CommandID, "error", err)
		return
	}
	a.out.ObserveHTTPStatus(rep)
}

// notifyProgress queues a progress report on the ordered notifier.
func (a *Agent) notifyProgress(id string, state models.CommandState, step string, percent *int) {
	if a.notify == nil {
		return
	}
	at := a.clock().Now()
	a.notify.Progress(id, string(state), step, percent, func(ctx context.Context) {
		a.sendProgress(ctx, models.CommandProgress{
			CommandID: id, State: string(state), Step: step, Progress: percent, At: at,
		})
	})
}

// notifyEvent queues a lifecycle event or any other fire-and-forget server
// notification. It runs on a fresh 10s context, never on the operation's
// (possibly expired) one (FR-046).
func (a *Agent) notifyEvent(job func(ctx context.Context)) {
	if a.notify == nil {
		return
	}
	a.notify.Event(job)
}

// ---------------------------------------------------------------------------
// Execution
// ---------------------------------------------------------------------------

// cmdRun is the execution context of one command: its identity, the class
// action context, the target locks it holds and its progress reporter.
type cmdRun struct {
	a       *Agent
	ctx     context.Context
	cancel  context.CancelFunc
	payload models.CommandPayload
	id      string
	action  string
	start   time.Time

	mu    sync.Mutex
	step  string
	state models.CommandState
	held  []string
}

func (a *Agent) newCmdRun(payload models.CommandPayload) *cmdRun {
	targets := 1
	if ids, ok := payload.Params["container_ids"].([]interface{}); ok && len(ids) > 0 {
		targets = len(ids)
	}
	ctx, cancel := context.WithTimeout(a.commandContext(), models.AgentHardMax(payload.Action, targets))
	return &cmdRun{
		a: a, ctx: ctx, cancel: cancel, payload: payload,
		id: payload.CommandID, action: payload.Action, start: a.clock().Now(),
		state: models.CommandRunning,
	}
}

// Step records and announces a new step (FR-076).
func (cr *cmdRun) Step(step string) { cr.setStep(step, models.CommandRunning) }

// Verifying announces the verification step, which the server tracks as the
// `verifying` state.
func (cr *cmdRun) Verifying(step string) { cr.setStep(step, models.CommandVerifying) }

func (cr *cmdRun) setStep(step string, state models.CommandState) {
	cr.mu.Lock()
	cr.step, cr.state = step, state
	cr.mu.Unlock()
	if err := cr.a.inflight.SetStep(cr.id, step); err != nil {
		slog.Warn("command: step marker not durable", "id", cr.id, "step", step, "error", err)
	}
	cr.a.notifyProgress(cr.id, state, step, nil)
}

// Percent announces a whole-number completion percentage on the current step.
// The notifier coalesces percent-only updates to one per second.
func (cr *cmdRun) Percent(p int) {
	cr.mu.Lock()
	step, state := cr.step, cr.state
	cr.mu.Unlock()
	cr.a.notifyProgress(cr.id, state, step, &p)
}

// Event queues a Docker lifecycle event (update_started, update_completed,
// update_failed) for the server. It is sent on a fresh context by the
// notifier, so it still goes out when the operation's own context has expired
// (FR-046).
func (cr *cmdRun) Event(target, image, action string) {
	evt := &models.DockerEvent{ContainerName: target, Action: action, Image: image}
	cr.a.notifyEvent(func(ctx context.Context) {
		if err := cr.a.tr.SendDockerEvent(ctx, evt); err != nil {
			slog.Warn("failed to send docker lifecycle event", "action", action, "container", target, "error", err)
		}
	})
}

func (cr *cmdRun) currentStep() string {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return cr.step
}

// markDocker durably updates the Docker record for key. A Docker mutation may
// run only after this returns nil (SEC C-4).
func (cr *cmdRun) markDocker(key string, fn func(*dockerRecord)) error {
	return cr.a.inflight.UpdateDocker(cr.id, key, fn)
}

// lock acquires further target keys for this command. On conflict nothing is
// acquired and the conflicting key and its holder are returned.
func (cr *cmdRun) lock(keys ...string) (conflictKey string, holder lockHolder, ok bool) {
	key, h, ok := cr.a.locks.TryLockAll(keys, cr.id, cr.action)
	if !ok {
		return key, h, false
	}
	cr.mu.Lock()
	cr.held = append(cr.held, keys...)
	cr.mu.Unlock()
	return "", lockHolder{}, true
}

// releaseLocks frees every key the command holds. It is deferred by runAction,
// so it covers failures and recovered panics (FR-056).
func (cr *cmdRun) releaseLocks() {
	cr.mu.Lock()
	keys := cr.held
	cr.held = nil
	cr.mu.Unlock()
	cr.a.locks.Unlock(keys, cr.id)
}

// conflictResult is the precheck failure for a command refused because another
// command holds the target (FR-055).
func conflictResult(key string, holder lockHolder) commandExecResult {
	return commandExecResult{
		Status:  models.ResultStatusError,
		Message: conflictMessage(key, holder),
		Step:    models.StepPrecheck,
		Reason:  models.ReasonConflict,
	}
}

// executeCommand runs the action, then hands the result to the outbox. The
// order is outbox.Add (durable), inflight.Remove, then delivery (AD-011).
func (a *Agent) executeCommand(cr *cmdRun) {
	defer a.cmdWG.Done()
	defer cr.cancel()

	res := a.runAction(cr)
	a.completeCommand(cr, res)
}

// runAction invokes the executor with panic recovery and guaranteed lock
// release.
func (a *Agent) runAction(cr *cmdRun) (res commandExecResult) {
	defer cr.releaseLocks()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("command: panic", "id", cr.id, "action", cr.action, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			res = commandExecResult{
				Status:  models.ResultStatusError,
				Message: fmt.Sprintf("internal error while executing %s: %v", cr.action, r),
				Step:    cr.currentStep(),
			}
		}
	}()
	exec := a.executor
	if exec == nil {
		exec = a.executeAction
	}
	return exec(cr)
}

// completeCommand turns an action result into the wire result and queues it.
func (a *Agent) completeCommand(cr *cmdRun, res commandExecResult) {
	durationMs := a.clock().Now().Sub(cr.start).Milliseconds()

	step := res.Step
	if step == "" {
		step = cr.currentStep()
	}
	outcome := models.OutcomeSucceeded
	if res.Status == models.ResultStatusError {
		outcome = models.OutcomeFailed
	}
	result := models.CommandResult{
		CommandID:  cr.id,
		Status:     res.Status,
		Message:    models.TruncateTail(models.SanitizeText(res.Message), models.MaxResultMsgBytes),
		ExitCode:   res.ExitCode,
		DurationMs: &durationMs,
		Output:     models.SanitizeText(TruncateOutputSmart(res.Output, maxOutputBytes)),
		Step:       models.SanitizeStep(step),
		Outcome:    outcome,
		Stderr:     models.TruncateTail(models.SanitizeText(res.Stderr), models.MaxStderrTailBytes),
		Reason:     models.SanitizeReason(res.Reason),
	}

	if res.Status == models.ResultStatusError {
		slog.Warn("command: failed", "id", cr.id, "action", cr.action, "step", result.Step, "error", res.Message)
		msg := fmt.Sprintf("Command %s failed: %s", cr.action, res.Message)
		a.notifyEvent(func(ctx context.Context) {
			a.sendAgentEvent(ctx, models.EventCommandCompleted, models.SeverityWarning, msg)
		})
	} else {
		slog.Info("command: completed", "id", cr.id, "action", cr.action, "status", res.Status)
	}

	if err := a.out.Add(result); err != nil {
		slog.Error("command: result not persisted, held in memory", "id", cr.id, "error", err)
	}
	if err := a.inflight.Remove(cr.id); err != nil {
		slog.Warn("command: could not clear in-flight marker", "id", cr.id, "error", err)
	}
	if a.notify != nil {
		a.notify.Forget(cr.id)
	}

	// Post-result reboot decision for in-window reboot-class runs (AD-008 step 7).
	if res.postResult != nil {
		res.postResult()
	}
}

// sendAgentEvent reports an agent event to the server on the given context.
func (a *Agent) sendAgentEvent(ctx context.Context, evtType models.EventType, severity models.EventSeverity, message string) {
	if err := a.tr.ReportEvent(ctx, &models.AgentEvent{Type: evtType, Severity: severity, Message: message}); err != nil {
		slog.Debug("command: event not delivered", "type", evtType, "error", err)
	}
}

// waitForCommands waits up to grace for executing commands to finish and
// reports whether they did. When the grace period runs out the commands are
// cancelled; correctness does not depend on the wait because startup recovery
// reconciles whatever was cut off (SEC C-4).
func (a *Agent) waitForCommands(grace time.Duration) bool {
	done := make(chan struct{})
	go func() {
		a.cmdWG.Wait()
		close(done)
	}()
	timer := a.clock().NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C():
		slog.Warn("shutdown: commands still running after the grace period", "grace", grace)
		if a.cmdCancel != nil {
			a.cmdCancel()
		}
		return false
	}
}

// ---------------------------------------------------------------------------
// Receipts
// ---------------------------------------------------------------------------

// handleReceipt processes a server `command_receipt`: any receipt for an ID in
// the outbox confirms delivery (AD-010).
func (a *Agent) handleReceipt(msg AgentWSMessage) {
	var receipt models.CommandReceipt
	if err := json.Unmarshal(msg.Data, &receipt); err != nil {
		slog.Warn("agent ws: invalid command_receipt", "error", err)
		return
	}
	if a.out == nil || !models.IsCanonicalUUID(receipt.CommandID) {
		return
	}
	a.out.Ack(receipt.CommandID)
}

// ---------------------------------------------------------------------------
// Startup recovery (FR-019, SEC C-4)
// ---------------------------------------------------------------------------

// recoverInterruptedCommands reports every command that was executing when the
// agent stopped as failed (FR-019). Agent self-update, reboot and shutdown
// markers are excluded: their completion is reported by the restart or reboot
// they cause. It runs before any loop starts.
func (a *Agent) recoverInterruptedCommands(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, models.StartupRecoveryBudget)
	defer cancel()

	for _, entry := range a.inflight.All() {
		m := entry.Marker
		switch m.Action {
		case models.ActionAgentUpdate, models.ActionReboot, models.ActionShutdown:
			if err := a.inflight.Remove(entry.ID); err != nil {
				slog.Warn("recovery: could not clear marker", "id", entry.ID, "action", m.Action, "error", err)
			}
			continue
		}

		summary, unreachable := a.reconcileInterrupted(ctx, entry.ID, m)

		if !m.Reported && !a.out.Has(entry.ID) {
			message := restartedMessage
			if summary != "" {
				message += "; reconciled: " + summary
			}
			res := models.CommandResult{
				CommandID: entry.ID,
				Status:    models.ResultStatusError,
				Outcome:   models.OutcomeFailed,
				Message:   message,
				Step:      models.SanitizeStep(m.Step),
				Reason:    models.ReasonRestarted,
			}
			if err := a.out.Add(res); err != nil {
				slog.Error("recovery: failure result not persisted", "id", entry.ID, "error", err)
			}
			if err := a.inflight.MarkReported(entry.ID); err != nil {
				slog.Warn("recovery: could not mark marker as reported", "id", entry.ID, "error", err)
			}
		}

		if unreachable {
			continue
		}
		if err := a.inflight.Remove(entry.ID); err != nil {
			slog.Warn("recovery: could not clear marker", "id", entry.ID, "error", err)
		}
	}

	a.cleanupSanitizedLeftovers(ctx)
}

// ---------------------------------------------------------------------------
// Notifier
// ---------------------------------------------------------------------------

// notifier delivers progress reports and lifecycle events from one ordered
// goroutine with a bounded queue and a fresh 10s context per send (AD-011).
// Percent-only progress for a command is coalesced to one per second; step and
// state changes are always queued.
type notifier struct {
	clk  clock
	jobs chan func(ctx context.Context)

	mu   sync.Mutex
	last map[string]progressMark
}

type progressMark struct {
	state, step string
	at          time.Time
}

func newNotifier(clk clock) *notifier {
	return &notifier{
		clk:  clk,
		jobs: make(chan func(ctx context.Context), models.NotifierQueueBound),
		last: map[string]progressMark{},
	}
}

// Run executes queued jobs in order until ctx ends.
func (n *notifier) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-n.jobs:
			sendCtx, cancel := context.WithTimeout(context.Background(), models.LifecycleSendTimeout)
			job(sendCtx)
			cancel()
		}
	}
}

// Progress queues a progress send. A percent-only update that arrives less than
// ProgressMinInterval after the previous send for the same command is dropped.
func (n *notifier) Progress(id, state, step string, percent *int, send func(ctx context.Context)) {
	now := n.clk.Now()
	n.mu.Lock()
	prev, seen := n.last[id]
	percentOnly := percent != nil && seen && prev.state == state && prev.step == step
	if percentOnly && now.Sub(prev.at) < models.ProgressMinInterval {
		n.mu.Unlock()
		return
	}
	n.last[id] = progressMark{state: state, step: step, at: now}
	n.mu.Unlock()

	if percentOnly {
		select {
		case n.jobs <- send:
		default:
			slog.Debug("notifier: queue full, dropping percent update", "command_id", id)
		}
		return
	}
	n.enqueue(send, "progress", id)
}

// Event queues a lifecycle event, waiting at most one second for queue space.
func (n *notifier) Event(job func(ctx context.Context)) {
	n.enqueue(job, "event", "")
}

func (n *notifier) enqueue(job func(ctx context.Context), kind, id string) {
	select {
	case n.jobs <- job:
		return
	default:
	}
	timer := n.clk.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case n.jobs <- job:
	case <-timer.C():
		slog.Warn("notifier: queue full, dropping notification", "kind", kind, "command_id", id)
	}
}

// Forget drops the coalescing state of a finished command.
func (n *notifier) Forget(id string) {
	n.mu.Lock()
	delete(n.last, id)
	n.mu.Unlock()
}

// Flush blocks until every job queued before the call has run, or ctx ends. It
// exists for tests and for the shutdown path.
func (n *notifier) Flush(ctx context.Context) {
	done := make(chan struct{})
	select {
	case n.jobs <- func(context.Context) { close(done) }:
	case <-ctx.Done():
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}
