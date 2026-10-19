package agent

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// limitedProcess is a child process that runLimited can stop in two stages:
// SIGTERM first, SIGKILL only if it is still running after the grace period
// (FR-044). The real implementation wraps exec.Cmd; tests supply a fake.
type limitedProcess interface {
	Start() error
	// Wait blocks until the process exits.
	Wait() error
	// Terminate asks the process to stop (SIGTERM; Kill on Windows).
	Terminate() error
	// Kill stops the process immediately.
	Kill() error
	// Output returns everything the process wrote to stdout and stderr.
	Output() []byte
}

// runLimited runs p for at most limit. When the limit is reached it sends
// SIGTERM, waits grace for the process to exit, and sends SIGKILL only if it is
// still running (AD-013). timedOut is true when the limit, not the process, ended
// the run. If ctx is cancelled first the same two-stage stop applies and err
// carries ctx.Err(). The timers are created before Start so a caller that
// observes the process starting can rely on the limit already being armed.
func runLimited(ctx context.Context, p limitedProcess, limit, grace time.Duration, clk clock) (out []byte, timedOut bool, err error) {
	limitTimer := clk.NewTimer(limit)
	defer limitTimer.Stop()

	if err := p.Start(); err != nil {
		return nil, false, err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- p.Wait() }()

	select {
	case werr := <-waitCh:
		return p.Output(), false, werr
	case <-limitTimer.C():
		timedOut = true
	case <-ctx.Done():
		err = ctx.Err()
	}

	// Stage 1: SIGTERM, then wait out the grace period.
	if terr := p.Terminate(); terr != nil {
		// The process may already have exited between the select and the signal;
		// the wait below settles that either way.
		slog.Debug("proclimit: terminate failed", "error", terr)
	}
	graceTimer := clk.NewTimer(grace)
	defer graceTimer.Stop()
	select {
	case werr := <-waitCh:
		if err == nil {
			err = werr
		}
		return p.Output(), timedOut, err
	case <-graceTimer.C():
	}

	// Stage 2: SIGKILL.
	if kerr := p.Kill(); kerr != nil {
		slog.Warn("proclimit: kill failed", "error", kerr)
	}
	werr := <-waitCh
	if err == nil {
		err = werr
	}
	return p.Output(), timedOut, err
}

// execProcess is the production limitedProcess over os/exec.
type execProcess struct {
	cmd *exec.Cmd
	buf lockedBuffer
}

// newExecProcess builds an unstarted process whose stdout and stderr are
// captured together. A nil env inherits the agent's environment; the compose
// children pass an explicit one (AUTOUPD-HARDEN-001 AD-016).
func newExecProcess(name string, env []string, args ...string) *execProcess {
	p := &execProcess{cmd: exec.Command(name, args...)}
	if env != nil {
		p.cmd.Env = env
	}
	p.cmd.Stdout = &p.buf
	p.cmd.Stderr = &p.buf
	// Bound how long Wait lingers on output pipes inherited by grandchildren
	// after the process itself has exited.
	p.cmd.WaitDelay = 5 * time.Second
	return p
}

func (p *execProcess) Start() error { return p.cmd.Start() }
func (p *execProcess) Wait() error  { return p.cmd.Wait() }
func (p *execProcess) Kill() error  { return p.cmd.Process.Kill() }

func (p *execProcess) Terminate() error {
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Windows cannot deliver SIGTERM to a child; fall back to Kill.
		return p.cmd.Process.Kill()
	}
	return nil
}

func (p *execProcess) Output() []byte { return p.buf.Bytes() }

// lockedBuffer is a bytes.Buffer safe for the writer goroutines of exec.Cmd and a
// concurrent Output call.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}
