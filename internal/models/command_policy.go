package models

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Action classes, queued TTL and execution deadlines (AD-005)
// ---------------------------------------------------------------------------

// CommandClass groups actions that share TTL, deadline and agent limits.
type CommandClass string

const (
	ClassPower        CommandClass = "power"
	ClassOSPatch      CommandClass = "os_patch"
	ClassDockerUpdate CommandClass = "docker_update"
	ClassDockerBulk   CommandClass = "docker_bulk_update"
	ClassAgentUpdate  CommandClass = "agent_update"
	ClassOther        CommandClass = "other"
)

// Command action names used by the lifecycle policy.
const (
	ActionReboot            = "reboot"
	ActionShutdown          = "shutdown"
	ActionOSUpdate          = "os_update"
	ActionEnableAutoUpdates = "enable_auto_updates"
	ActionDockerUpdate      = "docker_update"
	ActionDockerBulkUpdate  = "docker_bulk_update"
	ActionAgentUpdate       = "agent_update"
	ActionAgentUninstall    = "agent_uninstall"
)

// ActionClass maps an action name to its class. Every action that is not
// listed in the AD-005 table is ClassOther.
func ActionClass(action string) CommandClass {
	switch action {
	case ActionReboot, ActionShutdown:
		return ClassPower
	case ActionOSUpdate, ActionEnableAutoUpdates:
		return ClassOSPatch
	case ActionDockerUpdate:
		return ClassDockerUpdate
	case ActionDockerBulkUpdate:
		return ClassDockerBulk
	case ActionAgentUpdate:
		return ClassAgentUpdate
	default:
		return ClassOther
	}
}

const (
	dockerUpdateExecDeadline = 35 * time.Minute
	bulkPerTargetSlack       = 5 * time.Minute
	bulkExecDeadlineCap      = 6 * time.Hour
	// BulkDeadlineReserve is subtracted from a bulk command's execution
	// deadline to get the agent's own wall-time budget (AD-005: deadline - 5m - 30s).
	BulkDeadlineReserve = 5*time.Minute + 30*time.Second
)

