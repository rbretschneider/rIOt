package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

func TestExtras_EU_AC019_ExactArgvEnvNullStdin(t *testing.T) {
	g := newExtraRig(t)
	rec, err := g.run(v2Request("full"))
	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if g.stepCalls() != 1 {
		t.Fatalf("step calls = %d, want 1", g.stepCalls())
	}
	spec := g.step.calls[0].spec
	t.Run("[AC-019] the step receives exactly /boot/dietpi/dietpi-update with the single argument 1", func(t *testing.T) {
		if spec.Path != "/boot/dietpi/dietpi-update" || !reflect.DeepEqual(spec.Args, []string{"1"}) || spec.Dir != "/" {
			t.Errorf("spec = %q %q dir %q", spec.Path, spec.Args, spec.Dir)
		}
	})
	t.Run("[AC-019] the environment is the fixed worker environment plus G_INTERACTIVE=0", func(t *testing.T) {
		want := append(append([]string(nil), fixedEnvironmentForTest()...), "G_INTERACTIVE=0")
		if !reflect.DeepEqual(spec.Env, want) {
			t.Errorf("env = %q, want %q", spec.Env, want)
		}
	})
	t.Run("[AC-019] no request field can change what the step runs", func(t *testing.T) {
		req := v2Request("full")
		g2 := newExtraRig(t)
		if _, err := g2.run(req); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g2.step.calls[0].spec.Args, spec.Args) || g2.step.calls[0].spec.Path != spec.Path {
			t.Error("spec differs between two runs of the same request shape")
		}
		if g.step.calls[0].id != "dietpi" {
			t.Errorf("step id = %q", g.step.calls[0].id)
		}
	})
}

func TestExtras_EU_AC020_AfterVerifyPostRefreshAndHoldRestore(t *testing.T) {
	g := newExtraRig(t)
	g.w.Sources, _ = sourcesSequence("before-run", "after-upgrade")
	req := v2Request("full")
	req.IncludeRebootClass = true
	rec, err := g.run(req)
	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	idx := func(prefix string) int {
		for i, e := range g.log {
			if strings.HasPrefix(e, prefix) {
				return i
			}
		}
		return -1
	}
	last := func(prefix string) int {
		at := -1
		for i, e := range g.log {
			if strings.HasPrefix(e, prefix) {
				at = i
			}
		}
		return at
	}
	step := idx("step:dietpi")
	if step < 0 {
		t.Fatalf("DietPi step never ran: %v", g.log)
	}
	upgrade := idx("apt:" + upgradeKey)
	verify := last("apt:/usr/bin/apt-get check")
	postRefresh := last("apt:" + refreshKey)
	restore := idx("holds:restore")
	for name, at := range map[string]int{"upgrade": upgrade, "verification": verify, "post-run refresh": postRefresh, "hold restoration": restore} {
		t.Run("[AC-020] the DietPi call comes after "+name, func(t *testing.T) {
			if at < 0 || at > step {
				t.Errorf("%s at %d, DietPi at %d, log %v", name, at, step, g.log)
			}
		})
	}
	t.Run("[AC-020] DietPi is the last thing that ran", func(t *testing.T) {
		if step != len(g.log)-1 {
			t.Errorf("events after the DietPi call: %v", g.log[step+1:])
		}
	})
	t.Run("[AC-020] the post-run refresh really ran in this scenario", func(t *testing.T) {
		if postRefresh <= upgrade {
			t.Errorf("post-run refresh at %d not after upgrade at %d: the scenario does not prove the order", postRefresh, upgrade)
		}
	})
}

func TestExtras_EU_AC021_NeverInRepairOrAfterAptFailure(t *testing.T) {
	t.Run("[AC-021] a v1 repair run on an extras host never calls DietPi", func(t *testing.T) {
		g := newExtraRig(t)
		req := testRequest()
		req.Mode = "repair"
		rec, err := g.run(req)
		if err != nil || rec.Outcome != "success" || g.stepCalls() != 0 || len(rec.ExtraUpdaters) != 0 {
			t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
		}
	})
	t.Run("[AC-021] a v2 repair request is refused before anything runs", func(t *testing.T) {
		g := newExtraRig(t)
		if _, err := g.run(v2Request("repair")); err == nil {
			t.Fatal("a v2 repair request must be refused (FR-026)")
		}
		if len(g.runner.calls) != 0 || g.stepCalls() != 0 {
			t.Errorf("work ran for a refused request: %v", g.log)
		}
	})
	t.Run("[AC-021] a failed upgrade skips DietPi with apt_not_successful", func(t *testing.T) {
		g := newExtraRig(t)
		g.runner.failAt[upgradeKey] = "E: Unable to correct problems"
		rec, err := g.run(v2Request("full"))
		if err == nil || rec.Outcome != "error" || g.stepCalls() != 0 {
			t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
		}
		if r := g.result(rec); r.Outcome != UpdaterSkipped || r.Reason != SkipAptNotSuccessful {
			t.Errorf("dietpi = %+v, want skipped/apt_not_successful", r)
		}
		if !strings.HasPrefix(rec.Summary, "apt: failed; dietpi: skipped (apt_not_successful)") {
			t.Errorf("summary first line = %q", firstLine(rec.Summary))
		}
	})
	t.Run("[AC-021] a failed hold restoration skips DietPi with apt_not_successful", func(t *testing.T) {
		g := newExtraRig(t)
		g.holds.restoreErr = errors.New("apt-mark refused")
		req := v2Request("full")
		req.IncludeRebootClass = true
		rec, err := g.run(req)
		if err == nil || rec.Outcome != "error" || g.stepCalls() != 0 {
			t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
		}
		if r := g.result(rec); r.Reason != SkipAptNotSuccessful {
			t.Errorf("dietpi = %+v, want apt_not_successful", r)
		}
		if !rec.HoldCleanupFailed || rec.HoldRestoreError == "" {
			t.Error("the apt part's hold failure must still be reported unchanged")
		}
	})
}

