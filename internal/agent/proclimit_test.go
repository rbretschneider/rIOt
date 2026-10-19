package agent

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProcess is a limitedProcess driven by the test: it stays "running" until
// the test (or a signal it chooses to honour) lets it exit.
type fakeProcess struct {
	mu       sync.Mutex
	started  chan struct{}
	exit     chan struct{}
	exitErr  error
	out      []byte
	startErr error
	signals  []string
	log      *eventLog
	// honorTerm makes Terminate end the process (a well-behaved child).
	honorTerm bool
	// autoExit makes the process finish at once with exitErr (a quick command).
	autoExit bool
	// onTerminate runs when SIGTERM arrives, before the process reacts.
	onTerminate func()
	once     sync.Once
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{started: make(chan struct{}), exit: make(chan struct{})}
}

func (f *fakeProcess) Start() error {
	if f.startErr != nil {
		return f.startErr
	}
	f.log.add("start")
	close(f.started)
	if f.autoExit {
		f.finish(f.exitErr)
	}
	return nil
}

func (f *fakeProcess) Wait() error {
	<-f.exit
	return f.exitErr
}

func (f *fakeProcess) Terminate() error {
	f.mu.Lock()
	f.signals = append(f.signals, "SIGTERM")
	f.mu.Unlock()
	f.log.add("terminate")
	if f.onTerminate != nil {
		f.onTerminate()
	}
	if f.honorTerm {
		f.finish(errors.New("signal: terminated"))
	}
	return nil
}

func (f *fakeProcess) Kill() error {
	f.mu.Lock()
	f.signals = append(f.signals, "SIGKILL")
	f.mu.Unlock()
	f.log.add("kill")
	f.finish(errors.New("signal: killed"))
	return nil
}

func (f *fakeProcess) Output() []byte { return f.out }

// finish ends the process once.
func (f *fakeProcess) finish(err error) {
	f.once.Do(func() {
		f.exitErr = err
		close(f.exit)
	})
}

func (f *fakeProcess) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.signals...)
}

type limitedResult struct {
	out      []byte
	timedOut bool
	err      error
}

// runInBackground starts runLimited and returns its result channel once the
// limit timer is armed and the process has started.
func runInBackground(ctx context.Context, p *fakeProcess, limit, grace time.Duration, clk *fakeClock) <-chan limitedResult {
	done := make(chan limitedResult, 1)
	go func() {
		out, timedOut, err := runLimited(ctx, p, limit, grace, clk)
		done <- limitedResult{out, timedOut, err}
	}()
	<-p.started
	return done
}

// AC-050 (FR-044): SIGTERM first, SIGKILL only after the 60s grace period when
// the process is still running.
func TestCL_AC050_SigtermThenSigkillAfterGrace(t *testing.T) {
	clk := newFakeClock()
	log := &eventLog{}
	p := newFakeProcess()
	p.log = log
	p.out = []byte("partial output")
	done := runInBackground(context.Background(), p, 10*time.Minute, 60*time.Second, clk)

	clk.Advance(10*time.Minute - time.Second)
	assert.Empty(t, p.sent(), "[AC-050] nothing may be sent before the limit")

	clk.Advance(time.Second) // limit fires
	<-clk.timerCreated       // limit timer (created before Start)
	<-clk.timerCreated       // grace timer, created right after SIGTERM
	assert.Equal(t, []string{"SIGTERM"}, p.sent(), "[AC-050] the limit must send SIGTERM first")

	clk.Advance(59 * time.Second)
	assert.Equal(t, []string{"SIGTERM"}, p.sent(), "[AC-050] no SIGKILL inside the 60s grace period")

	clk.Advance(time.Second) // grace elapsed
	res := <-done
	assert.Equal(t, []string{"SIGTERM", "SIGKILL"}, p.sent(), "[AC-050] SIGKILL only after the grace period")
	assert.True(t, res.timedOut, "[AC-050] the run must be reported as timed out")
	assert.Equal(t, []byte("partial output"), res.out, "[AC-050] output captured so far is returned")
	assert.Equal(t, []string{"start", "terminate", "kill"}, log.all())
}

