package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// EXPECT-001: expected-activity windows. This file holds the policy table
// (Table 4.1 of the FRD), the bounds, the value types shared by the tracker,
// the event generator, the repository and the HTTP layer, and the name and
// message helpers that keep agent-supplied text out of events.

// Docker action names that open a window (the lifecycle policy only names the
// update actions).
const (
	ActionDockerRestart       = "docker_restart"
	ActionDockerStop          = "docker_stop"
	ActionDockerStackRedeploy = "docker_stack_redeploy"
	ActionDockerStackRestart  = "docker_stack_restart"
	ActionDockerStackUp       = "docker_stack_up"
	ActionDockerStackDown     = "docker_stack_down"
)

// Settle periods and return bounds (AD-003, DEC-2).
const (
	ExpectContainerSettle     = 2 * time.Minute
	ExpectHostSettle          = 3 * time.Minute
	ExpectHostReturnBound     = 15 * time.Minute
	ExpectAgentSettle         = 1 * time.Minute
	ExpectAgentReturnBound    = 10 * time.Minute
	ExpectShutdownReturnBound = 15 * time.Minute
	ExpectNonSuccessSettle    = 2 * time.Minute
)

// Readiness, grace and cap constants (AD-002, AD-008, AD-010, AD-011, AD-012).
const (
	// ExpectStaleAfter is how old the last successful reconcile may be before
	// every Cover* call fails open.
	ExpectStaleAfter = 90 * time.Second
	// ExpectLoopStaleAfter is how long the evaluation loop may go without
	// completing a tick before every Cover* call fails open.
	ExpectLoopStaleAfter = 15 * time.Second
	// OfflineDetectionGrace is the pause in offline detection after each server
	// start (FR-033).
	OfflineDetectionGrace = 2 * time.Minute
	// RestartSeriesGraceCap bounds the cumulative no-offline-detection period
	// across a series of rapid restarts (SEC-007).
	RestartSeriesGraceCap = 4 * time.Minute
	// OfflineHeartbeatThreshold is how old a heartbeat may be before the
	// device goes offline.
	OfflineHeartbeatThreshold = 45 * time.Second
	// ExpectContainerEscalationCap is the most container escalations one command
	// may raise; one summary escalation follows (FR-025).
	ExpectContainerEscalationCap = 20
	// ExpectEscalationDisplayTTL is how long an escalated device keeps its
	// failure label (FR-037).
	ExpectEscalationDisplayTTL = 24 * time.Hour
	// ExpectPushCeilingCount pushes per ExpectPushCeilingWindow, per device
	// (SEC-005). A summary push is allowed on top, at most once per window.
	ExpectPushCeilingCount  = 5
	ExpectPushCeilingWindow = time.Hour
	// ExpectDeferredRecheckBound is how long after its close time a window may
	// wait for its device to return before the container check is skipped with a
	// warning event (AD-011). It is also the lapse age of an unprocessed window.
	ExpectDeferredRecheckBound = 24 * time.Hour
	// ExpectPerDeviceWindowCap bounds the windows cached per device.
	ExpectPerDeviceWindowCap = 200
	// Reconcile query limits (AD-009).
	ExpectOpenWindowsLimit = 1000
	ExpectDueBacklogLimit  = 200
	ExpectLapseLimit       = 100
	// ExpectNameMaxBytes bounds container, actor and host names in messages.
	ExpectNameMaxBytes = 128
)

// StepWaitDpkgLock is the step token an agent reports while a reboot or
// shutdown waits for the dpkg lock. Reserved for EXPECT-002 (X-10): agents
// before it never report it, so the label falls back to plain "Rebooting".
const StepWaitDpkgLock = "wait_dpkg_lock"

// WindowClass groups actions that share suppression rules.
type WindowClass string

const (
	WindowContainer     WindowClass = "container"
	WindowContainerDown WindowClass = "container_down"
	WindowHost          WindowClass = "host"
	WindowShutdown      WindowClass = "shutdown"
	WindowAgent         WindowClass = "agent"
)

