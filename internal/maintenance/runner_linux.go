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
	var result CommandResult
	if err := ctx.Err(); err != nil {
		return CommandResult{ExitCode: 1, Interrupted: true}, err
	}
	capture := &outputCapture{limit: limit}
	cmd := exec.Command(path, args...)
	cmd.Dir = "/"
	cmd.Env = FixedEnvironment()
	cmd.Stdin = nil
	cmd.Stdout = io.MultiWriter(capture, stdout)
	cmd.Stderr = io.MultiWriter(capture, stderr)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	drainGrace := 5 * time.Second
	if grace < drainGrace {
		drainGrace = grace / 2
	}
	if drainGrace <= 0 {
		drainGrace = time.Millisecond
	}
	cmd.WaitDelay = drainGrace
	if err := cmd.Start(); err != nil {
		return CommandResult{ExitCode: 1}, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	var stopAt, killAt time.Time
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
			if leaderDone && alive && !terminating {
				result.Interrupted = true
				terminating = true
				stopAt = time.Now().Add(grace)
				_ = unix.Kill(-cmd.Process.Pid, unix.SIGTERM)
				err = errors.Join(err, errors.New("package descendants outlived parent; state requires manual review"))
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