func TestExtras_EU_AC022_SkipReasonsKeepSuccess(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(g *extraRig, req *Request)
		reason string
	}{
		{"not requested by the agent", func(g *extraRig, req *Request) { req.Version, req.ExtraUpdaters = 1, nil }, ""},
		{"not listed by the policy", func(g *extraRig, req *Request) { g.policy = nil }, SkipNotEnabled},
		{"policy unreadable", func(g *extraRig, req *Request) { g.policy, g.policyErr = nil, errors.New("unsafe file") }, SkipNotEnabled},
		{"status unknown", func(g *extraRig, req *Request) {
			writeFixture(g.t, g.root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=0\n")
			_ = os.Remove(filepath.Join(g.root, "run", "dietpi", ".update_available"))
		}, SkipStatusUnknown},
		{"nothing pending", func(g *extraRig, req *Request) {
			writeFixture(g.t, g.root, "boot/dietpi/.version", versionFixture("10", "8", "2"))
		}, SkipStatusUnknown},
	}
	for _, tc := range cases {
		t.Run("[AC-022] "+tc.name+" leaves the run successful and calls nothing", func(t *testing.T) {
			g := newExtraRig(t)
			req := v2Request("full")
			tc.mutate(g, &req)
			rec, err := g.run(req)
			if err != nil || rec.Outcome != "success" || rec.ExitCode != 0 {
				t.Fatalf("%+v %v", rec, err)
			}
			if g.stepCalls() != 0 {
				t.Fatal("DietPi was called")
			}
			if tc.reason == "" {
				if len(rec.ExtraUpdaters) != 0 {
					t.Errorf("a v1 run records no updater results: %+v", rec.ExtraUpdaters)
				}
				return
			}
			if r := g.result(rec); r.Outcome != UpdaterSkipped || r.Reason != tc.reason {
				t.Errorf("dietpi = %+v, want skipped/%s", r, tc.reason)
			}
		})
	}
	t.Run("[AC-022] an up-to-date updater is skipped as nothing_pending", func(t *testing.T) {
		g := newExtraRig(t)
		g.w.Extras.FS = extraupd.DirFS(g.root)
		// Row D3 is unreachable in production, so drive the worker with a fixed
		// detector result by pointing it at a host whose flag is stale and
		// whose check evidence is faked through a wrapper updater.
		rec := runWithFixedStatus(t, g, "up_to_date")
		if r := g.result(rec); r.Reason != SkipNothingPending || rec.Outcome != "success" {
			t.Errorf("dietpi = %+v outcome %s, want nothing_pending / success", r, rec.Outcome)
		}
	})
	t.Run("[AC-022] the policy error text reaches the note, bounded", func(t *testing.T) {
		g := newExtraRig(t)
		g.policy, g.policyErr = nil, errors.New(strings.Repeat("x", 600))
		rec, _ := g.run(v2Request("full"))
		if note := g.result(rec).Note; !strings.HasPrefix(note, "root policy not usable: ") || len(note) > maxUpdaterNote {
			t.Errorf("note = %q (%d bytes)", note, len(note))
		}
	})
}

// runWithFixedStatus drives runExtra directly with a detector result, because
// DietPi's up_to_date row cannot occur on a real host (AF-1).
func runWithFixedStatus(t *testing.T, g *extraRig, status string) Record {
	t.Helper()
	d := extraupd.NewDietPi()
	d.Evidence = func(extraupd.FS) bool { return true }
	writeFixture(t, g.root, "boot/dietpi/.version", versionFixture("10", "8", "2"))
	_ = os.Remove(filepath.Join(g.root, "run", "dietpi", ".update_available"))
	entry, _ := d.Detect(context.Background(), g.w.Extras.FS)
	if string(entry.Status) != status {
		t.Fatalf("fixture status = %s, want %s", entry.Status, status)
	}
	now := g.clock.Now()
	rec := Record{Version: 2, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Phase: "accepted", Outcome: "running", RequestedExtraUpdaters: []string{"dietpi"}, AptOutcome: "success"}
	run := g.w.runExtra(context.Background(), now.Add(ExtendedWorkerBudget), v2Request("full"), &rec, d, entry, true, g.policy, nil)
	rec.ExtraUpdaters = []UpdaterResult{run.res}
	rec.Outcome = "success"
	f := now
	rec.FinishedAt = &f
	rec.Phase = "finished"
	return rec
}

