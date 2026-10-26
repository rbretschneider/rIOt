package maintenance

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DIETPI-PATH-001 FR-012 to FR-014 and AD-3 in the worker rig. No process starts:
// the step runner, the trust walk, the lock probe and the flag cleanup are fakes.

func TestOutcome_DP_AC010_FailingAndRefused(t *testing.T) {
	failing := map[string]bool{UpdaterSuccess: false, UpdaterSkipped: false, UpdaterFailed: true, UpdaterTimedOut: true, UpdaterInterrupted: true, UpdaterRunning: true}
	for outcome, want := range failing {
		t.Run("[AC-010] outcome "+outcome+" failing="+fmt.Sprint(want), func(t *testing.T) {
			if got := (UpdaterResult{Outcome: outcome}).Failing(); got != want {
				t.Errorf("Failing() = %v, want %v", got, want)
			}
		})
	}
	t.Run("[AC-010] a skip is never failing, whatever the reason", func(t *testing.T) {
		for _, reason := range SkipReasons() {
			if (UpdaterResult{Outcome: UpdaterSkipped, Reason: reason}).Failing() {
				t.Errorf("a skip for %s must not fail the run", reason)
			}
		}
	})
	t.Run("[AC-011] Refused is exactly a skip that is not neutral and not apt_not_successful", func(t *testing.T) {
		for _, reason := range SkipReasons() {
			want := reason != SkipNotEnabled && reason != SkipStatusUnknown && reason != SkipNothingPending && reason != SkipAptNotSuccessful
			if got := (UpdaterResult{Outcome: UpdaterSkipped, Reason: reason}).Refused(); got != want {
				t.Errorf("Refused(%s) = %v, want %v", reason, got, want)
			}
		}
	})
	t.Run("[AC-011] only a skip can be refused", func(t *testing.T) {
		for _, outcome := range []string{UpdaterSuccess, UpdaterFailed, UpdaterTimedOut, UpdaterInterrupted, UpdaterRunning} {
			if (UpdaterResult{Outcome: outcome, Reason: SkipUnsafePath}).Refused() {
				t.Errorf("outcome %s is not a refusal", outcome)
			}
		}
	})
	t.Run("[AC-011] SkipReasons lists every known reason once, including the new one, sorted", func(t *testing.T) {
		got := SkipReasons()
		if len(got) != len(skipReasons) {
			t.Fatalf("got %d reasons, want %d", len(got), len(skipReasons))
		}
		has := map[string]bool{}
		for i, r := range got {
			if has[r] {
				t.Errorf("duplicate %s", r)
			}
			has[r] = true
			if i > 0 && got[i-1] >= r {
				t.Errorf("not sorted at %s", r)
			}
		}
		if !has["rundir_not_private"] {
			t.Error("rundir_not_private missing")
		}
	})
}

