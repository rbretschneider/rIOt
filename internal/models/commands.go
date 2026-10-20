package models

import "time"

// Command represents a remote command sent to a device.
//
// Status holds a canonical CommandState string. It stays a plain string so the
// JSON shape and existing callers do not change; use State() for the typed
// value. Rows stored with legacy values are mapped by Normalize (called by the
// repository scan and by the mocks).
type Command struct {
	ActorKind       string                 `json:"actor_kind"`
	ActorAuth       string                 `json:"actor_auth"`
	ActorName       string                 `json:"actor_name"`
	ActorSub        string                 `json:"-"`
	Steps           []DockerCommandStep    `json:"steps,omitempty"`
	TargetResults   []DockerTargetResult   `json:"target_results,omitempty"`
	ProbeKeys       []string               `json:"-"`
	IsDocker        bool                   `json:"-"`
	DockerExclusive bool                   `json:"-"`
	ID              string                 `json:"id"`
	DeviceID        string                 `json:"device_id"`
	Action          string                 `json:"action"` // docker_stop, docker_restart, docker_start, reboot, agent_update
	Params          map[string]interface{} `json:"params"`
	Status          string                 `json:"status"` // canonical CommandState value, see command_state.go
	ResultMsg       string                 `json:"result_msg"`
	DurationMs      *int64                 `json:"duration_ms,omitempty"`
	ExitCode        *int                   `json:"exit_code,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`

	// Lifecycle fields (CMD-LIFECYCLE-001, migration 000023).
	ExpiresAt      time.Time  `json:"expires_at"`
	ExecDeadlineS  int        `json:"exec_deadline_s"`
	DeadlineAt     *time.Time `json:"deadline_at,omitempty"` // set at claim, never moved by progress
	DispatchedAt   *time.Time `json:"dispatched_at,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	VerifyingAt    *time.Time `json:"verifying_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Step           string     `json:"step"`
	Progress       *int       `json:"progress"` // 0..100 or null
	FailureStep    string     `json:"failure_step"`
	StderrTail     string     `json:"stderr_tail"`
	Origin         string     `json:"origin"` // OriginUser, OriginAutoUpdate, OriginAutoPatch
	PolicyID       *int       `json:"policy_id"`
	// TargetKey is TargetKeys[0] or "" (derived by Normalize); WP-WEB matches on
	// TargetKeys.
	TargetKey  string   `json:"target_key"`
	TargetKeys []string `json:"target_keys"`

	// PreLifecycle marks rows that existed before migration 000023 (SEC-010):
	// the sweeper relabels them silently. SendAmbiguous is set by the FR-085
	// revert (SEC-016). Neither is exposed through the API.
	PreLifecycle  bool `json:"-"`
	SendAmbiguous bool `json:"-"`
}

// State returns Status as a canonical CommandState (legacy values mapped, an
// unknown value maps to failed with one Warn per distinct value).
func (c *Command) State() CommandState { return CanonicalStatus(c.Status) }

// Normalize canonicalizes Status, derives TargetKey from TargetKeys, replaces a
// nil TargetKeys with an empty slice (JSON [] instead of null) and defaults an
// empty Origin to OriginUser. Repositories and mocks call it after every scan
// so API output is always canonical (FR-083).
func (c *Command) Normalize() {
	c.Status = string(CanonicalStatus(c.Status))
	if c.TargetKeys == nil {
		c.TargetKeys = []string{}
	}
	c.TargetKey = ""
	if len(c.TargetKeys) > 0 {
		c.TargetKey = c.TargetKeys[0]
	}
	if c.Origin == "" {
		c.Origin = OriginUser
	}
}