// ContainerCover says which container deaths a window covers.
type ContainerCover int

const (
	CoverNoContainers ContainerCover = iota
	CoverTargetContainers
	CoverAllContainers
)

// WindowPolicy is one row of Table 4.1 (AD-003).
type WindowPolicy struct {
	Action string
	Class  WindowClass
	// CoversOffline: the window suppresses device_offline for its device.
	CoversOffline bool
	Containers    ContainerCover
	// Settle is how long the device must be back online (or, for containers,
	// how long after the terminal state) before the window may close early.
	Settle time.Duration
	// PostTerminal (P) is the longest the window lives after the terminal
	// state: the return bound or the container settle.
	PostTerminal time.Duration
	// RequireObservedReturn: early close needs a registration or an
	// offline-to-online transition after the command finished.
	RequireObservedReturn bool
	// EscalateNotBack: FR-021, the device is not back at the return bound.
	EscalateNotBack bool
	// CheckContainers: FR-022, containers whose death was suppressed must be
	// running again when the window closes.
	CheckContainers bool
}

var expectPolicies = []WindowPolicy{
	{Action: ActionReboot, Class: WindowHost, CoversOffline: true, Containers: CoverAllContainers,
		Settle: ExpectHostSettle, PostTerminal: ExpectHostReturnBound, RequireObservedReturn: true,
		EscalateNotBack: true, CheckContainers: true},
	{Action: ActionShutdown, Class: WindowShutdown, CoversOffline: true, Containers: CoverAllContainers,
		PostTerminal: ExpectShutdownReturnBound},
	{Action: ActionOSUpdate, Class: WindowHost, CoversOffline: true, Containers: CoverAllContainers,
		Settle: ExpectHostSettle, PostTerminal: ExpectHostReturnBound,
		EscalateNotBack: true, CheckContainers: true},
	{Action: ActionAgentUpdate, Class: WindowAgent, CoversOffline: true, Containers: CoverNoContainers,
		Settle: ExpectAgentSettle, PostTerminal: ExpectAgentReturnBound, RequireObservedReturn: true,
		EscalateNotBack: true},
	{Action: ActionDockerUpdate, Class: WindowContainer, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle, CheckContainers: true},
	{Action: ActionDockerBulkUpdate, Class: WindowContainer, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle, CheckContainers: true},
	{Action: ActionDockerRestart, Class: WindowContainer, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle, CheckContainers: true},
	{Action: ActionDockerStop, Class: WindowContainerDown, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle},
	{Action: ActionDockerStackRedeploy, Class: WindowContainer, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle, CheckContainers: true},
	{Action: ActionDockerStackRestart, Class: WindowContainer, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle, CheckContainers: true},
	{Action: ActionDockerStackUp, Class: WindowContainer, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle, CheckContainers: true},
	{Action: ActionDockerStackDown, Class: WindowContainerDown, Containers: CoverTargetContainers,
		Settle: ExpectContainerSettle, PostTerminal: ExpectContainerSettle},
}

// ExpectPolicy returns the window policy of an action. ok is false for every
// action outside Table 4.1 (they open no window).
func ExpectPolicy(action string) (WindowPolicy, bool) {
	for _, p := range expectPolicies {
		if p.Action == action {
			return p, true
		}
	}
	return WindowPolicy{}, false
}

// ExpectActions lists the 12 actions that open a window, in the order of the
// idx_commands_window_pending predicate of migration 000033.
func ExpectActions() []string {
	out := make([]string, len(expectPolicies))
	for i, p := range expectPolicies {
		out[i] = p.Action
	}
	return out
}

// ExpectActionsSQL renders ExpectActions as a quoted SQL literal list. The
// values are package constants (never request data) so the partial index
// predicate can match (a bind parameter would defeat it).
func ExpectActionsSQL() string {
	acts := ExpectActions()
	quoted := make([]string, len(acts))
	for i, a := range acts {
		quoted[i] = "'" + a + "'"
	}
	return strings.Join(quoted, ",")
}

