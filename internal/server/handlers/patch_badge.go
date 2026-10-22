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
)

// patchBadgeReasonCodes lists every code computePatchBadge can raise.
var patchBadgeReasonCodes = []string{
	reasonAutoPatchOff, reasonPatchingDisabled, reasonAgentPatchingOff, reasonCannotPatchHost,
	reasonHeldPackages, reasonRebootClassHeld, reasonHoldEnforcementBroke, reasonManualApproval,
	reasonLastRunFailed, reasonManualReboot,
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

// patchBadgeReason is one needs-you item.
type patchBadgeReason struct {
	Code      string `json:"code"`
	Text      string `json:"text"`
	Action    string `json:"action"`
	CommandID string `json:"command_id,omitempty"`
}

// patchBadgeExplanation is the four-line explanation: Automatic patching,
// Reboot, Needs you (summary plus items) and As of, plus optional notes.
type patchBadgeExplanation struct {
	Automatic       string             `json:"automatic"`
	Reboot          string             `json:"reboot"`
	NeedsYou        []patchBadgeReason `json:"needs_you"`
	NeedsYouSummary string             `json:"needs_you_summary"`
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
func computePatchBadge(in patchBadgeInput, now time.Time) patchBadge {
	now = now.UTC()
	u := in.Updates
	mode := in.OSPatch.Mode
	nextStart, haveStart := nextMaintenanceWindowStart(in.OSPatch, now)

	allowReboot := (*bool)(nil)
	holdRebootClass := (*bool)(nil)
	allowPatching := (*bool)(nil)
	if u != nil {
		allowReboot, holdRebootClass, allowPatching = u.AllowReboot, u.HoldRebootClass, u.AllowPatching
	}
	gated := in.OSPatch.RebootClass == "gated"

	modeUsable := mode == "anytime" || (mode == "window" && haveStart)
	agentRefuses := allowPatching != nil && !*allowPatching
	canPatch := u != nil && (u.PackageManager == "apt" || u.PackageManager == "dnf")
	autoPatchSetUp := in.AutoPatch && modeUsable && !agentRefuses && canPatch

	rebootExpected := u != nil && (u.RebootRequired || u.PendingRebootClassCount > 0)
	enforcementBroken := u != nil && (u.HoldEnforcement == "no_privilege")
	agentCanRelease := agentCanReleaseRebootClass(u)
	autoRebootSetUp := autoPatchSetUp && gated && u != nil && u.PendingRebootClassCount > 0 &&
		(allowReboot == nil || *allowReboot) && agentCanRelease

	var reasons []patchBadgeReason
	add := func(code, text, action string) {
		reasons = append(reasons, patchBadgeReason{Code: code, Text: text, Action: action})
	}

	// NY-1 .. NY-3: settings.
	if !in.AutoPatch {
		add(reasonAutoPatchOff, "Automatic patching is off for this host.", "Turn on Auto-patch for this host.")
	}
	if !modeUsable {
		text := "Automatic patching is disabled server-wide."
		if mode == "window" {
			text = "Automatic patching is disabled server-wide (the OS-patch window has no upcoming start)."
		}
		add(reasonPatchingDisabled, text, "Set an OS-patch window in Settings > Agents.")
	}
	if agentRefuses {
		add(reasonAgentPatchingOff, "This agent does not allow patching.", "Set commands.allow_patching: true in the agent configuration.")
	}
	// NY-4: rIOt cannot patch the host. When it applies, the package, hold and
	// reboot reasons below are meaningless and are skipped.
	if !canPatch {
		text := "rIOt cannot patch this host: no patch data has been reported."
		if u != nil {
			text = fmt.Sprintf("rIOt cannot patch this host: package manager %q is not supported.", safeLabel(u.PackageManager, "none"))
		}
		add(reasonCannotPatchHost, text, "Patch this host outside rIOt.")
	}
	if canPatch {
		// NY-5: holds not placed by rIOt.
		if names, total := otherHolds(u); total > 0 {
			add(reasonHeldPackages,
				"Held or excluded packages not placed by rIOt: "+listNames(names, total)+".",
				"Release or update these packages by hand.")
		}
		// NY-6: rIOt holds kernel/GPU packages and nothing releases them.
		if u.HoldEnforcement == "active" && len(u.HeldPackages) > 0 && !gated {
			add(reasonRebootClassHeld,
				"Kernel and GPU driver updates are held by rIOt and the reboot_class policy never releases them.",
				"Set reboot_class to gated in Settings > Agents, or update the kernel and GPU driver by hand.")
		}
		// NY-7
		if enforcementBroken {
			add(reasonHoldEnforcementBroke, "Hold enforcement is not working on this host.", "Re-run the agent installer on this host.")
		}
		// NY-8: only an explicit "available" counts.
		if ru := u.ReleaseUpgrade; ru != nil && ru.Status == models.FactStatusAvailable {
			text := "An OS release upgrade needs manual approval."
			if name := cleanReleaseName(ru.Name); name != "" {
				text = fmt.Sprintf("An OS release upgrade to %s needs manual approval.", name)
			}
			add(reasonManualApproval, text, "Start the release upgrade by hand; rIOt never starts it.")
		}
	}
	// NY-9: latest finished run failed.
	if r := in.LastRun; r != nil && (r.Status == string(models.CommandFailed) || r.Status == string(models.CommandTimedOut)) {
		outcome := "failed"
		if r.Status == string(models.CommandTimedOut) {
			outcome = "timed out"
		}
		reason := patchBadgeReason{
			Code:   reasonLastRunFailed,
			Text:   fmt.Sprintf("The last patch run %s: %s", outcome, runMessage(r.ResultMsg)),
			Action: "Review the failed command, fix the cause and run the patch again.",
		}
		if badgeUUIDRe.MatchString(r.ID) {
			reason.CommandID = r.ID
		}
		reasons = append(reasons, reason)
	}
	// NY-10: a reboot is expected and nothing will do it.
	var rebootCauses []string
	if canPatch && rebootExpected && !autoRebootSetUp {
		if u.PendingRebootClassCount == 0 {
			rebootCauses = append(rebootCauses, "no kernel or GPU update is pending")
		}
		if !gated {
			rebootCauses = append(rebootCauses, "the reboot_class policy is not gated")
		}
		if allowReboot != nil && !*allowReboot {
			rebootCauses = append(rebootCauses, "allow_reboot is off on the agent")
		}
		if holdRebootClass != nil && !*holdRebootClass {
			rebootCauses = append(rebootCauses, "hold_reboot_class is off on the agent")
		}
		if u.HoldEnforcement == "unsupported" || enforcementBroken {
			rebootCauses = append(rebootCauses, "hold enforcement is not working on the agent")
		}
		if !autoPatchSetUp {
			rebootCauses = append(rebootCauses, "automatic patching is not set up")
		}
		add(reasonManualReboot,
			"A reboot is needed and no automatic reboot is set up: "+strings.Join(rebootCauses, "; ")+".",
			"Reboot this host by hand.")
	}

	b := patchBadge{Explanation: patchBadgeExplanation{NeedsYou: reasons, Notes: []string{}}}
	if b.Explanation.NeedsYou == nil {
		b.Explanation.NeedsYou = []patchBadgeReason{}
	}

	// Badge kind, reboot line and reboot time.
	switch {
	case len(reasons) > 0:
		b.Kind, b.Label = badgeNeedsYou, "Needs you"
		switch {
		case !rebootExpected:
			b.Explanation.Reboot = "Not needed."
		case autoRebootSetUp:
			b.Explanation.Reboot = "Automatic once the items below are resolved (date not shown)."
		default:
			b.Explanation.Reboot = "Manual: you must reboot this host."
		}
	case !rebootExpected:
		b.Kind, b.Label = badgeAuto, "Auto"
		b.Explanation.Reboot = "Not needed."
	case mode == "window" && allowReboot != nil && *allowReboot:
		at := nextStart
		b.Kind, b.Label, b.RebootAt = badgeAutoRebootDate, "Auto (reboot on "+at.Format("2006-01-02")+")", &at
		b.Explanation.Reboot = "Automatic on " + at.Format("2006-01-02 15:04") + " UTC (estimate)."
	default:
		b.Kind, b.Label = badgeAutoRebootUnknown, "Auto (reboot date Unknown)"
		var why []string
		if mode == "anytime" {
			why = append(why, "the reboot happens on the next automatic patch run and the time is not predicted")
		}
		if allowReboot == nil {
			why = append(why, "this agent is too old to report allow_reboot; update the agent")
		}
		b.Explanation.Reboot = "Automatic, date Unknown: " + strings.Join(why, "; ") + "."
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

	// Line 3 summary.
	switch len(reasons) {
	case 0:
		b.Explanation.NeedsYouSummary = "Nothing."
	case 1:
		b.Explanation.NeedsYouSummary = "1 item needs you."
	default:
		b.Explanation.NeedsYouSummary = fmt.Sprintf("%d items need you.", len(reasons))
	}

	// Line 4: as of.
	b.Explanation.AsOf = asOfText(in.UpdatesAt, now)

	b.Explanation.Notes = patchBadgeNotes(in, u)
	return b
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
