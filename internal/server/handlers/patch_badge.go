package handlers

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Per-host patch badge. computePatchBadge maps device settings, the OS-patch
// window, the latest update telemetry and the latest finished os_update to one
// badge plus a pre-rendered four-line explanation. It is display-only: it is
// pure, reads no clock and no repository, and is never called from
// checkAutoPatch or any other dispatch path. The agent facts it reads
// (allow_reboot, allow_patching, hold_reboot_class) are self-reported and are
// never used to decide whether a command is sent.

// patchFactsMinAgentVersion is the first agent release that reports
// allow_patching, other_holds and release_upgrade. Used only in the
// "newer agent" note.
const patchFactsMinAgentVersion = "v2.75.0"

// Badge kinds.
const (
	badgeAuto              = "auto"
	badgeAutoRebootDate    = "auto_reboot_date"
	badgeAutoRebootUnknown = "auto_reboot_unknown"
	badgeNeedsYou          = "needs_you"
	// badgeManual is the neutral badge of a host with nothing for the operator
	// to do and no automatic patching (EXTRA-UPD-001 FR-081, DEC-018).
	badgeManual = "manual"
)

// Needs-you reason codes, in the order they are evaluated and listed.
const (
	reasonAutoPatchOff         = "auto_patch_off"
	reasonPatchingDisabled     = "patching_disabled_server"
	reasonAgentPatchingOff     = "agent_patching_not_allowed"
	reasonCannotPatchHost      = "cannot_patch_host"
	reasonHeldPackages         = "held_packages"
	reasonRebootClassHeld      = "reboot_class_held_no_release"
	reasonHoldEnforcementBroke = "hold_enforcement_broken"
	reasonManualApproval       = "manual_approval_update"
	reasonLastRunFailed        = "last_run_failed"
	reasonManualReboot         = "manual_reboot_needed"
	// Extra-updater reasons (EXTRA-UPD-001): one item per updater entry.
	reasonExtraNotApplied = "extra_update_not_applied"
	reasonExtraPending    = "extra_update_pending"
	reasonExtraUnknown    = "extra_update_unknown"
)

// patchBadgeReasonCodes lists every code computePatchBadge can raise.
var patchBadgeReasonCodes = []string{
	reasonAutoPatchOff, reasonPatchingDisabled, reasonAgentPatchingOff, reasonCannotPatchHost,
	reasonHeldPackages, reasonRebootClassHeld, reasonHoldEnforcementBroke, reasonManualApproval,
	reasonLastRunFailed, reasonManualReboot,
	reasonExtraNotApplied, reasonExtraPending, reasonExtraUnknown,
}

const (
	// maxBadgeListedPackages is how many package names one reason shows.
	maxBadgeListedPackages = 10
	// maxBadgeRunMessage caps the failed-run message, in characters.
	maxBadgeRunMessage = 200
	// maxBadgeNameLen and maxBadgeHolds mirror the agent's bounds; the server
	// re-applies them because the agent is not trusted.
	maxBadgeNameLen = 128
	maxBadgeHolds   = 50
	maxBadgeRelease = 64
)

var (
	badgePackageRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:~_*?-]*$`)
	badgeReleaseRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 .()_-]*$`)
	badgeUUIDRe    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// ansiRe matches CSI and OSC escape sequences.
	ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
)

// patchBadgeInput is everything the badge depends on.
type patchBadgeInput struct {
	AutoPatch bool
	OSPatch   models.MaintenanceWindow
	// Updates is the latest updates telemetry; nil means no patch data.
	Updates   *models.UpdateInfo
	UpdatesAt *time.Time
	// LastRun is the latest finished os_update (succeeded, failed or
	// timed_out); nil when there is none.
	LastRun *models.Command
	// AgentFactsMinVersion names the agent release that reports the newer facts.
	AgentFactsMinVersion string
}

// patchBadgeReason is one item: an action item under "Needs you", or an
// information item under "Information". Updater names the extra updater of an
// extra_update_* item.
type patchBadgeReason struct {
	Code      string `json:"code"`
	Text      string `json:"text"`
	Action    string `json:"action"`
	CommandID string `json:"command_id,omitempty"`
	Updater   string `json:"updater,omitempty"`
}

