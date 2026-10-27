package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

// DIETPI-REFRESH-001: the version 2 post-run package list refresh follows the
// extra-updater step. Nothing here starts apt, dpkg or dietpi-update: every
// command goes through a fake Runner, the updater through a fake StepRunner, and
// the stamp, the store and the clock are fakes (NFR-006).

const (
	noteBudget  = "post-run package list refresh skipped: worker time budget nearly exhausted"
	notePersist = "post-run package list refresh skipped: state not persisted"
	noteFailed  = "post-run package list refresh failed: "

	configureKey = "/usr/bin/dpkg --force-confold --force-confdef --configure -a"
	auditKey     = "/usr/bin/dpkg --audit"
	repairKey    = "/usr/bin/apt-get -y --no-remove -o DPkg::Lock::Timeout=120 -o Acquire::Retries=0 -o Dpkg::Options::=--force-confold -o Dpkg::Options::=--force-confdef --fix-broken install"
)

// cfgState is a mutable apt configuration fingerprint source. The first call is
// the baseline taken before the run's own refresh.
type cfgState struct {
	gen   int
	calls int
	errAt map[int]error
}

func (c *cfgState) bump() { c.gen++ }
func (c *cfgState) failCall(n int, err error) {
	if c.errAt == nil {
		c.errAt = map[int]error{}
	}
	c.errAt[n] = err
}

// sources returns the same fingerprint when nothing was bumped, even for a call
// that reports an error, so that only the error can cause a refresh.
func (c *cfgState) sources() (string, error) {
	c.calls++
	return fmt.Sprintf("gen-%d", c.gen), c.errAt[c.calls]
}

type refreshSeen struct {
	at       time.Time
	deadline time.Time
	hasDL    bool
}

// postRefreshRunner wraps the rig's orderRunner. It counts the refresh command,
// records the context each refresh received, and can replace the result of the
// N-th refresh (1-based), fail a command once, or interrupt the upgrade.
type postRefreshRunner struct {
	inner              *orderRunner
	total              int
	refreshCalls       int
	seen               []refreshSeen
	refreshResult      map[int]CommandResult
	refreshErr         map[int]error
	failOnce           map[string]string
	upgradeInterrupted bool
	upgradeOutput      string
}

func (r *postRefreshRunner) Run(ctx context.Context, p string, a ...string) (CommandResult, error) {
	key := p + " " + strings.Join(a, " ")
	r.total++
	if key == refreshKey {
		r.refreshCalls++
		dl, ok := ctx.Deadline()
		r.seen = append(r.seen, refreshSeen{at: time.Now(), deadline: dl, hasDL: ok})
	}
	res, err := r.inner.Run(ctx, p, a...)
	if key == refreshKey {
		if v, ok := r.refreshResult[r.refreshCalls]; ok {
			res = v
		}
		if e := r.refreshErr[r.refreshCalls]; e != nil {
			err = e
		}
	}
	if msg := r.failOnce[key]; msg != "" {
		delete(r.failOnce, key)
		return CommandResult{Output: msg, ExitCode: 100}, errors.New(msg)
	}
	if key == upgradeKey {
		if r.upgradeInterrupted {
			return CommandResult{ExitCode: 1, Interrupted: true}, errors.New("signal: terminated")
		}
		if r.upgradeOutput != "" && err == nil {
			res.Output = r.upgradeOutput
		}
	}
	return res, err
}

// loggingTouch is the fake lists stamp. It records every stamp in the rig log
// and can fail on the N-th call (1-based).
type loggingTouch struct {
	log    *[]string
	calls  int
	failOn map[int]error
}

func (l *loggingTouch) touch() error {
	l.calls++
	*l.log = append(*l.log, "stamp")
	return l.failOn[l.calls]
}

// drRig is an extraRig wired for the post-run refresh tests.
type drRig struct {
	*extraRig
	cfg   *cfgState
	pr    *postRefreshRunner
	touch *loggingTouch
}

func newDRRig(t *testing.T) *drRig {
	t.Helper()
	g := newExtraRig(t)
	cfg := &cfgState{}
	pr := &postRefreshRunner{inner: g.runner, failOnce: map[string]string{}, refreshResult: map[int]CommandResult{}, refreshErr: map[int]error{}}
	touch := &loggingTouch{log: &g.log}
	g.w.Runner = pr
	g.w.Sources = cfg.sources
	g.w.TouchLists = touch.touch
	return &drRig{extraRig: g, cfg: cfg, pr: pr, touch: touch}
}

// oracleRig is the same rig with no fingerprint source. Before and after this
// story a run without a Sources func makes no post-run check, so it records what
// the pre-story helper records for the same inputs.
func oracleRig(t *testing.T, setup func(*drRig)) *drRig {
	t.Helper()
	g := newDRRig(t)
	g.w.Sources = nil
	setup(g)
	return g
}

func (g *drRig) logIdx(entry string) int {
	for i, e := range g.log {
		if e == entry {
			return i
		}
	}
	return -1
}

func (g *drRig) lastLogIdx(entry string) int {
	at := -1
	for i, e := range g.log {
		if e == entry {
			at = i
		}
	}
	return at
}

// refreshesAfter counts post-run refresh commands logged after the given entry.
func (g *drRig) refreshesAfter(entry string) int {
	from := g.logIdx(entry)
	if from < 0 {
		return 0
	}
	n := 0
	for i, e := range g.log {
		if i > from && e == "apt:"+refreshKey {
			n++
		}
	}
	return n
}

func (g *drRig) savedPhases() []string {
	var out []string
	for _, r := range g.store.records {
		out = append(out, r.Phase)
	}
	return out
}

// aptResultLine is line 2 of a summary that has an updater first line, else
// line 1.
func aptResultLine(rec Record) string {
	lines := strings.Split(rec.Summary, "\n")
	if len(rec.ExtraUpdaters) > 0 && len(lines) > 1 {
		return lines[1]
	}
	return lines[0]
}

type outcomeSig struct {
	Outcome     string
	ExitCode    int
	Interrupted bool
	AptOutcome  string
}

func sigOf(rec Record) outcomeSig {
	return outcomeSig{rec.Outcome, rec.ExitCode, rec.Interrupted, rec.AptOutcome}
}

func realSources(root string) func() (string, error) {
	return func() (string, error) {
		entries, err := AptSourceEntries(root, 1000)
		if err != nil {
			return "", err
		}
		return AptSourcesFingerprint(entries), nil
	}
}

