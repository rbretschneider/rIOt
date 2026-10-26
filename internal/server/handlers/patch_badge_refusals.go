package handlers

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Refusals in the badge (DIETPI-PATH-001 FR-015 to FR-017, AD-5). A refusal is a
// skip that is not neutral: an updater was pending and wanted, and rIOt chose
// not to start it. The last finished os_update tells which, in the first line
// of its stored result. Everything parsed here comes from the host's own agent
// text, so it is bounded, matched with Go's linear-time regexp only, stripped
// of control characters, and shown as plain text.

const (
	// maxRefusalMessage is how much of a stored result is ever read (DP-SEC-007).
	maxRefusalMessage = 2048
	// maxRefusalParts is how many updater entries of the first line are read.
	maxRefusalParts = 8
	// maxRefusalDetail bounds the detail shown beside a refusal, in bytes.
	maxRefusalDetail = 160
)

// refusalFirstLine is the first line of a result whose apt part succeeded.
var refusalFirstLine = regexp.MustCompile(`^apt: succeeded((?:; [a-z][a-z0-9_-]{0,31}: [a-z_ ()0-9]+)+)$`)

// refusalSkipWords matches the words of a skipped updater part.
var refusalSkipWords = regexp.MustCompile(`^skipped \(([a-z_]+)\)$`)