// patchBadgeExplanation is the four-line explanation: Automatic patching,
// Reboot, Needs you (summary plus action items) and As of, plus information
// items and optional notes. NeedsYou holds action items only; Info holds the
// neutral ones (EXTRA-UPD-001 FR-079).
type patchBadgeExplanation struct {
	Automatic       string             `json:"automatic"`
	Reboot          string             `json:"reboot"`
	NeedsYou        []patchBadgeReason `json:"needs_you"`
	NeedsYouSummary string             `json:"needs_you_summary"`
	Info            []patchBadgeReason `json:"info"`
	AsOf            string             `json:"as_of"`
	Notes           []string           `json:"notes"`
}

// patchBadge is the badge as served on each patch-status row.
type patchBadge struct {
	Kind        string                `json:"kind"`
	Label       string                `json:"label"`
	Explanation patchBadgeExplanation `json:"explanation"`
	// RebootAt is the estimated reboot time; set only for auto_reboot_date.
	RebootAt *time.Time `json:"reboot_at,omitempty"`
}

// computePatchBadge decides the badge for one host. See the decision table:
// any needs-you reason wins; otherwise a reboot is either dated (window mode,
// allow_reboot true), unknown, or not needed.
//
// PATCH-FLOW-001 amends PATCH-BADGE-001 rule NY-10 and the Reboot line (no
// PATCH-BADGE-001 FRD/ADD exists in the repository, so the amendment is recorded
// here): "reboot needed" means a reboot is required now, which is only
// u.RebootRequired. A pending, not yet installed kernel/GPU update is a future
// reboot and never raises manual_reboot_needed. While the package lists are
// stale, nothing is derived from the pending list.
func computePatchBadge(in patchBadgeInput, now time.Time) patchBadge {
	now = now.UTC()
	u := in.Updates
	mode := in.OSPatch.Mode
	nextStart, haveStart := nextMaintenanceWindowStart(in.OSPatch, now)

	allowReboot := (*bool)(nil)
	allowPatching := (*bool)(nil)
	if u != nil {
		allowReboot, allowPatching = u.AllowReboot, u.AllowPatching
	}
	gated := in.OSPatch.RebootClass == "gated"

	modeUsable := mode == "anytime" || (mode == "window" && haveStart)
	agentRefuses := allowPatching != nil && !*allowPatching
	canPatch := u != nil && (u.PackageManager == "apt" || u.PackageManager == "dnf")
	autoPatchSetUp := in.AutoPatch && modeUsable && !agentRefuses && canPatch

	requiredNow := u != nil && u.RebootRequired
	stale := u != nil && u.Freshness() == string(models.ListsStale)
	pendingRC := u != nil && !stale && u.PendingRebootClassCount > 0
	enforcementBroken := u != nil && (u.HoldEnforcement == "no_privilege")
	agentCanRelease := agentCanReleaseRebootClass(u)
	autoRebootSetUp := autoPatchSetUp && gated && pendingRC &&
		(allowReboot == nil || *allowReboot) && agentCanRelease
	autoCovers := (requiredNow || pendingRC) && autoRebootSetUp

	// EXTRA-UPD-001 group K (amends NY-1 to NY-9): a reason is an action item
	// only when something is actually waiting and nothing will apply it. A
	// setting by itself, a hold with nothing under it, or a failure with nothing
	// pending is information. The conditions are pure facts of the telemetry.
	aptPending := u != nil && !stale && u.PendingUpdates > 0
	extras := badgeExtras(u)
	extraPending := false
	extraUnapplied := false
	for _, e := range extras {
		if e.Pending() {
			extraPending = true
			if !autoPatchSetUp || !e.Enabled() {
				extraUnapplied = true
			}
		}
	}
	waiting := aptPending || extraPending || requiredNow
	unapplied := (aptPending && !autoPatchSetUp) || extraUnapplied

	var actions, infos []patchBadgeReason
	legacy := 0 // NY-1 to NY-10 raised, whatever their class: the frozen lines read it
	raise := func(act bool, code, text, action string) *patchBadgeReason {
		legacy++
		r := patchBadgeReason{Code: code, Text: text, Action: action}
		if act {
			actions = append(actions, r)
			return &actions[len(actions)-1]
		}
		infos = append(infos, r)
		return &infos[len(infos)-1]
	}

	// NY-1 .. NY-3: settings.
	if !in.AutoPatch {
		raise(unapplied, reasonAutoPatchOff, "Automatic patching is off for this host.", "Turn on Auto-patch for this host.")
	}
	if !modeUsable {
		text := "Automatic patching is disabled server-wide."
		if mode == "window" {
			text = "Automatic patching is disabled server-wide (the OS-patch window has no upcoming start)."
		}
		raise(unapplied, reasonPatchingDisabled, text, "Set an OS-patch window in Settings > Agents.")
	}
	if agentRefuses {
		raise(unapplied, reasonAgentPatchingOff, "This agent does not allow patching.", "Set commands.allow_patching: true in the agent configuration.")
	}
	// NY-4: rIOt cannot patch the host. When it applies, the package, hold and
	// reboot reasons below are meaningless and are skipped. It needs action only
	// when updates are waiting on a host that reports them.
	if !canPatch {
		text := "rIOt cannot patch this host: no patch data has been reported."
		if u != nil {
			text = fmt.Sprintf("rIOt cannot patch this host: package manager %q is not supported.", safeLabel(u.PackageManager, "none"))
		}
		raise(aptPending, reasonCannotPatchHost, text, "Patch this host outside rIOt.")
	}
	if canPatch {
		// NY-5: holds not placed by rIOt. Action only when a held package has a
		// newer version available; anything the telemetry cannot show is information.
		if names, total := otherHolds(u); total > 0 {
			raise(heldWithUpdate(u, names), reasonHeldPackages,
				"Held or excluded packages not placed by rIOt: "+listNames(names, total)+".",
				"Release or update these packages by hand.")
		}
		// NY-6: rIOt holds kernel/GPU packages and nothing releases them. Action
		// only when such an update is actually pending.
		if u.HoldEnforcement == "active" && len(u.HeldPackages) > 0 && !gated {
			raise(pendingRC, reasonRebootClassHeld,
				"Kernel and GPU driver updates are held by rIOt and the reboot_class policy never releases them.",
				"Set reboot_class to gated in Settings > Agents, or update the kernel and GPU driver by hand.")
		}
		// NY-7: a configuration fault with nothing waiting by itself.
		if enforcementBroken {
			raise(false, reasonHoldEnforcementBroke, "Hold enforcement is not working on this host.", "Re-run the agent installer on this host.")
		}
		// NY-8: only an explicit "available" counts, and it is always waiting work.
		if ru := u.ReleaseUpgrade; ru != nil && ru.Status == models.FactStatusAvailable {
			text := "An OS release upgrade needs manual approval."
			if name := cleanReleaseName(ru.Name); name != "" {
				text = fmt.Sprintf("An OS release upgrade to %s needs manual approval.", name)
			}
			raise(true, reasonManualApproval, text, "Start the release upgrade by hand; rIOt never starts it.")
		}
	}
	// NY-9: latest finished run failed. A failure with nothing waiting is a fact
	// about the past: it stays visible as information and says so.
	if r := in.LastRun; r != nil && (r.Status == string(models.CommandFailed) || r.Status == string(models.CommandTimedOut)) {
		outcome := "failed"
		if r.Status == string(models.CommandTimedOut) {
			outcome = "timed out"
		}
		text := fmt.Sprintf("The last patch run %s: %s", outcome, runMessage(r.ResultMsg))
		if !waiting {
			text = sentenceEnd(text) + " Nothing is pending now."
		}
		reason := raise(waiting, reasonLastRunFailed, text, "Review the failed command, fix the cause and run the patch again.")
		if badgeUUIDRe.MatchString(r.ID) {
			reason.CommandID = r.ID
		}
	}
	// NY-10: a reboot is required now and nothing will do it. Unchanged.
	if canPatch && requiredNow && !autoCovers {
		causes := manualRebootCauses(u, pendingRC, gated, autoPatchSetUp)
		legacy++
		actions = append(actions, patchBadgeReason{
			Code:   reasonManualReboot,
			Text:   "A reboot is required now and nothing will reboot this host automatically: " + strings.Join(causes, "; ") + ".",
			Action: manualRebootAction(allowReboot),
		})
	}
	// Extra updaters (FR-051, FR-052, FR-071): one item per updater entry.
	for _, e := range extras {
		switch {
		case e.Pending() && !e.Enabled():
			text, action := extraNotAppliedText(e)
			actions = append(actions, patchBadgeReason{Code: reasonExtraNotApplied, Text: text, Action: action, Updater: e.ID})
		case e.Pending():
			text, action := extraPendingText(e, autoPatchSetUp, mode)
			infos = append(infos, patchBadgeReason{Code: reasonExtraPending, Text: text, Action: action, Updater: e.ID})
		case e.Status == models.ExtraStatusUnknown:
			text, action := extraUnknownText(e)
			infos = append(infos, patchBadgeReason{Code: reasonExtraUnknown, Text: text, Action: action, Updater: e.ID})
		}
	}

	b := patchBadge{Explanation: patchBadgeExplanation{NeedsYou: actions, Info: infos, Notes: []string{}}}
	if b.Explanation.NeedsYou == nil {
		b.Explanation.NeedsYou = []patchBadgeReason{}
	}
	if b.Explanation.Info == nil {
		b.Explanation.Info = []patchBadgeReason{}
	}

	// Badge kind, reboot line and reboot time. Needs you only for an action item;
	// otherwise Manual when nothing will patch the host automatically, else the
	// automatic kinds, whose rules are unchanged.
	dated := mode == "window" && allowReboot != nil && *allowReboot
	b.Explanation.Reboot = rebootLine(rebootFacts{
		requiredNow: requiredNow, pendingRC: pendingRC, autoCovers: autoCovers,
		reasons: legacy, dated: dated, at: nextStart, why: unknownDateReasons(mode, allowReboot),
	})
	switch {
	case len(actions) > 0:
		b.Kind, b.Label = badgeNeedsYou, "Needs you"
	case !autoPatchSetUp:
		b.Kind, b.Label = badgeManual, "Manual"
	case !autoCovers:
		b.Kind, b.Label = badgeAuto, "Auto"
	case dated:
		at := nextStart
		b.Kind, b.Label, b.RebootAt = badgeAutoRebootDate, "Auto (reboot on "+at.Format("2006-01-02")+")", &at
	default:
		b.Kind, b.Label = badgeAutoRebootUnknown, "Auto (reboot date Unknown)"
	}

	// Line 1: automatic patching.
	switch {
	case !autoPatchSetUp:
		b.Explanation.Automatic = "No."
	case mode == "window":
		b.Explanation.Automatic = "Yes. Next window starts " + nextStart.Format("2006-01-02 15:04") + " UTC."
	default:
		b.Explanation.Automatic = fmt.Sprintf("Yes. Any time (within the %d-minute cooldown).", in.OSPatch.CooldownMinutes)
	}

	// Line 3 summary: action items only (FR-080).
	switch len(actions) {
	case 0:
		b.Explanation.NeedsYouSummary = "Nothing."
	case 1:
		b.Explanation.NeedsYouSummary = "1 item needs you."
	default:
		b.Explanation.NeedsYouSummary = fmt.Sprintf("%d items need you.", len(actions))
	}

	// Line 4: as of.
	b.Explanation.AsOf = asOfText(in.UpdatesAt, now)

	b.Explanation.Notes = patchBadgeNotes(in, u)
	return b
}

