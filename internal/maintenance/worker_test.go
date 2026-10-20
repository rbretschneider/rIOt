package maintenance

import (
	"context"
	"errors"
	"os"
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
