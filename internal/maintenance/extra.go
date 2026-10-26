package maintenance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// This file is the extra-updater step of a version 2 run (EXTRA-UPD-001,
// AD-005). It runs after the apt part and after the managed holds have been
// restored. Every gate is checked here, as root, from root-owned facts: the
// request can only ask, and nothing in it supplies an argument, environment or
// path.

// LockKind is the answer of the dpkg lock probe.
type LockKind int

const (
	LockFree LockKind = iota
	LockHeld
	LockUnknown
)

// LockState is one dpkg lock observation.
type LockState struct {
	Kind   LockKind
	Detail string
}

// UnitState is the effective state of the maintenance service as systemd
// reports it (SEC-002, OPS-001).
type UnitState struct {
	RuntimeMax time.Duration
	PrivateTmp string
	MountFlags string
}

// StepOutcome is what a StepRunner reports about one updater process.
type StepOutcome struct {
	// Output is the captured output, already bounded by the runner.
	Output    string
	Truncated bool
	ExitCode  int
	// Skip is set when the launcher refused to start the step, so nothing
	// ran: tmp_not_private, rundir_not_private or
	// mount_propagation_unverified.
	Skip       string
	SkipDetail string
	// SetupErr is set for any other launcher failure before the step started.
	SetupErr string
	// Err is any process-level error: a context ending, a stray descendant, a
	// non-zero exit. Nil with ExitCode 0 means success.
	Err error
}

// StepRunner starts one updater step. The production implementation starts the
// fixed launcher (extra_linux.go); tests inject a fake and never start a
// process (SC-001).
type StepRunner interface {
	RunStep(ctx context.Context, id string, spec extraupd.ApplySpec) StepOutcome
}

// ExtraEnv holds the seams of the extra-updater step. Every member that is nil
// fails closed: the updater is refused, never run.
type ExtraEnv struct {
	// FS is the root filesystem detection and reboot indicators read.
	FS extraupd.FS
	// Policy returns the updater IDs the root-owned policy lists.
	Policy func() ([]string, error)
	// Lock and Busy are the dpkg lock probe and the DietPi busy scan.
	Lock func() LockState
	Busy func() (busy bool, detail string)
	// Unit reads the service's effective backstop and mount settings.
	Unit func() (UnitState, error)
	// Trust walks the spec's trust roots (SEC-004).
	Trust func(spec extraupd.ApplySpec, installed models.ExtraUpdater) error
	// LastStart returns the newest recorded start of an updater (SEC-006).
	LastStart func(id string) (time.Time, bool, error)
	// Step starts the step.
	Step StepRunner
	// ReconcileFlags removes the host's update flags after a successful step
	// and returns a note for a problem, or "" (AD-3). Nil does nothing.
	ReconcileFlags func(dir string) string
	// Sleep waits for d or until ctx ends. Nil means a real timer.
	Sleep func(ctx context.Context, d time.Duration)
	// StepLimit overrides the spec's step limit. Tests only; zero keeps it.
	StepLimit time.Duration
}