// sentenceEnd makes sure a text ends a sentence before another is appended.
func sentenceEnd(s string) string {
	if s == "" || strings.ContainsRune(".!?", rune(s[len(s)-1])) {
		return s
	}
	return s + "."
}

// heldWithUpdate reports whether any listed non-rIOt hold also appears in the
// host's pending update list (PATCH-FLOW-001 OQ-A8 answer). `apt list
// --upgradable` includes held packages, so a name in both lists is a held
// package with a newer version. Stale lists, a manager other than apt, no
// pending list and no overlap all read as "cannot tell", which is not action.
func heldWithUpdate(u *models.UpdateInfo, held []string) bool {
	if u == nil || len(held) == 0 || u.PackageManager != "apt" || u.Freshness() == string(models.ListsStale) {
		return false
	}
	upgradable := make(map[string]bool, len(u.Updates))
	for _, p := range u.Updates {
		upgradable[p.Name] = true
	}
	for _, name := range held {
		if upgradable[name] {
			return true
		}
	}
	return false
}

// badgeExtras returns the extra-updater entries the badge may show: at most eight,
// each with display-safe versions. The decoder already normalised the entry; this
// is the second look before text reaches the page.
func badgeExtras(u *models.UpdateInfo) []models.ExtraUpdater {
	if u == nil {
		return nil
	}
	var out []models.ExtraUpdater
	for _, e := range u.ExtraUpdaters {
		if len(out) >= models.MaxExtraUpdaters || !models.ExtraUpdaterIDPattern.MatchString(e.ID) {
			continue
		}
		e.InstalledVersion = models.TruncateHead(stripUnsafeText(e.InstalledVersion), models.MaxExtraUpdaterVersion)
		e.AvailableVersion = models.TruncateHead(stripUnsafeText(e.AvailableVersion), models.MaxExtraUpdaterVersion)
		out = append(out, e)
	}
	return out
}