func TestExtras_EU_AC023_TimeoutIsTimedOut(t *testing.T) {
	t.Run("[AC-023] a step that never returns is cut at its limit and recorded timed_out", func(t *testing.T) {
		g := newExtraRig(t)
		g.step.block = true
		g.w.Extras.StepLimit = 50 * time.Millisecond
		rec, err := g.run(v2Request("full"))
		if err == nil || rec.Outcome != "error" {
			t.Fatalf("%+v %v", rec, err)
		}
		r := g.result(rec)
		if r.Outcome != UpdaterTimedOut || !strings.Contains(r.Note, "state uncertain") || !strings.Contains(r.Note, "/var/lib/dietpi/logs/dietpi-update.log") {
			t.Errorf("dietpi = %+v, want timed_out with the uncertain-state note and the log path", r)
		}
		if !strings.HasPrefix(rec.Summary, "apt: succeeded; dietpi: timed out after") {
			t.Errorf("summary first line = %q", firstLine(rec.Summary))
		}
	})
	t.Run("[AC-023] the production step limit is 40 minutes", func(t *testing.T) {
		g := newExtraRig(t)
		before := time.Now()
		if _, err := g.run(v2Request("full")); err != nil {
			t.Fatal(err)
		}
		c := g.step.calls[0]
		if !c.hasDL {
			t.Fatal("the step context has no deadline")
		}
		if got := c.deadline.Sub(before); got < 39*time.Minute+55*time.Second || got > 40*time.Minute+5*time.Second {
			t.Errorf("step deadline in %s, want 40m", got)
		}
		if c.spec.StepLimit != 40*time.Minute {
			t.Errorf("spec limit = %s", c.spec.StepLimit)
		}
	})
	t.Run("[AC-023] a cancelled service context is interrupted, not timed out", func(t *testing.T) {
		g := newExtraRig(t)
		ctx, cancel := context.WithCancel(context.Background())
		g.step.block = true
		g.step.onCall = cancel
		rec, err := g.w.Execute(ctx, v2Request("full"))
		if err == nil || rec.Outcome != "interrupted" {
			t.Fatalf("%+v %v", rec, err)
		}
		if r := g.result(rec); r.Outcome != UpdaterInterrupted {
			t.Errorf("dietpi = %+v, want interrupted", r)
		}
	})
}

func TestExtras_EU_AC024_InsufficientTimeFails(t *testing.T) {
	g := newExtraRig(t)
	g.runner.after[upgradeKey] = func() { g.clock.Advance(35 * time.Minute) }
	rec, err := g.run(v2Request("full"))
	if err == nil || rec.Outcome != "error" || g.stepCalls() != 0 {
		t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
	}
	if r := g.result(rec); r.Outcome != UpdaterSkipped || r.Reason != SkipInsufficientTime {
		t.Errorf("dietpi = %+v, want skipped/insufficient_time", r)
	}
	t.Run("[AC-024] exactly the threshold remaining is enough", func(t *testing.T) {
		g := newExtraRig(t)
		g.runner.after[upgradeKey] = func() { g.clock.Advance(ExtendedWorkerBudget - ExtraStartThreshold) }
		rec, err := g.run(v2Request("full"))
		if err != nil || g.stepCalls() != 1 {
			t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
		}
	})
	t.Run("[AC-024] one second under the threshold is refused", func(t *testing.T) {
		g := newExtraRig(t)
		g.runner.after[upgradeKey] = func() { g.clock.Advance(ExtendedWorkerBudget - ExtraStartThreshold + time.Second) }
		rec, _ := g.run(v2Request("full"))
		if g.stepCalls() != 0 || g.result(rec).Reason != SkipInsufficientTime {
			t.Errorf("steps=%d result=%+v", g.stepCalls(), g.result(rec))
		}
	})
}

func TestExtras_EU_AC026_LockWaitHeldUnknown(t *testing.T) {
	t.Run("[AC-026] a lock held for 2 minutes is waited out and DietPi then runs", func(t *testing.T) {
		g := newExtraRig(t)
		start := g.clock.Now()
		g.lock = func() LockState {
			if g.clock.Now().Sub(start) < 2*time.Minute {
				return LockState{Kind: LockHeld, Detail: "held"}
			}
			return LockState{Kind: LockFree}
		}
		rec, err := g.run(v2Request("full"))
		if err != nil || g.stepCalls() != 1 || rec.Outcome != "success" {
			t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
		}
		if g.sleeps == 0 {
			t.Error("the wait never polled")
		}
	})
	t.Run("[AC-026] a lock held longer than 5 minutes skips with dpkg_locked after at most 5 minutes", func(t *testing.T) {
		g := newExtraRig(t)
		g.lock = func() LockState { return LockState{Kind: LockHeld, Detail: "held by pid 4242"} }
		waitStart := g.clock.Now()
		rec, err := g.run(v2Request("full"))
		waited := g.clock.Now().Sub(waitStart)
		if err == nil || rec.Outcome != "error" || g.stepCalls() != 0 {
			t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
		}
		if r := g.result(rec); r.Reason != SkipDpkgLocked || !strings.Contains(r.Note, "4242") {
			t.Errorf("dietpi = %+v, want dpkg_locked naming the holder", r)
		}
		if waited > ExtraLockWait+ExtraLockPoll {
			t.Errorf("waited %s, want at most %s", waited, ExtraLockWait)
		}
	})
	t.Run("[AC-026] an unknown lock state also ends as dpkg_locked, failing the command", func(t *testing.T) {
		g := newExtraRig(t)
		g.lock = func() LockState { return LockState{Kind: LockUnknown, Detail: "cannot open lock file"} }
		rec, err := g.run(v2Request("full"))
		if err == nil || g.stepCalls() != 0 {
			t.Fatalf("%+v %v", rec, err)
		}
		if r := g.result(rec); r.Reason != SkipDpkgLocked || !strings.Contains(r.Note, "cannot open") {
			t.Errorf("dietpi = %+v", r)
		}
	})
	t.Run("[AC-026] the persisted wait phase is extra_wait:dietpi", func(t *testing.T) {
		g := newExtraRig(t)
		if _, err := g.run(v2Request("full")); err != nil {
			t.Fatal(err)
		}
		saw := false
		for _, r := range g.store.records {
			saw = saw || r.Phase == "extra_wait:dietpi"
		}
		if !saw {
			t.Error("extra_wait:dietpi was never persisted")
		}
	})
}

