package maintenance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var errPersistence = errors.New("maintenance persistence failed")

type Holds interface {
	Snapshot(context.Context) (HoldSnapshot, error)
	Release(context.Context, HoldSnapshot) error
	Restore(context.Context, HoldSnapshot) error
	Changes(context.Context, HoldSnapshot) ([]VersionChange, error)
}
type CommandResult struct {
	Output                 string
	ExitCode               int
	Interrupted, Truncated bool
}
type Runner interface {
	Run(context.Context, string, ...string) (CommandResult, error)
}
type RecordStore interface {
	Current() (Record, error)
	Begin(Record) error
	Save(Record) error
	Finish(Record) error
}
type Worker struct {
	Store  RecordStore
	Runner Runner
	Holds  Holds
	Now    func() time.Time
	// Sources returns a fingerprint of the apt-effective configuration files
	// (PATCH-FLOW-001 AD-007). An error is treated as "changed". Nil disables the
	// post-run package list refresh.
	Sources func() (string, error)
	// TouchLists stamps /var/lib/apt/lists after a successful refresh so the
	// agent's freshness rule sees no-change refreshes. Nil disables the stamp.
	TouchLists func() error
	// Extras is the environment of the extra-updater step. It is required for a
	// version 2 request and ignored for version 1 (EXTRA-UPD-001).
	Extras *ExtraEnv
}

// PostRefreshBudget bounds the single extra package list refresh a run may
// perform after apt configuration changed during the upgrade (FR-042).
const PostRefreshBudget = 5 * time.Minute

// postRefreshMinRemaining is the worker budget that must remain before the
// extra refresh may start: its own budget, the SIGTERM->SIGKILL grace and a
// margin. It guarantees the parent context cannot expire during the refresh, so
// a successful run can never turn into "interrupted" because of it.
const postRefreshMinRemaining = PostRefreshBudget + TerminationGrace + 30*time.Second

// postRefreshBudgetNote is the summary note of a post-run refresh skipped for
// lack of time (FR-016).
const postRefreshBudgetNote = "post-run package list refresh skipped: worker time budget nearly exhausted"

// successSummary is the terminal summary of a verified run.
const successSummary = SuccessSummary

// maxSummaryBytes mirrors the limit enforced by Record.Validate.
const maxSummaryBytes = 4096

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
func aptOptions() []string {
	return []string{"-y", "--no-remove", "-o", "DPkg::Lock::Timeout=120", "-o", "Acquire::Retries=0", "-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Options::=--force-confdef"}
}

// aptState carries what the apt part leaves for the finaliser.
type aptState struct {
	postRefreshNote string
	// listsRefreshed is true only after the run's own refresh step returned
	// without error. A version 2 post-run refresh requires it (FR-010, FR-011).
	listsRefreshed bool
	// sourcesBefore and sourcesBeforeErr are the apt configuration baseline,
	// taken immediately before the run's own refresh step (FR-005).
	sourcesBefore    string
	sourcesBeforeErr error
}

// Execute is called only while the root exclusion is held. Its context belongs
// to the service, never to an agent observer or an HTTP request.
//
// A version 1 request runs the apt part and finalises exactly as before
// EXTRA-UPD-001. A version 2 request runs the same apt part, then the
// extra-updater step (extra.go), and finalises with the per-updater results.
func (w *Worker) Execute(ctx context.Context, req Request) (rec Record, retErr error) {
	if err := ValidateRequest(req); err != nil {
		return rec, err
	}
	if w.Store == nil || w.Runner == nil {
		return rec, errors.New("maintenance worker is not configured")
	}
	if req.Version == RequestVersionExtra && w.Extras == nil {
		return rec, errors.New("extra-updater environment is not configured")
	}
	now := w.now()
	rec = Record{Version: req.Version, CommandID: req.CommandID, Mode: req.Mode, IncludeRebootClass: req.IncludeRebootClass, StartedAt: now, UpdatedAt: now, Phase: "accepted", Outcome: "running"}
	if req.Version == RequestVersionExtra {
		rec.RequestedExtraUpdaters = append([]string(nil), req.ExtraUpdaters...)
	}
	if err := w.Store.Begin(rec); err != nil {
		return rec, err
	}
	budget := WorkerBudget
	if req.Version == RequestVersionExtra {
		budget = ExtendedWorkerBudget
	}
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	aptCtx := runCtx
	if req.Version == RequestVersionExtra {
		var cancelApt context.CancelFunc
		aptCtx, cancelApt = context.WithTimeout(runCtx, WorkerBudget)
		defer cancelApt()
	}
	var st aptState
	aptErr := w.executeApt(aptCtx, req, &rec, &st)
	if req.Version == ProtocolVersion {
		err := w.finalizeV1(aptCtx, &rec, &st, aptErr)
		return rec, err
	}
	err := w.executeExtras(ctx, runCtx, aptCtx, req, &rec, &st, aptErr, now)
	return rec, err
}

