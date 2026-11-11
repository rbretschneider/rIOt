package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

// SHUTDOWN-DPKG-001 AD-010 (security condition C-1). The real-process branch of
// execReboot and execShutdown starts `sudo systemctl reboot|poweroff` or
// `shutdown /r|/s`. In a test binary that must be impossible whatever harness,
// config or runner state a test sets up, so both branches consult inTestBinary
// first. Tests prove it with state and AST checks and never run the real branch.

// inTestBinary reports whether this process is a `go test` binary (Go 1.21+).
// Importing testing from non-test code registers no flags; they are registered in
// testing.Init. Power commands never run in a test binary.
func inTestBinary() bool { return testing.Testing() }

// errPowerProcessInTest is what a real power branch returns in a test binary.
var errPowerProcessInTest = errors.New("refusing to start a real power command inside a test binary")

// EXPECT-007 AD-015 (security condition C-1). The update pipeline (preflight
// sudo probe, unit reset, apply launch) and the hand-off watcher's unit-state
// query used to fall back to a real process runner whenever Agent.runner was
// nil, with no test-binary refusal; the guard above covers power actions only.
// processRunner is the one place those fallbacks are decided, and like the
// power guard it depends on no host state.

// errProcessInTest is what a real-process fallback returns in a test binary
// when no fake runner is injected.
var errProcessInTest = errors.New("refusing to start a real process inside a test binary")

// refuseProcessInTest starts nothing and always fails with errProcessInTest.
func refuseProcessInTest(context.Context, string, ...string) ([]byte, error) {
	return nil, errProcessInTest
}

// processRunner is the runner of the update and hand-off paths: the injected
// runner when set; otherwise, inside a test binary, a runner that refuses;
// otherwise the real execRunner. Production is unchanged.
func (a *Agent) processRunner() collectors.CommandRunner {
	if a.runner != nil {
		return a.runner
	}
	if inTestBinary() {
		return refuseProcessInTest
	}
	return execRunner
}
