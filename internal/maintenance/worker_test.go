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
)

type memoryStore struct {
	records   []Record
	beginErr  error
	failPhase string
}

func (s *memoryStore) Current() (Record, error) {
	if len(s.records) == 0 {
		return Record{}, os.ErrNotExist
	}
	return s.records[len(s.records)-1], nil
}
func (s *memoryStore) Begin(r Record) error {
	if s.beginErr != nil {
		return s.beginErr
	}
	s.records = append(s.records, r)
	return nil
}
func (s *memoryStore) Save(r Record) error {
	if r.Phase == s.failPhase {
		return errors.New("disk full")
	}
	s.records = append(s.records, r)
	return nil
}
func (s *memoryStore) Finish(r Record) error { return s.Save(r) }

type invocation struct {
	path string
	args []string
}
type recordingRunner struct {
	calls   []invocation
	failAt  map[int]string
	outputs map[int]string
	hook    func()
}

func (r *recordingRunner) Run(_ context.Context, p string, a ...string) (CommandResult, error) {
	i := len(r.calls)
	r.calls = append(r.calls, invocation{p, append([]string(nil), a...)})
	if r.hook != nil {
		r.hook()
	}
	if msg := r.failAt[i]; msg != "" {
		return CommandResult{Output: msg, ExitCode: 100}, errors.New(msg)
	}
	return CommandResult{Output: r.outputs[i]}, nil
}
func newWorker() (*Worker, *recordingRunner, *memoryStore) {
	runner := &recordingRunner{}
	store := &memoryStore{}
	return &Worker{Store: store, Runner: runner}, runner, store
}
func phases(r *recordingRunner) []string {
	var out []string
	for _, c := range r.calls {
		out = append(out, c.path+" "+strings.Join(c.args, " "))
	}
	return out
}
func TestFixedRecoveryPlan(t *testing.T) {
	t.Run("[MAINT-RECOVERY AC-003] healthy repair stops before refresh upgrade and hold release", func(t *testing.T) {
		w, r, _ := newWorker()
		req := testRequest()
		req.Mode = "repair"
		rec, err := w.Execute(context.Background(), req)
		if err != nil || rec.Outcome != "success" || len(r.calls) != 3 {
			t.Fatalf("%+v %v %v", rec, err, phases(r))
		}
		want := []invocation{{"/usr/bin/dpkg", []string{"--force-confold", "--force-confdef", "--configure", "-a"}}, {"/usr/bin/dpkg", []string{"--audit"}}, {"/usr/bin/apt-get", []string{"check"}}}
		if !reflect.DeepEqual(r.calls, want) {
			t.Fatal(phases(r))
		}
	})
	t.Run("[MAINT-RECOVERY AC-003] configure failure gets one guarded repair before refresh", func(t *testing.T) {
		w, r, _ := newWorker()
		r.failAt = map[int]string{0: "interrupted dpkg"}
		rec, err := w.Execute(context.Background(), testRequest())
		if err != nil || rec.Outcome != "success" {
			t.Fatalf("%+v %v", rec, err)
		}
		if len(r.calls) != 9 || r.calls[1].path != "/usr/bin/apt-get" || !strings.HasSuffix(strings.Join(r.calls[1].args, " "), "--fix-broken install") || r.calls[5].args[2] != "update" {
			t.Fatal(phases(r))
		}
		for _, c := range r.calls {
			if c.path == "/usr/bin/apt-get" && (strings.Contains(strings.Join(c.args, " "), "install") || strings.Contains(strings.Join(c.args, " "), "upgrade")) {
				if !strings.Contains(" "+strings.Join(c.args, " ")+" ", " --no-remove ") {
					t.Fatal("missing no-remove", c)
				}
			}
		}
	})
	t.Run("[MAINT-RECOVERY AC-004] nonempty zero-exit audit triggers repair and cannot report clean success", func(t *testing.T) {
		w, r, _ := newWorker()
		r.outputs = map[int]string{1: "half-configured package", 4: "still broken"}
		rec, err := w.Execute(context.Background(), testRequest())
		if err == nil || rec.Outcome != "error" || len(r.calls) != 5 {
			t.Fatalf("%+v %v %v", rec, err, phases(r))
		}
	})
	for _, reason := range []string{"removal required", "unauthenticated repository", "invalid configuration", "held dependency", "native lock timeout", "maintainer script failed"} {
		t.Run("[MAINT-RECOVERY AC-004] bounded repair failure "+reason, func(t *testing.T) {
			w, r, _ := newWorker()
			r.failAt = map[int]string{0: "broken dpkg", 1: reason}
			rec, err := w.Execute(context.Background(), testRequest())
			if err == nil || rec.Outcome != "error" || len(r.calls) != 2 {
				t.Fatalf("%+v %v %v", rec, err, phases(r))
			}
		})
	}
	t.Run("[MAINT-RECOVERY AC-004] failed refresh never upgrades", func(t *testing.T) {
		w, r, _ := newWorker()
		r.failAt = map[int]string{3: "repository signature failed"}
		rec, err := w.Execute(context.Background(), testRequest())
		if err == nil || rec.Outcome != "error" || len(r.calls) != 4 {
			t.Fatalf("%+v %v", rec, err)
		}
	})
	t.Run("[MAINT-RECOVERY AC-003] conservative security uses guarded upgrade", func(t *testing.T) {
		w, r, _ := newWorker()
		req := testRequest()
		req.Mode = "security"
		_, err := w.Execute(context.Background(), req)
		if err != nil || r.calls[4].args[len(r.calls[4].args)-1] != "upgrade" {
			t.Fatal(err, phases(r))
		}
	})
	t.Run("[MAINT-RECOVERY AC-010] persistence failure prevents every package mutation", func(t *testing.T) {
		w, r, s := newWorker()
		s.failPhase = "configure"
		_, err := w.Execute(context.Background(), testRequest())
		if err == nil || len(r.calls) != 0 {
			t.Fatal(err, phases(r))
		}
	})
	t.Run("[MAINT-RECOVERY AC-006] duplicate or unfinished command refusal never executes", func(t *testing.T) {
		w, r, s := newWorker()
		s.beginErr = ErrBusy
		_, err := w.Execute(context.Background(), testRequest())
		if !errors.Is(err, ErrBusy) || len(r.calls) != 0 {
			t.Fatal(err, phases(r))
		}
	})
}