// extraLabel is the name an updater goes by in text.
func extraLabel(id string) string {
	if id == models.ExtraUpdaterDietPi {
		return "DietPi"
	}
	return id
}

// extraChange names the update: the updater and, when known, both versions.
func extraChange(e models.ExtraUpdater) string {
	label := extraLabel(e.ID)
	switch {
	case e.InstalledVersion != "" && e.AvailableVersion != "":
		return label + " " + e.InstalledVersion + " to " + e.AvailableVersion
	case e.AvailableVersion != "":
		return label + " update to " + e.AvailableVersion
	}
	return label + " update"
}

// extraNotAppliedText is FR-071: the update, why it will not be applied, and
// what the operator does about it.
func extraNotAppliedText(e models.ExtraUpdater) (text, action string) {
	var why string
	switch e.Readiness {
	case models.ExtraReadinessNotOptedIn:
		why = "not opted in"
		action = "Add " + e.ID + " to commands.allow_extra_updaters in /etc/riot/agent.yaml and re-run install.sh as root on this host."
	case models.ExtraReadinessRootSideMissing:
		why = "the root-side opt-in is missing"
		action = "Re-run install.sh as root on this host."
	case models.ExtraReadinessPatchingDisabled:
		why = "patching is turned off on the agent"
		action = "Set commands.allow_patching: true in /etc/riot/agent.yaml."
	case models.ExtraReadinessUnsupportedHelper:
		why = "the installed maintenance helper is too old"
		action = "Re-run install.sh as root on this host to install the newer maintenance helper."
	case models.ExtraReadinessNonDefaultUpdateSource:
		why = "its update source is not the default repository"
		action = "Remove DEV_GITOWNER from /boot/dietpi.txt: rIOt only runs DietPi from the MichaIng repository."
	default:
		why = "its apply readiness is unknown"
		action = "Run riot-agent doctor on this host."
	}
	return extraChange(e) + " is waiting; not applied: " + why + ".", action
}