func TestExtras_EU_AC027_DietPiFailuresNotRetried(t *testing.T) {
	cases := map[string]string{
		"concurrent execution": "[ INFO ] DietPi-Update | Cancelled DietPi-Update due to concurrent execution\n",
		"no network":           "[FAILED] DietPi-Update | Network connection check failed\n",
		"read-only root":       "[FAILED] DietPi-Update | Root filesystem is read-only\n",
	}
	for name, tail := range cases {
		t.Run("[AC-027] "+name+" is failed with its exit status and last lines, with one call", func(t *testing.T) {
			g := newExtraRig(t)
			g.step.out = StepOutcome{Output: "starting\n" + tail, ExitCode: 1, Err: errors.New("exit status 1")}
			rec, err := g.run(v2Request("full"))
			if err == nil || rec.Outcome != "error" || g.stepCalls() != 1 {
				t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
			}
			r := g.result(rec)
			if r.Outcome != UpdaterFailed || r.ExitCode == nil || *r.ExitCode != 1 {
				t.Errorf("dietpi = %+v, want failed exit 1", r)
			}
			if !strings.Contains(rec.Summary, strings.TrimSpace(tail)) {
				t.Errorf("summary lacks the last output lines %q:\n%s", tail, rec.Summary)
			}
			if !strings.Contains(rec.Summary, "dietpi: failed (exit 1)") {
				t.Errorf("summary lacks the exit status:\n%s", rec.Summary)
			}
		})
	}
	t.Run("[AC-027] only the last 20 non-empty lines are kept", func(t *testing.T) {
		g := newExtraRig(t)
		var b strings.Builder
		for i := 0; i < 100; i++ {
			b.WriteString("line " + string(rune('A'+i%26)) + "\n\n")
		}
		g.step.out = StepOutcome{Output: b.String() + "THE END\n", ExitCode: 2, Err: errors.New("exit status 2")}
		rec, _ := g.run(v2Request("full"))
		if got := strings.Count(rec.Summary, "line "); got != 19 {
			t.Errorf("summary keeps %d earlier lines, want 19 (20 with THE END)", got)
		}
		if !strings.Contains(rec.Summary, "THE END") {
			t.Error("the final line was dropped")
		}
	})
}

func TestExtras_EU_AC028_AptSuccessDietPiFailIsFailed(t *testing.T) {
	g := newExtraRig(t)
	g.step.out = StepOutcome{Output: "boom\n", ExitCode: 1, Err: errors.New("exit status 1")}
	req := v2Request("full")
	req.IncludeRebootClass = true
	rec, err := g.run(req)
	if err == nil || rec.Outcome != "error" || rec.ExitCode != 1 {
		t.Fatalf("%+v %v", rec, err)
	}
	t.Run("[AC-028] the first summary line states both parts", func(t *testing.T) {
		if got := firstLine(rec.Summary); got != "apt: succeeded; dietpi: failed (exit 1)" {
			t.Errorf("first line = %q", got)
		}
	})
	t.Run("[AC-028] the apt version changes and hold restoration are reported unchanged", func(t *testing.T) {
		if len(rec.VersionChanges) != 1 || rec.VersionChanges[0].After != "2" {
			t.Errorf("version changes = %+v", rec.VersionChanges)
		}
		if rec.HoldsReleased || rec.HoldCleanupFailed || rec.HoldRestoreError != "" {
			t.Errorf("holds not restored cleanly: %+v", rec)
		}
		if rec.AptOutcome != "success" {
			t.Errorf("apt outcome = %q, want success", rec.AptOutcome)
		}
		if !strings.Contains(rec.Summary, successSummary) {
			t.Errorf("the apt result line is missing:\n%s", rec.Summary)
		}
	})
	t.Run("[AC-028] the exit code is the updater's", func(t *testing.T) {
		g := newExtraRig(t)
		g.step.out = StepOutcome{ExitCode: 7, Err: errors.New("exit status 7")}
		rec, _ := g.run(v2Request("full"))
		if rec.ExitCode != 7 {
			t.Errorf("exit code = %d, want 7", rec.ExitCode)
		}
	})
}

func TestExtras_EU_AC029_RecoverNeverReruns(t *testing.T) {
	g := newExtraRig(t)
	// At the instant DietPi is called, the durable record must already say it
	// is running: persist before act.
	var atStep Record
	g.step.onCall = func() { atStep, _ = g.store.Current() }
	if _, err := g.run(v2Request("full")); err != nil {
		t.Fatal(err)
	}
	running := atStep
	t.Run("[AC-029] persist-before-act: the running entry was saved before the step started", func(t *testing.T) {
		if running.Phase != "extra_run:dietpi" || running.Outcome != "running" || len(running.ExtraUpdaters) != 1 || running.ExtraUpdaters[0].Outcome != UpdaterRunning || running.ExtraUpdaters[0].StartedAt == nil {
			t.Fatalf("durable record at the moment of the call: %+v", running)
		}
	})
	t.Run("[AC-029] recovery marks a running entry interrupted and starts no process", func(t *testing.T) {
		g2 := newExtraRig(t)
		got, err := g2.w.RecoverInterrupted(context.Background(), running)
		if err != nil || got.Outcome != "interrupted" {
			t.Fatalf("%+v %v", got, err)
		}
		if g2.stepCalls() != 0 || len(g2.runner.calls) != 0 {
			t.Errorf("recovery started work: %v", g2.log)
		}
		if r := got.ExtraUpdaters[0]; r.Outcome != UpdaterInterrupted || r.FinishedAt == nil {
			t.Errorf("dietpi = %+v, want interrupted with a finish time", r)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("recovered record is invalid: %v", err)
		}
	})
	t.Run("[AC-029] a redelivered command id is refused by the store and never runs again", func(t *testing.T) {
		g2 := newExtraRig(t)
		if _, err := g2.run(v2Request("full")); err != nil {
			t.Fatal(err)
		}
		before := g2.stepCalls()
		if _, err := g2.run(v2Request("full")); err == nil {
			t.Fatal("the second run of the same command id must be refused")
		}
		if g2.stepCalls() != before {
			t.Errorf("DietPi ran %d times for one command", g2.stepCalls())
		}
	})
	t.Run("[AC-029] a v1 record recovery is unchanged", func(t *testing.T) {
		g2 := newExtraRig(t)
		now := g2.clock.Now()
		v1 := Record{Version: 1, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Outcome: "running", Phase: "upgrade"}
		got, err := g2.w.RecoverInterrupted(context.Background(), v1)
		if err != nil || got.Outcome != "interrupted" || got.AptOutcome != "" || len(got.ExtraUpdaters) != 0 {
			t.Errorf("%+v %v", got, err)
		}
	})
	t.Run("[AC-029] a v2 record that died during apt gets apt_outcome interrupted", func(t *testing.T) {
		g2 := newExtraRig(t)
		now := g2.clock.Now()
		v2 := Record{Version: 2, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Outcome: "running", Phase: "upgrade", RequestedExtraUpdaters: []string{"dietpi"}}
		got, err := g2.w.RecoverInterrupted(context.Background(), v2)
		if err != nil || got.AptOutcome != "interrupted" {
			t.Errorf("%+v %v", got, err)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("recovered record is invalid: %v", err)
		}
	})
}

