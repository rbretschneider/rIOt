package expect

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Activity computation (AD-014, FR-034 to FR-043): what each device is doing,
// from the server's view of commands and device state, for GET /api/v1/activity
// and the activity_update WebSocket message.

// activityLocked picks the one activity to show for a device, in priority order:
//
//  1. a host or agent window in progress or in its return phase (latest dispatch);
//  2. an open device escalation (under 24 h, not cleared);
//  3. a powered-off record;
//  4. a queued host or agent command;
//  5. container-class windows in progress or settling.
//
// A deferred window (awaiting its container re-check) produces no label. The
// caller holds t.mu.
func (t *Tracker) activityLocked(dev *devState, now time.Time) *models.DeviceActivity {
	dv := dev.view()
	var host *window
	var hostTL timeline
	var queued *window
	var docker []*window
	var dockerTL []timeline
	for _, w := range dev.windows {
		if w.deferred {
			continue
		}
		tl := evaluate(w, dv, now, t.graceEnd)
		switch w.policy.Class {
		case models.WindowHost, models.WindowShutdown, models.WindowAgent:
			if !tl.opened {
				if tl.phase == models.PhaseQueued && (queued == nil || w.cmd.CreatedAt.After(queued.cmd.CreatedAt)) {
					queued = w
				}
				continue
			}
			if tl.due || (tl.phase != models.PhaseInProgress && tl.phase != models.PhaseReturn) {
				continue
			}
			if host == nil || tl.d.After(hostTL.d) {
				host, hostTL = w, tl
			}
		default:
			if tl.opened && !tl.due && (tl.phase == models.PhaseInProgress || tl.phase == models.PhaseSettling) {
				docker = append(docker, w)
				dockerTL = append(dockerTL, tl)
			}
		}
	}

	switch {
	case host != nil:
		return t.hostActivityLocked(dev, host, hostTL)
	case dev.escalation != nil:
		e := dev.escalation
		return &models.DeviceActivity{
			DeviceID: dev.id, Kind: models.ActivityEscalated, Phase: models.PhaseEscalated, Action: e.action,
			CommandID: e.commandID, ActorPhrase: e.actor, StartedAt: e.startedAt, DeviceOnline: dev.online, CoversOffline: false,
			Escalation:       &models.ActivityEscalation{Reason: e.reason, Bound: e.bound, At: e.at},
			AgentFromVersion: e.fromVersion, AgentToVersion: e.toVersion,
		}
	case dev.poweredOff != nil:
		p := dev.poweredOff
		at := p.ShutdownAt
		return &models.DeviceActivity{
			DeviceID: dev.id, Kind: models.ActivityPoweredOff, Phase: models.PhasePoweredOff, Action: models.ActionShutdown,
			CommandID: p.CommandID, ActorPhrase: p.ActorPhrase, StartedAt: p.ShutdownAt, PoweredOffAt: &at, DeviceOnline: dev.online,
		}
	case queued != nil:
		kind := models.ActivityHost
		if queued.policy.Class == models.WindowAgent {
			kind = models.ActivityAgent
		}
		return &models.DeviceActivity{
			DeviceID: dev.id, Kind: kind, Phase: models.PhaseQueued, Action: queued.cmd.Action, CommandID: queued.cmd.ID,
			ActorPhrase: t.actorPhraseLocked(queued), StartedAt: queued.cmd.CreatedAt, DeviceOnline: dev.online,
			AgentFromVersion: queued.fromVersion,
		}
	case len(docker) > 0:
		return t.dockerActivityLocked(dev, docker, dockerTL)
	}
	return nil
}