// refusalCases drives each non-neutral skip reason into the rig.
var refusalCases = []struct {
	reason string
	setup  func(g *extraRig)
	steps  int
}{
	{SkipUnsafePath, func(g *extraRig) { g.trustErr = errors.New("/boot/dietpi/x is writable by group or others") }, 0},
	{SkipDpkgLocked, func(g *extraRig) { g.lock = func() LockState { return LockState{Kind: LockHeld, Detail: "held"} } }, 0},
	{SkipInsufficientTime, func(g *extraRig) { g.unit = UnitState{RuntimeMax: 35 * time.Minute, PrivateTmp: "no"} }, 0},
	{SkipMinInterval, func(g *extraRig) { g.last, g.hasLast = g.clock.Now().Add(-30*time.Minute), true }, 0},
	{SkipBusy, func(g *extraRig) { g.busy = func() (bool, string) { return true, "pid 812" } }, 0},
	{SkipTmpNotPrivate, func(g *extraRig) { g.step.out = StepOutcome{Skip: SkipTmpNotPrivate, SkipDetail: "x"} }, 1},
	{SkipMountPropagationUnver, func(g *extraRig) { g.step.out = StepOutcome{Skip: SkipMountPropagationUnver, SkipDetail: "x"} }, 1},
	{SkipRunDirNotPrivate, func(g *extraRig) { g.step.out = StepOutcome{Skip: SkipRunDirNotPrivate, SkipDetail: "x"} }, 1},
	{SkipNonDefaultSource, func(g *extraRig) {
		writeFixture(g.t, g.root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=SomeFork\n")
	}, 0},
}

func TestExtras_DP_AC010_RefusalKeepsSuccess(t *testing.T) {
	for _, tc := range refusalCases {
		t.Run("[AC-010] apt succeeds and DietPi is skipped for "+tc.reason+": the outcome is success", func(t *testing.T) {
			g := newExtraRig(t)
			tc.setup(g)
			rec, err := g.run(v2Request("full"))
			if err != nil {
				t.Fatalf("a refusal must not return an error: %v", err)
			}
			if rec.Outcome != "success" || rec.ExitCode != 0 || rec.Interrupted {
				t.Fatalf("outcome %q exit %d interrupted %v, want a clean success", rec.Outcome, rec.ExitCode, rec.Interrupted)
			}
			r := g.result(rec)
			if r.Outcome != UpdaterSkipped || r.Reason != tc.reason || r.StartedAt != nil || r.ExitCode != nil {
				t.Errorf("result = %+v, want skipped/%s with no start", r, tc.reason)
			}
			if !r.Refused() {
				t.Errorf("a skip for %s must count as a refusal", tc.reason)
			}
			if want := "apt: succeeded; dietpi: skipped (" + tc.reason + ")"; firstLine(rec.Summary) != want {
				t.Errorf("first line = %q, want %q", firstLine(rec.Summary), want)
			}
			if !strings.Contains(rec.Summary, "dietpi: skipped ("+tc.reason+")") || !strings.Contains(rec.Summary, successSummary) {
				t.Errorf("the detail line or apt result is missing:\n%s", rec.Summary)
			}
			if got := g.stepCalls(); got != tc.steps {
				t.Errorf("step calls = %d, want %d", got, tc.steps)
			}
			if len(g.store.records) == 0 || g.store.records[len(g.store.records)-1].Outcome != "success" {
				t.Error("the stored final record must be the success record")
			}
		})
	}
	t.Run("[AC-010] every non-neutral reason the worker can produce is covered by the table", func(t *testing.T) {
		covered := map[string]bool{}
		for _, tc := range refusalCases {
			covered[tc.reason] = true
		}
		for _, reason := range SkipReasons() {
			neutral := NeutralSkip(reason) || reason == SkipAptNotSuccessful
			if !neutral && !covered[reason] {
				t.Errorf("reason %s has no refusal case", reason)
			}
		}
	})
	t.Run("[AC-010] DietPi started and failed, timed out or interrupted still fail the run", func(t *testing.T) {
		g := newExtraRig(t)
		g.step.out = StepOutcome{Output: "boom\n", ExitCode: 1, Err: errors.New("exit status 1")}
		if rec, err := g.run(v2Request("full")); err == nil || rec.Outcome != "error" || g.result(rec).Outcome != UpdaterFailed {
			t.Errorf("failed: %+v %v", rec, err)
		}
	})
	t.Run("[DP-SEC-004] a launcher setup failure is failed, never skipped, and still fails the run", func(t *testing.T) {
		g := newExtraRig(t)
		g.step.out = StepOutcome{SetupErr: "no ok line within 10s"}
		rec, err := g.run(v2Request("full"))
		if err == nil || rec.Outcome != "error" || rec.ExitCode == 0 {
			t.Fatalf("%+v %v", rec, err)
		}
		if r := g.result(rec); r.Outcome != UpdaterFailed || r.Refused() || !r.Failing() {
			t.Errorf("result = %+v, want failed", r)
		}
	})
}

// spyFlagFS records the order of flag reads against the flag cleanup.
type spyFlagFS struct {
	extraupd.FS
	log *[]string
}

func (s spyFlagFS) ReadTrustedFlag(name string, max int64) ([]byte, error) {
	*s.log = append(*s.log, "read:"+name)
	return s.FS.ReadTrustedFlag(name, max)
}

func TestExtras_DP_AC007_ReconcileAfterSuccessBeforeDetection(t *testing.T) {
	t.Run("[AC-007] after a successful step the flags are reconciled in /run/dietpi, before the post-run detection reads them", func(t *testing.T) {
		g := newExtraRig(t)
		var log []string
		g.w.Extras.FS = spyFlagFS{FS: g.w.Extras.FS, log: &log}
		var dirs []string
		g.w.Extras.ReconcileFlags = func(dir string) string {
			dirs = append(dirs, dir)
			log = append(log, "reconcile")
			return ""
		}
		g.step.onCall = func() { log = append(log, "step") }
		rec, err := g.run(v2Request("full"))
		if err != nil || rec.Outcome != "success" || g.result(rec).Outcome != UpdaterSuccess {
			t.Fatalf("%+v %v", rec, err)
		}
		if len(dirs) != 1 || dirs[0] != "/run/dietpi" {
			t.Fatalf("reconcile calls = %v, want exactly one on /run/dietpi", dirs)
		}
		stepAt, reconcileAt, readAfter := -1, -1, -1
		for i, e := range log {
			switch {
			case e == "step":
				stepAt = i
			case e == "reconcile":
				reconcileAt = i
			case strings.HasPrefix(e, "read:") && reconcileAt >= 0 && readAfter < 0:
				readAfter = i
			}
		}
		if !(stepAt >= 0 && stepAt < reconcileAt && reconcileAt < readAfter) {
			t.Errorf("order: step %d, reconcile %d, first flag read after %d\n%q", stepAt, reconcileAt, readAfter, log)
		}
	})
	t.Run("[AC-007] a failed, timed out or refused step reconciles nothing", func(t *testing.T) {
		for name, setup := range map[string]func(*extraRig){
			"failed":    func(g *extraRig) { g.step.out = StepOutcome{ExitCode: 1, Err: errors.New("exit status 1")} },
			"timed out": func(g *extraRig) { g.w.Extras.StepLimit = time.Nanosecond; g.step.block = true },
			"refused":   func(g *extraRig) { g.trustErr = errors.New("unsafe") },
			"launcher":  func(g *extraRig) { g.step.out = StepOutcome{Skip: SkipRunDirNotPrivate} },
			"setup":     func(g *extraRig) { g.step.out = StepOutcome{SetupErr: "x"} },
		} {
			g := newExtraRig(t)
			calls := 0
			g.w.Extras.ReconcileFlags = func(string) string { calls++; return "" }
			setup(g)
			g.run(v2Request("full"))
			if calls != 0 {
				t.Errorf("%s: reconcile ran %d times", name, calls)
			}
		}
	})
	t.Run("[AC-007] a cleanup problem becomes a note and the outcome stays success", func(t *testing.T) {
		g := newExtraRig(t)
		g.w.Extras.ReconcileFlags = func(string) string { return "flag cleanup failed: .live_patches: input/output error" }
		rec, err := g.run(v2Request("full"))
		if err != nil || rec.Outcome != "success" {
			t.Fatalf("%+v %v", rec, err)
		}
		r := g.result(rec)
		if r.Outcome != UpdaterSuccess || r.Note != "flag cleanup failed: .live_patches: input/output error" {
			t.Errorf("result = %+v, want success with the cleanup note", r)
		}
		if !strings.Contains(rec.Summary, "flag cleanup failed") {
			t.Errorf("the note is missing from the summary:\n%s", rec.Summary)
		}
	})
	t.Run("[AC-007] a long note is bounded to the record's note limit", func(t *testing.T) {
		g := newExtraRig(t)
		g.w.Extras.ReconcileFlags = func(string) string { return strings.Repeat("n", 5000) }
		rec, err := g.run(v2Request("full"))
		if err != nil || len(g.result(rec).Note) > maxUpdaterNote {
			t.Fatalf("note %d bytes, %v", len(g.result(rec).Note), err)
		}
	})
	t.Run("[AC-007] without a reconcile seam nothing happens and the run still succeeds", func(t *testing.T) {
		g := newExtraRig(t)
		g.w.Extras.ReconcileFlags = nil
		if rec, err := g.run(v2Request("full")); err != nil || rec.Outcome != "success" {
			t.Fatalf("%+v %v", rec, err)
		}
	})
	t.Run("[AC-007] DietPi's spec asks for the reconcile and production wires it", func(t *testing.T) {
		if !extraupd.NewDietPi().Apply().ReconcileFlags {
			t.Error("the DietPi spec must set ReconcileFlags")
		}
	})
}

// TestExtras_DP_AC013_PiCUPSLayoutRuns runs the worker rig with the real trust
// walk over the PiCUPS layout: /run/dietpi 0777 and not sticky, and a
// DietPi-RAMlog mirror below /var/tmp/dietpi/logs. Before this fix the same
// layout ended as skipped (unsafe_dietpi_path).
func TestExtras_DP_AC013_PiCUPSLayoutRuns(t *testing.T) {
	g := newExtraRig(t)
	layout := stockDietPi9()
	g.w.Extras.Trust = func(spec extraupd.ApplySpec, installed models.ExtraUpdater) error {
		g.trustSeen++
		return trustWalk(layout, spec, installed)
	}
	rec, err := g.run(v2Request("full"))
	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if g.stepCalls() != 1 || g.result(rec).Outcome != UpdaterSuccess {
		t.Fatalf("steps = %d, result = %+v: DietPi must run on the stock layout", g.stepCalls(), g.result(rec))
	}
	if want := "apt: succeeded; dietpi: succeeded"; firstLine(rec.Summary) != want {
		t.Errorf("first line = %q, want %q", firstLine(rec.Summary), want)
	}
	t.Run("[AC-013] the same host with a hostile boot tree is still refused", func(t *testing.T) {
		g := newExtraRig(t)
		bad := stockDietPi9()
		bad.file("/boot/dietpi/func/dietpi-globals", 0o666, 0)
		g.w.Extras.Trust = func(spec extraupd.ApplySpec, installed models.ExtraUpdater) error {
			return trustWalk(bad, spec, installed)
		}
		rec, err := g.run(v2Request("full"))
		if err != nil || g.stepCalls() != 0 || g.result(rec).Reason != SkipUnsafePath {
			t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
		}
		if r := g.result(rec); !strings.Contains(r.Note, "/boot/dietpi/func/dietpi-globals is writable by group or others") {
			t.Errorf("note = %q, want the named path", r.Note)
		}
	})
}
