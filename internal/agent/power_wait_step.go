package agent

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXPECT-007 AD-001 and AD-002. A standalone reboot or shutdown reports the
// dpkg-lock wait as the command step models.StepWaitDpkgLock, so the dashboard
// can say "Rebooting (waiting for dpkg lock)".
//
// The reporter reaches the shared gate through the context, so no gate
// signature changes (every REBOOT-DPKG-001 and SHUTDOWN-DPKG-001 AST rule pins
// them). It cannot influence clearance: no method has a return value, it mints
// no permit and it runs no guard.
//
// Order (FR-004, FR-006):
//   - enter runs from the gate at the first "held" observation. cr.Step makes a
//     synchronous durable write and the notifier can wait up to 1 s for queue
//     space, so enter runs it on its own goroutine and returns at once;
//   - the step that ends the wait (models.StepPowerAction) is queued by leave,
//     only after the power action has started, so nothing is inserted between
//     the gate's final dpkg-lock check and the power action;
//   - settle joins the goroutine on every return path, so none outlives its
//     command.
//
// Bounds (AD-014): at most one goroutine per power command and at most two step
// changes (enter, leave) inside the existing 64-step cap of a command.

type powerWaitReporterKey struct{}

// powerWaitReporter reports the dpkg-wait step of one standalone power command.
type powerWaitReporter struct {
	cr   *cmdRun
	once sync.Once
	done chan struct{} // set before began; closed when the step is queued
	// began is set by enter. Everything enter writes is published before it.
	began atomic.Bool
}

// newPowerWaitReporter returns the reporter of cr's power command.
func newPowerWaitReporter(cr *cmdRun) *powerWaitReporter {
	return &powerWaitReporter{cr: cr}
}

// withPowerWaitReporter returns ctx carrying r for the gate.
func withPowerWaitReporter(ctx context.Context, r *powerWaitReporter) context.Context {
	return context.WithValue(ctx, powerWaitReporterKey{}, r)
}

// powerWaitReporterFrom returns the reporter carried by ctx, or nil.
func powerWaitReporterFrom(ctx context.Context) *powerWaitReporter {
	r, _ := ctx.Value(powerWaitReporterKey{}).(*powerWaitReporter)
	return r
}

// enter reports the waiting step. It is the only reporter method the gate calls
// (security condition C-3 d), it never blocks and a second call is a no-op
// (FR-003).
func (r *powerWaitReporter) enter() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		r.done = make(chan struct{})
		r.began.Store(true)
		go func() {
			defer close(r.done)
			r.cr.Step(models.StepWaitDpkgLock)
		}()
	})
}

// entered reports whether the gate saw the lock held and enter ran.
func (r *powerWaitReporter) entered() bool {
	return r != nil && r.began.Load()
}

// settle waits for the waiting-step report to be queued. It is bounded by one
// local atomic write plus at most 1 s of notifier queue wait. A no-op when the
// wait never began.
func (r *powerWaitReporter) settle() {
	if r.entered() {
		<-r.done
	}
}

// leave queues the step that ends the wait. The handler calls it after the
// power action has started, never before (FR-004). A no-op when the wait never
// began, so a command that did not wait behaves exactly as before.
func (r *powerWaitReporter) leave() {
	if !r.entered() {
		return
	}
	r.settle()
	r.cr.Step(models.StepPowerAction)
}
