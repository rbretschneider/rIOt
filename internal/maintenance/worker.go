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
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}
func aptOptions() []string {
	return []string{"-y", "--no-remove", "-o", "DPkg::Lock::Timeout=120", "-o", "Acquire::Retries=0", "-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Options::=--force-confdef"}
}

// Execute is called only while the root exclusion is held. Its context belongs
// to the service, never to an agent observer or an HTTP request.
func (w *Worker) Execute(ctx context.Context, req Request) (rec Record, retErr error) {
	if err := ValidateRequest(req); err != nil {
		return rec, err
	}
	if w.Store == nil || w.Runner == nil {
		return rec, errors.New("maintenance worker is not configured")
	}
	now := w.now()
	rec = Record{Version: ProtocolVersion, CommandID: req.CommandID, Mode: req.Mode, IncludeRebootClass: req.IncludeRebootClass, StartedAt: now, UpdatedAt: now, Phase: "accepted", Outcome: "running"}
	if err := w.Store.Begin(rec); err != nil {
		return rec, err
	}
	ctx, cancel := context.WithTimeout(ctx, WorkerBudget)
	defer cancel()
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
			if err := w.Store.Save(rec); err != nil {
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
		rec.UpdatedAt = w.now()
		finished := rec.UpdatedAt
		rec.FinishedAt = &finished
		rec.Phase = "finished"
		if retErr == nil {
			rec.Outcome = "success"
			rec.ExitCode = 0
			rec.Summary = "Package state verified; maintenance completed"
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
		if err := w.Store.Finish(rec); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("persist maintenance result: %w", err))
		}
	}()
	step := func(phase, path string, args ...string) (CommandResult, error) {
		if err := ctx.Err(); err != nil {
			return CommandResult{Interrupted: true}, err
		}
		rec.Phase = phase
		rec.UpdatedAt = w.now()
		if err := w.Store.Save(rec); err != nil {
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
			return rec, errors.New("authorized hold adapter unavailable")
		}
		snapshot, err := w.Holds.Snapshot(ctx)
		if err != nil {
			return rec, fmt.Errorf("snapshot managed holds: %w", err)
		}
		rec.Holds = snapshot
		rec.HoldsReleased = true
		rec.Phase = "hold_release"
		rec.UpdatedAt = w.now()
		if err = w.Store.Save(rec); err != nil {
			rec.HoldsReleased = false
			return rec, err
		}
		releaseAttempted = true
		if err = w.Holds.Release(ctx, snapshot); err != nil {
			return rec, fmt.Errorf("release managed holds: %w", err)
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
		return rec, configured
	}
	if errors.Is(verified, errPersistence) {
		return rec, verified
	}
	if configured != nil || verified != nil {
		if ctx.Err() != nil {
			return rec, ctx.Err()
		}
		args := append(aptOptions(), "--fix-broken", "install")
		if _, err := step("repair", "/usr/bin/apt-get", args...); err != nil {
			return rec, err
		}
		if err := configure(); err != nil {
			return rec, err
		}
		if err := verify(); err != nil {
			return rec, err
		}
	}
	if req.Mode == "repair" {
		return rec, nil
	}

	if _, err := step("refresh", "/usr/bin/apt-get", "-o", "Acquire::Retries=0", "update"); err != nil {
		return rec, err
	}
	action := "dist-upgrade"
	if req.Mode == "security" {
		action = "upgrade"
	}
	if _, err := step("upgrade", "/usr/bin/apt-get", append(aptOptions(), action)...); err != nil {
		return rec, err
	}
	if err := verify(); err != nil {
		return rec, err
	}
	return rec, nil
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
	return rec, errors.Join(cleanupErr, w.Store.Finish(rec))
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