type fakeHolds struct {
	store              *memoryStore
	restored, released int
	restoreErr         error
}

func (h *fakeHolds) Snapshot(context.Context) (HoldSnapshot, error) {
	return HoldSnapshot{Packages: []string{"linux-image-generic"}, Versions: map[string]string{"linux-image-generic": "1"}}, nil
}
func (h *fakeHolds) Release(context.Context, HoldSnapshot) error {
	h.released++
	r, _ := h.store.Current()
	if !r.HoldsReleased || len(r.Holds.Packages) != 1 {
		return errors.New("release preceded durable snapshot")
	}
	return nil
}
func (h *fakeHolds) Restore(ctx context.Context, _ HoldSnapshot) error {
	h.restored++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return h.restoreErr
}
func (h *fakeHolds) Changes(context.Context, HoldSnapshot) ([]VersionChange, error) {
	return []VersionChange{{"linux-image-generic", "1", "2"}}, nil
}
func TestHoldCleanup(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run("[MAINT-RECOVERY AC-008] persists before release and restores on result", func(t *testing.T) {
			w, r, s := newWorker()
			h := &fakeHolds{store: s}
			w.Holds = h
			req := testRequest()
			req.IncludeRebootClass = true
			if failure {
				r.failAt = map[int]string{3: "refresh failed"}
			}
			rec, err := w.Execute(context.Background(), req)
			if (err != nil) != failure || h.released != 1 || h.restored != 1 || rec.HoldsReleased {
				t.Fatalf("%+v %v %+v", rec, err, h)
			}
			if !failure && len(rec.VersionChanges) != 1 {
				t.Fatal("lost proven version change")
			}
		})
	}
	t.Run("[MAINT-RECOVERY AC-008] failed hold restoration is persistent and prevents success", func(t *testing.T) {
		w, _, s := newWorker()
		h := &fakeHolds{store: s, restoreErr: errors.New("apt-mark refused")}
		w.Holds = h
		req := testRequest()
		req.IncludeRebootClass = true
		rec, err := w.Execute(context.Background(), req)
		if err == nil || rec.Outcome != "error" || !rec.HoldsReleased || !rec.HoldCleanupFailed || rec.HoldRestoreError == "" {
			t.Fatalf("%+v %v", rec, err)
		}
	})
	t.Run("[MAINT-RECOVERY AC-007] canceled worker still uses independent bounded hold cleanup", func(t *testing.T) {
		w, r, s := newWorker()
		h := &fakeHolds{store: s}
		w.Holds = h
		ctx, cancel := context.WithCancel(context.Background())
		r.hook = cancel
		req := testRequest()
		req.IncludeRebootClass = true
		rec, err := w.Execute(ctx, req)
		if err == nil || rec.Outcome != "interrupted" || h.restored != 1 {
			t.Fatalf("%+v %v %+v", rec, err, h)
		}
	})
	t.Run("[MAINT-RECOVERY AC-009] recovered unfinished record never repeats package work", func(t *testing.T) {
		w, r, s := newWorker()
		h := &fakeHolds{store: s}
		w.Holds = h
		now := time.Now().UTC()
		rec := Record{Version: 1, CommandID: testRequest().CommandID, Mode: "full", StartedAt: now, UpdatedAt: now, Outcome: "running", Phase: "upgrade", HoldsReleased: true, Holds: HoldSnapshot{Packages: []string{"linux-image-generic"}}}
		got, err := w.RecoverInterrupted(context.Background(), rec)
		if err != nil || got.Outcome != "interrupted" || h.restored != 1 || len(r.calls) != 0 || got.HoldsReleased {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

// --- PATCH-FLOW-001: post-run list refresh and lists stamp -------------------

// Call indexes of a full-mode run with no failures: 0 configure, 1 audit,
// 2 check, 3 refresh, 4 upgrade, 5 audit, 6 check, 7 post-run refresh.
const (
	callRefresh        = 3
	callUpgrade        = 4
	callPostRefresh    = 7
	callsWithoutExtra  = 7
	callsWithExtra     = 8
	postRefreshMarker  = "[post_refresh]"
	postRefreshFailure = "post-run package list refresh failed"
)

// sourcesSequence returns a Sources func that reports values[i] on call i, the
// last value repeating.
func sourcesSequence(values ...string) (func() (string, error), *int) {
	calls := 0
	return func() (string, error) {
		i := calls
		calls++
		if i >= len(values) {
			i = len(values) - 1
		}
		return values[i], nil
	}, &calls
}

func countingTouch() (func() error, *int) {
	n := 0
	return func() error { n++; return nil }, &n
}

func isRefresh(c invocation) bool {
	return c.path == "/usr/bin/apt-get" && strings.Join(c.args, " ") == "-o Acquire::Retries=0 update"
}

func TestWorker_PF_AC032_ConfigChangedDuringUpgradeRefreshesOnce(t *testing.T) {
	w, r, s := newWorker()
	w.Sources, _ = sourcesSequence("before-run", "after-upgrade")
	touch, touched := countingTouch()
	w.TouchLists = touch
	r.outputs = map[int]string{callPostRefresh: "Hit:1 deb.debian.org"}

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("[AC-032] run must succeed: %+v %v", rec, err)
	}
	if len(r.calls) != callsWithExtra {
		t.Fatalf("[AC-032] want exactly one extra command, got %d calls: %v", len(r.calls), phases(r))
	}
	if !isRefresh(r.calls[callPostRefresh]) {
		t.Errorf("[AC-032] extra command = %v, want the allowlisted refresh argv", r.calls[callPostRefresh])
	}
	refreshes := 0
	for _, c := range r.calls {
		if isRefresh(c) {
			refreshes++
		}
	}
	if refreshes != 2 {
		t.Errorf("[AC-032] refresh count = %d, want 2 (run refresh + exactly one post-run refresh)", refreshes)
	}
	if !strings.Contains(rec.Output, postRefreshMarker+"\nHit:1 deb.debian.org") {
		t.Errorf("[AC-032] post-run refresh output missing from run output: %q", rec.Output)
	}
	if *touched != 2 {
		t.Errorf("[AC-032] lists stamped %d times, want 2 (after each successful refresh)", *touched)
	}
	if rec.Summary != successSummary {
		t.Errorf("[AC-032] summary = %q, want the plain success summary", rec.Summary)
	}
	sawPhase := false
	for _, saved := range s.records {
		if saved.Phase == "post_refresh" {
			sawPhase = true
		}
	}
	if !sawPhase {
		t.Error("[AC-032] post_refresh phase was never persisted before the extra refresh")
	}
}

func TestWorker_PF_AC032_OutputStaysWithinBound(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources, _ = sourcesSequence("a", "b")
	r.outputs = map[int]string{
		callRefresh:     strings.Repeat("x", MaxOutputBytes),
		callPostRefresh: strings.Repeat("y", MaxOutputBytes),
	}
	rec, err := w.Execute(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Output) > MaxOutputBytes {
		t.Errorf("[AC-032] output = %d bytes, want <= %d", len(rec.Output), MaxOutputBytes)
	}
	if verr := rec.Validate(); verr != nil {
		t.Errorf("[AC-032] record must stay valid: %v", verr)
	}
}

func TestWorker_PF_AC033_NoChangeNoExtraRefresh(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources, _ = sourcesSequence("same")
	touch, touched := countingTouch()
	w.TouchLists = touch

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if len(r.calls) != callsWithoutExtra {
		t.Errorf("[AC-033] calls = %d, want %d with no extra refresh: %v", len(r.calls), callsWithoutExtra, phases(r))
	}
	if *touched != 1 {
		t.Errorf("[AC-033] lists stamped %d times, want 1 (the run refresh only)", *touched)
	}
}

func TestWorker_PF_AC033_RepairModeNoRefreshAtAll(t *testing.T) {
	w, r, _ := newWorker()
	sources, sourceCalls := sourcesSequence("a", "b")
	w.Sources = sources
	touch, touched := countingTouch()
	w.TouchLists = touch
	req := testRequest()
	req.Mode = "repair"

	rec, err := w.Execute(context.Background(), req)

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if len(r.calls) != 3 || *sourceCalls != 0 || *touched != 0 {
		t.Errorf("[AC-033] repair must not refresh: calls=%d sourcesCalls=%d stamps=%d", len(r.calls), *sourceCalls, *touched)
	}
}

func TestWorker_PF_AC033_FailedUpgradeNoExtraRefresh(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources, _ = sourcesSequence("a", "b")
	r.failAt = map[int]string{callUpgrade: "E: dpkg returned an error"}

	rec, err := w.Execute(context.Background(), testRequest())

	if err == nil || rec.Outcome != "error" {
		t.Fatalf("upgrade failure must fail the run: %+v %v", rec, err)
	}
	if len(r.calls) != callUpgrade+1 {
		t.Errorf("[AC-033] no post-run refresh after a failed upgrade, got %d calls: %v", len(r.calls), phases(r))
	}
}

func TestWorker_PF_AC032_SourcesErrorIsTreatedAsChanged(t *testing.T) {
	w, r, _ := newWorker()
	calls := 0
	w.Sources = func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("stat failed")
		}
		return "x", nil
	}
	rec, err := w.Execute(context.Background(), testRequest())
	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if len(r.calls) != callsWithExtra {
		t.Errorf("[AC-032] a fingerprint error must fail toward one extra refresh, got %d calls", len(r.calls))
	}
}