// DeviceIsLive is the one liveness predicate (SEC-004): every status except
// offline is live, so a warning device is moved offline like an online one.
func DeviceIsLive(status DeviceStatus) bool {
	return status != DeviceStatusOffline
}

// WindowOutcome is stored in commands.window_outcome by CloseWindow.
type WindowOutcome string

const (
	WindowOutcomeClosed     WindowOutcome = "closed"
	WindowOutcomeEscalated  WindowOutcome = "escalated"
	WindowOutcomePoweredOff WindowOutcome = "powered_off"
	WindowOutcomeNotOpened  WindowOutcome = "not_opened"
	WindowOutcomeLapsed     WindowOutcome = "lapsed"
)

// WindowEndReason says why a window left processing (AD-009). Every reason goes
// through one routine that runs the backstop.
type WindowEndReason string

const (
	EndSettled          WindowEndReason = "settled"
	EndReturnBound      WindowEndReason = "return_bound"
	EndHardBound        WindowEndReason = "hard_bound"
	EndNonSuccessSettle WindowEndReason = "nonsuccess_settle"
	EndPoweredOff       WindowEndReason = "powered_off"
	EndNotOpened        WindowEndReason = "not_opened"
	EndLapsed           WindowEndReason = "lapsed"
	EndDeleted          WindowEndReason = "deleted"
)

// ---------------------------------------------------------------------------
// Names, actor phrase, labels
// ---------------------------------------------------------------------------

// SafeName makes an agent- or operator-supplied name safe for an event message
// and an event column (AD-015, SEC-006): invalid UTF-8 and NUL are repaired,
// every control (Cc) and format (Cf, including bidi overrides and zero-width
// characters) rune is removed, and so are the line and paragraph separators.
// The result is cut to ExpectNameMaxBytes on a rune boundary.
func SafeName(s string) string {
	s = SanitizeText(s)
	clean := true
	for _, r := range s {
		if unsafeNameRune(r) {
			clean = false
			break
		}
	}
	if !clean {
		var b strings.Builder
		b.Grow(len(s))
		for _, r := range s {
			if !unsafeNameRune(r) {
				b.WriteRune(r)
			}
		}
		s = b.String()
	}
	return TruncateHead(s, ExpectNameMaxBytes)
}