// refusalPathRe is a path the detail of unsafe_dietpi_path may name.
var refusalPathRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,120}$`)

// Refusal classes (DP-SEC-001).
const (
	// refusalBlocked: rIOt's own isolation check or a temporary condition. The
	// operator has nothing to do, so it never counts as "needs you".
	refusalBlocked = iota
	// refusalAction: something only the operator can change or look into.
	refusalAction
)

// refusalReasons are the non-neutral skip reasons, the closed set a refusal may
// carry. apt_not_successful is not one: it follows an apt failure.
var refusalReasons = map[string]bool{
	maintenance.SkipUnsafePath: true, maintenance.SkipMinInterval: true, maintenance.SkipTmpNotPrivate: true,
	maintenance.SkipMountPropagationUnver: true, maintenance.SkipRunDirNotPrivate: true, maintenance.SkipBusy: true,
	maintenance.SkipDpkgLocked: true, maintenance.SkipInsufficientTime: true, maintenance.SkipNonDefaultSource: true,
}

// neutralPartWords are updater parts of a first line that carry no failure.
var neutralPartWords = map[string]bool{
	"succeeded":                      true,
	"skipped (not_enabled)":          true,
	"skipped (status_unknown)":       true,
	"skipped (nothing_pending)":      true,
	"skipped (excluded_by_operator)": true,
}

// refusal is one refused updater of the last run.
type refusal struct {
	Updater string
	Reason  string
	Detail  string
}

// refusalRun is what the last run says about refusals. Sole is true when the run
// is a failed one whose only cause is its refusals: a result of a worker older
// than this fix, which failed a run for a skip (FR-016, DP-SEC-004).
type refusalRun struct {
	Refusals []refusal
	Sole     bool
}

// parseRefusals reads the stored result of the latest finished os_update. At
// most the first 2048 bytes are looked at, split into lines, with at most eight
// updater entries on the first line. Anything that does not match exactly is
// ignored, so a malformed or hostile result raises nothing and changes nothing.
func parseRefusals(run *models.Command) refusalRun {
	if run == nil {
		return refusalRun{}
	}
	failed := run.Status == string(models.CommandFailed)
	if !failed && run.Status != string(models.CommandSucceeded) {
		return refusalRun{}
	}
	msg := run.ResultMsg
	if len(msg) > maxRefusalMessage {
		msg = msg[:maxRefusalMessage]
	}
	lines := strings.Split(msg, "\n")
	m := refusalFirstLine.FindStringSubmatch(lines[0])
	if m == nil {
		return refusalRun{}
	}
	parts := strings.Split(strings.TrimPrefix(m[1], "; "), "; ")
	if len(parts) > maxRefusalParts {
		return refusalRun{}
	}
	var out refusalRun
	ids := map[string]bool{}
	other := false
	for _, part := range parts {
		id, words, _ := strings.Cut(part, ": ")
		ids[id] = true
		if sm := refusalSkipWords.FindStringSubmatch(words); sm != nil && refusalReasons[sm[1]] {
			out.Refusals = append(out.Refusals, refusal{Updater: id, Reason: sm[1], Detail: refusalDetail(lines[1:], id, sm[1])})
			continue
		}
		if !neutralPartWords[words] {
			other = true
		}
	}
	if len(out.Refusals) == 0 {
		return refusalRun{}
	}
	if !failed {
		return out
	}
	// A failed run is reinterpreted only when the refusal is the sole cause: no
	// updater part failed, line 2 is exactly the success summary of the apt
	// part, and every later line is the detail line of a listed updater.
	if other || !successLine(lines) || !onlyDetailLines(lines[2:], ids) {
		return refusalRun{}
	}
	out.Sole = true
	return out
}

// successLine reports that line 2 is the worker's success summary, with the
// optional post-run note.
func successLine(lines []string) bool {
	if len(lines) < 2 {
		return false
	}
	return lines[1] == maintenance.SuccessSummary || strings.HasPrefix(lines[1], maintenance.SuccessSummary+"; ")
}

// onlyDetailLines reports that every non-empty line starts with "<id>: " for an
// updater of the first line.
func onlyDetailLines(lines []string, ids map[string]bool) bool {
	for _, line := range lines {
		if line == "" {
			continue
		}
		id, _, found := strings.Cut(line, ": ")
		if !found || !ids[id] {
			return false
		}
	}
	return true
}

// refusalDetail finds the detail line of one refusal: the first line starting
// "<id>: skipped (<reason>); ". The rest is stripped of control characters and
// cut to 160 bytes.
func refusalDetail(lines []string, id, reason string) string {
	prefix := id + ": skipped (" + reason + "); "
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return truncateBytes(stripUnsafeText(rest), maxRefusalDetail)
		}
	}
	return ""
}

// truncateBytes cuts s to at most n bytes without splitting a character.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// legacyRunDirDetail marks the stock false positive of a worker older than
// this fix, whose trust walk refused DietPi's own 0777 /run/dietpi.
const legacyRunDirDetailPrefix = "/run/dietpi "

// refusalClassOf classifies a refusal (DP-SEC-001). unsafe_dietpi_path is an
// action, since stock hosts now pass: it means DietPi's code, its settings or
// /var/tmp/dietpi is not root-only. The one exception is the false positive of
// an older worker.
func refusalClassOf(reason, detail string) int {
	switch reason {
	case maintenance.SkipUnsafePath:
		if strings.HasPrefix(detail, legacyRunDirDetailPrefix) {
			return refusalBlocked
		}
		return refusalAction
	case maintenance.SkipNonDefaultSource, maintenance.SkipInsufficientTime:
		return refusalAction
	}
	return refusalBlocked
}

// refusalWords is the plain-language reason of a refusal.
func refusalWords(reason, detail string) string {
	switch reason {
	case maintenance.SkipUnsafePath:
		if fields := strings.Fields(detail); len(fields) > 0 && refusalPathRe.MatchString(fields[0]) {
			return fields[0] + " permissions"
		}
		return "a DietPi path failed rIOt's safety check"
	case maintenance.SkipTmpNotPrivate:
		return "rIOt could not isolate /tmp"
	case maintenance.SkipMountPropagationUnver:
		return "mount propagation could not be verified"
	case maintenance.SkipRunDirNotPrivate:
		return "rIOt could not isolate /run/dietpi"
	case maintenance.SkipBusy:
		return "another DietPi process was running"
	case maintenance.SkipMinInterval:
		return "DietPi ran less than an hour earlier"
	case maintenance.SkipDpkgLocked:
		return "the package lock was held"
	case maintenance.SkipNonDefaultSource:
		return "the host's update source is not the default repository"
	case maintenance.SkipInsufficientTime:
		return "the patch window was too short for the step"
	}
	return strings.ReplaceAll(reason, "_", " ")
}

// isolationSuffix is added to the Blocked text of the reasons that mean rIOt's
// own isolation check failed. It must not promise that a retry fixes them.
const isolationSuffix = "; this host's mount layout prevented rIOt from isolating the step"

// legacyRunDirSuffix is the Blocked text of the older worker's false positive.
const legacyRunDirSuffix = " (fixed by this rIOt release; re-run install.sh as root on this host to apply the fix)"

// refusalText is the explanation item of a refusal and the action that goes
// with it, for either class.
func refusalText(label, reason, detail string) (text, action string) {
	text = label + " update blocked: " + refusalWords(reason, detail)
	switch reason {
	case maintenance.SkipUnsafePath:
		if strings.HasPrefix(detail, legacyRunDirDetailPrefix) {
			text += legacyRunDirSuffix
			action = "Re-run install.sh as root on this host to apply the fix."
		} else {
			action = "Investigate the named path on this host (owner and permissions); rIOt will not run DietPi until it is root-only."
		}
	case maintenance.SkipTmpNotPrivate, maintenance.SkipMountPropagationUnver, maintenance.SkipRunDirNotPrivate:
		text += isolationSuffix
		action = "Run riot-agent doctor on this host."
	case maintenance.SkipNonDefaultSource:
		action = "Set DEV_GITOWNER back to MichaIng in /boot/dietpi.txt."
	case maintenance.SkipInsufficientTime:
		action = "Lengthen this host's patch window to at least 90 minutes."
	default:
		action = "Nothing to do; rIOt retries on the next patch run."
	}
	if detail != "" {
		text += ". " + detail
	}
	return text, action
}

// refusalInfoText is the single information line of a refusal whose updater is
// no longer pending.
func refusalInfoText(label, reason, detail string) string {
	return "The last patch run skipped " + label + ": " + refusalWords(reason, detail) + "."
}