// AC-051 (FR-044): a process that exits 5s after SIGTERM is never killed.
func TestCL_AC051_ExitWithinGraceNoSigkill(t *testing.T) {
	clk := newFakeClock()
	p := newFakeProcess()
	done := runInBackground(context.Background(), p, 10*time.Minute, 60*time.Second, clk)

	clk.Advance(10 * time.Minute)
	<-clk.timerCreated
	<-clk.timerCreated
	clk.Advance(5 * time.Second)
	p.finish(errors.New("signal: terminated"))

	res := <-done
	assert.Equal(t, []string{"SIGTERM"}, p.sent(), "[AC-051] a process that exits inside the grace period must not be killed")
	assert.True(t, res.timedOut, "[AC-051] the limit still counts as the cause")
}

// AC-050: a child that honours SIGTERM ends immediately without waiting for the
// grace timer.
func TestCL_AC050_WellBehavedChildStopsOnSigterm(t *testing.T) {
	clk := newFakeClock()
	p := newFakeProcess()
	p.honorTerm = true
	done := runInBackground(context.Background(), p, time.Minute, 60*time.Second, clk)

	clk.Advance(time.Minute)
	res := <-done

	assert.True(t, res.timedOut)
	assert.Equal(t, []string{"SIGTERM"}, p.sent())
}

// FR-043: a process that finishes inside its limit is not signalled, and its
// error and output are returned as is.
func TestCL_AC049_ProcessFinishingBeforeLimitIsNotSignalled(t *testing.T) {
	clk := newFakeClock()
	p := newFakeProcess()
	p.out = []byte("pulled")
	done := runInBackground(context.Background(), p, 15*time.Minute, 60*time.Second, clk)

	clk.Advance(14 * time.Minute)
	p.finish(errors.New("exit status 1"))
	res := <-done

	assert.False(t, res.timedOut, "[AC-049] finishing inside the limit is not a timeout")
	assert.EqualError(t, res.err, "exit status 1")
	assert.Empty(t, p.sent(), "[AC-049] no signal may be sent to a process that exited by itself")
	assert.Equal(t, []byte("pulled"), res.out)
}

// A cancelled parent context stops the child the same two-stage way and the
// caller sees the context error, not a timeout.
func TestCL_AC050_ParentContextCancelUsesSameGrace(t *testing.T) {
	clk := newFakeClock()
	p := newFakeProcess()
	ctx, cancel := context.WithCancel(context.Background())
	done := runInBackground(ctx, p, time.Hour, 60*time.Second, clk)

	cancel()
	<-clk.timerCreated // limit
	<-clk.timerCreated // grace
	assert.Equal(t, []string{"SIGTERM"}, p.sent())
	clk.Advance(60 * time.Second)
	res := <-done

	assert.False(t, res.timedOut, "[AC-050] a cancelled context is not a step timeout")
	assert.ErrorIs(t, res.err, context.Canceled)
	assert.Equal(t, []string{"SIGTERM", "SIGKILL"}, p.sent())
}

func TestCL_AC050_StartFailureIsReturned(t *testing.T) {
	p := newFakeProcess()
	p.startErr = errors.New("exec: docker: not found")

	_, timedOut, err := runLimited(context.Background(), p, time.Minute, time.Minute, newFakeClock())

	assert.False(t, timedOut)
	assert.EqualError(t, err, "exec: docker: not found", "[AC-050] a process that cannot start fails immediately")
}

// The production process wrapper captures combined output and reports exit
// status. The test binary itself is the child (-test.list prints test names), so
// no external program is assumed.
func TestCL_AC050_ExecProcessCapturesOutput(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	p := newExecProcess(exe, "-test.list=TestCL_AC050_ExecProcessCapturesOutput")

	out, timedOut, err := runLimited(context.Background(), p, time.Minute, time.Second, realClock{})

	require.NoError(t, err)
	assert.False(t, timedOut)
	assert.Contains(t, string(out), "TestCL_AC050_ExecProcessCapturesOutput", "[AC-050] combined output must be captured")
}