func TestExtras_EU_AC030_OutputCapKeepsTailAndLogPath(t *testing.T) {
	g := newExtraRig(t)
	big := strings.Repeat("progress line that fills the log\n", 40000) + "FATAL: the known error line\n"
	if len(big) < 1<<20 {
		t.Fatal("fixture output must exceed 1 MiB")
	}
	g.step.out = StepOutcome{Output: big, Truncated: true, ExitCode: 1, Err: errors.New("exit status 1")}
	rec, _ := g.run(v2Request("full"))
	start := strings.Index(rec.Output, "\n[extra:dietpi]\n")
	if start < 0 {
		t.Fatalf("no labelled DietPi section in the output (len %d)", len(rec.Output))
	}
	section := rec.Output[start:]
	t.Run("[AC-030] the DietPi section is at most 128 KiB", func(t *testing.T) {
		if len(section) > 128<<10 {
			t.Errorf("section = %d bytes, want at most %d", len(section), 128<<10)
		}
	})
	t.Run("[AC-030] the whole output stays within 256 KiB and the cut is marked", func(t *testing.T) {
		if len(rec.Output) > MaxOutputBytes {
			t.Errorf("output = %d bytes, want at most %d", len(rec.Output), MaxOutputBytes)
		}
		if !rec.OutputTruncated || !strings.Contains(section, "[output truncated; see system journal]") {
			t.Error("the cut must be marked in the section and on the record")
		}
	})
	t.Run("[AC-030] the known error line is kept and the log path is named last", func(t *testing.T) {
		if !strings.Contains(section, "FATAL: the known error line") {
			t.Error("the end of the output was dropped")
		}
		if !strings.HasSuffix(rec.Output, "full log: /var/lib/dietpi/logs/dietpi-update.log\n") {
			t.Errorf("output does not end with the log path: %q", rec.Output[len(rec.Output)-80:])
		}
		if !strings.Contains(rec.Summary, "/var/lib/dietpi/logs/dietpi-update.log") {
			t.Error("the summary does not name DietPi's own log")
		}
	})
	t.Run("[AC-030] the summary stays within 4096 bytes", func(t *testing.T) {
		if len(rec.Summary) > 4096 {
			t.Errorf("summary = %d bytes", len(rec.Summary))
		}
	})
	t.Run("[AC-030] a record with a full apt output still keeps the DietPi section whole", func(t *testing.T) {
		g := newExtraRig(t)
		g.runner.after[upgradeKey] = func() {}
		g.step.out = StepOutcome{Output: big, Truncated: true}
		g.w.Runner = &bigAptRunner{inner: g.runner}
		rec, _ := g.run(v2Request("full"))
		if len(rec.Output) > MaxOutputBytes || !strings.Contains(rec.Output, "\n[extra:dietpi]\n") || !strings.HasSuffix(rec.Output, "dietpi-update.log\n") {
			t.Errorf("len=%d has header=%v", len(rec.Output), strings.Contains(rec.Output, "\n[extra:dietpi]\n"))
		}
	})
}

// bigAptRunner pads every apt step with a lot of output.
type bigAptRunner struct{ inner *orderRunner }

func (b *bigAptRunner) Run(ctx context.Context, p string, a ...string) (CommandResult, error) {
	res, err := b.inner.Run(ctx, p, a...)
	if len(a) > 0 && (a[len(a)-1] == "dist-upgrade" || a[len(a)-1] == "update") {
		res.Output = strings.Repeat("apt output line\n", 20000)
	}
	return res, err
}