// extraPendingText is FR-051 for an updater that will be applied: how.
func extraPendingText(e models.ExtraUpdater, autoPatchSetUp bool, mode string) (text, action string) {
	switch {
	case !autoPatchSetUp:
		return extraChange(e) + " is waiting; it will be applied only by a manual Patch.", "Turn on Auto-patch for this host, or patch it by hand."
	case mode == "window":
		return extraChange(e) + " is waiting; it will be applied automatically in the patch window.", "Nothing to do."
	}
	return extraChange(e) + " is waiting; it will be applied automatically at any time (within the cooldown).", "Nothing to do."
}

// extraUnknownText is FR-052: an updater whose state is not known is never
// shown as up to date and never asks for action.
func extraUnknownText(e models.ExtraUpdater) (text, action string) {
	label := extraLabel(e.ID)
	var why string
	switch e.UnknownReason {
	case models.ExtraReasonCheckDisabled:
		why = "its own update check is turned off"
	case models.ExtraReasonConfigUnreadable:
		why = "its settings file could not be read"
	case models.ExtraReasonNotCheckedSinceBoot:
		why = "it has not run its update check since the host booted"
	case models.ExtraReasonVersionUnreadable:
		why = "the installed version could not be read"
	case models.ExtraReasonDetectionFailed:
		why = "detection did not finish"
	default:
		why = "the reason was not reported"
	}
	return label + " update status is unknown: " + why + ". rIOt will not apply it until an update is known to be waiting.", "No action needed unless you expect an update."
}

// pendingKernelLine opens every Reboot line that describes a future reboot.
const pendingKernelLine = "The pending patch includes a kernel/GPU update; a reboot will be needed after it installs."