// executeApt is the apt part of a run: hold release, recovery, refresh, upgrade,
// verification and, for version 1, the post-run refresh, then (in the deferred
// cleanup) hold restoration and the version changes. It never finishes the
// record.
func (w *Worker) executeApt(ctx context.Context, req Request, rec *Record, st *aptState) (retErr error) {
	releaseAttempted := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), CleanupBudget)
		defer stop()
		if retErr == nil && ctx.Err() != nil {
			retErr = ctx.Err()
		}
		if releaseAttempted && rec.HoldsReleased {
			rec.Phase = "hold_restore"
			rec.UpdatedAt = w.now()
			if err := w.Store.Save(*rec); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("persist hold cleanup phase: %w", err))
			}
			err := w.Holds.Restore(cleanup, rec.Holds)
			if err != nil {
				rec.HoldCleanupFailed = true
				rec.HoldRestoreError = err.Error()
				retErr = errors.Join(retErr, fmt.Errorf("restore managed holds: %w", err))
			} else {
				rec.HoldsReleased = false
			}
		}
		if retErr == nil && req.IncludeRebootClass && w.Holds != nil {
			changes, err := w.Holds.Changes(cleanup, rec.Holds)
			if err != nil {
				retErr = fmt.Errorf("verify package versions: %w", err)
			} else {
				rec.VersionChanges = changes
			}
		}
	}()
	step := func(phase, path string, args ...string) (CommandResult, error) {
		if err := ctx.Err(); err != nil {
			return CommandResult{Interrupted: true}, err
		}
		rec.Phase = phase
		rec.UpdatedAt = w.now()
		if err := w.Store.Save(*rec); err != nil {
			return CommandResult{}, fmt.Errorf("%w: phase %s: %v", errPersistence, phase, err)
		}
		out, err := w.Runner.Run(ctx, path, args...)
		rec.Output, rec.OutputTruncated = appendBounded(rec.Output, "\n["+phase+"]\n"+out.Output, rec.OutputTruncated || out.Truncated)
		if out.Interrupted {
			rec.Interrupted = true
		}
		if err != nil {
			rec.ExitCode = out.ExitCode
			return out, fmt.Errorf("%s failed: %w", phase, err)
		}
		return out, nil
	}
	configure := func() error {
		_, err := step("configure", "/usr/bin/dpkg", "--force-confold", "--force-confdef", "--configure", "-a")
		return err
	}
	verify := func() error {
		out, err := step("audit", "/usr/bin/dpkg", "--audit")
		if err != nil {
			return err
		}
		if out.Truncated || strings.TrimSpace(out.Output) != "" {
			return errors.New("dpkg audit reports unresolved package state")
		}
		_, err = step("check", "/usr/bin/apt-get", "check")
		return err
	}
	if req.IncludeRebootClass {
		if w.Holds == nil {
			return errors.New("authorized hold adapter unavailable")
		}
		snapshot, err := w.Holds.Snapshot(ctx)
		if err != nil {
			return fmt.Errorf("snapshot managed holds: %w", err)
		}
		rec.Holds = snapshot
		rec.HoldsReleased = true
		rec.Phase = "hold_release"
		rec.UpdatedAt = w.now()
		if err = w.Store.Save(*rec); err != nil {
			rec.HoldsReleased = false
			return err
		}
		releaseAttempted = true
		if err = w.Holds.Release(ctx, snapshot); err != nil {
			return fmt.Errorf("release managed holds: %w", err)
		}
	}
	// Recovery always precedes refresh. A failed repair is terminal, not a
	// reason to retry with broader package-manager privileges.
	configured := configure()
	var verified error
	if configured == nil {
		verified = verify()
	}
	if errors.Is(configured, errPersistence) {
		return configured
	}
	if errors.Is(verified, errPersistence) {
		return verified
	}
	if configured != nil || verified != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		args := append(aptOptions(), "--fix-broken", "install")
		if _, err := step("repair", "/usr/bin/apt-get", args...); err != nil {
			return err
		}
		if err := configure(); err != nil {
			return err
		}
		if err := verify(); err != nil {
			return err
		}
	}
	if req.Mode == "repair" {
		return nil
	}

	st.sourcesBefore, st.sourcesBeforeErr = w.fingerprintSources()
	if _, err := step("refresh", "/usr/bin/apt-get", aptRefreshArgs()...); err != nil {
		return err
	}
	st.listsRefreshed = true
	w.stampLists(rec)
	action := "dist-upgrade"
	if req.Mode == "security" {
		action = "upgrade"
	}
	if _, err := step("upgrade", "/usr/bin/apt-get", append(aptOptions(), action)...); err != nil {
		return err
	}
	if err := verify(); err != nil {
		return err
	}
	// A version 2 run makes this check after the extra-updater step instead
	// (DIETPI-REFRESH-001 FR-002, FR-003), so an updater's apt changes are covered.
	if req.Version == ProtocolVersion {
		st.postRefreshNote = w.postRunRefresh(ctx, rec, st.sourcesBefore, st.sourcesBeforeErr)
	}
	return nil
}