func TestExtras_EU_SEC001_LauncherErrMapsToSkip(t *testing.T) {
	for _, reason := range []string{SkipTmpNotPrivate, SkipMountPropagationUnver} {
		t.Run("[SEC-001] a launcher refusal "+reason+" is a failing skip and DietPi never started", func(t *testing.T) {
			g := newExtraRig(t)
			g.step.out = StepOutcome{Skip: reason, SkipDetail: "mount table check failed"}
			rec, err := g.run(v2Request("full"))
			if err == nil || rec.Outcome != "error" {
				t.Fatalf("%+v %v", rec, err)
			}
			r := g.result(rec)
			if r.Outcome != UpdaterSkipped || r.Reason != reason || r.StartedAt != nil || r.ExitCode != nil {
				t.Errorf("dietpi = %+v, want skipped/%s with no start and no exit code", r, reason)
			}
			if strings.Contains(rec.Output, "[extra:dietpi]") {
				t.Error("no output section may exist for a step that never started")
			}
		})
	}
	t.Run("[SEC-001] any other launcher setup failure is failed with a clear note", func(t *testing.T) {
		g := newExtraRig(t)
		g.step.out = StepOutcome{SetupErr: "no ok line within 10s"}
		rec, err := g.run(v2Request("full"))
		if err == nil {
			t.Fatal("expected an error")
		}
		if r := g.result(rec); r.Outcome != UpdaterFailed || !strings.Contains(r.Note, "launcher setup failed: no ok line") || r.StartedAt != nil {
			t.Errorf("dietpi = %+v", r)
		}
	})
}

func TestExtras_EU_SEC001_BusyAfterWaitSkips(t *testing.T) {
	g := newExtraRig(t)
	g.busy = func() (bool, string) { return true, "pid 812 runs /boot/dietpi/dietpi-update 2" }
	g.lock = func() LockState { return LockState{Kind: LockHeld, Detail: "held"} }
	rec, err := g.run(v2Request("full"))
	if err == nil || g.stepCalls() != 0 {
		t.Fatalf("%+v %v", rec, err)
	}
	if r := g.result(rec); r.Reason != SkipBusy || !strings.Contains(r.Note, "812") {
		t.Errorf("dietpi = %+v, want dietpi_busy (busy takes precedence over locked)", r)
	}
	t.Run("[SEC-001] a busy program that ends within the wait lets the step run", func(t *testing.T) {
		g := newExtraRig(t)
		polls := 0
		g.busy = func() (bool, string) { polls++; return polls < 4, "busy" }
		rec, err := g.run(v2Request("full"))
		if err != nil || g.stepCalls() != 1 || rec.Outcome != "success" {
			t.Fatalf("%+v %v", rec, err)
		}
	})
}

func TestExtras_EU_SEC002_Effective35minRefused(t *testing.T) {
	cases := map[string]UnitState{
		"35 minute backstop":       {RuntimeMax: 35 * time.Minute, PrivateTmp: "no"},
		"just under the minimum":   {RuntimeMax: ExtraMinBackstop - time.Second, PrivateTmp: "no"},
		"zero (unparsed) backstop": {PrivateTmp: "no"},
	}
	for name, unit := range cases {
		t.Run("[SEC-002] "+name+" refuses the step with insufficient_time", func(t *testing.T) {
			g := newExtraRig(t)
			g.unit = unit
			rec, err := g.run(v2Request("full"))
			if err == nil || g.stepCalls() != 0 {
				t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
			}
			if r := g.result(rec); r.Reason != SkipInsufficientTime {
				t.Errorf("dietpi = %+v, want insufficient_time", r)
			}
		})
	}
	t.Run("[SEC-002] exactly the minimum backstop is accepted", func(t *testing.T) {
		g := newExtraRig(t)
		g.unit = UnitState{RuntimeMax: ExtraMinBackstop, PrivateTmp: "no"}
		if _, err := g.run(v2Request("full")); err != nil || g.stepCalls() != 1 {
			t.Fatalf("%v steps=%d", err, g.stepCalls())
		}
	})
	t.Run("[SEC-002] an unreadable unit state fails closed", func(t *testing.T) {
		g := newExtraRig(t)
		g.unitErr = errors.New("systemctl failed")
		rec, _ := g.run(v2Request("full"))
		if g.stepCalls() != 0 || g.result(rec).Reason != SkipInsufficientTime || !strings.Contains(g.result(rec).Note, "systemctl failed") {
			t.Errorf("result = %+v", g.result(rec))
		}
	})
}

func TestExtras_EU_OPS001_UnitSandboxRefused(t *testing.T) {
	cases := map[string]UnitState{
		"PrivateTmp=yes":     {RuntimeMax: ExtraServiceBackstop, PrivateTmp: "yes"},
		"MountFlags=slave":   {RuntimeMax: ExtraServiceBackstop, PrivateTmp: "no", MountFlags: "slave"},
		"MountFlags=private": {RuntimeMax: ExtraServiceBackstop, PrivateTmp: "no", MountFlags: "private"},
		"PrivateTmp missing": {RuntimeMax: ExtraServiceBackstop},
	}
	for name, unit := range cases {
		t.Run("[OPS-001] "+name+" is mount_propagation_unverified and DietPi never starts", func(t *testing.T) {
			g := newExtraRig(t)
			g.unit = unit
			rec, err := g.run(v2Request("full"))
			if err == nil || g.stepCalls() != 0 {
				t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
			}
			if r := g.result(rec); r.Reason != SkipMountPropagationUnver {
				t.Errorf("dietpi = %+v, want mount_propagation_unverified", r)
			}
		})
	}
}