func (e *ExtraEnv) sleep(ctx context.Context, d time.Duration) {
	if e.Sleep != nil {
		e.Sleep(ctx, d)
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// extraRun carries the in-memory detail of one updater beside its persisted
// result, for the summary.
type extraRun struct {
	res    UpdaterResult
	target string
	tail   string
	limit  time.Duration
	log    string
	label  string
}

func (w *Worker) aptOutcome(aptCtx context.Context, rec *Record, aptErr error) string {
	switch {
	case aptErr == nil:
		return aptOutcomeSuccess
	case rec.Interrupted || aptCtx.Err() != nil:
		return aptOutcomeInterrupt
	default:
		return aptOutcomeError
	}
}

// executeExtras runs the step for every present updater and finalises the
// version 2 record. started is the worker's own start time, from which the
// extended budget is measured on the injected clock.
func (w *Worker) executeExtras(svc, runCtx, aptCtx context.Context, req Request, rec *Record, st *aptState, aptErr error, started time.Time) error {
	rec.AptOutcome = w.aptOutcome(aptCtx, rec, aptErr)
	env := w.Extras
	deadline := started.Add(ExtendedWorkerBudget)
	var policy []string
	var policyErr error
	if env.Policy != nil {
		policy, policyErr = env.Policy()
	} else {
		policyErr = errors.New("policy reader not configured")
	}
	var runs extraRuns
	for _, u := range extraupd.Registry() {
		if env.FS == nil {
			break
		}
		entry, present := u.Detect(context.Background(), env.FS)
		if !present {
			continue
		}
		run := w.runExtra(runCtx, deadline, req, rec, u, entry, aptErr == nil, policy, policyErr)
		rec.ExtraUpdaters = upsertResult(rec.ExtraUpdaters, run.res)
		runs = append(runs, run)
	}
	return w.finalizeV2(svc, runCtx, rec, st, aptErr, runs)
}

func upsertResult(list []UpdaterResult, res UpdaterResult) []UpdaterResult {
	for i := range list {
		if list[i].ID == res.ID {
			list[i] = res
			return list
		}
	}
	return append(list, res)
}

func contains(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// saveExtraPhase persists the record in an extra-updater phase before anything
// is waited for or started.
func (w *Worker) saveExtraPhase(rec *Record, phase string) error {
	rec.Phase = phase
	rec.UpdatedAt = w.now()
	return w.Store.Save(*rec)
}

// runExtra applies AD-005's checks 1 to 12 to one present updater. The first
// check that fails decides the result, and no check after it runs.
func (w *Worker) runExtra(runCtx context.Context, deadline time.Time, req Request, rec *Record, u extraupd.Updater, entry models.ExtraUpdater, aptOK bool, policy []string, policyErr error) extraRun {
	env := w.Extras
	id := u.ID()
	spec := u.Apply()
	run := extraRun{res: UpdaterResult{ID: id, VersionBefore: boundedText(entry.InstalledVersion, maxUpdaterVersion)}, target: entry.AvailableVersion, limit: spec.StepLimit, log: spec.LogPath, label: spec.Label}
	skip := func(reason, note string) extraRun {
		run.res.Outcome, run.res.Reason, run.res.Note = UpdaterSkipped, reason, boundedText(note, maxUpdaterNote)
		return run
	}
	fail := func(note string) extraRun {
		run.res.Outcome, run.res.Note = UpdaterFailed, boundedText(note, maxUpdaterNote)
		return run
	}

	// 1. The apt part must have succeeded, hold restoration included.
	if !aptOK {
		return skip(SkipAptNotSuccessful, "")
	}
	// 2. Requested by the agent and listed by the root-owned policy.
	if !contains(req.ExtraUpdaters, id) || !extraupd.PolicyListsImplemented(policy, id) {
		note := ""
		if contains(req.ExtraUpdaters, id) && policyErr != nil {
			note = "root policy not usable: " + policyErr.Error()
		}
		return skip(SkipNotEnabled, note)
	}
	// 3. Fresh status.
	switch entry.Status {
	case models.ExtraStatusUnknown:
		return skip(SkipStatusUnknown, entry.UnknownReason)
	case models.ExtraStatusUpdateAvailable:
	default:
		return skip(SkipNothingPending, "")
	}
	// 4. Update source: readable, and owned by the one trusted owner.
	src := u.Source(env.FS)
	run.res.SourceOwner = printableBounded(src.Owner, maxUpdaterSource)
	run.res.SourceBranch = printableBounded(src.Branch, maxUpdaterSource)
	if !src.Readable || src.Owner != spec.TrustedOwner {
		return skip(SkipNonDefaultSource, fmt.Sprintf("update source %s/%s is not %s", run.res.SourceOwner, run.res.SourceBranch, spec.TrustedOwner))
	}
	// 5. Trust walk over the updater's files and the paths it consumes.
	if env.Trust == nil {
		return skip(SkipUnsafePath, "trust walk not configured")
	}
	if err := env.Trust(spec, entry); err != nil {
		return skip(SkipUnsafePath, err.Error())
	}
	// 6. The unit must really have the extended backstop and no mount sandbox.
	if env.Unit == nil {
		return skip(SkipInsufficientTime, "service state not available")
	}
	unit, err := env.Unit()
	if err != nil {
		return skip(SkipInsufficientTime, "cannot read the service state: "+err.Error())
	}
	if unit.PrivateTmp != "no" || unit.MountFlags != "" {
		return skip(SkipMountPropagationUnver, fmt.Sprintf("service has PrivateTmp=%s MountFlags=%s; mounts would not reach the host", boundedText(unit.PrivateTmp, 16), boundedText(unit.MountFlags, 32)))
	}
	if unit.RuntimeMax < ExtraMinBackstop {
		return skip(SkipInsufficientTime, fmt.Sprintf("service backstop is %s, need at least %s", unit.RuntimeMax, ExtraMinBackstop))
	}
	// 7. Enough of the extended budget must remain.
	if remaining := deadline.Sub(w.now()); remaining < ExtraStartThreshold {
		return skip(SkipInsufficientTime, fmt.Sprintf("%s of the run budget remain, need %s", remaining.Round(time.Second), ExtraStartThreshold))
	}
	// 8. A minimum interval between two starts.
	if env.LastStart == nil {
		return skip(SkipMinInterval, "start history not available")
	}
	last, found, err := env.LastStart(id)
	if err != nil {
		return skip(SkipMinInterval, "cannot read the start history: "+err.Error())
	}
	if found {
		now := w.now()
		switch {
		case last.After(now):
			// A record dated in the future cannot prove an interval; ignore it.
			// KNOWN LIMIT (QA N-1): LatestExtraStart returns the maximum
			// recorded start, so after a backward clock jump this one future
			// record hides every later, older-dated start. The 60-minute
			// minimum is then off until the clock passes that record, not just
			// once. The other gates in this function still apply. Documented
			// in docs/ops/os-maintenance.md ("Known limits").
		case now.Sub(last) < ExtraMinInterval:
			return skip(SkipMinInterval, fmt.Sprintf("last started %s ago, minimum is %s", now.Sub(last).Round(time.Second), ExtraMinInterval))
		}
	}
	// 9. Busy scan and dpkg lock, polled for at most ExtraLockWait.
	if env.Busy == nil || env.Lock == nil {
		return skip(SkipDpkgLocked, "lock probe not configured")
	}
	if err = w.saveExtraPhase(rec, phasePrefixExtraWait+id); err != nil {
		return fail("state not persisted")
	}
	waitStart := w.now()
	for {
		busy, busyDetail := env.Busy()
		lock := env.Lock()
		if !busy && lock.Kind == LockFree {
			break
		}
		if w.now().Sub(waitStart) >= ExtraLockWait {
			if busy {
				return skip(SkipBusy, busyDetail)
			}
			return skip(SkipDpkgLocked, lock.Detail)
		}
		if runCtx.Err() != nil {
			run.res.Outcome, run.res.Note = UpdaterInterrupted, "service stopped before the step started"
			return run
		}
		env.sleep(runCtx, ExtraLockPoll)
	}
	// 10. Persist the running entry before anything starts.
	begun := w.now()
	run.res.Outcome, run.res.StartedAt = UpdaterRunning, &begun
	rec.ExtraUpdaters = upsertResult(rec.ExtraUpdaters, run.res)
	if err = w.saveExtraPhase(rec, phasePrefixExtraRun+id); err != nil {
		run.res.Outcome, run.res.StartedAt = UpdaterFailed, nil
		run.res.Note = "state not persisted"
		return run
	}
	// 11. Run the step under its own limit.
	if env.Step == nil {
		run.res.Outcome, run.res.StartedAt, run.res.Note = UpdaterFailed, nil, "step runner not configured"
		return run
	}
	limit := spec.StepLimit
	if env.StepLimit > 0 {
		limit = env.StepLimit
	}
	stepCtx, cancel := context.WithTimeout(runCtx, limit)
	out := env.Step.RunStep(stepCtx, id, spec)
	stepErr := stepCtx.Err()
	cancel()

	// 12. The outcome, from what really happened.
	finished := w.now()
	if out.Skip != "" || out.SetupErr != "" {
		// The launcher refused before exec: DietPi never started.
		run.res.StartedAt = nil
		if out.Skip != "" {
			return skip(out.Skip, out.SkipDetail)
		}
		return fail("launcher setup failed: " + out.SetupErr)
	}
	run.res.FinishedAt = &finished
	exit := out.ExitCode
	run.res.ExitCode = &exit
	section := extraSection(id, out, spec)
	rec.Output, rec.OutputTruncated = appendBounded(rec.Output, section, rec.OutputTruncated || out.Truncated)
	stepOK := runCtx.Err() == nil && stepErr == nil && out.Err == nil && out.ExitCode == 0
	// The step's own flag removals died with its private /run/dietpi. After a
	// success the worker does what DietPi would have done, before the post-run
	// detection reads the flags (FR-008, FR-009). A problem is a note only.
	if stepOK && spec.ReconcileFlags && env.ReconcileFlags != nil {
		run.res.Note = boundedText(env.ReconcileFlags(DietPiRunDir), maxUpdaterNote)
	}
	if after, present := u.Detect(context.Background(), env.FS); present {
		run.res.VersionAfter = boundedText(after.InstalledVersion, maxUpdaterVersion)
	}
	run.res.RebootRequiredAfter, _ = extraupd.RebootIndicators(env.FS)
	switch {
	case runCtx.Err() != nil:
		run.res.Outcome = UpdaterInterrupted
		run.res.Note = "service stopped during the step; state uncertain"
	case stepErr != nil:
		run.res.Outcome = UpdaterTimedOut
		run.res.Note = fmt.Sprintf("timed out after %s; %s state uncertain: services may be stopped; see %s", models.FormatDurationShort(limit), spec.Label, spec.LogPath)
	case stepOK:
		run.res.Outcome = UpdaterSuccess
	default:
		run.res.Outcome = UpdaterFailed
		if out.Err != nil && out.ExitCode == 0 {
			exit = 1
			run.res.Note = boundedText(out.Err.Error(), maxUpdaterNote)
		}
	}
	if run.res.Outcome != UpdaterSuccess {
		run.tail = lastLines(out.Output, 20, 200)
	}
	return run
}

// extraSection renders the updater's labelled output section. It is cut to the
// spec's cap, keeping head and tail with the marker, and always ends with the
// updater's own log path so it survives the whole-run cap (FR-036).
func extraSection(id string, out StepOutcome, spec extraupd.ApplySpec) string {
	header := "\n[" + extraOutputPrefix + id + "]\n"
	footer := "\n[" + extraOutputPrefix + id + "] full log: " + spec.LogPath + "\n"
	room := spec.OutputCap - extraSectionSlack - len(header) - len(footer)
	if room < 1024 {
		room = 1024
	}
	body, _ := appendBoundedLimit("", out.Output, out.Truncated, room)
	return header + body + footer
}

// extraSectionSlack leaves headroom so the whole section survives the whole-run
// cap's tail half even with the truncation marker in play.
const (
	extraSectionSlack = 512
	extraOutputPrefix = "extra:"
)

// lastLines returns the last n non-empty lines of s, each cut to width bytes.
func lastLines(s string, n, width int) string {
	var lines []string
	for _, line := range strings.Split(strings.ToValidUTF8(s, "?"), "\n") {
		if line = strings.TrimRight(line, " \t\r"); line != "" {
			lines = append(lines, boundedText(line, width))
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// printableBounded keeps ASCII printable bytes of s, cut to n, so a hostile
// source line cannot put control characters into a record.
func printableBounded(s string, n int) string {
	var b strings.Builder
	for i := 0; i < len(s) && b.Len() < n; i++ {
		if s[i] >= 0x20 && s[i] <= 0x7e {
			b.WriteByte(s[i])
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

// outcomeWords renders one updater's result for the first summary line.
func (r extraRun) words() string {
	switch r.res.Outcome {
	case UpdaterSuccess:
		return "succeeded"
	case UpdaterFailed:
		if r.res.ExitCode != nil {
			return fmt.Sprintf("failed (exit %d)", *r.res.ExitCode)
		}
		return "failed"
	case UpdaterTimedOut:
		return fmt.Sprintf("timed out after %s (%s state uncertain)", models.FormatDurationShort(r.limit), r.label)
	case UpdaterInterrupted:
		return "interrupted"
	case UpdaterSkipped:
		return "skipped (" + r.res.Reason + ")"
	default:
		return r.res.Outcome
	}
}

func aptWords(outcome string) string {
	switch outcome {
	case aptOutcomeSuccess:
		return "succeeded"
	case aptOutcomeInterrupt:
		return "interrupted"
	default:
		return "failed"
	}
}

// detail renders one updater's detail line (section 6.2).
func (r extraRun) detail() string {
	parts := []string{r.res.ID + ": " + r.words()}
	if r.res.Outcome != UpdaterSkipped {
		if v := r.versionChange(); v != "" {
			parts = append(parts, v)
		}
		if r.res.SourceOwner != "" {
			parts = append(parts, "source "+r.res.SourceOwner+"/"+r.res.SourceBranch)
		}
		reboot := "no"
		if r.res.RebootRequiredAfter {
			reboot = "yes"
		}
		parts = append(parts, "reboot required after "+r.label+": "+reboot)
		parts = append(parts, r.label+" log: "+r.log)
	}
	if r.res.Note != "" {
		parts = append(parts, r.res.Note)
	}
	return strings.Join(parts, "; ")
}

func (r extraRun) versionChange() string {
	before := r.res.VersionBefore
	switch {
	case r.res.Outcome == UpdaterSuccess && r.res.VersionAfter != "" && r.res.VersionAfter != before:
		return before + " -> " + r.res.VersionAfter
	case r.res.Outcome == UpdaterSuccess:
		return "version " + before + " unchanged"
	case before != "" && r.target != "":
		return before + " -> " + r.target + " not applied"
	case before != "":
		return before + " update not applied"
	}
	return ""
}

// finalizeV2 finishes a version 2 record: the overall outcome is success only
// when the apt part succeeded and no updater started and failed. An updater
// skipped before it started, for any reason, leaves it success (FR-012).
func (w *Worker) finalizeV2(svc, runCtx context.Context, rec *Record, st *aptState, aptErr error, runs extraRuns) error {
	rec.UpdatedAt = w.now()
	finished := rec.UpdatedAt
	rec.FinishedAt = &finished
	rec.Phase = "finished"

	var firstFailing *UpdaterResult
	updaterInterrupted := false
	for i := range rec.ExtraUpdaters {
		r := &rec.ExtraUpdaters[i]
		if r.Outcome == UpdaterInterrupted {
			updaterInterrupted = true
		}
		if r.Failing() && firstFailing == nil && r.Reason != SkipAptNotSuccessful {
			firstFailing = r
		}
	}
	aptInterrupted := rec.AptOutcome == aptOutcomeInterrupt
	retErr := aptErr
	switch {
	case aptInterrupted || updaterInterrupted:
		rec.Outcome = "interrupted"
		rec.Interrupted = true
		if rec.ExitCode == 0 {
			rec.ExitCode = 1
		}
	case aptErr != nil || firstFailing != nil:
		rec.Outcome = "error"
		switch {
		case aptErr != nil && rec.ExitCode == 0:
			rec.ExitCode = 1
		case aptErr == nil && firstFailing != nil:
			rec.ExitCode = 1
			if firstFailing.ExitCode != nil && *firstFailing.ExitCode != 0 {
				rec.ExitCode = *firstFailing.ExitCode
			}
		}
	default:
		rec.Outcome = "success"
		rec.ExitCode = 0
	}
	rec.Summary = w.summaryV2(rec, st, aptErr, runs)
	if rec.Outcome == "error" && aptErr == nil {
		retErr = fmt.Errorf("extra updater did not succeed: %s", runs.firstFailureWords(rec))
	}
	if rec.Outcome == "interrupted" && retErr == nil {
		retErr = errors.New("extra updater interrupted")
	}
	if err := w.Store.Finish(*rec); err != nil {
		retErr = errors.Join(retErr, fmt.Errorf("persist maintenance result: %w", err))
	}
	return retErr
}

type extraRuns []extraRun

// summaryV2 builds the record summary (section 6.2): the first line states the
// apt part and every updater, then the apt result as a version 1 run would
// state it, then one detail line per updater, then the last output lines of an
// updater that did not succeed.
func (w *Worker) summaryV2(rec *Record, st *aptState, aptErr error, runs extraRuns) string {
	var lines []string
	if len(runs) > 0 {
		words := []string{"apt: " + aptWords(rec.AptOutcome)}
		for _, r := range runs {
			words = append(words, r.res.ID+": "+r.words())
		}
		lines = append(lines, strings.Join(words, "; "))
	}
	switch {
	case aptErr == nil:
		apt := successSummary
		if st.postRefreshNote != "" {
			apt += "; " + st.postRefreshNote
		}
		lines = append(lines, apt)
	case rec.AptOutcome == aptOutcomeInterrupt:
		lines = append(lines, "Maintenance interrupted; package state requires manual review: "+boundedText(aptErr.Error(), 1500))
	default:
		lines = append(lines, boundedText(aptErr.Error(), 1500))
	}
	for _, r := range runs {
		lines = append(lines, r.detail())
		if r.tail != "" {
			lines = append(lines, "last output lines of "+r.res.ID+":\n"+r.tail)
		}
	}
	return boundedText(strings.Join(lines, "\n"), maxSummaryBytes)
}

func (rs extraRuns) firstFailureWords(rec *Record) string {
	for _, r := range rs {
		if r.res.Failing() && r.res.Reason != SkipAptNotSuccessful {
			return r.res.ID + " " + r.words()
		}
	}
	return "unknown"
}