func TestWorker_PF_AC034_FailedPostRefreshKeepsSuccessWithNote(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources, _ = sourcesSequence("a", "b")
	touch, touched := countingTouch()
	w.TouchLists = touch
	r.failAt = map[int]string{callPostRefresh: "W: Failed to fetch"}

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" || rec.ExitCode != 0 {
		t.Fatalf("[AC-034] outcome must stay success: %+v %v", rec, err)
	}
	if !strings.Contains(rec.Summary, successSummary) || !strings.Contains(rec.Summary, postRefreshFailure) {
		t.Errorf("[AC-034] summary = %q, want success text plus %q", rec.Summary, postRefreshFailure)
	}
	if *touched != 1 {
		t.Errorf("[AC-034] a failed refresh must not stamp the lists; stamps = %d", *touched)
	}
	if verr := rec.Validate(); verr != nil {
		t.Errorf("[AC-034] record invalid: %v", verr)
	}
}

// interruptingRunner reports Interrupted on one call index, like a runner whose
// child was killed by the sub-context timeout.
type interruptingRunner struct {
	*recordingRunner
	at int
}

func (r *interruptingRunner) Run(ctx context.Context, p string, a ...string) (CommandResult, error) {
	i := len(r.calls)
	out, err := r.recordingRunner.Run(ctx, p, a...)
	if i == r.at {
		return CommandResult{Output: "timed out", ExitCode: 1, Interrupted: true}, errors.New("signal: terminated")
	}
	return out, err
}