func setMtime(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func mustRun(t *testing.T, g *drRig, wantErr bool) Record {
	t.Helper()
	rec, err := g.run(v2Request("full"))
	if (err != nil) != wantErr {
		t.Fatalf("run err = %v, wantErr %v; summary %q", err, wantErr, rec.Summary)
	}
	return rec
}

// ---------------------------------------------------------------------------

func TestPostRefreshV2_DR_AC003_DietPiRewritesPreferencesD(t *testing.T) {
	const rel = "etc/apt/preferences.d/dietpi-raspberrypi"
	prepare := func(t *testing.T, touchInStep bool) (*drRig, string) {
		g := newDRRig(t)
		writeFixture(t, g.root, rel, "Package: *\nPin: release n=trixie\nPin-Priority: 1\n")
		path := filepath.Join(g.root, filepath.FromSlash(rel))
		setMtime(t, path, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
		g.w.Sources = realSources(g.root)
		if _, err := g.w.Sources(); err != nil {
			t.Fatalf("[AC-003] fixture fingerprint unreadable, the test would pass for the wrong reason: %v", err)
		}
		if touchInStep {
			g.step.onCall = func() { setMtime(t, path, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)) }
		}
		return g, path
	}
	t.Run("[AC-003] one post-run refresh after the DietPi step, stamped after it, no note", func(t *testing.T) {
		g, _ := prepare(t, true)
		rec := mustRun(t, g, false)
		step := g.logIdx("step:dietpi")
		if g.pr.refreshCalls != 2 {
			t.Fatalf("[AC-003] refresh commands = %d, want 2 (own + one post-run): %v", g.pr.refreshCalls, g.log)
		}
		if g.refreshesAfter("step:dietpi") != 1 {
			t.Errorf("[AC-003] the post-run refresh must start after the DietPi step: %v", g.log)
		}
		if g.lastLogIdx("stamp") < step {
			t.Errorf("[AC-003] the list stamp must be set after the DietPi step ended: %v", g.log)
		}
		if rec.Outcome != "success" || rec.ExitCode != 0 {
			t.Errorf("[AC-003] outcome %s exit %d, want success/0", rec.Outcome, rec.ExitCode)
		}
		if got := aptResultLine(rec); got != SuccessSummary {
			t.Errorf("[AC-003] apt result line = %q, want exactly %q", got, SuccessSummary)
		}
	})
	t.Run("[AC-003] control: the same run without the rewrite does not refresh again", func(t *testing.T) {
		g, _ := prepare(t, false)
		mustRun(t, g, false)
		if g.pr.refreshCalls != 1 {
			t.Errorf("[AC-003] refresh commands = %d, want 1: the real fingerprint must be stable when nothing changed", g.pr.refreshCalls)
		}
	})
}

func TestPostRefreshV2_DR_AC004_AptChangeRefreshedAfterUpdater(t *testing.T) {
	g := newDRRig(t)
	g.runner.after[upgradeKey] = g.cfg.bump
	rec := mustRun(t, g, false)
	upgrade, step := g.logIdx("apt:"+upgradeKey), g.logIdx("step:dietpi")
	for i := upgrade; i < step; i++ {
		if g.log[i] == "apt:"+refreshKey {
			t.Errorf("[AC-004] a post-run refresh ran during the apt part: %v", g.log)
		}
	}
	if g.refreshesAfter("step:dietpi") != 1 || g.pr.refreshCalls != 2 {
		t.Errorf("[AC-004] want exactly one refresh after the updater step, log %v", g.log)
	}
	extra, post := strings.Index(rec.Output, "[extra:dietpi]"), strings.Index(rec.Output, "[post_refresh]")
	if extra < 0 || post < extra {
		t.Errorf("[AC-004] [post_refresh] (at %d) must follow the updater section (at %d)", post, extra)
	}
}

func TestPostRefreshV2_DR_AC005_AptAndUpdaterChangeRefreshOnce(t *testing.T) {
	g := newDRRig(t)
	g.runner.after[upgradeKey] = g.cfg.bump
	g.step.onCall = g.cfg.bump
	mustRun(t, g, false)
	if g.pr.refreshCalls != 2 {
		t.Errorf("[AC-005] refresh commands = %d, want 2 in the whole run: %v", g.pr.refreshCalls, g.log)
	}
}

func TestPostRefreshV2_DR_AC006_NoChangeRecordIdenticalToOracle(t *testing.T) {
	g := newDRRig(t)
	rec := mustRun(t, g, false)
	if g.pr.refreshCalls != 1 {
		t.Errorf("[AC-006] refresh commands = %d, want only the run's own", g.pr.refreshCalls)
	}
	for _, p := range g.savedPhases() {
		if p == "post_refresh" {
			t.Errorf("[AC-006] post_refresh phase persisted without a change: %v", g.savedPhases())
		}
	}
	og := oracleRig(t, func(*drRig) {})
	orec := mustRun(t, og, false)
	got, _ := json.Marshal(rec)
	want, _ := json.Marshal(orec)
	if string(got) != string(want) {
		t.Errorf("[AC-006] record differs from the pre-story record\n got: %s\nwant: %s", got, want)
	}
	if g.cfg.calls != 2 {
		t.Errorf("[AC-006] fingerprint calls = %d, want 2 (baseline and one check)", g.cfg.calls)
	}
}

func TestPostRefreshV2_DR_AC007_AfterUpdaterFingerprintErrorRefreshes(t *testing.T) {
	g := newDRRig(t)
	g.w.Sources = sourcesWithErrors("same", nil, errors.New("stat failed"))
	mustRun(t, g, false)
	if g.pr.refreshCalls != 2 || g.refreshesAfter("step:dietpi") != 1 {
		t.Errorf("[AC-007] an unreadable configuration after the updater must refresh once: %v", g.log)
	}
	g2 := newDRRig(t)
	g2.cfg.failCall(2, errors.New("stat failed"))
	mustRun(t, g2, false)
	if g2.pr.refreshCalls != 2 {
		t.Errorf("[AC-007] cfgState error on the check: refresh commands = %d, want 2", g2.pr.refreshCalls)
	}
}

func TestPostRefreshV2_DR_AC008_ChangeDuringRecoveryIsBaseline(t *testing.T) {
	g := newDRRig(t)
	g.pr.failOnce[configureKey] = "dpkg was interrupted"
	g.runner.after[configureKey] = g.cfg.bump
	mustRun(t, g, false)
	if !strings.Contains(strings.Join(g.log, "\n"), "--fix-broken install") {
		t.Fatalf("[AC-008] the scenario must have gone through repair: %v", g.log)
	}
	if g.pr.refreshCalls != 1 {
		t.Errorf("[AC-008] refresh commands = %d, want 1: a change before the run's own refresh is the baseline", g.pr.refreshCalls)
	}
}

func TestPostRefreshV2_DR_AC009_NoUpdaterPresentStillChecks(t *testing.T) {
	g := newDRRig(t)
	if err := os.Remove(filepath.Join(g.root, "boot", "dietpi", ".version")); err != nil {
		t.Fatal(err)
	}
	g.runner.after[upgradeKey] = g.cfg.bump
	rec := mustRun(t, g, false)
	if len(rec.ExtraUpdaters) != 0 || g.stepCalls() != 0 {
		t.Fatalf("[AC-009] the scenario must have no present updater: %+v", rec.ExtraUpdaters)
	}
	if g.pr.refreshCalls != 2 {
		t.Errorf("[AC-009] refresh commands = %d, want 2", g.pr.refreshCalls)
	}
	phases := g.savedPhases()
	if n := len(phases); n < 2 || phases[n-2] != "post_refresh" || phases[n-1] != "finished" {
		t.Errorf("[AC-009] last saved phases = %v, want ..., post_refresh, finished", phases)
	}
	if i := strings.LastIndex(rec.Output, "\n["); i < 0 || !strings.HasPrefix(rec.Output[i:], "\n[post_refresh]") {
		t.Errorf("[AC-009] [post_refresh] must be the last output section: %q", rec.Output)
	}
}

func TestPostRefreshV2_DR_AC010_FailureBeforeOwnRefresh(t *testing.T) {
	cases := map[string]func(g *drRig){
		"configure and repair fail": func(g *drRig) {
			g.runner.failAt[configureKey] = "dpkg broken"
			g.runner.failAt[repairKey] = "cannot repair"
		},
		"verification fails after repair": func(g *drRig) {
			g.pr.failOnce[configureKey] = "dpkg was interrupted"
			g.runner.failAt[auditKey] = "still broken"
		},
	}
	for name, setup := range cases {
		t.Run("[AC-010] "+name, func(t *testing.T) {
			g := newDRRig(t)
			setup(g)
			g.runner.after[configureKey] = g.cfg.bump
			rec := mustRun(t, g, true)
			if g.pr.refreshCalls != 0 {
				t.Errorf("[AC-010] refresh commands = %d, want 0: %v", g.pr.refreshCalls, g.log)
			}
			if g.cfg.calls != 0 {
				t.Errorf("[AC-010][C-4] fingerprint calls = %d, want 0: the guards must run before any I/O", g.cfg.calls)
			}
			if strings.Contains(rec.Summary, "post-run") {
				t.Errorf("[AC-010] summary carries a post-run note: %q", rec.Summary)
			}
		})
	}
}

func TestPostRefreshV2_DR_AC011_OwnRefreshFailsNoRetry(t *testing.T) {
	g := newDRRig(t)
	g.runner.failAt[refreshKey] = "repository signature failed"
	g.runner.after[refreshKey] = g.cfg.bump
	rec := mustRun(t, g, true)
	if g.pr.refreshCalls != 1 {
		t.Errorf("[AC-011] refresh commands = %d, want exactly 1 (no retry, no post-run refresh): %v", g.pr.refreshCalls, g.log)
	}
	if strings.Contains(rec.Summary, "post-run") {
		t.Errorf("[AC-011] summary carries a post-run note: %q", rec.Summary)
	}
}

func TestPostRefreshV2_DR_AC011_RefreshPhaseNotPersisted(t *testing.T) {
	g := newDRRig(t)
	g.store.failPhase = "refresh"
	g.runner.after[upgradeKey] = g.cfg.bump
	rec := mustRun(t, g, true)
	if g.pr.refreshCalls != 0 {
		t.Errorf("[M-4] refresh commands = %d, want 0: the run's own refresh never ran, so there is nothing to follow up: %v", g.pr.refreshCalls, g.log)
	}
	if g.cfg.calls > 1 {
		t.Errorf("[M-4] fingerprint calls = %d, want at most the baseline", g.cfg.calls)
	}
	if strings.Contains(rec.Summary, "post-run") {
		t.Errorf("[M-4] summary carries a post-run note: %q", rec.Summary)
	}
}

func upgradeFailsAfterChange(g *drRig, msg string) {
	g.runner.after[upgradeKey] = g.cfg.bump
	g.runner.failAt[upgradeKey] = msg
}

func TestPostRefreshV2_DR_AC012_UpgradeFailureStillRefreshes(t *testing.T) {
	const msg = "E: Unable to correct problems"
	g := newDRRig(t)
	upgradeFailsAfterChange(g, msg)
	rec := mustRun(t, g, true)
	if g.pr.refreshCalls != 2 {
		t.Fatalf("[AC-012] refresh commands = %d, want 2: %v", g.pr.refreshCalls, g.log)
	}
	lastApt := ""
	for _, e := range g.log {
		if strings.HasPrefix(e, "apt:") {
			lastApt = e
		}
	}
	if lastApt != "apt:"+refreshKey {
		t.Errorf("[AC-012] the post-run refresh must be the last apt call, last was %q", lastApt)
	}
	if rec.Outcome != "error" || rec.AptOutcome != aptOutcomeError || rec.ExitCode != 100 {
		t.Errorf("[AC-012] outcome %s apt %s exit %d, want error/error/100", rec.Outcome, rec.AptOutcome, rec.ExitCode)
	}
	og := oracleRig(t, func(o *drRig) { upgradeFailsAfterChange(o, msg) })
	orec := mustRun(t, og, true)
	if got, want := g.result(rec), og.result(orec); !reflect.DeepEqual(got, want) {
		t.Errorf("[AC-012] updater result changed by the refresh\n got: %+v\nwant: %+v", got, want)
	}
	if r := g.result(rec); r.Outcome != UpdaterSkipped || r.Reason != SkipAptNotSuccessful {
		t.Errorf("[AC-012] dietpi = %+v, want skipped/apt_not_successful", r)
	}
	if sigOf(rec) != sigOf(orec) {
		t.Errorf("[AC-012] outcome %+v differs from the pre-story %+v", sigOf(rec), sigOf(orec))
	}
}

func TestPostRefreshV2_DR_AC013_UpdaterFailedStillRefreshes(t *testing.T) {
	setup := func(g *drRig) {
		g.step.onCall = g.cfg.bump
		g.step.out = StepOutcome{Output: "boom\n", ExitCode: 1, Err: errors.New("exit status 1")}
	}
	g := newDRRig(t)
	setup(g)
	rec := mustRun(t, g, true)
	if g.pr.refreshCalls != 2 || g.refreshesAfter("step:dietpi") != 1 {
		t.Errorf("[AC-013] want one post-run refresh after the step: %v", g.log)
	}
	if rec.Outcome != "error" || rec.ExitCode != 1 {
		t.Errorf("[AC-013] outcome %s exit %d, want error/1", rec.Outcome, rec.ExitCode)
	}
	og := oracleRig(t, setup)
	orec := mustRun(t, og, true)
	if got, want := g.result(rec), og.result(orec); !reflect.DeepEqual(got, want) {
		t.Errorf("[AC-013] dietpi result changed by the refresh\n got: %+v\nwant: %+v", got, want)
	}
	if r := g.result(rec); r.Outcome != UpdaterFailed || r.ExitCode == nil || *r.ExitCode != 1 {
		t.Errorf("[AC-013] dietpi = %+v, want failed with exit 1", r)
	}
}

func TestPostRefreshV2_DR_AC014_UpdaterTimedOutStillRefreshes(t *testing.T) {
	g := newDRRig(t)
	g.step.block = true
	g.w.Extras.StepLimit = 50 * time.Millisecond
	g.step.onCall = g.cfg.bump
	rec := mustRun(t, g, true)
	if g.pr.refreshCalls != 2 || g.refreshesAfter("step:dietpi") != 1 {
		t.Errorf("[AC-014] want one post-run refresh after the timed-out step: %v", g.log)
	}
	if r := g.result(rec); r.Outcome != UpdaterTimedOut {
		t.Errorf("[AC-014] dietpi outcome = %s, want timed_out", r.Outcome)
	}
}

func TestPostRefreshV2_DR_AC015_UpdaterRefusedStillRefreshes(t *testing.T) {
	setup := func(g *drRig) {
		g.lock = func() LockState { return LockState{Kind: LockHeld, Detail: "held by apt"} }
		g.runner.after[upgradeKey] = g.cfg.bump
	}
	g := newDRRig(t)
	setup(g)
	rec := mustRun(t, g, false)
	if g.pr.refreshCalls != 2 || g.stepCalls() != 0 || g.log[len(g.log)-2] != "apt:"+refreshKey {
		t.Errorf("[AC-015] want one post-run refresh as the last apt call and no DietPi step: %v", g.log)
	}
	og := oracleRig(t, setup)
	orec := mustRun(t, og, false)
	got := g.result(rec)
	if got.Outcome != UpdaterSkipped || got.Reason != SkipDpkgLocked {
		t.Errorf("[AC-015] dietpi = %+v, want skipped/dpkg_locked", got)
	}
	if want := og.result(orec); !reflect.DeepEqual(got, want) {
		t.Errorf("[AC-015] dietpi result changed by the refresh\n got: %+v\nwant: %+v", got, want)
	}
	if sigOf(rec) != sigOf(orec) {
		t.Errorf("[AC-015] run outcome %+v differs from the pre-story %+v", sigOf(rec), sigOf(orec))
	}
}

func TestPostRefreshV2_DR_AC016_AptInterruptedNoRefresh(t *testing.T) {
	t.Run("[AC-016] the upgrade command is interrupted", func(t *testing.T) {
		g := newDRRig(t)
		g.runner.after[upgradeKey] = g.cfg.bump
		g.pr.upgradeInterrupted = true
		rec := mustRun(t, g, true)
		if rec.AptOutcome != aptOutcomeInterrupt {
			t.Fatalf("[AC-016] apt outcome = %s, want interrupted", rec.AptOutcome)
		}
		if g.pr.refreshCalls != 1 || g.cfg.calls > 1 {
			t.Errorf("[AC-016] refresh commands %d, fingerprint calls %d, want 1 and at most 1: %v", g.pr.refreshCalls, g.cfg.calls, g.log)
		}
		if strings.Contains(rec.Summary, "post-run") {
			t.Errorf("[AC-016] summary carries a post-run note: %q", rec.Summary)
		}
	})
	t.Run("[AC-016] the service context ends during the apt part", func(t *testing.T) {
		g := newDRRig(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		g.runner.after[upgradeKey] = func() { g.cfg.bump(); cancel() }
		rec, err := g.w.Execute(ctx, v2Request("full"))
		if err == nil || rec.AptOutcome != aptOutcomeInterrupt {
			t.Fatalf("[AC-016] err %v apt outcome %s, want an interrupted apt part", err, rec.AptOutcome)
		}
		if g.pr.refreshCalls != 1 || g.cfg.calls > 1 {
			t.Errorf("[AC-016] refresh commands %d, fingerprint calls %d, want 1 and at most 1", g.pr.refreshCalls, g.cfg.calls)
		}
		if strings.Contains(rec.Summary, "post-run") {
			t.Errorf("[AC-016] summary carries a post-run note: %q", rec.Summary)
		}
	})
}

func TestSummaryV2_DR_AC016_InterruptedArmNeverCarriesNote(t *testing.T) {
	const note = "post-run package list refresh failed: x"
	one := extraRun{res: UpdaterResult{ID: "dietpi", Outcome: UpdaterSkipped, Reason: SkipAptNotSuccessful}}
	for name, runs := range map[string]extraRuns{"no updater": nil, "one updater": {one}} {
		t.Run("[AC-016][DR-SEC-003] "+name, func(t *testing.T) {
			w := &Worker{}
			rec := &Record{AptOutcome: aptOutcomeInterrupt}
			st := &aptState{postRefreshNote: note}
			got := w.summaryV2(rec, st, errors.New("signal: terminated"), runs)
			if !strings.Contains(got, "Maintenance interrupted;") {
				t.Errorf("[AC-016] summary lacks the interruption text: %q", got)
			}
			if strings.Contains(got, "post-run") {
				t.Errorf("[AC-016] the interrupted arm must never carry the note: %q", got)
			}
		})
	}
}

func TestPostRefreshV2_DR_AC017_UpdaterInterruptedNoRefresh(t *testing.T) {
	g := newDRRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.runner.after[upgradeKey] = g.cfg.bump
	g.step.block = true
	g.step.onCall = cancel
	rec, err := g.w.Execute(ctx, v2Request("full"))
	if err == nil || rec.Outcome != "interrupted" {
		t.Fatalf("[AC-017] err %v outcome %s, want interrupted", err, rec.Outcome)
	}
	if r := g.result(rec); r.Outcome != UpdaterInterrupted {
		t.Fatalf("[AC-017] dietpi = %+v, want interrupted", r)
	}
	if g.pr.refreshCalls != 1 {
		t.Errorf("[AC-017] refresh commands = %d, want only the run's own", g.pr.refreshCalls)
	}
	if g.cfg.calls != 1 {
		t.Errorf("[AC-017][C-4] fingerprint calls = %d, want 1 (the baseline only)", g.cfg.calls)
	}
	if strings.Contains(rec.Summary, "post-run") {
		t.Errorf("[AC-017] summary carries a post-run note: %q", rec.Summary)
	}
}

func TestPostRefreshV2_DR_AC017_StoppedServiceSkipsSilently(t *testing.T) {
	g := newDRRig(t)
	g.cfg.bump()
	svc, cancel := context.WithCancel(context.Background())
	cancel()
	rec := &Record{AptOutcome: aptOutcomeSuccess}
	st := &aptState{listsRefreshed: true, sourcesBefore: "gen-0"}
	got := g.w.postRunRefreshV2(svc, context.Background(), WorkerBudget, rec, st, g.clock.Now())
	if got != "" {
		t.Errorf("[AC-017] note = %q, want none for a stopping service", got)
	}
	if g.pr.total != 0 {
		t.Errorf("[AC-017] runner calls = %d, want 0", g.pr.total)
	}
	if g.cfg.calls != 0 {
		t.Errorf("[AC-017][C-4] fingerprint calls = %d, want 0", g.cfg.calls)
	}
}

// advanceInStep moves the injected clock from inside the updater step, after
// check 7 has passed.
func advanceInStep(g *drRig, d time.Duration) {
	g.runner.after[upgradeKey] = g.cfg.bump
	g.step.onCall = func() { g.clock.Advance(d) }
}

func TestPostRefreshV2_DR_AC018_BudgetNearlyExhausted(t *testing.T) {
	t.Run("[AC-018] 5 minutes of the extended budget left skips with the note", func(t *testing.T) {
		g := newDRRig(t)
		advanceInStep(g, ExtendedWorkerBudget-5*time.Minute)
		rec := mustRun(t, g, false)
		if g.refreshesAfter("step:dietpi") != 0 || g.pr.refreshCalls != 1 {
			t.Errorf("[AC-018] a refresh ran with too little budget: %v", g.log)
		}
		if got := aptResultLine(rec); !strings.HasSuffix(got, "; "+noteBudget) {
			t.Errorf("[AC-018] apt result line = %q, want it to end with the budget note", got)
		}
		og := oracleRig(t, func(o *drRig) { advanceInStep(o, ExtendedWorkerBudget-5*time.Minute) })
		orec := mustRun(t, og, false)
		if sigOf(rec) != sigOf(orec) {
			t.Errorf("[AC-018] outcome %+v differs from the run without a change %+v", sigOf(rec), sigOf(orec))
		}
	})
	t.Run("[AC-018] the real context deadline also guards", func(t *testing.T) {
		g := newDRRig(t)
		g.runner.after[upgradeKey] = g.cfg.bump
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		rec, err := g.w.Execute(ctx, v2Request("full"))
		if err != nil {
			t.Fatal(err)
		}
		if g.refreshesAfter("step:dietpi") != 0 {
			t.Errorf("[AC-018] a refresh ran with 6 minutes of real deadline left: %v", g.log)
		}
		if got := aptResultLine(rec); !strings.HasSuffix(got, "; "+noteBudget) {
			t.Errorf("[AC-018] apt result line = %q, want it to end with the budget note", got)
		}
	})
}

func TestPostRefreshV2_DR_AC019_BudgetBoundary(t *testing.T) {
	t.Run("[AC-019] exactly the minimum remaining is enough", func(t *testing.T) {
		g := newDRRig(t)
		advanceInStep(g, ExtendedWorkerBudget-postRefreshMinRemaining)
		rec := mustRun(t, g, false)
		if g.refreshesAfter("step:dietpi") != 1 {
			t.Errorf("[AC-019] 7m00s remaining must refresh: %v", g.log)
		}
		if got := aptResultLine(rec); got != SuccessSummary {
			t.Errorf("[AC-019] apt result line = %q, want the plain success summary", got)
		}
	})
	t.Run("[AC-019] one second less skips with the note", func(t *testing.T) {
		g := newDRRig(t)
		advanceInStep(g, ExtendedWorkerBudget-postRefreshMinRemaining+time.Second)
		rec := mustRun(t, g, false)
		if g.refreshesAfter("step:dietpi") != 0 {
			t.Errorf("[AC-019] 6m59s remaining must not refresh: %v", g.log)
		}
		if got := aptResultLine(rec); !strings.HasSuffix(got, "; "+noteBudget) {
			t.Errorf("[AC-019] apt result line = %q, want the budget note", got)
		}
	})
}

func TestPostRefreshV2_DR_AC020_RefreshLimit(t *testing.T) {
	t.Run("[AC-020] the refresh deadline is within its limit and the extended budget", func(t *testing.T) {
		g := newDRRig(t)
		g.runner.after[upgradeKey] = g.cfg.bump
		rec := mustRun(t, g, false)
		after := time.Now()
		if g.pr.refreshCalls != 2 {
			t.Fatalf("[AC-020] refresh commands = %d, want 2: %v", g.pr.refreshCalls, g.log)
		}
		s := g.pr.seen[1]
		if !s.hasDL {
			t.Fatal("[AC-020] the post-run refresh context has no deadline")
		}
		if d := s.deadline.Sub(s.at); d > PostRefreshBudget {
			t.Errorf("[AC-020] deadline %s after the call, want at most %s", d, PostRefreshBudget)
		}
		if s.deadline.After(after.Add(ExtendedWorkerBudget)) {
			t.Errorf("[AC-020] deadline %s is beyond the extended budget", s.deadline)
		}
		if rec.Outcome != "success" {
			t.Errorf("[AC-020] outcome = %s", rec.Outcome)
		}
	})
	t.Run("[AC-020] a short service deadline bounds the refresh deadline", func(t *testing.T) {
		g := newDRRig(t)
		g.runner.after[upgradeKey] = g.cfg.bump
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute+30*time.Second)
		defer cancel()
		svcDeadline, _ := ctx.Deadline()
		if _, err := g.w.Execute(ctx, v2Request("full")); err != nil {
			t.Fatal(err)
		}
		if g.pr.refreshCalls != 2 {
			t.Fatalf("[AC-020] refresh commands = %d, want 2 (7m30s left is enough): %v", g.pr.refreshCalls, g.log)
		}
		if s := g.pr.seen[1]; !s.hasDL || s.deadline.After(svcDeadline) {
			t.Errorf("[AC-020] refresh deadline %v (set %v) is later than the service deadline %v", s.deadline, s.hasDL, svcDeadline)
		}
	})
}

func TestPostRefreshV2_DR_AC021_PhaseNotPersisted(t *testing.T) {
	g := newDRRig(t)
	g.store.failPhase = "post_refresh"
	g.runner.after[upgradeKey] = g.cfg.bump
	rec := mustRun(t, g, false)
	if g.refreshesAfter("step:dietpi") != 0 {
		t.Errorf("[AC-021] a refresh ran although its phase could not be persisted: %v", g.log)
	}
	if got := aptResultLine(rec); !strings.HasSuffix(got, "; "+notePersist) {
		t.Errorf("[AC-021] apt result line = %q, want it to end with %q", got, notePersist)
	}
	if rec.Outcome != "success" {
		t.Errorf("[AC-021] outcome = %s, want success", rec.Outcome)
	}
}

func TestPostRefreshV2_DR_AC022_RefreshFailsAfterFullSuccess(t *testing.T) {
	g := newDRRig(t)
	g.runner.after[upgradeKey] = g.cfg.bump
	g.pr.refreshResult[2] = CommandResult{Output: "Err:1 deb", Interrupted: true, ExitCode: 100}
	g.pr.refreshErr[2] = errors.New(strings.Repeat("r", 1024))
	rec := mustRun(t, g, false)
	if rec.Outcome != "success" || rec.ExitCode != 0 || rec.Interrupted {
		t.Errorf("[AC-022] outcome %s exit %d interrupted %v, want success/0/false", rec.Outcome, rec.ExitCode, rec.Interrupted)
	}
	want := SuccessSummary + "; " + noteFailed + strings.Repeat("r", 200)
	if got := aptResultLine(rec); got != want {
		t.Errorf("[AC-022] apt result line = %q, want %q", got, want)
	}
	if err := rec.Validate(); err != nil {
		t.Errorf("[AC-022] record invalid: %v", err)
	}
	if r := g.result(rec); r.Outcome != UpdaterSuccess {
		t.Errorf("[AC-022] the updater result must be untouched, got %+v", r)
	}
}

func TestPostRefreshV2_DR_AC023_NoteAfterAptFailure(t *testing.T) {
	g := newDRRig(t)
	long := strings.Repeat("e", 2048)
	upgradeFailsAfterChange(g, long)
	g.pr.refreshErr[2] = errors.New("refresh boom")
	rec := mustRun(t, g, true)
	want := boundedText("upgrade failed: "+long, 1500) + "; " + noteFailed + "refresh boom"
	if got := aptResultLine(rec); got != want {
		t.Errorf("[AC-023] apt result line differs\n got: %.200q (len %d)\nwant: %.200q (len %d)", got, len(got), want, len(want))
	}
	if rec.Outcome != "error" || rec.AptOutcome != aptOutcomeError {
		t.Errorf("[AC-023] outcome %s apt %s, want error/error", rec.Outcome, rec.AptOutcome)
	}
}

func TestSummaryV2_DR_AC023_ErrorArmAppendsNoteOnce(t *testing.T) {
	const note = "post-run package list refresh failed: x"
	w := &Worker{}
	rec := &Record{AptOutcome: aptOutcomeError}
	st := &aptState{postRefreshNote: note}
	got := w.summaryV2(rec, st, errors.New(strings.Repeat("x", 2000)), nil)
	if want := strings.Repeat("x", 1500) + "; " + note; got != want {
		t.Errorf("[AC-023][DR-SEC-003] summary = %.80q (len %d), want the 1500-byte failure text then the note", got, len(got))
	}
	if n := strings.Count(got, "post-run"); n != 1 {
		t.Errorf("[AC-023] the note appears %d times, want once", n)
	}
}

func TestPostRefreshV2_DR_AC024_StampFailureIsOutputOnly(t *testing.T) {
	run := func(t *testing.T, stampErr error) Record {
		g := newDRRig(t)
		g.runner.after[upgradeKey] = g.cfg.bump
		if stampErr != nil {
			g.touch.failOn = map[int]error{2: stampErr}
		}
		return mustRun(t, g, false)
	}
	failed := run(t, errors.New("read-only file system"))
	ok := run(t, nil)
	stamp, post := strings.Index(failed.Output, "[lists_stamp]"), strings.Index(failed.Output, "[post_refresh]")
	if stamp < 0 || post < 0 || stamp < post {
		t.Errorf("[AC-024] [lists_stamp] (at %d) must follow [post_refresh] (at %d): %q", stamp, post, failed.Output)
	}
	if strings.Contains(ok.Output, "[lists_stamp]") {
		t.Errorf("[AC-024] a working stamp must leave no [lists_stamp] line")
	}
	// The apt result line is the part of the summary a stamp failure could reach;
	// comparing it (not the updater lines) keeps the test independent of DietPi
	// detection on the fixture host.
	if failed.Outcome != ok.Outcome || failed.ExitCode != ok.ExitCode || aptResultLine(failed) != aptResultLine(ok) {
		t.Errorf("[AC-024] a stamp failure changed the outcome or summary: %q vs %q", failed.Summary, ok.Summary)
	}
	if got := aptResultLine(failed); got != SuccessSummary {
		t.Errorf("[AC-024] apt result line = %q, want exactly %q", got, SuccessSummary)
	}
}

func TestPostRefreshV2_DR_AC025_OutputAndSummaryBounds(t *testing.T) {
	g := newDRRig(t)
	g.runner.after[upgradeKey] = g.cfg.bump
	g.pr.upgradeOutput = strings.Repeat("u", 200*1024)
	g.pr.refreshResult[2] = CommandResult{Output: strings.Repeat("p", 64*1024-8) + "ENDTAIL\n"}
	g.pr.refreshErr[2] = errors.New(strings.Repeat("r", 1024))
	rec := mustRun(t, g, false)
	if len(rec.Output) > MaxOutputBytes {
		t.Errorf("[AC-025] output is %d bytes, over the %d limit", len(rec.Output), MaxOutputBytes)
	}
	if !rec.OutputTruncated || !strings.Contains(rec.Output, "[output truncated; see system journal]") {
		t.Errorf("[AC-025] output must carry the truncation flag and marker")
	}
	if !strings.HasSuffix(rec.Output, "ENDTAIL\n") {
		t.Errorf("[AC-025] output must end with the [post_refresh] content, ends %q", rec.Output[len(rec.Output)-40:])
	}
	if len(rec.Summary) > 4096 {
		t.Errorf("[AC-025] summary is %d bytes, over 4096", len(rec.Summary))
	}
	line := aptResultLine(rec)
	i := strings.Index(line, noteFailed)
	if i < 0 {
		t.Fatalf("[AC-025] no failure note in %q", line)
	}
	if reason := line[i+len(noteFailed):]; len(reason) != 200 {
		t.Errorf("[AC-025] reason is %d bytes, want 200", len(reason))
	}
}

func TestPostRefreshV2_DR_AC026_SameRefreshCommand(t *testing.T) {
	g := newDRRig(t)
	g.runner.after[upgradeKey] = g.cfg.bump
	mustRun(t, g, false)
	var refreshes []invocation
	for _, c := range g.runner.calls {
		if isRefresh(c) {
			refreshes = append(refreshes, c)
		}
	}
	if len(refreshes) != 2 {
		t.Fatalf("[AC-026] refresh invocations = %d, want 2", len(refreshes))
	}
	if !reflect.DeepEqual(refreshes[0], refreshes[1]) {
		t.Errorf("[AC-026] the post-run refresh %v differs from the run's own %v", refreshes[1], refreshes[0])
	}
}

// drScenarios are the terminal-record scenarios of AC-003..AC-025 and the
// DR-SEC-001 cases, each driven through the real Execute.
func drScenarios() map[string]func(t *testing.T, g *drRig) (Record, error) {
	run := func(g *drRig) (Record, error) { return g.run(v2Request("full")) }
	return map[string]func(t *testing.T, g *drRig) (Record, error){
		"AC-003 updater rewrites configuration": func(t *testing.T, g *drRig) (Record, error) {
			g.step.onCall = g.cfg.bump
			return run(g)
		},
		"AC-004 apt changes configuration": func(t *testing.T, g *drRig) (Record, error) {
			g.runner.after[upgradeKey] = g.cfg.bump
			return run(g)
		},
		"AC-007 unreadable configuration": func(t *testing.T, g *drRig) (Record, error) {
			g.cfg.failCall(2, errors.New("stat failed"))
			return run(g)
		},
		"AC-009 no updater present": func(t *testing.T, g *drRig) (Record, error) {
			_ = os.Remove(filepath.Join(g.root, "boot", "dietpi", ".version"))
			g.runner.after[upgradeKey] = g.cfg.bump
			return run(g)
		},
		"AC-012 upgrade fails": func(t *testing.T, g *drRig) (Record, error) {
			upgradeFailsAfterChange(g, "E: Unable to correct problems")
			return run(g)
		},
		"AC-013 updater fails": func(t *testing.T, g *drRig) (Record, error) {
			g.step.onCall = g.cfg.bump
			g.step.out = StepOutcome{ExitCode: 1, Err: errors.New("exit status 1")}
			return run(g)
		},
		"AC-014 updater times out": func(t *testing.T, g *drRig) (Record, error) {
			g.step.block = true
			g.w.Extras.StepLimit = 50 * time.Millisecond
			g.step.onCall = g.cfg.bump
			return run(g)
		},
		"AC-015 updater refused": func(t *testing.T, g *drRig) (Record, error) {
			g.lock = func() LockState { return LockState{Kind: LockHeld, Detail: "held"} }
			g.runner.after[upgradeKey] = g.cfg.bump
			return run(g)
		},
		"AC-018 budget nearly exhausted": func(t *testing.T, g *drRig) (Record, error) {
			advanceInStep(g, ExtendedWorkerBudget-5*time.Minute)
			return run(g)
		},
		"AC-021 phase not persisted": func(t *testing.T, g *drRig) (Record, error) {
			g.store.failPhase = "post_refresh"
			g.runner.after[upgradeKey] = g.cfg.bump
			return run(g)
		},
		"AC-022 refresh fails": func(t *testing.T, g *drRig) (Record, error) {
			g.runner.after[upgradeKey] = g.cfg.bump
			g.pr.refreshResult[2] = CommandResult{Interrupted: true}
			g.pr.refreshErr[2] = errors.New("boom")
			return run(g)
		},
		"AC-023 apt failure and refresh failure": func(t *testing.T, g *drRig) (Record, error) {
			upgradeFailsAfterChange(g, strings.Repeat("e", 2048))
			g.pr.refreshErr[2] = errors.New("boom")
			return run(g)
		},
		"AC-024 stamp fails": func(t *testing.T, g *drRig) (Record, error) {
			g.runner.after[upgradeKey] = g.cfg.bump
			g.touch.failOn = map[int]error{2: errors.New("read-only")}
			return run(g)
		},
		"AC-025 output bound": func(t *testing.T, g *drRig) (Record, error) {
			g.runner.after[upgradeKey] = g.cfg.bump
			g.pr.upgradeOutput = strings.Repeat("u", 200*1024)
			g.pr.refreshResult[2] = CommandResult{Output: strings.Repeat("p", 64*1024)}
			g.pr.refreshErr[2] = errors.New(strings.Repeat("r", 1024))
			return run(g)
		},
		"DR-SEC-001 35 minute backstop": func(t *testing.T, g *drRig) (Record, error) {
			g.unit.RuntimeMax = 35 * time.Minute
			g.runner.after[upgradeKey] = func() { g.cfg.bump(); g.clock.Advance(24 * time.Minute) }
			return run(g)
		},
	}
}

func TestPostRefreshV2_DR_AC027_TerminalRecordsValidAndParse(t *testing.T) {
	for name, scenario := range drScenarios() {
		t.Run("[AC-027] "+name, func(t *testing.T) {
			g := newDRRig(t)
			rec, _ := scenario(t, g)
			if err := rec.Validate(); err != nil {
				t.Fatalf("[AC-027] record invalid: %v", err)
			}
			data, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parseRecord(data)
			if err != nil {
				t.Fatalf("[AC-027] record does not parse: %v", err)
			}
			if !parsed.Terminal() {
				t.Errorf("[AC-027] parsed record is not terminal: outcome %s phase %s", parsed.Outcome, parsed.Phase)
			}
			if saved := g.store.records[len(g.store.records)-1]; saved.Phase != "finished" {
				t.Errorf("[AC-027] last saved phase = %s, want finished", saved.Phase)
			}
		})
	}
}

// unverifiedBasisCases runs the DR-SEC-001 pair of cases for a run in which no
// updater passed check 6: the 30-minute WorkerBudget is the basis.
func unverifiedBasisCases(t *testing.T, id string, prepare func(g *drRig)) {
	t.Helper()
	t.Run("["+id+"] start+24m leaves under 7 minutes of the 30-minute budget and skips", func(t *testing.T) {
		g := newDRRig(t)
		prepare(g)
		g.runner.after[upgradeKey] = func() { g.cfg.bump(); g.clock.Advance(24 * time.Minute) }
		rec := mustRun(t, g, false)
		if g.pr.refreshCalls != 1 {
			t.Errorf("[%s] refresh commands = %d, want only the run's own: %v", id, g.pr.refreshCalls, g.log)
		}
		if got := aptResultLine(rec); !strings.HasSuffix(got, "; "+noteBudget) {
			t.Errorf("[%s] apt result line = %q, want it to end with the budget note", id, got)
		}
		if rec.Outcome != "success" {
			t.Errorf("[%s] outcome = %s, want success", id, rec.Outcome)
		}
	})
	t.Run("["+id+"] start+20m refreshes inside the 30-minute budget", func(t *testing.T) {
		g := newDRRig(t)
		prepare(g)
		g.runner.after[upgradeKey] = func() { g.cfg.bump(); g.clock.Advance(20 * time.Minute) }
		mustRun(t, g, false)
		after := time.Now()
		if g.pr.refreshCalls != 2 {
			t.Fatalf("[%s] refresh commands = %d, want 2: %v", id, g.pr.refreshCalls, g.log)
		}
		s := g.pr.seen[1]
		if !s.hasDL || s.deadline.After(after.Add(WorkerBudget)) {
			t.Errorf("[%s] refresh deadline %v (set %v) is beyond the 30-minute budget", id, s.deadline, s.hasDL)
		}
		if d := s.deadline.Sub(s.at); d > PostRefreshBudget {
			t.Errorf("[%s] refresh deadline %s after the call, want at most %s", id, d, PostRefreshBudget)
		}
	})
}

func TestPostRefreshV2_DR_SEC001a_UnitLimit35MinUsesWorkerBudget(t *testing.T) {
	unverifiedBasisCases(t, "DR-SEC-001a", func(g *drRig) { g.unit.RuntimeMax = 35 * time.Minute })
}

func TestPostRefreshV2_DR_SEC001b_UnitErrorUsesWorkerBudget(t *testing.T) {
	unverifiedBasisCases(t, "DR-SEC-001b", func(g *drRig) { g.unitErr = errors.New("systemctl failed") })
}

func TestPostRefreshV2_DR_SEC001c_PreCheck6SkipUsesWorkerBudget(t *testing.T) {
	t.Run("[DR-SEC-001c] status_unknown via Execute never reads the unit", func(t *testing.T) {
		var counters []*int
		unverifiedBasisCases(t, "DR-SEC-001c", func(g *drRig) {
			_ = os.Remove(filepath.Join(g.root, "run", "dietpi", ".update_available"))
			n := new(int)
			counters = append(counters, n)
			inner := g.w.Extras.Unit
			g.w.Extras.Unit = func() (UnitState, error) { *n++; return inner() }
		})
		if len(counters) != 2 {
			t.Fatalf("[DR-SEC-001c] ran %d runs, want 2", len(counters))
		}
		for i, n := range counters {
			if *n != 0 {
				t.Errorf("[DR-SEC-001c] run %d read Unit %d times, want 0", i, *n)
			}
		}
	})
	t.Run("[DR-SEC-001c] nothing_pending via runExtra leaves the backstop unverified", func(t *testing.T) {
		g := newDRRig(t)
		unitCalls := 0
		inner := g.w.Extras.Unit
		g.w.Extras.Unit = func() (UnitState, error) { unitCalls++; return inner() }
		d := extraupd.NewDietPi()
		d.Evidence = func(extraupd.FS) bool { return true }
		writeFixture(t, g.root, "boot/dietpi/.version", versionFixture("10", "8", "2"))
		_ = os.Remove(filepath.Join(g.root, "run", "dietpi", ".update_available"))
		entry, _ := d.Detect(context.Background(), g.w.Extras.FS)
		if string(entry.Status) != "up_to_date" {
			t.Fatalf("[DR-SEC-001c] fixture status = %s, want up_to_date", entry.Status)
		}
		now := g.clock.Now()
		rec := Record{Version: 2, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Phase: "accepted", Outcome: "running", RequestedExtraUpdaters: []string{"dietpi"}, AptOutcome: "success"}
		run := g.w.runExtra(context.Background(), now.Add(ExtendedWorkerBudget), v2Request("full"), &rec, d, entry, true, g.policy, nil)
		if run.res.Reason != SkipNothingPending {
			t.Fatalf("[DR-SEC-001c] result = %+v, want skipped/nothing_pending", run.res)
		}
		if run.backstopVerified {
			t.Error("[DR-SEC-001c] backstopVerified must be false when check 6 was never reached")
		}
		if unitCalls != 0 {
			t.Errorf("[DR-SEC-001c] Unit was read %d times, want 0", unitCalls)
		}
	})
}

func TestPostRefreshV2_DR_SEC001_VerifiedBackstopIsRecordedOnTheRun(t *testing.T) {
	g := newDRRig(t)
	d, _ := extraupd.Lookup("dietpi")
	entry, present := d.Detect(context.Background(), g.w.Extras.FS)
	if !present {
		t.Fatal("[DR-SEC-001] the fixture host must have DietPi")
	}
	now := g.clock.Now()
	rec := Record{Version: 2, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Phase: "accepted", Outcome: "running", RequestedExtraUpdaters: []string{"dietpi"}, AptOutcome: "success"}
	run := g.w.runExtra(context.Background(), now.Add(ExtendedWorkerBudget), v2Request("full"), &rec, d, entry, true, g.policy, nil)
	if !run.backstopVerified {
		t.Errorf("[DR-SEC-001] a run that passed check 6 must record backstopVerified, result %+v", run.res)
	}
	g2 := newDRRig(t)
	g2.unit.RuntimeMax = 35 * time.Minute
	run2 := g2.w.runExtra(context.Background(), now.Add(ExtendedWorkerBudget), v2Request("full"), &rec, d, entry, true, g2.policy, nil)
	if run2.backstopVerified || run2.res.Reason != SkipInsufficientTime {
		t.Errorf("[DR-SEC-001] a 35 minute backstop must stay unverified, got verified=%v result %+v", run2.backstopVerified, run2.res)
	}
}