// finalizeV1 finishes a version 1 record. Its bytes and outcomes equal the
// pre-EXTRA-UPD-001 finaliser (AC-044).
func (w *Worker) finalizeV1(ctx context.Context, rec *Record, st *aptState, retErr error) error {
	rec.UpdatedAt = w.now()
	finished := rec.UpdatedAt
	rec.FinishedAt = &finished
	rec.Phase = "finished"
	if retErr == nil {
		rec.Outcome = "success"
		rec.ExitCode = 0
		rec.Summary = successSummary
		if st.postRefreshNote != "" {
			rec.Summary = boundedText(successSummary+"; "+st.postRefreshNote, maxSummaryBytes)
		}
	} else {
		rec.Outcome = "error"
		if rec.ExitCode == 0 {
			rec.ExitCode = 1
		}
		rec.Summary = boundedText(retErr.Error(), 4096)
		if rec.Interrupted || ctx.Err() != nil {
			rec.Outcome = "interrupted"
			rec.Interrupted = true
			rec.Summary = "Maintenance interrupted; package state requires manual review: " + boundedText(retErr.Error(), 3900)
		}
	}
	if err := w.Store.Finish(*rec); err != nil {
		retErr = errors.Join(retErr, fmt.Errorf("persist maintenance result: %w", err))
	}
	return retErr
}

func aptRefreshArgs() []string {
	return []string{"-o", "Acquire::Retries=0", "update"}
}

// fingerprintSources reports the apt configuration fingerprint, or ("", nil)
// when the feature is not wired.
func (w *Worker) fingerprintSources() (string, error) {
	if w.Sources == nil {
		return "", nil
	}
	return w.Sources()
}

