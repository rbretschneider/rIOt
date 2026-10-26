package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

// SHUTDOWN-SUDO-001 AD-009. Doctor asks sudo whether the unprivileged user may
// run the agent's shutdown command, using a policy listing (sudo -n -l). The
// listing never runs the command and never prompts for a password.

// shutdownProbeTimeout bounds the listing. It is below the 15 s bound of the
// other doctor sudo checks (FR-028).
const shutdownProbeTimeout = 10 * time.Second

// shutdownSudoCommand is the command the shutdown path hands to sudo. It is the
// one place outside execShutdown that the power verb may appear, and it is data
// only: the C-4 scan allows it by shape (power_paths_test.go) and the reference
// rule in shutdown_doctor_test.go limits who may use it. A test pins it to the
// argv execShutdown really launches, so the two cannot drift apart.
func shutdownSudoCommand() []string {
	return []string{"systemctl", "poweroff"}
}

// shutdownProbeArgs is the sudo argument list of the listing probe.
func shutdownProbeArgs() []string {
	return append([]string{"-n", "-l"}, shutdownSudoCommand()...)
}

// reportShutdownPrivilege prints the Remote Shutdown section. It does nothing
// unless shutdown is enabled in the config on Linux (FR-029). The runner is
// injected so tests can check every invocation (FR-030).
func reportShutdownPrivilege(goos string, allow bool, run collectors.CommandRunner, timeout time.Duration) {
	if goos != "linux" || !allow {
		return
	}
	fmt.Println()
	section("Remote Shutdown")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := strings.Join(shutdownSudoCommand(), " ")
	_, err := run(ctx, "sudo", shutdownProbeArgs()...)
	if err == nil {
		pass("shutdown sudo rule present: riot may run %s without a password", cmd)
		return
	}
	why := "not allowed without a password"
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		why = fmt.Sprintf("timed out after %s", timeout)
	case errors.Is(err, exec.ErrNotFound):
		why = "sudo not found"
	}
	fail("shutdown sudo rule missing or unverifiable (%s): %s; remote shutdown will report failed", cmd, why)
	warn("  Set commands.allow_shutdown: true in agent.yaml (it is set here) and re-run the install script as root (curl … | sudo bash); agent-only updates do not change /etc/sudoers.d/riot-agent")
}