// staleListsNote explains a badge that ignores the pending-update list (FR-036).
const staleListsNote = "Package lists are out of date on this host. Pending updates will be recounted after the next refresh; running Patch refreshes the package lists first."

// rebootFacts is what the Reboot line depends on.
type rebootFacts struct {
	requiredNow bool
	pendingRC   bool
	autoCovers  bool
	reasons     int
	dated       bool
	at          time.Time
	why         string
}

// rebootLine renders the Reboot line (PATCH-FLOW-001 AD-001). "Required now" and
// "will be needed" are different facts and never share wording.
func rebootLine(f rebootFacts) string {
	switch {
	case !f.requiredNow && !f.pendingRC:
		return "Not needed."
	case f.requiredNow && !f.autoCovers:
		return "Required now: reboot this host now; nothing will reboot it automatically."
	case !f.autoCovers:
		return pendingKernelLine + " It will not happen automatically."
	}
	head := pendingKernelLine + " It will happen automatically"
	if f.requiredNow {
		head = "Required now. Automatic"
	}
	switch {
	case f.reasons > 0:
		return head + " once the items below are resolved (date not shown)."
	case f.dated:
		return head + " on " + f.at.Format("2006-01-02 15:04") + " UTC (estimate)."
	default:
		return head + ", date Unknown: " + f.why + "."
	}
}

// unknownDateReasons explains why an automatic reboot has no predicted date.
func unknownDateReasons(mode string, allowReboot *bool) string {
	var why []string
	if mode == "anytime" {
		why = append(why, "the reboot happens on the next automatic patch run and the time is not predicted")
	}
	if allowReboot == nil {
		why = append(why, "this agent is too old to report allow_reboot; update the agent")
	}
	return strings.Join(why, "; ")
}

// manualRebootCauses lists, in order, why no automatic reboot will follow. It is
// never empty when a reboot is required now and not covered: either no pending
// kernel/GPU update exists to trigger one, or the automatic path is missing a
// condition listed here.
func manualRebootCauses(u *models.UpdateInfo, pendingRC, gated, autoPatchSetUp bool) []string {
	var causes []string
	if !pendingRC {
		causes = append(causes, "rIOt reboots automatically only after an automatic patch run installs a kernel or GPU update")
	}
	if !gated {
		causes = append(causes, "the reboot_class policy is not gated")
	}
	if u.AllowReboot != nil && !*u.AllowReboot {
		causes = append(causes, "allow_reboot is off on the agent")
	}
	if u.HoldRebootClass != nil && !*u.HoldRebootClass {
		causes = append(causes, "hold_reboot_class is off on the agent")
	}
	if u.HoldEnforcement == "unsupported" || u.HoldEnforcement == "no_privilege" {
		causes = append(causes, "hold enforcement is not working on the agent")
	}
	if !autoPatchSetUp {
		causes = append(causes, "automatic patching is not set up")
	}
	return causes
}

// manualRebootAction tells the operator to reboot now; when the agent forbids
// reboots it says the reboot must happen outside rIOt.
func manualRebootAction(allowReboot *bool) string {
	if allowReboot != nil && !*allowReboot {
		return "Reboot this host now, outside rIOt (commands.allow_reboot is off on the agent)."
	}
	return "Reboot this host now."
}

// agentCanReleaseRebootClass reports whether the agent can apply reboot-class
// packages during an automatic run. Unknown counts as yes.
func agentCanReleaseRebootClass(u *models.UpdateInfo) bool {
	if u == nil {
		return false
	}
	if u.HoldRebootClass != nil && !*u.HoldRebootClass {
		return false
	}
	return u.HoldEnforcement != "unsupported" && u.HoldEnforcement != "no_privilege"
}