// stampLists records a successful refresh in the lists directory mtime. A
// failure is an output line, never a run failure.
func (w *Worker) stampLists(rec *Record) {
	if w.TouchLists == nil {
		return
	}
	if err := w.TouchLists(); err != nil {
		note := "\n[lists_stamp]\ncould not update /var/lib/apt/lists timestamp: " + boundedText(err.Error(), 200) + "\n"
		rec.Output, rec.OutputTruncated = appendBounded(rec.Output, note, rec.OutputTruncated)
	}
}

// postRunRefresh is the version 1 post-run refresh. It runs at most one extra
// package list refresh when the apt configuration changed between the run's
// refresh and the end of its upgrade (PATCH-FLOW-001 FR-040..FR-045). It never
// changes the run outcome: it goes through Runner.Run, not step(), so runner
// interruption flags and exit codes stay off the record. It returns a summary
// note, or "" when nothing needs saying.
//
// A version 2 run does not call it; see postRunRefreshV2.
func (w *Worker) postRunRefresh(ctx context.Context, rec *Record, before string, beforeErr error) string {
	if !w.sourcesChanged(before, beforeErr) {
		return ""
	}
	// time.Until, not w.now(): context deadlines run on the real clock.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < postRefreshMinRemaining {
		return postRefreshBudgetNote
	}
	return w.runPostRefresh(ctx, rec)
}

// postRunRefreshV2 is the version 2 post-run refresh (DIETPI-REFRESH-001
// FR-002..FR-018). executeExtras calls it once, after every present updater's
// step has ended, so a change made by apt or by an updater is covered by one
// refresh. Like postRunRefresh it never changes the run outcome, the apt
// outcome or any updater result; it returns a summary note, or "" when nothing
// needs saying.
//
// The run is ineligible, silently and before any filesystem read, when its own
// refresh did not complete (FR-010, FR-011, BR-005), when the apt part or any
// updater was interrupted, or when the service is stopping (FR-014, BR-004).
// An apt error after a successful own refresh, and every other updater
// outcome, stay eligible (FR-012, FR-013).
//
// basisCtx and basisBudget are the time basis (DR-SEC-001). The caller passes
// the extended budget only when this run verified the unit's backstop root-side
// (runExtra check 6); otherwise the apt context and WorkerBudget, which is the
// pre-story bound. The refresh starts only when postRefreshMinRemaining of the
// basis remains on the injected clock and on the real context deadline, and its
// context derives from basisCtx, never from svc.
func (w *Worker) postRunRefreshV2(svc, basisCtx context.Context, basisBudget time.Duration, rec *Record, st *aptState, started time.Time) string {
	if !st.listsRefreshed || rec.AptOutcome == aptOutcomeInterrupt || svc.Err() != nil {
		return ""
	}
	for _, r := range rec.ExtraUpdaters {
		if r.Outcome == UpdaterInterrupted {
			return ""
		}
	}
	if !w.sourcesChanged(st.sourcesBefore, st.sourcesBeforeErr) {
		return ""
	}
	// Strictly less than: exactly postRefreshMinRemaining is enough (AC-019).
	if started.Add(basisBudget).Sub(w.now()) < postRefreshMinRemaining {
		return postRefreshBudgetNote
	}
	// time.Until, not w.now(): context deadlines run on the real clock.
	if deadline, ok := basisCtx.Deadline(); ok && time.Until(deadline) < postRefreshMinRemaining {
		return postRefreshBudgetNote
	}
	return w.runPostRefresh(basisCtx, rec)
}

// sourcesChanged reports whether the apt configuration differs from the
// baseline taken before the run's own refresh. An unreadable baseline or an
// unreadable second observation counts as changed (FR-007). It calls Sources at
// most once, and not at all when the feature is not wired.
func (w *Worker) sourcesChanged(before string, beforeErr error) bool {
	if w.Sources == nil {
		return false
	}
	after, afterErr := w.Sources()
	return beforeErr != nil || afterErr != nil || before != after
}

