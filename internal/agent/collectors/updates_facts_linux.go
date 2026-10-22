//go:build linux

package collectors

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// runReleaseCheck runs `do-release-upgrade -c` as the agent user, in its own
// process group so a timeout kills the whole group.
func runReleaseCheck(ctx context.Context) (int, string, error) {
	cmd := exec.CommandContext(ctx, releaseUpgradeBinary, "-c")
	cmd.Env = releaseCheckEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	if err == nil {
		return 0, out.String(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && ctx.Err() == nil {
		return exitErr.ExitCode(), out.String(), nil
	}
	return -1, out.String(), err
}