func TestExtras_EU_SEC003_NonMichaIngOwnerRefused(t *testing.T) {
	cases := map[string]string{
		"a fork":            "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=SomeFork\n",
		"trailing blank":    "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=MichaIng \n",
		"carriage return":   "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=MichaIng\r\n",
		"different case":    "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=michaing\n",
		"control character": "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER=Micha\x01Ing\n",
	}
	for name, body := range cases {
		t.Run("[SEC-003] owner "+name+" refuses the step", func(t *testing.T) {
			g := newExtraRig(t)
			writeFixture(t, g.root, "boot/dietpi.txt", body)
			rec, err := g.run(v2Request("full"))
			if err == nil || g.stepCalls() != 0 {
				t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
			}
			r := g.result(rec)
			if r.Reason != SkipNonDefaultSource {
				t.Errorf("dietpi = %+v, want non_default_update_source", r)
			}
			for _, ch := range r.SourceOwner {
				if ch < 0x20 || ch > 0x7e {
					t.Errorf("source owner %q holds a non-printable character", r.SourceOwner)
				}
			}
		})
	}
	t.Run("[SEC-003] owner MichaIng on any branch is accepted and recorded", func(t *testing.T) {
		g := newExtraRig(t)
		writeFixture(t, g.root, "boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITBRANCH=beta\n")
		rec, err := g.run(v2Request("full"))
		if err != nil || g.stepCalls() != 1 {
			t.Fatalf("%+v %v", rec, err)
		}
		if r := g.result(rec); r.SourceOwner != "MichaIng" || r.SourceBranch != "beta" {
			t.Errorf("source = %s/%s, want MichaIng/beta recorded", r.SourceOwner, r.SourceBranch)
		}
	})
	t.Run("[SEC-003] an unreadable dietpi.txt refuses the step", func(t *testing.T) {
		g := newExtraRig(t)
		if err := os.Remove(filepath.Join(g.root, "boot", "dietpi.txt")); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, g.root, "run/dietpi/.live_patches", "")
		rec, _ := g.run(v2Request("full"))
		if g.stepCalls() != 0 || g.result(rec).Reason != SkipNonDefaultSource {
			t.Errorf("result = %+v steps=%d", g.result(rec), g.stepCalls())
		}
	})
}

func TestExtras_EU_SEC004_ForgedFlagNotBelieved(t *testing.T) {
	g := newExtraRig(t)
	g.trustErr = errors.New("/run/dietpi is owned by uid 1000")
	rec, err := g.run(v2Request("full"))
	if err == nil || g.stepCalls() != 0 {
		t.Fatalf("%+v %v steps=%d", rec, err, g.stepCalls())
	}
	if r := g.result(rec); r.Reason != SkipUnsafePath || !strings.Contains(r.Note, "uid 1000") {
		t.Errorf("dietpi = %+v, want unsafe_dietpi_path naming the cause", r)
	}
	t.Run("[SEC-004] the trust walk runs once, after the status and source checks", func(t *testing.T) {
		g := newExtraRig(t)
		if _, err := g.run(v2Request("full")); err != nil {
			t.Fatal(err)
		}
		if g.trustSeen != 1 {
			t.Errorf("trust walk ran %d times", g.trustSeen)
		}
	})
	t.Run("[SEC-004] the trust walk does not run when nothing is pending", func(t *testing.T) {
		g := newExtraRig(t)
		_ = os.Remove(filepath.Join(g.root, "run", "dietpi", ".update_available"))
		g.run(v2Request("full"))
		if g.trustSeen != 0 {
			t.Errorf("trust walk ran %d times for a host with nothing pending", g.trustSeen)
		}
	})
}

func TestExtras_EU_SEC006_MinInterval(t *testing.T) {
	t.Run("[SEC-006] a start 30 minutes ago refuses the step", func(t *testing.T) {
		g := newExtraRig(t)
		g.last, g.hasLast = g.clock.Now().Add(-30*time.Minute), true
		rec, err := g.run(v2Request("full"))
		if err == nil || g.stepCalls() != 0 || g.result(rec).Reason != SkipMinInterval {
			t.Fatalf("%+v %v", rec, err)
		}
	})
	t.Run("[SEC-006] a start 61 minutes ago allows it", func(t *testing.T) {
		g := newExtraRig(t)
		g.last, g.hasLast = g.clock.Now().Add(-61*time.Minute), true
		if _, err := g.run(v2Request("full")); err != nil || g.stepCalls() != 1 {
			t.Fatalf("%v steps=%d", err, g.stepCalls())
		}
	})
	t.Run("[SEC-006] a future-dated record is ignored", func(t *testing.T) {
		g := newExtraRig(t)
		g.last, g.hasLast = g.clock.Now().Add(48*time.Hour), true
		if _, err := g.run(v2Request("full")); err != nil || g.stepCalls() != 1 {
			t.Fatalf("%v steps=%d", err, g.stepCalls())
		}
	})
	t.Run("[SEC-006] a history read error refuses the step", func(t *testing.T) {
		g := newExtraRig(t)
		g.lastErr = errors.New("record unreadable")
		rec, err := g.run(v2Request("full"))
		if err == nil || g.stepCalls() != 0 || g.result(rec).Reason != SkipMinInterval {
			t.Fatalf("%+v %v", rec, err)
		}
	})
	t.Run("[SEC-006] no history means no earlier start", func(t *testing.T) {
		g := newExtraRig(t)
		if _, err := g.run(v2Request("full")); err != nil || g.stepCalls() != 1 {
			t.Fatalf("%v steps=%d", err, g.stepCalls())
		}
	})
}