// runPostRefresh persists the post_refresh phase, runs the one post-run package
// list refresh under PostRefreshBudget, appends its output and stamps the lists.
// It returns "" on success, or the summary note. It writes only Phase,
// UpdatedAt, Output and OutputTruncated, never an outcome field.
func (w *Worker) runPostRefresh(ctx context.Context, rec *Record) string {
	rec.Phase = "post_refresh"
	rec.UpdatedAt = w.now()
	if err := w.Store.Save(*rec); err != nil {
		return "post-run package list refresh skipped: state not persisted"
	}
	sub, cancel := context.WithTimeout(ctx, PostRefreshBudget)
	defer cancel()
	out, err := w.Runner.Run(sub, "/usr/bin/apt-get", aptRefreshArgs()...)
	rec.Output, rec.OutputTruncated = appendBounded(rec.Output, "\n[post_refresh]\n"+out.Output, rec.OutputTruncated || out.Truncated)
	if err != nil {
		return "post-run package list refresh failed: " + boundedText(err.Error(), 200)
	}
	w.stampLists(rec)
	return ""
}

// RecoverInterrupted restores only the recorded managed holds. It never
// repeats configure, repair, refresh or upgrade, and requires a fresh retry.
func (w *Worker) RecoverInterrupted(ctx context.Context, rec Record) (Record, error) {
	if rec.Terminal() && !rec.HoldsReleased {
		return rec, nil
	}
	var cleanupErr error
	if rec.HoldsReleased {
		if w.Holds == nil {
			cleanupErr = errors.New("hold recovery adapter unavailable")
		} else {
			cleanup, stop := context.WithTimeout(ctx, CleanupBudget)
			cleanupErr = w.Holds.Restore(cleanup, rec.Holds)
			stop()
		}
		if cleanupErr != nil {
			rec.HoldCleanupFailed = true
			rec.HoldRestoreError = cleanupErr.Error()
		} else {
			rec.HoldsReleased = false
			rec.HoldCleanupFailed = false
			rec.HoldRestoreError = ""
		}
	}
	rec.Outcome = "interrupted"
	rec.Interrupted = true
	rec.ExitCode = 1
	rec.Phase = "finished"
	rec.Summary = "Previous worker stopped without a verified result; package state requires manual review"
	rec.UpdatedAt = w.now()
	finished := rec.UpdatedAt
	rec.FinishedAt = &finished
	markExtraInterrupted(&rec, finished)
	return rec, errors.Join(cleanupErr, w.Store.Finish(rec))
}

// markExtraInterrupted closes a version 2 record that a dead worker left behind.
// An updater step that was running becomes interrupted, never re-run, and never
// reported as success (FR-035, FR-040). The apt outcome is unknown unless the
// apt part had already ended.
func markExtraInterrupted(rec *Record, at time.Time) {
	if rec.Version != RequestVersionExtra {
		return
	}
	if rec.AptOutcome == "" {
		rec.AptOutcome = aptOutcomeInterrupt
	}
	for i := range rec.ExtraUpdaters {
		if rec.ExtraUpdaters[i].Outcome == UpdaterRunning {
			finished := at
			rec.ExtraUpdaters[i].Outcome = UpdaterInterrupted
			rec.ExtraUpdaters[i].FinishedAt = &finished
			rec.ExtraUpdaters[i].Note = "worker stopped during the step; state uncertain, never re-run"
		}
	}
}

func boundedText(s string, n int) string {
	s = strings.ToValidUTF8(s, "?")
	if len(s) <= n {
		return s
	}
	s = s[:n]
	return strings.ToValidUTF8(s, "")
}
func appendBounded(old, next string, truncated bool) (string, bool) {
	return appendBoundedLimit(old, next, truncated, MaxOutputBytes)
}
func appendBoundedLimit(old, next string, truncated bool, limit int) (string, bool) {
	s := old + strings.ToValidUTF8(next, "?")
	if len(s) <= limit {
		return s, truncated
	}
	const marker = "\n[output truncated; see system journal]\n"
	half := (limit - len(marker)) / 2
	return boundedText(s, half) + marker + strings.ToValidUTF8(s[len(s)-half:], ""), true
}