func TestWorker_PF_AC034_InterruptedPostRefreshKeepsSuccessNotInterrupted(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources, _ = sourcesSequence("a", "b")
	w.Runner = &interruptingRunner{recordingRunner: r, at: callPostRefresh}

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" || rec.Interrupted {
		t.Fatalf("[AC-034] extra refresh must not mark the run interrupted: %+v %v", rec, err)
	}
	if verr := rec.Validate(); verr != nil {
		t.Errorf("[AC-034] success with Interrupted=true would be rejected by Validate: %v", verr)
	}
	if !strings.Contains(rec.Summary, postRefreshFailure) {
		t.Errorf("[AC-034] summary = %q, want the failure note", rec.Summary)
	}
}

func TestWorker_PF_AC034_LowBudgetSkipsWithNote(t *testing.T) {
	w, r, _ := newWorker()
	w.Sources, _ = sourcesSequence("a", "b")
	ctx, cancel := context.WithTimeout(context.Background(), postRefreshMinRemaining-time.Minute)
	defer cancel()

	rec, err := w.Execute(ctx, testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if len(r.calls) != callsWithoutExtra {
		t.Errorf("[AC-034] low budget must skip the extra refresh, got %d calls", len(r.calls))
	}
	if !strings.Contains(rec.Summary, "post-run package list refresh skipped: worker time budget nearly exhausted") {
		t.Errorf("[AC-034] summary = %q, want the budget skip note", rec.Summary)
	}
}

func TestWorker_PF_AC034_StatePersistFailureSkipsWithNote(t *testing.T) {
	w, r, s := newWorker()
	w.Sources, _ = sourcesSequence("a", "b")
	s.failPhase = "post_refresh"

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if len(r.calls) != callsWithoutExtra || !strings.Contains(rec.Summary, "post-run package list refresh skipped: state not persisted") {
		t.Errorf("[AC-034] want skipped refresh with note; calls=%d summary=%q", len(r.calls), rec.Summary)
	}
}

func TestWorker_PF_AC032_StampFailureIsAnOutputLineNotAFailure(t *testing.T) {
	w, _, _ := newWorker()
	w.TouchLists = func() error { return errors.New("not a real directory") }

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("[AC-032] stamp failure must not fail the run: %+v %v", rec, err)
	}
	if !strings.Contains(rec.Output, "[lists_stamp]\ncould not update /var/lib/apt/lists timestamp: not a real directory") {
		t.Errorf("[AC-032] output lacks the bounded stamp note: %q", rec.Output)
	}
}