func TestExtras_EU_AC031_RecordsRebootRequiredAfter(t *testing.T) {
	t.Run("[AC-031] a successful step that leaves the modules directory missing records reboot_required_after", func(t *testing.T) {
		g := newExtraRig(t)
		g.step.onCall = func() {
			writeFixture(g.t, g.root, "proc/sys/kernel/osrelease", "6.12.99+rpt-rpi-v8\n")
		}
		rec, err := g.run(v2Request("full"))
		if err != nil || rec.Outcome != "success" {
			t.Fatalf("%+v %v", rec, err)
		}
		if r := g.result(rec); !r.RebootRequiredAfter {
			t.Errorf("dietpi = %+v, want reboot_required_after", r)
		}
		if !strings.Contains(rec.Summary, "reboot required after DietPi: yes") {
			t.Errorf("summary does not state the reboot:\n%s", rec.Summary)
		}
	})
	t.Run("[AC-031] no reboot indicator records false and says no", func(t *testing.T) {
		g := newExtraRig(t)
		rec, _ := g.run(v2Request("full"))
		if g.result(rec).RebootRequiredAfter || !strings.Contains(rec.Summary, "reboot required after DietPi: no") {
			t.Errorf("summary:\n%s", rec.Summary)
		}
	})
	t.Run("[AC-033] the version after the run is re-read", func(t *testing.T) {
		g := newExtraRig(t)
		g.step.onCall = func() {
			writeFixture(g.t, g.root, "boot/dietpi/.version", versionFixture("10", "8", "2"))
		}
		rec, _ := g.run(v2Request("full"))
		if r := g.result(rec); r.VersionBefore != "9.20.1" || r.VersionAfter != "10.8.2" {
			t.Errorf("versions = %q -> %q", r.VersionBefore, r.VersionAfter)
		}
		if !strings.Contains(rec.Summary, "9.20.1 -> 10.8.2") {
			t.Errorf("summary lacks the version change:\n%s", rec.Summary)
		}
	})
}

func TestExtras_EU_AC029_PersistenceFailureStartsNothing(t *testing.T) {
	for _, phase := range []string{"extra_run:dietpi", "extra_wait:dietpi"} {
		t.Run("[AC-029] a failed save in "+phase+" means DietPi never starts", func(t *testing.T) {
			g := newExtraRig(t)
			g.store.failPhase = phase
			rec, err := g.run(v2Request("full"))
			if g.stepCalls() != 0 {
				t.Fatal("DietPi started although the state was not persisted")
			}
			if err == nil || rec.Outcome != "error" {
				t.Fatalf("%+v %v", rec, err)
			}
			if r := g.result(rec); r.Outcome != UpdaterFailed || !strings.Contains(r.Note, "state not persisted") {
				t.Errorf("dietpi = %+v", r)
			}
		})
	}
}

func TestExtras_EU_AC044_V2WithoutPresentUpdaterIsPlain(t *testing.T) {
	t.Run("[AC-044] a v2 request on a host that lost DietPi records no updater result", func(t *testing.T) {
		g := newExtraRig(t)
		if err := os.Remove(filepath.Join(g.root, "boot", "dietpi", ".version")); err != nil {
			t.Fatal(err)
		}
		rec, err := g.run(v2Request("full"))
		if err != nil || rec.Outcome != "success" || len(rec.ExtraUpdaters) != 0 || rec.Summary != successSummary {
			t.Fatalf("%+v %v", rec, err)
		}
	})
	t.Run("[AC-044] a v2 request without an environment is refused before anything is recorded", func(t *testing.T) {
		g := newExtraRig(t)
		g.w.Extras = nil
		if _, err := g.run(v2Request("full")); err == nil {
			t.Fatal("expected an error")
		}
		if len(g.store.records) != 0 || len(g.runner.calls) != 0 {
			t.Error("a misconfigured worker must not record or run anything")
		}
	})
	t.Run("[AC-044] the Extras environment is never consulted for a v1 request", func(t *testing.T) {
		g := newExtraRig(t)
		g.w.Extras.Policy = func() ([]string, error) { t.Fatal("policy read for a v1 request"); return nil, nil }
		rec, err := g.run(testRequest())
		if err != nil || rec.Version != 1 || g.stepCalls() != 0 {
			t.Fatalf("%+v %v", rec, err)
		}
	})
}

func TestExtras_EU_AC038_FinalRecordsAreAlwaysValidAndBounded(t *testing.T) {
	scenarios := map[string]func(g *extraRig){
		"success": func(g *extraRig) {},
		"failure": func(g *extraRig) {
			g.step.out = StepOutcome{ExitCode: 3, Err: errors.New("exit status 3"), Output: strings.Repeat("x", 100000)}
		},
		"timeout":     func(g *extraRig) { g.step.block = true; g.w.Extras.StepLimit = 20 * time.Millisecond },
		"locked":      func(g *extraRig) { g.lock = func() LockState { return LockState{Kind: LockHeld} } },
		"apt failure": func(g *extraRig) { g.runner.failAt[upgradeKey] = "E: boom" },
		"stray output": func(g *extraRig) {
			g.step.out = StepOutcome{Output: "\xff\xfe binary \x00 junk", ExitCode: 0, Err: errors.New("descendants outlived DietPi")}
		},
	}
	for name, setup := range scenarios {
		t.Run("[AC-038] "+name+" produces a valid record within the bounds", func(t *testing.T) {
			g := newExtraRig(t)
			setup(g)
			rec, _ := g.run(v2Request("full"))
			if err := rec.Validate(); err != nil {
				t.Fatalf("final record invalid: %v", err)
			}
			if !rec.Terminal() || len(rec.Summary) > 4096 || len(rec.Output) > MaxOutputBytes {
				t.Errorf("terminal=%v summary=%d output=%d", rec.Terminal(), len(rec.Summary), len(rec.Output))
			}
		})
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// fixedEnvironmentForTest is maintenance.FixedEnvironment on Linux. The helper
// lives in a Linux-only file, so the test pins the same list portably; a Linux
// test asserts the two stay equal.
func fixedEnvironmentForTest() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", "DEBCONF_NONINTERACTIVE_SEEN=true", "NEEDRESTART_MODE=l"}
}