// hostActivityLocked builds the host-class activity. The "(waiting for dpkg lock)"
// suffix is shown only while the device is online (EXPECT-007 AD-003, FR-009,
// BR-006): a step that is still current on a device the server already marked
// offline can be stale, because the host went down before the step change or the
// result was delivered. A command that reached a terminal state has ended its wait,
// so it never claims one either (FR-009).
func (t *Tracker) hostActivityLocked(dev *devState, w *window, tl timeline) *models.DeviceActivity {
	kind := models.ActivityHost
	if w.policy.Class == models.WindowAgent {
		kind = models.ActivityAgent
	}
	a := &models.DeviceActivity{
		DeviceID: dev.id, Kind: kind, Phase: tl.phase, Action: w.cmd.Action, CommandID: w.cmd.ID,
		ActorPhrase: t.actorPhraseLocked(w), StartedAt: startOf(&w.cmd), DeviceOnline: dev.online,
		WaitingDpkgLock: dev.online && !w.cmd.State().IsTerminal() && w.cmd.Step == models.StepWaitDpkgLock, Progress: copyIntPtr(w.cmd.Progress),
		AgentFromVersion: w.fromVersion, CoversOffline: w.policy.CoversOffline,
	}
	if tl.phase == models.PhaseReturn {
		a.BackBy = copyTimePtr(tl.backBy)
	}
	if w.policy.SelfUpdate {
		// An agent self-update window: "Updating agent <from> -> <to>" by agent
		// auto-update. The DTO carries no back_by (AD-005); both versions are
		// re-validated ones, or both "" (AD-009).
		a.AgentFromVersion, a.AgentToVersion = w.selfFrom, w.selfTo
		a.BackBy = nil
	}
	return a
}

func (t *Tracker) dockerActivityLocked(dev *devState, ws []*window, tls []timeline) *models.DeviceActivity {
	latest := 0
	phase := models.PhaseSettling
	for i := range ws {
		if tls[i].d.After(tls[latest].d) {
			latest = i
		}
		if tls[i].phase == models.PhaseInProgress {
			phase = models.PhaseInProgress
		}
	}
	w := ws[latest]
	a := &models.DeviceActivity{
		DeviceID: dev.id, Kind: models.ActivityDocker, Phase: phase, Action: w.cmd.Action, CommandID: w.cmd.ID,
		ActorPhrase: t.actorPhraseLocked(w), StartedAt: startOf(&w.cmd), DeviceOnline: dev.online,
		DockerActions: len(ws), Progress: copyIntPtr(w.cmd.Progress),
	}
	if len(ws) == 1 {
		a.Target = dockerTarget(&w.cmd)
	}
	return a
}

// dockerTarget names what a Docker command works on: the container, the stack,
// or "N containers" for a bulk command. SafeName-clean and at most 128 bytes.
func dockerTarget(c *models.Command) string {
	if s := models.CommandTargetLabel(*c); s != "" {
		return s
	}
	var names []string
	for _, k := range c.TargetKeys {
		if n, ok := strings.CutPrefix(k, "docker:container:"); ok {
			names = append(names, n)
		}
	}
	switch {
	case len(names) == 1 && c.Action != models.ActionDockerBulkUpdate:
		return models.SafeName(names[0])
	case len(c.TargetKeys) > 1 || c.Action == models.ActionDockerBulkUpdate:
		return fmt.Sprintf("%d containers", len(c.TargetKeys))
	case len(names) == 1:
		return models.SafeName(names[0])
	}
	return ""
}

// Activities returns the activity of every device that has one, sorted by
// device ID. It reads memory only. When the tracker is not ready it still
// returns the last cached state: labels are informational, alert decisions fail
// open independently.
func (t *Tracker) Activities() models.ActivityResponse {
	newest := ""
	if t.opt.NewestAgentVersion != nil {
		newest = t.opt.NewestAgentVersion()
	}
	now := t.now()
	t.mu.Lock()
	out := make([]models.DeviceActivity, 0, len(t.devs))
	for _, dev := range t.devs {
		if a := t.activityLocked(dev, now); a != nil {
			out = append(out, *a)
		}
	}
	t.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return models.ActivityResponse{GeneratedAt: now, NewestAgentVersion: newest, Activities: out}
}

// broadcastActivity queues one activity_update per device whose activity
// changed since the last one that was queued successfully. A dropped push leaves
// the "last sent" state alone, so the change is retried on the next tick.
func (t *Tracker) broadcastActivity() {
	type change struct {
		devID string
		a     *models.DeviceActivity
	}
	now := t.now()
	var changes []change
	t.mu.Lock()
	for id, dev := range t.devs {
		cur := t.activityLocked(dev, now)
		if !reflect.DeepEqual(cur, dev.lastActivity) {
			changes = append(changes, change{id, cur})
		}
	}
	t.mu.Unlock()
	sort.Slice(changes, func(i, j int) bool { return changes[i].devID < changes[j].devID })
	for _, c := range changes {
		if !t.pub.enqueue(job{kind: jobActivity, deviceID: c.devID, activity: c.a}) {
			continue
		}
		t.mu.Lock()
		if dev := t.devs[c.devID]; dev != nil {
			dev.lastActivity = c.a
		}
		t.mu.Unlock()
	}
}
