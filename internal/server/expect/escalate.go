package expect

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

// Escalator turns an escalation request into an event and delivers its push.
// *events.Generator implements it (EXPECT-001 WP-2). The tracker never imports
// the events package, so there is no import cycle.
type Escalator interface {
	// PrepareEscalation finds the matching enabled alert rule for
	// req.EventType() (device_offline for device kinds, container_died for
	// container kinds) and builds the event to store: the rule's severity, or
	// warning when no enabled rule matches; req.Message as the message;
	// CommandID, CommandAttr "escalation" and ContainerName from the request.
	// Skip is true when nothing must be raised (the container is excluded from
	// the rule). It reads rules only: it stores and sends nothing.
	PrepareEscalation(ctx context.Context, req models.EscalationRequest) (models.PreparedEscalation, error)
	// PublishEscalation sends the notification for an event that is already
	// stored and broadcast, through rule's channels. It neither checks nor
	// starts a rule cooldown (X-8). The tracker calls it from a publisher
	// worker with a 30 s deadline; it must honour ctx.
	PublishEscalation(ctx context.Context, evt models.Event, rule *models.AlertRule, hostname string) error
}

// Broadcaster pushes stored events and activity changes to the dashboards.
// *websocket.Hub implements it (BroadcastActivity is added by WP-5).
type Broadcaster interface {
	BroadcastEvent(e *models.Event)
	// BroadcastActivity sends one device's activity; nil means "no activity".
	BroadcastActivity(deviceID string, a *models.DeviceActivity)
}

// DeviceLister reads the devices (db.DeviceRepository satisfies it).
type DeviceLister interface {
	List(ctx context.Context) ([]models.Device, error)
}

// PolicyLister reads the auto-update policies for the actor phrase
// (db.AutoUpdateRepository satisfies it).
type PolicyLister interface {
	ListAll(ctx context.Context) ([]models.AutoUpdatePolicy, error)
}

// TelemetryReader reads the latest container list for the FR-022 check
// (db.TelemetryRepository satisfies it).
type TelemetryReader interface {
	LatestSnapshotTime(ctx context.Context, deviceID string) (ts time.Time, ok bool, err error)
	GetLatestSnapshot(ctx context.Context, deviceID string) (*models.TelemetrySnapshot, error)
}

// CommandReader reads one command, to label an escalation that outlives its
// window (db.CommandRepository satisfies it).
type CommandReader interface {
	GetByID(ctx context.Context, id string) (*models.Command, error)
}

// escalate records one escalation exactly once and publishes it (AD-010):
// PrepareEscalation, one transaction for marker and event, then the publisher.
// inserted is false when the marker already existed; skipped is true when the
// generator said nothing must be raised.
func (t *Tracker) escalate(ctx context.Context, req models.EscalationRequest) (inserted, skipped bool, err error) {
	var prep models.PreparedEscalation
	if t.opt.Escalator != nil {
		prep, err = t.opt.Escalator.PrepareEscalation(ctx, req)
		if err != nil {
			return false, false, fmt.Errorf("prepare escalation: %w", err)
		}
		if prep.Skip {
			return false, true, nil
		}
	} else {
		prep.Event = models.Event{Severity: models.SeverityWarning}
	}
	evt := prep.Event
	// The invariants of an escalation event hold whatever the generator did.
	evt.DeviceID = req.DeviceID
	evt.Type = req.EventType()
	evt.Message = req.Message
	evt.ContainerName = req.ContainerName
	evt.CommandID = req.Command.ID
	evt.CommandAttr = models.CommandAttrEscalation
	evt.CreatedAt = req.At
	if evt.Severity == "" {
		evt.Severity = models.SeverityWarning
	}
	marker := models.Escalation{CommandID: req.Command.ID, Target: req.Target(), DeviceID: req.DeviceID, CreatedAt: req.At}
	inserted, err = t.opt.Repo.RecordEscalation(ctx, &marker, &evt)
	if err != nil {
		return false, false, fmt.Errorf("record escalation: %w", err)
	}
	if !inserted {
		return false, false, nil
	}
	t.log.Info("expected-activity escalation", "device_id", req.DeviceID, "command_id", req.Command.ID,
		"action", req.Command.Action, "kind", string(req.Kind))
	t.pub.enqueue(job{kind: jobPush, evt: evt, rule: prep.Rule, hostname: req.Hostname, deviceID: req.DeviceID})
	return true, false, nil
}