// QueuedTTL is how long a command may wait in queued before it expires.
func QueuedTTL(action string) time.Duration {
	switch ActionClass(action) {
	case ClassPower:
		return 15 * time.Minute
	case ClassOSPatch:
		return 30 * time.Minute
	case ClassDockerUpdate, ClassDockerBulk, ClassAgentUpdate:
		return 60 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// ExpiresAt is now + QueuedTTL(action), capped at windowEnd when windowEnd is
// non-zero and earlier (FR-025). The auto dispatchers pass windowEnd only for
// window-mode maintenance windows. A windowEnd at or before now yields now
// (the command is born expired and is never delivered).
func ExpiresAt(action string, now, windowEnd time.Time) time.Time {
	exp := now.Add(QueuedTTL(action))
	if !windowEnd.IsZero() && windowEnd.Before(exp) {
		if windowEnd.Before(now) {
			return now
		}
		return windowEnd
	}
	return exp
}

// Reboot budget (REBOOT-DPKG-001 AD-004). A reboot waits for the dpkg lock for
// at most DpkgLockWaitBound before it is skipped, so it needs a larger budget
// than an action that does not wait. The queued TTL is deliberately unchanged:
// it bounds undelivered commands only.
const (
	// DpkgLockWaitBound is the longest one reboot attempt waits for the dpkg lock
	// (BR-004). All three reboot paths share it.
	DpkgLockWaitBound = 10 * time.Minute
	// RebootAgentHardMax is the agent's time budget for a standalone reboot: the
	// wait bound plus room for the guards and the final lock check.
	RebootAgentHardMax = DpkgLockWaitBound + 2*time.Minute
	// RebootExecDeadline is the server's wait for a reboot result. It exceeds
	// RebootAgentHardMax + DeadlineSlack.
	RebootExecDeadline = 20 * time.Minute
)

// Shutdown budget (SHUTDOWN-DPKG-001 AD-005). Shutdown waits for the dpkg lock
// like reboot but has its own constants, so either can change without the other
// (BR-004). None of these reference a Reboot* constant or DpkgLockWaitBound; an
// AST test enforces it.
const (
	// ShutdownDpkgLockWaitBound is the longest one shutdown command waits for the
	// dpkg lock (FR-017).
	ShutdownDpkgLockWaitBound = 10 * time.Minute
	// ShutdownAgentHardMax is the agent's time budget for a shutdown: the wait
	// bound plus room for the guards, the final lock check and the start check.
	ShutdownAgentHardMax = ShutdownDpkgLockWaitBound + 2*time.Minute
	// ShutdownExecDeadline is the server's wait for a shutdown result. It exceeds
	// ShutdownAgentHardMax + DeadlineSlack.
	ShutdownExecDeadline = 20 * time.Minute
	// ShutdownStartCheckBound is how long the agent watches the poweroff command's
	// exit status (FR-027). A sudo refusal returns within milliseconds and
	// `systemctl poweroff` enqueues and returns well under a second, so 3s separates
	// refusal from acceptance; a command still running at the bound counts as
	// initiated. It is far below the 20s SIGTERM grace the result is written in.
	ShutdownStartCheckBound = 3 * time.Second
)

// ExecDeadline is how long after dispatch the server waits for a result before
// declaring timed_out. nTargets matters only for docker_bulk_update
// (min(35m*N + 5m, 6h), with N < 1 treated as 1). reboot and shutdown are 20m
// because each may wait up to 10m for the dpkg lock; their constants are separate.
func ExecDeadline(action string, nTargets int) time.Duration {
	if action == "docker_remove_items" {
		return 10 * time.Minute
	}
	if strings.HasPrefix(action, "docker_stack_") {
		return 35 * time.Minute
	}
	switch ActionClass(action) {
	case ClassPower:
		switch action {
		case ActionReboot:
			return RebootExecDeadline
		case ActionShutdown:
			return ShutdownExecDeadline
		}
		return 10 * time.Minute
	case ClassOSPatch:
		return 40 * time.Minute
	case ClassDockerUpdate:
		return dockerUpdateExecDeadline
	case ClassDockerBulk:
		if nTargets < 1 {
			nTargets = 1
		}
		d := time.Duration(nTargets)*dockerUpdateExecDeadline + bulkPerTargetSlack
		if d > bulkExecDeadlineCap || d < 0 {
			return bulkExecDeadlineCap
		}
		return d
	case ClassAgentUpdate:
		return 15 * time.Minute
	default:
		return 15 * time.Minute
	}
}

// ExecDeadlineSeconds is ExecDeadline in whole seconds, the value stored in
// commands.exec_deadline_s.
func ExecDeadlineSeconds(action string, nTargets int) int {
	return int(ExecDeadline(action, nTargets) / time.Second)
}

// Agent budgets (AD-013). All are time.Duration.
const (
	AckBudget                = 5 * time.Second
	DockerPrecheckBudget     = 15 * time.Second
	ComposePullLimit         = 15 * time.Minute // the sanitize retry shares this budget
	ComposeUpLimit           = 10 * time.Minute
	StandalonePullLimit      = 15 * time.Minute
	ChildGrace               = 60 * time.Second // SIGTERM -> SIGKILL grace
	StopDependentsBudget     = 20 * time.Second
	VerifyWindow             = 10 * time.Second
	VerifyBudget             = 15 * time.Second
	DependentRestoreBudget   = 2 * time.Minute
	DependentComposeUpLimit  = 90 * time.Second // compose fallback inside the restore budget
	StandaloneRecreateBudget = 40 * time.Second
	RollbackBudget           = 60 * time.Second
	CleanupBudget            = 15 * time.Second
	// AgentShutdownGrace is how long SIGTERM waits for in-flight commands. It is
	// below systemd's default 90s TimeoutStopSec; install.sh is unchanged.
	AgentShutdownGrace = 20 * time.Second
	// LifecycleSendTimeout is the fresh context for lifecycle events and
	// results, independent of the operation's expired context (FR-046).
	LifecycleSendTimeout = 10 * time.Second
	// StartupRecoveryBudget is the total budget for interrupted-update recovery.
	StartupRecoveryBudget = 2 * time.Minute
)

// ComposeMaxWallTime is the agent's worst-case compose docker_update wall time:
// 5s + 15s + 16m + 20s + 11m + 15s + 2m = 29m55s. It is also the agent hard max
// for the docker_update class.
const ComposeMaxWallTime = AckBudget + DockerPrecheckBudget +
	(ComposePullLimit + ChildGrace) + StopDependentsBudget +
	(ComposeUpLimit + ChildGrace) + VerifyBudget + DependentRestoreBudget

// BulkContainerWorstCase is the per-container worst case the bulk loop checks
// before starting the next container: elapsed + 29m50s <= budget (AD-013).
const BulkContainerWorstCase = 29*time.Minute + 50*time.Second

// BulkSkippedMessage is the per-container message when the bulk wall-time
// budget is exhausted.
const BulkSkippedMessage = "skipped: bulk time budget exhausted"

// BulkAgentBudget is the agent's own wall-time budget for a bulk command:
// ExecDeadline - 5m - 30s.
func BulkAgentBudget(nTargets int) time.Duration {
	return ExecDeadline(ActionDockerBulkUpdate, nTargets) - BulkDeadlineReserve
}

// AgentHardMax is the longest the agent may spend executing action (the value
// the DeadlineInvariant is checked against).
//
//	reboot 12m, shutdown 12m, os_update 31m30s, enable_auto_updates 5m,
//	docker_update 29m55s, docker_bulk_update deadline-5m30s,
//	agent_update 9m, other 9m.
func AgentHardMax(action string, nTargets int) time.Duration {
	if action == "docker_remove_items" {
		return 9 * time.Minute
	}
	if strings.HasPrefix(action, "docker_stack_") {
		return 30 * time.Minute
	}
	switch ActionClass(action) {
	case ClassPower:
		switch action {
		case ActionReboot:
			return RebootAgentHardMax
		case ActionShutdown:
			return ShutdownAgentHardMax
		}
		return time.Minute
	case ClassOSPatch:
		if action == ActionEnableAutoUpdates {
			return 5 * time.Minute
		}
		return 31*time.Minute + 30*time.Second
	case ClassDockerUpdate:
		return ComposeMaxWallTime
	case ClassDockerBulk:
		return BulkAgentBudget(nTargets)
	default:
		return 9 * time.Minute
	}
}

// DeadlineSlack is the minimum margin between AgentHardMax and ExecDeadline
// (FRD 7.3 invariant: deadline > agent max + 5 minutes).
const DeadlineSlack = 5 * time.Minute

// AutoUpdateBackoff is the minimum wait after the n-th consecutive failed
// policy command: min(15m * 2^(n-1), 6h). It is 0 for n <= 0.
func AutoUpdateBackoff(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	const (
		base = 15 * time.Minute
		ceil = 6 * time.Hour
	)
	// 15m * 2^5 = 8h already exceeds the cap; avoid shifting past it.
	if n > 6 {
		return ceil
	}
	d := base << (n - 1)
	if d > ceil {
		return ceil
	}
	return d
}

// FormatDurationShort renders a duration as "35m", "1h10m", "6h", "31m30s",
// "45s". Sub-second remainders are dropped; non-positive durations are "0s".
func FormatDurationShort(d time.Duration) string {
	secs := int64(d / time.Second)
	if secs <= 0 {
		return "0s"
	}
	h, m, s := secs/3600, (secs%3600)/60, secs%60
	var b strings.Builder
	if h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if s > 0 {
		fmt.Fprintf(&b, "%ds", s)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Origins
// ---------------------------------------------------------------------------

// Command origins (commands.origin CHECK constraint).
const (
	OriginUser       = "user"
	OriginAutoUpdate = "auto_update"
	OriginAutoPatch  = "auto_patch"
)

// IsValidOrigin reports whether o is one of the three allowed origins.
func IsValidOrigin(o string) bool {
	return o == OriginUser || o == OriginAutoUpdate || o == OriginAutoPatch
}

// ---------------------------------------------------------------------------
// Target keys (AD-004). Frozen strings; the web client computes the same keys.
// ---------------------------------------------------------------------------

const (
	// TargetKeyHost is the single device-level key for host-scoped actions.
	TargetKeyHost = "host"
	// TargetKeyDockerPrefix prefixes every Docker target key. The agent uses it
	// for HeldWithPrefix (restart/update exclusion, SEC C-4(c)).
	TargetKeyDockerPrefix = "docker:"

	targetKeyComposePrefix   = "docker:compose:"
	targetKeyContainerPrefix = "docker:container:"
	targetKeyIDPrefix        = "docker:id:"
)

// Compose labels read from a container.
const (
	LabelComposeProject    = "com.docker.compose.project"
	LabelComposeService    = "com.docker.compose.service"
	LabelComposeWorkingDir = "com.docker.compose.project.working_dir"
)

// TargetKeyCompose is the key for a compose project or any container in it:
// the working directory verbatim with trailing slashes removed.
func TargetKeyCompose(workDir string) string {
	return targetKeyComposePrefix + strings.TrimRight(workDir, "/")
}

// TargetKeyContainer is the key for a standalone container: the name without
// any leading slash.
func TargetKeyContainer(name string) string {
	return targetKeyContainerPrefix + strings.TrimLeft(name, "/")
}

// TargetKeyContainerID is the fallback key when a container cannot be resolved
// from telemetry.
func TargetKeyContainerID(id string) string {
	return targetKeyIDPrefix + id
}

// TargetKeyForContainer picks the key for a container: compose key when the
// project, service and working_dir labels are all present, else the container
// name key, else (no name) the container ID key.
func TargetKeyForContainer(c ContainerInfo) string {
	if wd := c.Labels[LabelComposeWorkingDir]; wd != "" &&
		c.Labels[LabelComposeProject] != "" && c.Labels[LabelComposeService] != "" {
		return TargetKeyCompose(wd)
	}
	if strings.TrimLeft(c.Name, "/") != "" {
		return TargetKeyContainer(c.Name)
	}
	return TargetKeyContainerID(c.ID)
}

// IsDockerTargetKey reports whether k is a Docker target key.
func IsDockerTargetKey(k string) bool {
	return strings.HasPrefix(k, TargetKeyDockerPrefix)
}

// IsHostAction reports whether action is serialized by the device-level host
// key: reboot, shutdown, os_update, enable_auto_updates, agent_update.
func IsHostAction(action string) bool {
	switch action {
	case ActionReboot, ActionShutdown, ActionOSUpdate, ActionEnableAutoUpdates, ActionAgentUpdate:
		return true
	}
	return false
}

// HostTargetKeys returns {TargetKeyHost} for host actions and nil otherwise.
// Docker actions are resolved by the server from params and telemetry
// (internal/server/commands/targets.go), not here.
func HostTargetKeys(action string) []string {
	if IsHostAction(action) {
		return []string{TargetKeyHost}
	}
	return nil
}

// NormalizeTargetKeys drops empty strings and duplicates, preserving first-seen
// order. It never returns nil (so the column stays '{}' rather than NULL).
func NormalizeTargetKeys(keys ...string) []string {
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// Steps, reasons and report bounds (SEC C-1 / C-2)
// ---------------------------------------------------------------------------

// docker_update step vocabulary (FR-076). "verify" is reported with state
// "verifying".
const (
	StepPrecheck          = "precheck"
	StepResolve           = "resolve"
	StepPull              = "pull"
	StepStopDependents    = "stop_dependents"
	StepRecreate          = "recreate"
	StepVerify            = "verify"
	StepRestartDependents = "restart_dependents"
	StepRollback          = "rollback"
	StepCleanup           = "cleanup"
)

// Result reasons (the `reason` field of a final result).
const (
	ReasonTimeout     = "timeout"
	ReasonRolledBack  = "rolled_back"
	ReasonRestarted   = "restarted"
	ReasonConflict    = "conflict"
	ReasonShutdown    = "shutdown"
	ReasonPersistence = "persistence"
)

// ReportReasons is the closed enum for CommandResult.Reason. Anything else is
// stored as "". Treat it as read-only.
var ReportReasons = []string{
	ReasonTimeout, ReasonRolledBack, ReasonRestarted,
	ReasonConflict, ReasonShutdown, ReasonPersistence,
}

// IsReportReason reports whether r is in ReportReasons (the empty string is
// not; callers store "" for "no reason").
func IsReportReason(r string) bool {
	for _, v := range ReportReasons {
		if r == v {
			return true
		}
	}
	return false
}

// SanitizeReason returns r when it is in the enum, otherwise "".
func SanitizeReason(r string) string {
	if IsReportReason(r) {
		return r
	}
	return ""
}

// Bounds on agent-supplied report content, enforced server-side on both WS and
// HTTP (SEC C-1).
const (
	MaxReportStepBytes      = 64
	MaxProgressMessageBytes = 1024
	MaxResultMsgBytes       = 65536
	MaxStderrTailBytes      = 8192
	MaxReportOutputBytes    = 262144
	MaxTargetKeyFilterBytes = 512
	// MaxReportBodyBytes caps the HTTP report body (413 above it).
	MaxReportBodyBytes = 512 << 10
	// MaxAgentWSMessageBytes is the server's WS read limit for agent sockets.
	MaxAgentWSMessageBytes = 4 << 20
)

// StepPattern is the only accepted shape of `step` and `failure_step`. A value
// that does not match is stored as "" and the transition still applies.
var StepPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// SanitizeStep returns s when it matches StepPattern, otherwise "". It is
// hand-rolled over bytes so the hot path allocates nothing.
func SanitizeStep(s string) string {
	if len(s) == 0 || len(s) > MaxReportStepBytes {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return ""
		}
	}
	return s
}

// DedupeCriticalActions are the actions the agent must refuse (fail at
// precheck, reason persistence) when its seen-ID store cannot be persisted
// (SEC C-8). Treat it as read-only.
var DedupeCriticalActions = []string{
	ActionReboot, ActionShutdown, ActionOSUpdate, ActionAgentUpdate,
}

// IsDedupeCriticalAction reports whether action is in DedupeCriticalActions.
func IsDedupeCriticalAction(action string) bool {
	if action == "docker_remove_items" || strings.HasPrefix(action, "docker_stack_") {
		return true
	}
	for _, a := range DedupeCriticalActions {
		if a == action {
			return true
		}
	}
	return false
}

// IsCanonicalUUID reports whether s is a canonical lowercase UUID:
// 36 bytes, hex digits a-f/0-9 only, dashes at offsets 8, 13, 18 and 23. It is
// the gate in front of every command_id on both server and agent (SEC C-1,
// C-7). It performs no allocation and does not check version bits.
func IsCanonicalUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}

// SanitizeText makes s safe for a Postgres TEXT column and a JSON broadcast:
// every NUL byte is removed and every run of invalid UTF-8 bytes is replaced by
// one U+FFFD. It never fails (SEC C-2). Apply it before truncating, then
// truncate with the rune-safe helpers below.
func SanitizeText(s string) string {
	if !strings.ContainsRune(s, 0) && utf8.ValidString(s) {
		return s
	}
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	return strings.ToValidUTF8(s, "�")
}

// TruncateHead keeps the first n bytes of s, cutting back to a rune
// boundary. Valid UTF-8 in gives valid UTF-8 out (SanitizeText first).
func TruncateHead(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// TruncateTail keeps the last n bytes of s, cutting forward to a rune
// boundary. Valid UTF-8 in gives valid UTF-8 out (SanitizeText first).
func TruncateTail(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// TruncationMarker separates the head and the tail kept by TruncateHeadTail.
const TruncationMarker = "\n...[output truncated]...\n"

// TruncateHeadTail keeps the beginning and the end of s, joined by
// TruncationMarker, in at most n bytes in total. When n is too small for a
// marker plus content it degrades to TruncateHead. Both cuts are rune-safe.
func TruncateHeadTail(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	room := n - len(TruncationMarker)
	if room < 2 {
		return TruncateHead(s, n)
	}
	headLen := room / 2
	tailLen := room - headLen
	return TruncateHead(s, headLen) + TruncationMarker + TruncateTail(s, tailLen)
}

// ---------------------------------------------------------------------------
// Agent persistence / delivery bounds (AD-010, AD-011). Shared so the agent
// tests and any server-side assertion use one number.
// ---------------------------------------------------------------------------

const (
	OutboxMaxEntries    = 200
	OutboxMaxFileBytes  = 1 << 20
	OutboxMaxEntryBytes = 256 << 10
	OutboxMaxAge        = 24 * time.Hour
	OutboxBackoffBase   = 5 * time.Second
	OutboxBackoffCap    = 5 * time.Minute
	// LegacyServerModeDuration is how long the agent stays WS-only after an
	// HTTP 404/405 without a code (FR-022).
	LegacyServerModeDuration = time.Hour
	SeenMaxEntries           = 1000
	SeenMaxAge               = 48 * time.Hour
	// NotifierQueueBound is the bound of the agent's ordered progress queue.
	NotifierQueueBound = 64
	// ProgressMinInterval is the per-command minimum spacing of accepted
	// running->running writes (server gate) and of percent-only updates (agent).
	ProgressMinInterval = time.Second
	// HeartbeatClaimLimit is the maximum number of commands one heartbeat claims.
	HeartbeatClaimLimit = 10
)

// OutboxBackoff is the delay before the next delivery attempt after `attempts`
// failures: min(5s * 2^(attempts-1), 5m). It is 0 for attempts <= 0.
func OutboxBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return 0
	}
	if attempts > 7 { // 5s << 6 = 320s already exceeds the cap
		return OutboxBackoffCap
	}
	d := OutboxBackoffBase << (attempts - 1)
	if d > OutboxBackoffCap {
		return OutboxBackoffCap
	}
	return d
}

// ---------------------------------------------------------------------------
// API error codes (JSON `code` field), shared by server and clients.
// ---------------------------------------------------------------------------

const (
	CodeConflict          = "conflict"
	CodeAlreadyTerminal   = "already_terminal"
	CodeInvalidTransition = "invalid_transition"
	CodeCommandNotFound   = "command_not_found"
	CodeInvalidReport     = "invalid_report"
	CodeInvalidFilter     = "invalid_filter"
	CodeRateLimited       = "rate_limited"
	CodeNotCancellable    = "not_cancellable"

	// PATCH-FLOW-001 AD-004: manual os_update reboot authorization rejections.
	CodeRebootNotAllowed      = "reboot_not_allowed"
	CodeRebootAuthUnsupported = "reboot_authorization_unsupported"
)

// os_update parameter names (PATCH-FLOW-001). include_reboot_class is derived
// by the server only; reboot_if_required is the operator's per-command opt-in.
const (
	ParamRebootIfRequired   = "reboot_if_required"
	ParamIncludeRebootClass = "include_reboot_class"
)

// os_update extra-updater parameters (EXTRA-UPD-001 AD-006, section 6.3).
// exclude_extra_updaters is the operator's per-command opt-out. The budget
// stamp is server-owned: Dispatch sets it and deletes every client-supplied
// variant first.
const (
	ParamExcludeExtraUpdaters = "exclude_extra_updaters"
	ParamExtraUpdaterBudget   = "extra_updater_budget"
)

// Extended os_update bounds for a host with an enabled extra updater
// (EXTRA-UPD-001 AD-006). The chain is strictly nested: DietPi step limit 40m <
// worker budget 80m < service backstop 85m < agent hard max 88m < deadline 95m.
const (
	OSUpdateExtraAgentHardMax = 88 * time.Minute
	OSUpdateExtraExecDeadline = 95 * time.Minute
)

// ExtraUpdaterBudget reports whether params carry the server's extended-budget
// stamp. Only a JSON boolean true counts.
func ExtraUpdaterBudget(params map[string]interface{}) bool {
	stamped, _ := params[ParamExtraUpdaterBudget].(bool)
	return stamped
}

// ExtendedOSUpdate reports whether a command runs under the extended os_update
// bounds: an os_update that carries the server's budget stamp.
func ExtendedOSUpdate(action string, params map[string]interface{}) bool {
	return action == ActionOSUpdate && ExtraUpdaterBudget(params)
}
