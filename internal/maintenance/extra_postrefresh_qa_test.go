package maintenance

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Added by QA Engineer (DIETPI-REFRESH-001).

// cancelOnNthRefresh wraps a Runner. On the N-th refresh command it cancels the
// service context and records whether the context it was handed saw that cancel.
type cancelOnNthRefresh struct {
	inner     Runner
	n, seen   int
	cancel    context.CancelFunc
	sawCancel bool
}

func (c *cancelOnNthRefresh) Run(ctx context.Context, p string, a ...string) (CommandResult, error) {
	if p+" "+strings.Join(a, " ") == refreshKey {
		c.seen++
		if c.seen == c.n {
			c.cancel()
			c.sawCancel = ctx.Err() != nil
			return CommandResult{Output: "stopped", Interrupted: true, ExitCode: 1}, errors.New("package process failed: context canceled")
		}
	}
	return c.inner.Run(ctx, p, a...)
}

// Covers AC-022, FR-019, FR-017: a service stop during the post-run refresh must
// reach the refresh (its context derives from the run context, not from
// Background), and must neither change the outcome nor mark the run interrupted.
// The ADD tests assert the deadline only, and a 5 minute sub-context always ends
// before a basis that still has 7 minutes left, so a refresh context detached
// from the run would pass them.
func TestPostRefreshV2_QA_AC022_ServiceStopDuringRefreshReachesItAndKeepsOutcome(t *testing.T) {
	g := newDRRig(t)
	g.runner.after[upgradeKey] = g.cfg.bump
	svc, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &cancelOnNthRefresh{inner: g.w.Runner, n: 2, cancel: cancel}
	g.w.Runner = c
	rec, err := g.w.Execute(svc, v2Request("full"))
	if err != nil {
		t.Fatalf("[AC-022] a failed post-run refresh must not fail the run: %v", err)
	}
	if !c.sawCancel {
		t.Error("[AC-022][FR-017] the refresh context did not see the service stop: it must derive from the run context")
	}
	if rec.Outcome != "success" || rec.ExitCode != 0 || rec.Interrupted {
		t.Errorf("[AC-022][FR-019] outcome %s exit %d interrupted %v, want success/0/false", rec.Outcome, rec.ExitCode, rec.Interrupted)
	}
	if got := aptResultLine(rec); !strings.HasPrefix(got, SuccessSummary+"; "+noteFailed) {
		t.Errorf("[AC-022] apt result line = %q, want the success text then the failed note", got)
	}
	if err := rec.Validate(); err != nil {
		t.Errorf("[AC-027] record invalid: %v", err)
	}
}

// Covers AC-007, FR-007: an unreadable BASELINE (taken before the run's own
// refresh) also counts as changed. AC-007 only fails the second observation.
func TestPostRefreshV2_QA_AC007_UnreadableBaselineRefreshesOnce(t *testing.T) {
	g := newDRRig(t)
	g.cfg.failCall(1, errors.New("stat failed"))
	mustRun(t, g, false)
	if g.pr.refreshCalls != 2 || g.refreshesAfter("step:dietpi") != 1 {
		t.Errorf("[AC-007] an unreadable baseline must give exactly one post-run refresh after the step: %v", g.log)
	}
}

// Covers AC-005, FR-009: whatever the number of changes, a version 2 run makes
// one post-run check only (the fingerprint source is read exactly twice).
func TestPostRefreshV2_QA_AC005_FingerprintReadTwiceAtMost(t *testing.T) {
	g := newDRRig(t)
	g.runner.after[upgradeKey] = g.cfg.bump
	g.step.onCall = g.cfg.bump
	mustRun(t, g, false)
	if g.cfg.calls != 2 {
		t.Errorf("[AC-005] fingerprint reads = %d, want 2 (baseline and the one post-run check)", g.cfg.calls)
	}
}

// Covers AC-017, FR-014: an interrupted updater alone (the service itself is not
// stopping) makes the run ineligible, silently and before any fingerprint read.
// That is the real shape of an updater interrupted by the 80 minute run context
// expiring: runExtra marks the updater interrupted from runCtx.Err() while the
// service context is still live. The end-to-end AC-017 test cancels the service
// context, so the svc.Err() guard hides the updater-outcome guard from it.
func TestPostRefreshV2_QA_AC017_InterruptedUpdaterAloneSkipsSilently(t *testing.T) {
	g := newDRRig(t)
	g.cfg.bump()
	rec := &Record{
		AptOutcome:    aptOutcomeSuccess,
		ExtraUpdaters: []UpdaterResult{{ID: "dietpi", Outcome: UpdaterInterrupted}},
	}
	st := &aptState{listsRefreshed: true, sourcesBefore: "gen-0"}
	got := g.w.postRunRefreshV2(context.Background(), context.Background(), WorkerBudget, rec, st, g.clock.Now())
	if got != "" {
		t.Errorf("[AC-017] note = %q, want none when an updater was interrupted", got)
	}
	if g.pr.total != 0 {
		t.Errorf("[AC-017] runner calls = %d, want 0", g.pr.total)
	}
	if g.cfg.calls != 0 {
		t.Errorf("[AC-017][C-4] fingerprint calls = %d, want 0", g.cfg.calls)
	}
}