func TestWorker_PF_AC019_HoldsRestoredOnFailedAuthorizedRun(t *testing.T) {
	w, r, s := newWorker()
	h := &fakeHolds{store: s}
	w.Holds = h
	w.Sources, _ = sourcesSequence("a", "b")
	r.failAt = map[int]string{callUpgrade: "E: broken"}
	req := testRequest()
	req.IncludeRebootClass = true

	rec, err := w.Execute(context.Background(), req)

	if err == nil || rec.Outcome != "error" {
		t.Fatalf("%+v %v", rec, err)
	}
	if h.released != 1 || h.restored != 1 || rec.HoldsReleased {
		t.Errorf("[AC-019] holds must be restored on a failed authorized run: released=%d restored=%d stillReleased=%v", h.released, h.restored, rec.HoldsReleased)
	}
}

func TestWorker_PF_AC038_PostinstRewriteTriggersOneRefresh(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "etc/apt/sources.list.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	omv := filepath.Join(dir, "omvextras.sources")
	if err := os.WriteFile(omv, []byte("Types: deb"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, r, _ := newWorker()
	w.Sources = func() (string, error) {
		entries, err := AptSourceEntries(root, 1000)
		return AptSourcesFingerprint(entries), err
	}
	// The package's postinst rewrites the file (same content, new mtime) while
	// the upgrade step runs.
	r.hook = func() {
		if len(r.calls) == callUpgrade+1 {
			later := time.Now().Add(time.Hour)
			if err := os.Chtimes(omv, later, later); err != nil {
				t.Error(err)
			}
		}
	}

	rec, err := w.Execute(context.Background(), testRequest())

	if err != nil || rec.Outcome != "success" {
		t.Fatalf("%+v %v", rec, err)
	}
	if len(r.calls) != callsWithExtra || !isRefresh(r.calls[callPostRefresh]) {
		t.Errorf("[AC-038] a postinst rewrite must cause exactly one more refresh: %v", phases(r))
	}
}