// CommandOutput holds captured stdout/stderr from a command execution.
type CommandOutput struct {
	ID        int64     `json:"id"`
	CommandID string    `json:"command_id"`
	Stream    string    `json:"stream"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// CommandPayload is the data sent to an agent over WebSocket.
type CommandPayload struct {
	CommandID string                 `json:"command_id"`
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
}

// Final result outcomes (CommandResult.Outcome). Status stays success/error
// for legacy servers (FR-081).
const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
)

// CommandResult is the response from an agent.
//
// Status stays in the legacy vocabulary (ResultStatusSuccess /
// ResultStatusError) so a legacy server stores something it understands; a new
// server derives the canonical state from Outcome first (ResolveState).
type CommandResult struct {
	Steps         []DockerCommandStep  `json:"steps,omitempty"`
	TargetResults []DockerTargetResult `json:"target_results,omitempty"`
	CommandID     string               `json:"command_id"`
	Status        string               `json:"status"` // success, error
	Message       string               `json:"message"`
	ExitCode      *int                 `json:"exit_code,omitempty"`
	DurationMs    *int64               `json:"duration_ms,omitempty"`
	Output        string               `json:"output,omitempty"`
	// New in CMD-LIFECYCLE-001 (all optional on the wire):
	Step    string `json:"step,omitempty"`    // failing step, or the last step on success
	Outcome string `json:"outcome,omitempty"` // OutcomeSucceeded | OutcomeFailed
	Stderr  string `json:"stderr,omitempty"`  // stderr/error text; the server keeps the last 8 KiB
	Reason  string `json:"reason,omitempty"`  // one of ReportReasons
}

// ResolveState derives the canonical target state of a final result
// (AD-008): a valid Outcome wins; otherwise Status success -> succeeded and
// error -> failed. recognized is false when neither was usable, in which case
// the state is CommandFailed and the caller logs a Warn.
func (r CommandResult) ResolveState() (state CommandState, recognized bool) {
	switch r.Outcome {
	case OutcomeSucceeded:
		return CommandSucceeded, true
	case OutcomeFailed:
		return CommandFailed, true
	}
	switch r.Status {
	case ResultStatusSuccess:
		return CommandSucceeded, true
	case ResultStatusError:
		return CommandFailed, true
	}
	return CommandFailed, false
}

// CommandTransition is the input of CommandRepository.Transition: one guarded
// UPDATE ... WHERE id=$ [AND device_id=$] AND status IN (GuardStates()).
type CommandTransition struct {
	ResultBytes   *int64
	Steps         []DockerCommandStep
	TargetResults []DockerTargetResult
	ID            string
	// DeviceID scopes the UPDATE (and the follow-up classification read) to one
	// device. It is mandatory for ActorAgent and optional ("" = unscoped) for
	// ActorServer and ActorOperator.
	DeviceID string
	// Actor selects the allowed edges (AllowedFrom). Required.
	Actor CommandActor
	To    CommandState
	// From optionally narrows the guard (for example only dispatched for the
	// FR-085 revert). Every entry is canonicalized; entries that are not a legal
	// source for To/Actor are ignored. Empty means "all legal sources".
	From []CommandState

	// Nil pointer fields leave the column unchanged.
	Step        *string
	ProgressSet bool // when true Progress is written; a nil Progress writes NULL
	Progress    *int
	ResultMsg   *string
	FailureStep *string
	StderrTail  *string
	ExitCode    *int
	DurationMs  *int64
	// Now is the transition time written to updated_at and the per-state
	// timestamps. Required; the repository never reads the wall clock.
	Now time.Time
}

// GuardStates returns the statuses (canonical plus legacy aliases) that the
// UPDATE's WHERE clause must accept: AllowedFrom(To, Actor), intersected with
// From when From is non-empty. The slice is in lifecycle order and is meant
// for SQLStatusList or an ANY($n) bind. It is empty when the edge is not
// allowed at all; the repository then returns "not applied" without querying.
func (t CommandTransition) GuardStates() []CommandState {
	allowed := AllowedFrom(t.To, t.Actor)
	if len(t.From) == 0 {
		return allowed
	}
	want := map[CommandState]bool{}
	for _, f := range t.From {
		if c := f.Canonical(); c != "" {
			want[c] = true
		}
	}
	out := make([]CommandState, 0, len(allowed))
	for _, s := range allowed {
		if want[s.Canonical()] {
			out = append(out, s)
		}
	}
	return out
}

// Kinds of agent report (the `kind` field of POST .../report).
type CommandReportKind string

const (
	ReportKindAck      CommandReportKind = "ack"
	ReportKindProgress CommandReportKind = "progress"
	ReportKindResult   CommandReportKind = "result"
)

// CommandReport is the transport-neutral agent report: the body of
// POST /api/v1/devices/{id}/commands/{commandId}/report, and the form
// CommandAck, CommandProgress and CommandResult (WS) convert to. It is the
// input of Service.HandleAgentReport.
type CommandReport struct {
	Kind CommandReportKind `json:"kind"`
	// CommandID is optional in the HTTP body; when present it must equal the
	// path ID. WS reports always carry it.
	CommandID string `json:"command_id,omitempty"`
	// State is the progress state: "running" or "verifying".
	State    string         `json:"state,omitempty"`
	Step     string         `json:"step,omitempty"`
	Progress *int           `json:"progress,omitempty"`
	Message  string         `json:"message,omitempty"`
	Result   *CommandResult `json:"result,omitempty"`
}

// CommandAck is the WS message `command_ack`.
type CommandAck struct {
	CommandID string    `json:"command_id"`
	At        time.Time `json:"at"`
}

// Report converts the WS ack to the neutral form.
func (a CommandAck) Report() CommandReport {
	return CommandReport{Kind: ReportKindAck, CommandID: a.CommandID}
}

// CommandProgress is the WS message `command_progress`.
type CommandProgress struct {
	CommandID string    `json:"command_id"`
	State     string    `json:"state"` // "running" | "verifying"
	Step      string    `json:"step"`
	Progress  *int      `json:"progress,omitempty"`
	Message   string    `json:"message,omitempty"`
	At        time.Time `json:"at"`
}

// Report converts the WS progress message to the neutral form.
func (p CommandProgress) Report() CommandReport {
	return CommandReport{
		Kind: ReportKindProgress, CommandID: p.CommandID,
		State: p.State, Step: p.Step, Progress: p.Progress, Message: p.Message,
	}
}

// Report converts a WS `command_result` payload to the neutral form.
func (r CommandResult) Report() CommandReport {
	res := r
	return CommandReport{Kind: ReportKindResult, CommandID: r.CommandID, Result: &res}
}

// ReportDisposition is what the server did with one agent report (AD-008).
type ReportDisposition string

const (
	ReportAccepted          ReportDisposition = "accepted"
	ReportDuplicate         ReportDisposition = "duplicate"
	ReportCoalesced         ReportDisposition = "coalesced"
	ReportAlreadyTerminal   ReportDisposition = "already_terminal"
	ReportInvalidTransition ReportDisposition = "invalid_transition"
	ReportNotFound          ReportDisposition = "not_found"
)

// IsSuccessful reports whether the agent may drop its outbox entry because the
// server holds (or already held) the outcome: accepted, duplicate, coalesced.
func (d ReportDisposition) IsSuccessful() bool {
	return d == ReportAccepted || d == ReportDuplicate || d == ReportCoalesced
}

// CommandReceipt is the WS message `command_receipt` (server -> agent). Foreign
// and unknown commands produce a byte-identical receipt (Status is "").
type CommandReceipt struct {
	CommandID string            `json:"command_id"`
	Result    ReportDisposition `json:"result"`
	Status    string            `json:"status"`
}

// ReportResponse is the HTTP body of the report endpoint: 200 with
// CommandID/Result/Status, or an error body (409/404/400/429) with Error/Code
// (and Status for 409).
type ReportResponse struct {
	CommandID string            `json:"command_id,omitempty"`
	Result    ReportDisposition `json:"result,omitempty"`
	Status    string            `json:"status,omitempty"`
	Error     string            `json:"error,omitempty"`
	Code      string            `json:"code,omitempty"`
}

// CommandUpdate is the dashboard WS message `command_update`, broadcast once
// per accepted transition.
type CommandUpdate struct {
	ActorKind     string               `json:"actor_kind"`
	ActorAuth     string               `json:"actor_auth"`
	ActorName     string               `json:"actor_name"`
	TargetResults []DockerTargetResult `json:"target_results,omitempty"`
	CommandID     string               `json:"command_id"`
	DeviceID      string               `json:"device_id"`
	Action        string               `json:"action"`
	TargetKey     string               `json:"target_key"`
	TargetKeys    []string             `json:"target_keys"`
	Status        string               `json:"status"`
	Step          string               `json:"step"`
	Progress      *int                 `json:"progress"`
	Message       string               `json:"message"`
	FailureStep   string               `json:"failure_step"`
	Origin        string               `json:"origin"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

// NewCommandUpdate builds the broadcast for a command's current row.
func NewCommandUpdate(c Command) CommandUpdate {
	keys := c.TargetKeys
	if keys == nil {
		keys = []string{}
	}
	target := c.TargetKey
	if target == "" && len(keys) > 0 {
		target = keys[0]
	}
	results := []DockerTargetResult(nil)
	if c.State().IsTerminal() {
		results = c.TargetResults
	}
	return CommandUpdate{
		ActorKind: c.ActorKind, ActorAuth: c.ActorAuth, ActorName: c.ActorName, TargetResults: results, CommandID: c.ID, DeviceID: c.DeviceID, Action: c.Action,
		TargetKey: target, TargetKeys: keys,
		Status: string(CanonicalStatus(c.Status)), Step: c.Step, Progress: c.Progress,
		Message: c.ResultMsg, FailureStep: c.FailureStep, Origin: c.Origin,
		UpdatedAt: c.UpdatedAt,
	}
}

// CommandConflictResponse is the 409 body of POST .../commands and
// POST .../docker/bulk-update (code "conflict").
type CommandConflictResponse struct {
	Error             string         `json:"error"`
	Code              string         `json:"code"`
	ExistingCommandID string         `json:"existing_command_id"`
	ExistingStatus    string         `json:"existing_status,omitempty"`
	TargetKey         string         `json:"target_key,omitempty"`
	Conflicts         []BulkConflict `json:"conflicts,omitempty"`
}

// ConflictMessage is the 409 `error` text for a single-target conflict.
const ConflictMessage = "a command is already in progress for this target"

// BulkConflict is one skipped target of a bulk update (FR-060).
type BulkConflict struct {
	ContainerID       string `json:"container_id"`
	TargetKey         string `json:"target_key"`
	ExistingCommandID string `json:"existing_command_id"`
}

// BulkCommandResponse is the 201 body of the bulk update: the created Command
// flattened, plus the skipped conflicts.
type BulkCommandResponse struct {
	Command
	Conflicts []BulkConflict `json:"conflicts"`
}

// AutoUpdatePolicy defines an auto-update rule for a container or compose stack.
type AutoUpdatePolicy struct {
	ID              int        `json:"id"`
	DeviceID        string     `json:"device_id"`
	Target          string     `json:"target"` // container name or compose project
	IsStack         bool       `json:"is_stack"`
	ComposeWorkDir  string     `json:"compose_work_dir"`
	Enabled         bool       `json:"enabled"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`

	// ConsecutiveFailures and LastFailureAt drive the failure backoff (AD-007).
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
}

// BackoffUntil returns the earliest time the policy may dispatch again after a
// failure, or nil when it is not backing off (no failures recorded).
func (p AutoUpdatePolicy) BackoffUntil() *time.Time {
	if p.ConsecutiveFailures <= 0 || p.LastFailureAt == nil {
		return nil
	}
	t := p.LastFailureAt.Add(AutoUpdateBackoff(p.ConsecutiveFailures))
	return &t
}

// InBackoff reports whether now is before BackoffUntil. The boundary instant
// itself is eligible.
func (p AutoUpdatePolicy) InBackoff(now time.Time) bool {
	until := p.BackoffUntil()
	return until != nil && now.Before(*until)
}
