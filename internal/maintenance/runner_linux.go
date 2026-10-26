//go:build linux

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func FixedEnvironment() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C", "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", "DEBCONF_NONINTERACTIVE_SEEN=true", "NEEDRESTART_MODE=l"}
}
func CommandAllowed(path string, args []string) bool {
	if path == "/usr/bin/dpkg" {
		return reflect.DeepEqual(args, []string{"--force-confold", "--force-confdef", "--configure", "-a"}) || reflect.DeepEqual(args, []string{"--audit"})
	}
	if path != "/usr/bin/apt-get" {
		return false
	}
	if reflect.DeepEqual(args, []string{"check"}) || reflect.DeepEqual(args, []string{"-o", "Acquire::Retries=0", "update"}) {
		return true
	}
	for _, tail := range [][]string{{"--fix-broken", "install"}, {"dist-upgrade"}, {"upgrade"}} {
		if reflect.DeepEqual(args, append(aptOptions(), tail...)) {
			return true
		}
	}
	return false
}

type outputCapture struct {
	mu        sync.Mutex
	output    string
	truncated bool
	limit     int
}

func (c *outputCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	limit := c.limit
	if limit == 0 {
		limit = MaxOutputBytes
	}
	c.output, c.truncated = appendBoundedLimit(c.output, string(p), c.truncated, limit)
	return len(p), nil
}

// ExecRunner never inherits the invoking agent's environment or terminal.
// Its only descriptors are owned by the root service and journal.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, path string, args ...string) (CommandResult, error) {
	if !CommandAllowed(path, args) {
		return CommandResult{ExitCode: 1}, errors.New("command is outside the fixed maintenance plan")
	}
	return runProcess(ctx, path, args, TerminationGrace, os.Stdout, os.Stderr)
}
func runProcess(ctx context.Context, path string, args []string, grace time.Duration, stdout, stderr io.Writer) (CommandResult, error) {
	return runProcessBounded(ctx, path, args, grace, stdout, stderr, MaxOutputBytes)
}
func runProcessBounded(ctx context.Context, path string, args []string, grace time.Duration, stdout, stderr io.Writer, limit int) (CommandResult, error) {
	res, err := runProcessSpec(ctx, processSpec{path: path, args: args, grace: grace, stdout: stdout, stderr: stderr, limit: limit})
	return res.CommandResult, err
}

// processSpec describes one supervised process. Its zero values give the apt
// behaviour exactly: FixedEnvironment, Dir /, no settle time, no gate and no
// extra descriptors.
type processSpec struct {
	path string
	args []string
	// env and dir default to FixedEnvironment() and "/".
	env []string
	dir string

	grace          time.Duration
	limit          int
	stdout, stderr io.Writer

	// settle is how long members of the process group may outlive the leader
	// before they count as stray (EXTRA-UPD-001: DietPi's tee). Zero keeps the
	// apt rule: any live member is stray at once.
	settle time.Duration
	// extraFiles become the child's descriptors 3 and up. runProcessSpec closes
	// the parent's copies once the child has started.
	extraFiles []*os.File
	// gate runs in its own goroutine after the process started. A non-nil error
	// kills the whole group at once; it is returned in processResult.GateErr.
	gate func(ctx context.Context) error
}

type processResult struct {
	CommandResult
	GateErr error
}