// patchBadgeNotes builds the optional notes: the unattended-upgrades caveat,
// facts that could not be checked, and the newer-agent line.
func patchBadgeNotes(in patchBadgeInput, u *models.UpdateInfo) []string {
	notes := []string{}
	if u == nil {
		return notes
	}
	if u.Freshness() == string(models.ListsStale) {
		notes = append(notes, staleListsNote)
	}
	if u.UnattendedUpgrades {
		notes = append(notes, "unattended-upgrades is installed on this host. rIOt cannot see what it applies or whether it reboots, and it does not count as automatic patching by rIOt.")
	}
	if u.OtherHolds != nil && u.OtherHolds.Status != models.FactStatusOK {
		notes = append(notes, "Held and excluded packages could not be checked on this host.")
	}
	if u.ReleaseUpgrade != nil && u.ReleaseUpgrade.Status == models.FactStatusUnavailable {
		notes = append(notes, "Whether a new OS release needs manual approval could not be checked on this host.")
	}
	if u.AllowPatching == nil || u.OtherHolds == nil || u.ReleaseUpgrade == nil {
		line := "Some checks need a newer agent"
		if in.AgentFactsMinVersion != "" {
			line += " (" + in.AgentFactsMinVersion + " or later)"
		}
		notes = append(notes, line+".")
	}
	return notes
}

// otherHolds returns the validated hold names not placed by rIOt and the count
// of such holds. Agent data is re-validated here: status, charset, length and
// count limits.
func otherHolds(u *models.UpdateInfo) (names []string, total int) {
	oh := u.OtherHolds
	if oh == nil || oh.Status != models.FactStatusOK {
		return nil, 0
	}
	managed := make(map[string]bool, len(u.HeldPackages))
	for _, m := range u.HeldPackages {
		managed[m] = true
	}
	total = oh.Total
	if total < 0 {
		total = 0
	}
	seen := map[string]bool{}
	for _, n := range oh.Packages {
		if len(names) >= maxBadgeHolds {
			break
		}
		if len(n) > maxBadgeNameLen || !badgePackageRe.MatchString(n) || seen[n] {
			continue
		}
		seen[n] = true
		if managed[n] {
			if total > 0 {
				total--
			}
			continue
		}
		names = append(names, n)
	}
	if total < len(names) {
		total = len(names)
	}
	return names, total
}

// listNames renders up to maxBadgeListedPackages names and "and N more".
func listNames(names []string, total int) string {
	shown := names
	if len(shown) > maxBadgeListedPackages {
		shown = shown[:maxBadgeListedPackages]
	}
	s := strings.Join(shown, ", ")
	if s == "" {
		s = "names not reported"
	}
	if more := total - len(shown); more > 0 && len(shown) > 0 {
		s += fmt.Sprintf(" and %d more", more)
	} else if more > 0 {
		s = fmt.Sprintf("%d packages (names not reported)", total)
	}
	return s
}

// cleanReleaseName returns the release label when it passes the charset and
// length checks, otherwise "".
func cleanReleaseName(name string) string {
	name = strings.TrimSpace(models.SanitizeText(name))
	if name == "" || len(name) > maxBadgeRelease || !badgeReleaseRe.MatchString(name) {
		return ""
	}
	return name
}

// safeLabel makes agent text safe for display and falls back when empty.
func safeLabel(s, fallback string) string {
	s = stripUnsafeText(s)
	if s == "" {
		return fallback
	}
	return models.TruncateHead(s, 32)
}

// runMessage returns the failed-run message with control, ANSI and
// bidirectional-control characters removed, cut to maxBadgeRunMessage characters.
func runMessage(msg string) string {
	msg = stripUnsafeText(msg)
	if msg == "" {
		return "no message was recorded."
	}
	runes := []rune(msg)
	if len(runes) > maxBadgeRunMessage {
		return string(runes[:maxBadgeRunMessage]) + "..."
	}
	return msg
}

// stripUnsafeText removes ANSI sequences, control characters and bidi
// controls, and collapses whitespace.
func stripUnsafeText(s string) string {
	s = models.SanitizeText(s)
	s = ansiRe.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		case r == '؜' || r == '‎' || r == '‏' ||
			(r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩'):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// asOfText renders the patch-data time in UTC with its age.
func asOfText(at *time.Time, now time.Time) string {
	if at == nil {
		return "No patch data reported yet."
	}
	t := at.UTC()
	age := now.Sub(t)
	var ago string
	switch {
	case age < time.Minute:
		ago = "just now"
	case age < time.Hour:
		ago = fmt.Sprintf("%d min ago", int(age.Minutes()))
	case age < 48*time.Hour:
		ago = fmt.Sprintf("%d h ago", int(age.Hours()))
	default:
		ago = fmt.Sprintf("%d d ago", int(age.Hours()/24))
	}
	return t.Format("2006-01-02 15:04") + " UTC (" + ago + ")"
}
