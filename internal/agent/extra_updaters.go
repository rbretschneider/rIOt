package agent

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Extra updaters in the OS patch flow (EXTRA-UPD-001, AD-010 and AD-011). The
// agent never runs an updater: it detects what is present, works out whether an
// updater is enabled, asks the root worker for it with a version 2 request when
// it is, and reports what the worker recorded. Every privileged decision is
// made again by the worker from root-owned facts.

// extraFactsTTL is how long the root-side facts are reused when none of the
// files they come from has changed.
const extraFactsTTL = 10 * time.Minute

// maxCapabilitiesBytes bounds the helper's answer to --capabilities.
const maxCapabilitiesBytes = 256

var capabilityID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// helperCaps is what `riot-maintenance --capabilities` printed.
type helperCaps struct {
	versions []int
	updaters []string
}

// supports reports whether the installed helper accepts a version 2 request
// that names id.
func (c helperCaps) supports(id string) bool {
	version2 := false
	for _, v := range c.versions {
		if v == maintenance.RequestVersionExtra {
			version2 = true
		}
	}
	if !version2 {
		return false
	}
	for _, u := range c.updaters {
		if u == id {
			return true
		}
	}
	return false
}

// parseCapabilities is a closed parser: at most 256 bytes, exactly the two keys
// request-versions and extra-updaters, in that order. Anything else means an old
// or unknown helper (AD-002).
func parseCapabilities(out string) (helperCaps, bool) {
	var caps helperCaps
	if out == "" || len(out) > maxCapabilitiesBytes {
		return caps, false
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 {
		return caps, false
	}
	versions, ok := strings.CutPrefix(lines[0], "request-versions=")
	if !ok {
		return caps, false
	}
	updaters, ok := strings.CutPrefix(lines[1], "extra-updaters=")
	if !ok {
		return caps, false
	}
	for _, v := range strings.Split(versions, ",") {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 9 {
			return helperCaps{}, false
		}
		caps.versions = append(caps.versions, n)
	}
	if updaters != "" {
		for _, u := range strings.Split(updaters, ",") {
			if !capabilityID.MatchString(u) {
				return helperCaps{}, false
			}
			caps.updaters = append(caps.updaters, u)
		}
	}
	return caps, true
}

// extraFacts are the root-side facts readiness is computed from.
type extraFacts struct {
	caps   helperCaps
	capsOK bool

	policy   []string
	policyOK bool

	unit   maintenance.UnitState
	unitOK bool

	at  time.Time
	key string
}

// extraState is the agent's per-process extra-updater state.
type extraState struct {
	mu    sync.Mutex
	facts extraFacts
	have  bool
	// fs overrides the filesystem detection reads; nil is the real root. Tests only.
	fs extraupd.FS
}

func (a *Agent) extraFS() extraupd.FS {
	if a.extra.fs != nil {
		return a.extra.fs
	}
	return extraupd.OSRoot()
}

// extraRootFacts reads the helper capabilities, the root policy and the unit's
// effective backstop. They are reused for ten minutes while the helper, the
// policy file and the drop-in are unchanged (stat signature).
func (a *Agent) extraRootFacts(ctx context.Context) extraFacts {
	m := a.maintenance()
	key := ""
	if m.signature != nil {
		key = m.signature()
	}
	now := a.clock().Now()
	a.extra.mu.Lock()
	if a.extra.have && key != "" && key == a.extra.facts.key && now.Sub(a.extra.facts.at) < extraFactsTTL {
		facts := a.extra.facts
		a.extra.mu.Unlock()
		return facts
	}
	a.extra.mu.Unlock()

	facts := extraFacts{at: now, key: key}
	if m.capabilities != nil {
		if out, err := m.capabilities(ctx); err == nil {
			facts.caps, facts.capsOK = parseCapabilities(string(out))
		}
	}
	if m.rootPolicy != nil {
		if data, err := m.rootPolicy(); err == nil {
			if ids, perr := extraupd.ParsePolicy(data); perr == nil {
				facts.policy, facts.policyOK = ids, true
			}
		}
	}
	if m.unitShow != nil {
		if out, err := m.unitShow(ctx); err == nil {
			if unit, perr := maintenance.ParseUnitShow(out); perr == nil {
				facts.unit, facts.unitOK = unit, true
			}
		}
	}
	a.extra.mu.Lock()
	a.extra.facts, a.extra.have = facts, true
	a.extra.mu.Unlock()
	return facts
}

// extraReadiness decides FR-024 for one present updater. The first matching
// reason wins, in AD-010's order.
func (a *Agent) extraReadiness(id string, src extraupd.Source, trustedOwner string, facts extraFacts) string {
	listed := false
	for _, v := range a.config.Commands.EnabledExtraUpdaters() {
		if v == id {
			listed = true
		}
	}
	switch {
	case !listed:
		return models.ExtraReadinessNotOptedIn
	case !a.config.Commands.AllowPatching:
		return models.ExtraReadinessPatchingDisabled
	case !facts.capsOK || !facts.caps.supports(id):
		return models.ExtraReadinessUnsupportedHelper
	case !facts.policyOK || !extraupd.PolicyListsImplemented(facts.policy, id):
		return models.ExtraReadinessRootSideMissing
	case !facts.unitOK || facts.unit.RuntimeMax < maintenance.ExtraMinBackstop || facts.unit.PrivateTmp != "no" || facts.unit.MountFlags != "":
		return models.ExtraReadinessRootSideMissing
	case !src.Readable || src.Owner != trustedOwner:
		return models.ExtraReadinessNonDefaultUpdateSource
	}
	return models.ExtraReadinessEnabled
}

// stampExtraReadiness fills Readiness on telemetry entries (FR-024).
func (a *Agent) stampExtraReadiness(ctx context.Context, list []models.ExtraUpdater) {
	if len(list) == 0 {
		return
	}
	facts := a.extraRootFacts(ctx)
	fs := a.extraFS()
	for i := range list {
		list[i].Readiness = a.readinessOf(list[i].ID, fs, facts)
	}
}

func (a *Agent) readinessOf(id string, fs extraupd.FS, facts extraFacts) string {
	u, ok := extraupd.Lookup(id)
	if !ok {
		return models.ExtraReadinessNotOptedIn
	}
	return a.extraReadiness(id, u.Source(fs), u.Apply().TrustedOwner, facts)
}

// extraEntry is one present updater in a run plan.
type extraEntry struct {
	entry     models.ExtraUpdater
	readiness string
}

// extraPlan is what one os_update does about extra updaters (AD-010).
type extraPlan struct {
	mode     string
	present  []extraEntry
	excluded bool
	budget   bool
	// request lists the enabled updaters, in registry order, for a version 2
	// request. Empty means a version 1 request.
	request []string
}

func (p *extraPlan) sendsV2() bool { return p != nil && len(p.request) > 0 }

// planExtras detects the present updaters and decides what the request carries.
// It returns nil for repair mode and on hosts with no present updater, so those
// runs are exactly what they were before this story (FR-026, FR-066).
func (a *Agent) planExtras(ctx context.Context, mode string, params map[string]interface{}) *extraPlan {
	if mode == "repair" {
		return nil
	}
	fs := a.extraFS()
	present := extraupd.DetectAll(ctx, fs)
	if len(present) == 0 {
		return nil
	}
	plan := &extraPlan{mode: mode, budget: models.ExtraUpdaterBudget(params)}
	plan.excluded, _ = params[models.ParamExcludeExtraUpdaters].(bool)
	facts := a.extraRootFacts(ctx)
	for _, e := range present {
		e.Readiness = a.readinessOf(e.ID, fs, facts)
		plan.present = append(plan.present, extraEntry{entry: e, readiness: e.Readiness})
		if e.Readiness == models.ExtraReadinessEnabled && plan.budget && !plan.excluded {
			plan.request = append(plan.request, e.ID)
		}
	}
	return plan
}

// aptWordsOf says how the apt part of a finished run is worded in the first line
// of a message.
func aptWordsOf(r commandExecResult) string {
	switch {
	case r.Status == models.ResultStatusSuccess:
		return "succeeded"
	case r.Reason == models.ReasonRestarted:
		return "interrupted"
	}
	return "failed"
}

// applyExtraV1Lines adds the per-updater lines to the result of a run that sent
// a version 1 request, where the worker recorded nothing about updaters
// (AD-010). Status is re-read after the run so a skipped update shows what is
// still pending.
func (a *Agent) applyExtraV1Lines(ctx context.Context, plan *extraPlan, r commandExecResult) commandExecResult {
	if plan == nil || plan.sendsV2() || r.Detached || r.Step == models.StepPrecheck {
		return r
	}
	aptOK := r.Status == models.ResultStatusSuccess
	fresh := map[string]models.ExtraUpdater{}
	for _, e := range extraupd.DetectAll(ctx, a.extraFS()) {
		fresh[e.ID] = e
	}
	var words, details []string
	failing := false
	for _, p := range plan.present {
		e := p.entry
		if now, ok := fresh[e.ID]; ok {
			e.Status, e.UnknownReason, e.AvailableVersion = now.Status, now.UnknownReason, now.AvailableVersion
		}
		outcome, reason := "", ""
		switch {
		case !aptOK:
			reason = maintenance.SkipAptNotSuccessful
		case plan.excluded:
			reason = "excluded_by_operator"
		case p.readiness != models.ExtraReadinessEnabled:
			reason = maintenance.SkipNotEnabled
		case e.Status == models.ExtraStatusUnknown:
			reason = maintenance.SkipStatusUnknown
		case e.Status == models.ExtraStatusUpToDate:
			reason = maintenance.SkipNothingPending
		default:
			// Enabled and pending, but the server did not extend this run's budget,
			// so the worker was never asked (AF-7).
			reason = maintenance.SkipInsufficientTime
			failing = true
		}
		outcome = "skipped (" + reason + ")"
		words = append(words, e.ID+": "+outcome)
		line := e.ID + ": " + outcome
		if reason == maintenance.SkipNotEnabled {
			line += "; readiness: " + p.readiness
		}
		if reason == maintenance.SkipInsufficientTime {
			line += "; the server did not allow the extended time for this run"
		}
		details = append(details, line)
	}
	if len(words) == 0 {
		return r
	}
	first := "apt: " + aptWordsOf(r) + "; " + strings.Join(words, "; ")
	msg := first
	if r.Message != "" {
		msg += "\n" + r.Message
	}
	r.Message = msg + "\n" + strings.Join(details, "\n")
	if failing && aptOK {
		r.Status = models.ResultStatusError
		code := 1
		r.ExitCode = &code
	}
	return r
}

// extraRebootEntry is the pseudo-entry a scheduled run adds to
// RebootClassApplied when an updater succeeded and a reboot is now required
// (OQ-A5, DEC-008). It is the updater's own identifier.
func extraRebootEntries(rec maintenance.Record) []string {
	if rec.Version != maintenance.RequestVersionExtra || !rec.IncludeRebootClass || rec.Mode == "repair" || rec.Outcome != "success" {
		return nil
	}
	var out []string
	for _, u := range rec.ExtraUpdaters {
		if u.Outcome == maintenance.UpdaterSuccess && u.RebootRequiredAfter {
			out = append(out, u.ID)
		}
	}
	return out
}

// extraStepName is the progress step of an updater, matching models.StepPattern.
func extraStepName(id string, outcome string) string {
	name := "extra_" + strings.ReplaceAll(id, "-", "_")
	if outcome != "" {
		name += "_" + outcome
	}
	return name
}

// reportExtraProgress emits, from a root record, one start and one end progress
// step and agent event per updater that ran, at most once each across agent
// restarts (FR-044, FR-045). Both are recorded durably first: when the marker
// cannot be written, nothing is sent.
func (a *Agent) reportExtraProgress(cr *cmdRun, commandID string, rec maintenance.Record) {
	for _, u := range rec.ExtraUpdaters {
		if u.StartedAt == nil && u.Outcome != maintenance.UpdaterRunning {
			continue
		}
		a.extraEvent(cr, commandID, u.ID, "start", "")
		if u.Outcome != maintenance.UpdaterRunning {
			a.extraEvent(cr, commandID, u.ID, "end", u.Outcome)
		}
	}
}

func (a *Agent) extraEvent(cr *cmdRun, commandID, id, kind, outcome string) {
	if a.inflight == nil {
		return
	}
	added, err := a.inflight.AddExtraEvent(commandID, id+":"+kind)
	if err != nil {
		slog.Warn("extra updater: event marker not durable; event not sent", "command_id", commandID, "updater", id, "error", err)
		return
	}
	if !added {
		return
	}
	evt := &models.AgentEvent{CommandID: commandID, Updater: id}
	if kind == "start" {
		evt.Type, evt.Severity = models.EventExtraUpdaterStarted, models.SeverityInfo
		evt.Message = fmt.Sprintf("%s update started", id)
		slog.Info("extra updater step started", "command_id", commandID, "updater", id)
		if cr != nil {
			cr.Step(extraStepName(id, ""))
		}
	} else {
		evt.Type, evt.Outcome = models.EventExtraUpdaterFinished, outcome
		evt.Severity = models.SeverityWarning
		if outcome == maintenance.UpdaterSuccess {
			evt.Severity = models.SeverityInfo
		}
		evt.Message = fmt.Sprintf("%s update finished: %s", id, outcome)
		slog.Info("extra updater step finished", "command_id", commandID, "updater", id, "outcome", outcome)
		if cr != nil {
			cr.Step(extraStepName(id, outcome))
		}
	}
	a.notifyEvent(func(ctx context.Context) {
		if err := a.tr.ReportEvent(ctx, evt); err != nil {
			slog.Warn("extra updater: event not delivered", "type", evt.Type, "updater", id, "error", err)
		}
	})
}

// extraProgressStep is the step an observer announces while a record shows a
// running updater, instead of the generic verify step, so the dashboard keeps
// naming the updater for the whole step.
func extraProgressStep(phase string) string {
	if id, ok := strings.CutPrefix(phase, "extra_run:"); ok && capabilityID.MatchString(id) {
		return extraStepName(id, "")
	}
	return models.StepVerify
}