func runProcessSpec(ctx context.Context, ps processSpec) (processResult, error) {
	var result processResult
	if err := ctx.Err(); err != nil {
		result.ExitCode, result.Interrupted = 1, true
		return result, err
	}
	capture := &outputCapture{limit: ps.limit}
	cmd := exec.Command(ps.path, ps.args...)
	cmd.Dir = "/"
	if ps.dir != "" {
		cmd.Dir = ps.dir
	}
	cmd.Env = FixedEnvironment()
	if ps.env != nil {
		cmd.Env = ps.env
	}
	cmd.Stdin = nil
	cmd.ExtraFiles = ps.extraFiles
	stdout, stderr := ps.stdout, ps.stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	cmd.Stdout = io.MultiWriter(capture, stdout)
	cmd.Stderr = io.MultiWriter(capture, stderr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	grace := ps.grace
	drainGrace := 5 * time.Second
	if grace < drainGrace {
		drainGrace = grace / 2
	}
	if drainGrace <= 0 {
		drainGrace = time.Millisecond
	}
	cmd.WaitDelay = drainGrace
	if ps.settle > 0 {
		// Members that may legitimately outlive the leader for the settle time
		// still hold the output pipe; do not cut the pipe before they may end.
		cmd.WaitDelay = ps.settle + drainGrace
	}
	err := cmd.Start()
	// The child owns its copy of every extra descriptor now; the parent's copies
	// must go so a reader sees end-of-file when the child closes its own.
	for _, f := range ps.extraFiles {
		_ = f.Close()
	}
	if err != nil {
		result.ExitCode = 1
		return result, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var gateDone chan error
	if ps.gate != nil {
		gateCtx, stopGate := context.WithCancel(ctx)
		defer stopGate()
		gateDone = make(chan error, 1)
		go func() { gateDone <- ps.gate(gateCtx) }()
	}

	var stopAt, killAt, leaderExit time.Time
	leaderDone, terminating, killed := false, false, false
	ctxDone := ctx.Done()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if leaderDone || terminating {
			alive := processGroupAlive(cmd.Process.Pid)
			if leaderDone && !alive {
				break
			}
			settling := false
			if leaderDone && alive && !terminating {
				if ps.settle > 0 {
					if leaderExit.IsZero() {
						leaderExit = time.Now()
					}
					settling = time.Since(leaderExit) < ps.settle
				}
				if !settling {
					result.Interrupted = true
					terminating = true
					stopAt = time.Now().Add(grace)
					_ = unix.Kill(-cmd.Process.Pid, unix.SIGTERM)
					err = errors.Join(err, errors.New("package descendants outlived parent; state requires manual review"))
				}
			}
			if terminating && !killed && !time.Now().Before(stopAt) {
				_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
				killed = true
				killAt = time.Now().Add(drainGrace)
				err = errors.Join(err, errors.New("forced package process termination; state unknown"))
			}
			if killed && !time.Now().Before(killAt) {
				err = errors.Join(err, errors.New("package process tree did not fully settle; service cgroup cleanup required"))
				break
			}
		}
		select {
		case waitErr := <-done:
			leaderDone = true
			done = nil
			err = errors.Join(err, waitErr)
		case gateErr := <-gateDone:
			gateDone = nil
			if gateErr != nil && result.GateErr == nil {
				result.GateErr = gateErr
				if !killed {
					terminating, killed = true, true
					_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
					killAt = time.Now().Add(drainGrace)
				}
			}
		case <-ctxDone:
			ctxDone = nil
			result.Interrupted = true
			err = errors.Join(err, ctx.Err())
			if !terminating {
				terminating = true
				stopAt = time.Now().Add(grace)
				_ = unix.Kill(-cmd.Process.Pid, unix.SIGTERM)
			}
		case <-tick.C:
		}
	}

	capture.mu.Lock()
	result.Output = capture.output
	result.Truncated = capture.truncated
	capture.mu.Unlock()
	if leaderDone && cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil && result.ExitCode == 0 {
		result.ExitCode = 1
	}
	if err != nil {
		return result, fmt.Errorf("package process failed: %w", err)
	}
	return result, nil
}

// Zombies cannot mutate package state and may await the service manager's
// reaping after their parent dies. Do not mistake them for a live mutator.
func processGroupAlive(pgid int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return unix.Kill(-pgid, 0) == nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		text := string(data)
		end := strings.LastIndex(text, ")")
		if end < 0 {
			continue
		}
		fields := strings.Fields(text[end+1:])
		if len(fields) < 3 {
			continue
		}
		group, err := strconv.Atoi(fields[2])
		if err == nil && group == pgid && fields[0] != "Z" && fields[0] != "X" {
			return true
		}
	}
	return false
}