// deviceKind picks the FR-020 / FR-021 escalation kind of a window ending with
// its device offline (AD-009). ok is false when the command needs none: a
// succeeded command whose policy has no FR-021 (shutdown).
func deviceKind(w *window) (models.EscalationKind, bool) {
	switch s := w.cmd.State(); {
	case s == models.CommandFailed || s == models.CommandTimedOut:
		return models.EscDeviceNonSuccess, true
	case s.IsActive():
		return models.EscDeviceNoResult, true
	case s == models.CommandSucceeded && w.policy.EscalateNotBack:
		return models.EscDeviceNotBack, true
	}
	return "", false
}

// escalateDevice raises the device escalation of a window (idempotent through
// the marker) and records the failure label for the dashboard.
func (t *Tracker) escalateDevice(ctx context.Context, w window, dv devSnap, kind models.EscalationKind) (bool, error) {
	now := t.now()
	actor := t.actorPhrase(&w)
	total := execBudget(&w.cmd) + w.policy.PostTerminal
	req := models.EscalationRequest{
		Kind: kind, Command: w.cmd, DeviceID: w.cmd.DeviceID, Hostname: dv.hostname, At: now,
		Message: deviceEscalationMessage(kind, models.SafeName(dv.hostname), &w, actor, total),
	}
	if w.policy.SelfUpdate {
		req.Message = selfEscalationMessage(kind, hostnameOf(dv), &w)
	}
	if w.policy.Reinstall {
		req.Message = models.ReinstallNotBackMessage(hostnameOf(dv), models.ShortCommandID(w.cmd.ID))
	}
	inserted, _, err := t.escalate(ctx, req)
	if err != nil {
		return false, err
	}
	t.markDeviceEscalated(w, kind, actor, now)
	return inserted, nil
}

// markDeviceEscalated remembers the device's escalated state for the dashboard
// (FR-037); it ends when the device comes online or after 24 hours.
func (t *Tracker) markDeviceEscalated(w window, kind models.EscalationKind, actor string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	dev := t.devs[w.cmd.DeviceID]
	if dev == nil || (dev.escalation != nil && dev.escalation.commandID != w.cmd.ID) {
		return
	}
	dev.escalation = &openEscalation{
		commandID: w.cmd.ID, action: w.cmd.Action, actor: actor, reason: escalationReason(kind, w.cmd.State()),
		bound: models.FormatDurationShort(w.policy.PostTerminal), at: at,
		startedAt: startOf(&w.cmd), origin: w.cmd.Origin,
	}
	if w.policy.SelfUpdate {
		// The escalated label names the update and its 10-minute bound (AD-009).
		dev.escalation.reason = selfEscalationReason(kind)
		dev.escalation.bound = models.FormatDurationShort(models.ExpectSelfUpdateBound)
		dev.escalation.fromVersion, dev.escalation.toVersion = w.selfFrom, w.selfTo
	}
	if w.policy.Reinstall {
		// The escalated label names the reinstall and its 10-minute bound (EXPECT-008 AD-010).
		dev.escalation.reason = models.EscalationReasonNotBack
		dev.escalation.bound = models.FormatDurationShort(models.ExpectReinstallBound)
	}
}

func escalationReason(kind models.EscalationKind, s models.CommandState) string {
	switch kind {
	case models.EscDeviceNonSuccess:
		if s == models.CommandTimedOut {
			return models.EscalationReasonTimedOut
		}
		return models.EscalationReasonFailed
	case models.EscDeviceNoResult:
		return models.EscalationReasonNoResult
	}
	return models.EscalationReasonNotBack
}