func unsafeNameRune(r rune) bool {
	return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
		unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// ContainerNameKey is the form of a container name stored in new events and
// compared by the FR-022 check (C-6). An ordinary name is unchanged (minus a
// leading slash). A name that SafeName would alter gets its safe prefix plus
// "~" and 16 hex characters of the SHA-256 of the raw name, so distinct raw
// names stay distinct, the key is at most 128 bytes and it is idempotent.
func ContainerNameKey(raw string) string {
	n := strings.TrimLeft(raw, "/")
	safe := SafeName(n)
	if safe == n {
		return n
	}
	sum := sha256.Sum256([]byte(n))
	return TruncateHead(safe, ExpectNameMaxBytes-17) + "~" + hex.EncodeToString(sum[:])[:16]
}

// ShortCommandID is the first 8 characters of a command ID (<id8>).
func ShortCommandID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// ActorPhrase says who or what started a command (FR-018, AD-015).
// policyTarget is the resolved auto-update policy target ("" when unknown).
func ActorPhrase(c Command, policyTarget string) string {
	switch c.Origin {
	case OriginAutoUpdate:
		if t := SafeName(policyTarget); t != "" {
			return "by auto-update policy for " + t
		}
		if c.PolicyID != nil {
			return fmt.Sprintf("by auto-update policy #%d", *c.PolicyID)
		}
		return "by an auto-update policy"
	case OriginAutoPatch:
		return "by the auto-patch schedule"
	}
	if n := SafeName(c.ActorName); n != "" {
		return "by " + n
	}
	return "by an operator"
}

var expectActionLabels = map[string]string{
	ActionReboot:              "Reboot",
	ActionShutdown:            "Shutdown",
	ActionOSUpdate:            "OS update",
	ActionAgentUpdate:         "Agent update",
	ActionDockerUpdate:        "Docker update",
	ActionDockerBulkUpdate:    "Docker bulk update",
	ActionDockerRestart:       "Docker restart",
	ActionDockerStop:          "Docker stop",
	ActionDockerStackRedeploy: "Docker stack redeploy",
	ActionDockerStackRestart:  "Docker stack restart",
	ActionDockerStackUp:       "Docker stack up",
	ActionDockerStackDown:     "Docker stack down",
}

// ExpectActionLabel is the human-readable action name used in event messages.
// Actions outside the table fall back to the SafeName of the action itself.
func ExpectActionLabel(action string) string {
	if l, ok := expectActionLabels[action]; ok {
		return l
	}
	return SafeName(action)
}

// CommandTargetLabel is the stack name of a docker_stack_* command (the base
// name of its compose working directory), or "" for any other command.
func CommandTargetLabel(c Command) string {
	if !strings.HasPrefix(c.Action, "docker_stack_") {
		return ""
	}
	for _, k := range c.TargetKeys {
		if wd, ok := strings.CutPrefix(k, targetKeyComposePrefix); ok {
			wd = strings.TrimRight(wd, "/")
			if i := strings.LastIndex(wd, "/"); i >= 0 {
				wd = wd[i+1:]
			}
			return SafeName(wd)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Coverage results (what the generator gets back from the tracker)
// ---------------------------------------------------------------------------

// Coverage is the answer for a covered container event: which command covers
// it and the pieces of its message.
type Coverage struct {
	CommandID   string
	ShortID     string
	Action      string
	ActionLabel string
	ActorPhrase string
	// Target is the stack name for docker_stack_* commands, else "".
	Target string
	Class  WindowClass
}

// DeathMessage is the message of a suppressed container_died event.
func (c Coverage) DeathMessage(containerKey string) string {
	of := ""
	if c.Target != "" {
		of = " of " + c.Target
	}
	return fmt.Sprintf("Container %s died during %s%s %s (expected)", containerKey, c.ActionLabel, of, c.ActorPhrase)
}

// OOMMessage is the message of an OOM alert that happens inside a window
// (FR-011): the alert still fires and names the action in progress.
func (c Coverage) OOMMessage(containerKey string) string {
	return fmt.Sprintf("Container %s OOM killed (during %s %s, command %s)", containerKey, c.ActionLabel, c.ActorPhrase, c.ShortID)
}

// OfflineKind distinguishes the two ways an offline transition is covered.
type OfflineKind string

const (
	// OfflineKindWindow: the transition falls inside a host or agent window.
	OfflineKindWindow OfflineKind = "window"
	// OfflineKindPoweredOff: first offline after a succeeded shutdown.
	OfflineKindPoweredOff OfflineKind = "powered_off"
)

// OfflineCoverage is the answer for a covered offline transition.
type OfflineCoverage struct {
	Kind        OfflineKind
	CommandID   string
	ShortID     string
	Action      string
	ActionLabel string
	ActorPhrase string
	// BackBy is the latest time the device is expected back (the return bound
	// of a succeeded command); nil while the command is still running.
	BackBy *time.Time
	// ShutdownAt is when a shutdown finished (Kind powered_off).
	ShutdownAt time.Time
}

// Message is the message of the suppressed or powered-off event (AD-015).
func (c OfflineCoverage) Message(host string) string {
	host = SafeName(host)
	if c.Kind == OfflineKindPoweredOff {
		return fmt.Sprintf("Device %s powered off %s at %s", host, c.ActorPhrase, c.ShutdownAt.Local().Format("15:04"))
	}
	back := ""
	if c.BackBy != nil {
		back = ", back by " + c.BackBy.Local().Format("15:04")
	}
	return fmt.Sprintf("Device %s went offline during %s %s (expected%s)", host, c.ActionLabel, c.ActorPhrase, back)
}

// OnlineNote says that a device came online inside or right after a window or
// out of the powered-off state (FR-044, FR-030).
type OnlineNote struct {
	CommandID   string
	ShortID     string
	ActionLabel string
	ActorPhrase string
	// Offline is how long the device was offline (or powered off).
	Offline    time.Duration
	PoweredOff bool
}

// Message is the device_online message (AD-015).
func (n OnlineNote) Message(host string) string {
	host = SafeName(host)
	if n.PoweredOff {
		return fmt.Sprintf("Device %s came online after Shutdown %s (powered off %s)", host, n.ActorPhrase, FormatDurationShort(n.Offline))
	}
	return fmt.Sprintf("Device %s came online after %s %s (offline %s)", host, n.ActionLabel, n.ActorPhrase, FormatDurationShort(n.Offline))
}

// ---------------------------------------------------------------------------
// Escalation
// ---------------------------------------------------------------------------

// EscalationKind is the reason an escalation is raised (AD-010).
type EscalationKind string

const (
	// EscDeviceNonSuccess: FR-020, the command failed or timed out with its
	// device offline.
	EscDeviceNonSuccess EscalationKind = "device_nonsuccess"
	// EscDeviceNoResult: the window hit its hard bound with the command still
	// active (no result, no sweeper) and the device offline.
	EscDeviceNoResult EscalationKind = "device_no_result"
	// EscDeviceNotBack: FR-021, the device is not back at the return bound.
	EscDeviceNotBack EscalationKind = "device_not_back"
	// EscContainer: FR-022, a container whose death was suppressed is not running.
	EscContainer EscalationKind = "container"
	// EscContainerSummary: FR-025, the cap of container escalations is reached.
	EscContainerSummary EscalationKind = "container_summary"
)

// IsDevice reports whether the escalation concerns the device (not a container).
func (k EscalationKind) IsDevice() bool {
	return k == EscDeviceNonSuccess || k == EscDeviceNoResult || k == EscDeviceNotBack
}

// Escalation marker targets.
const (
	EscalationTargetDevice  = "device"
	EscalationTargetSummary = "summary"
	// escalationTargetContainerPrefix is followed by 32 hex characters.
	escalationTargetContainerPrefix = "container:"
)

// EscalationTarget is the marker key of an escalation: collision-free for
// distinct container keys (a SHA-256 prefix of the key), at most 42 bytes.
func EscalationTarget(kind EscalationKind, containerKey string) string {
	switch kind {
	case EscContainer:
		sum := sha256.Sum256([]byte(containerKey))
		return escalationTargetContainerPrefix + hex.EncodeToString(sum[:])[:32]
	case EscContainerSummary:
		return EscalationTargetSummary
	}
	return EscalationTargetDevice
}

// IsContainerEscalationTarget reports whether a marker target is a per-container marker.
func IsContainerEscalationTarget(target string) bool {
	return strings.HasPrefix(target, escalationTargetContainerPrefix)
}

// Escalation is one exactly-once marker row of command_escalations.
type Escalation struct {
	CommandID string     `json:"command_id"`
	Target    string     `json:"target"`
	DeviceID  string     `json:"device_id"`
	EventID   *int64     `json:"event_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ClearedAt *time.Time `json:"cleared_at,omitempty"`
}

// EscalationRequest is what the tracker hands the event generator to turn into
// an event (Escalator.PrepareEscalation). Message is already rendered.
type EscalationRequest struct {
	Kind     EscalationKind
	Command  Command
	DeviceID string
	Hostname string
	// ContainerName is the container key (ContainerNameKey form); "" for device
	// kinds and the summary.
	ContainerName string
	// Count is the number of further containers (summary only).
	Count   int
	Message string
	At      time.Time
}

// EventType is the event type (and the alert rule metric) of the escalation.
func (r EscalationRequest) EventType() EventType {
	if r.Kind.IsDevice() {
		return EventDeviceOffline
	}
	return EventContainerDied
}

// Target is the marker key of the escalation.
func (r EscalationRequest) Target() string { return EscalationTarget(r.Kind, r.ContainerName) }

// PreparedEscalation is the generator's answer: the event to store and the
// matching enabled rule (nil when none matches). Skip is true when nothing
// must be raised (the container is excluded from the rule).
type PreparedEscalation struct {
	Event Event
	Rule  *AlertRule
	Skip  bool
}

// PoweredOffState is the one durable row per device of a successful shutdown
// (AD-007). It is a label and a count source, never a coverage reason.
type PoweredOffState struct {
	DeviceID    string    `json:"device_id"`
	CommandID   string    `json:"command_id"`
	ActionLabel string    `json:"action_label"`
	ActorPhrase string    `json:"actor_phrase"`
	ShutdownAt  time.Time `json:"shutdown_at"`
	OfflineAt   time.Time `json:"offline_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// ContainerDeath is the last suppressed container_died event of one container
// in one command (key form of the name).
type ContainerDeath struct {
	ContainerName string
	At            time.Time
}

// ---------------------------------------------------------------------------
// Activity DTOs (GET /api/v1/activity, WS activity_update)
// ---------------------------------------------------------------------------

type ActivityKind string

const (
	ActivityHost       ActivityKind = "host"
	ActivityAgent      ActivityKind = "agent"
	ActivityDocker     ActivityKind = "docker"
	ActivityPoweredOff ActivityKind = "powered_off"
	ActivityEscalated  ActivityKind = "escalated"
)

type ActivityPhase string

const (
	PhaseQueued     ActivityPhase = "queued"
	PhaseInProgress ActivityPhase = "in_progress"
	PhaseReturn     ActivityPhase = "return"
	PhaseSettling   ActivityPhase = "settling"
	PhasePoweredOff ActivityPhase = "powered_off"
	PhaseEscalated  ActivityPhase = "escalated"
	// PhaseAwaitingReturn is internal: a deferred container check. It never
	// produces a label.
	PhaseAwaitingReturn ActivityPhase = "awaiting_return"
	// PhaseClosed is internal: the window no longer covers anything.
	PhaseClosed ActivityPhase = "closed"
)

// Escalation reasons shown on the dashboard.
const (
	EscalationReasonFailed   = "failed"
	EscalationReasonTimedOut = "timed_out"
	EscalationReasonNoResult = "no_result"
	EscalationReasonNotBack  = "not_back"
)

// ActivityEscalation is the escalated state of a device activity.
type ActivityEscalation struct {
	Reason string    `json:"reason"`
	Bound  string    `json:"bound"`
	At     time.Time `json:"at"`
}

// DeviceActivity is the server-computed activity of one device (AD-014).
type DeviceActivity struct {
	DeviceID         string              `json:"device_id"`
	Kind             ActivityKind        `json:"kind"`
	Phase            ActivityPhase       `json:"phase"`
	Action           string              `json:"action"`
	CommandID        string              `json:"command_id"`
	ActorPhrase      string              `json:"actor_phrase"`
	StartedAt        time.Time           `json:"started_at"`
	BackBy           *time.Time          `json:"back_by"`
	WaitingDpkgLock  bool                `json:"waiting_dpkg_lock"`
	Progress         *int                `json:"progress"`
	AgentFromVersion string              `json:"agent_from_version"`
	Target           string              `json:"target"`
	DockerActions    int                 `json:"docker_actions"`
	CoversOffline    bool                `json:"covers_offline"`
	DeviceOnline     bool                `json:"device_online"`
	Escalation       *ActivityEscalation `json:"escalation"`
	PoweredOffAt     *time.Time          `json:"powered_off_at"`
}

// ActivityResponse is the body of GET /api/v1/activity.
type ActivityResponse struct {
	GeneratedAt        time.Time        `json:"generated_at"`
	NewestAgentVersion string           `json:"newest_agent_version"`
	Activities         []DeviceActivity `json:"activities"`
}