// containerCheck is the FR-022 check (AD-011): every container whose death the
// window suppressed must have been observed running after its last suppressed
// death. A container that was not gets one escalation, under the FR-025 cap.
// since is the period the containers had to come back; it only shapes the text.
func (t *Tracker) containerCheck(ctx context.Context, w window, dv devSnap, deaths []models.ContainerDeath, since time.Duration) (raised int, err error) {
	starts, err := t.opt.Repo.ContainerStartsSince(ctx, w.cmd.DeviceID, *w.cmd.DispatchedAt, 1000)
	if err != nil {
		return 0, fmt.Errorf("container starts: %w", err)
	}
	startKeys := make(map[string]time.Time, len(starts))
	for raw, at := range starts {
		k := models.ContainerNameKey(raw)
		if prev, ok := startKeys[k]; !ok || at.After(prev) {
			startKeys[k] = at
		}
	}
	snapAt, running, haveSnap, err := t.runningContainers(ctx, w.cmd.DeviceID)
	if err != nil {
		return 0, err
	}

	var down []string
	for _, d := range deaths {
		key := models.ContainerNameKey(d.ContainerName)
		if at, ok := startKeys[key]; ok && at.After(d.At) {
			continue
		}
		if haveSnap && snapAt.After(d.At) && running[key] {
			continue
		}
		down = append(down, key)
	}
	if len(down) == 0 {
		return 0, nil
	}
	sort.Strings(down)

	existing, err := t.opt.Repo.ListEscalationTargets(ctx, w.cmd.ID)
	if err != nil {
		return 0, fmt.Errorf("escalation targets: %w", err)
	}
	have := make(map[string]bool, len(existing))
	nContainer := 0
	for _, tg := range existing {
		have[tg] = true
		if models.IsContainerEscalationTarget(tg) {
			nContainer++
		}
	}

	actor := t.actorPhrase(&w)
	overflow := 0
	for _, key := range down {
		target := models.EscalationTarget(models.EscContainer, key)
		if have[target] {
			continue
		}
		if nContainer >= models.ExpectContainerEscalationCap {
			overflow++
			continue
		}
		req := models.EscalationRequest{
			Kind: models.EscContainer, Command: w.cmd, DeviceID: w.cmd.DeviceID, Hostname: dv.hostname,
			ContainerName: key, At: t.now(), Message: containerEscalationMessage(key, since, &w, actor),
		}
		inserted, skipped, err := t.escalate(ctx, req)
		if err != nil {
			return raised, err
		}
		if skipped {
			continue
		}
		if inserted {
			raised++
		}
		nContainer++
	}
	if overflow > 0 && !have[models.EscalationTargetSummary] {
		req := models.EscalationRequest{
			Kind: models.EscContainerSummary, Command: w.cmd, DeviceID: w.cmd.DeviceID, Hostname: dv.hostname,
			Count: overflow, At: t.now(), Message: containerSummaryMessage(overflow, since, &w, actor),
		}
		inserted, _, err := t.escalate(ctx, req)
		if err != nil {
			return raised, err
		}
		if inserted {
			raised++
		}
	}
	return raised, nil
}

// runningContainers reads the container states of the device's latest
// telemetry snapshot (names in key form). haveSnap is false when the device has
// none.
func (t *Tracker) runningContainers(ctx context.Context, deviceID string) (at time.Time, running map[string]bool, haveSnap bool, err error) {
	if t.opt.Telemetry == nil {
		return time.Time{}, nil, false, nil
	}
	ts, ok, err := t.opt.Telemetry.LatestSnapshotTime(ctx, deviceID)
	if err != nil {
		return time.Time{}, nil, false, fmt.Errorf("latest snapshot time: %w", err)
	}
	if !ok {
		return time.Time{}, nil, false, nil
	}
	snap, err := t.opt.Telemetry.GetLatestSnapshot(ctx, deviceID)
	if err != nil {
		return time.Time{}, nil, false, fmt.Errorf("latest snapshot: %w", err)
	}
	running = map[string]bool{}
	if snap != nil && snap.Data.Docker != nil {
		for _, c := range snap.Data.Docker.Containers {
			if c.State == "running" {
				running[models.ContainerNameKey(c.Name)] = true
			}
		}
	}
	return ts, running, true, nil
}

// The tracker's collaborators are satisfied by the repositories as they are.
var (
	_ DeviceLister    = db.DeviceRepository(nil)
	_ PolicyLister    = db.AutoUpdateRepository(nil)
	_ TelemetryReader = db.TelemetryRepository(nil)
	_ CommandReader   = db.CommandRepository(nil)
)
