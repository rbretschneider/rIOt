package agent

import (
	"errors"
	"testing"
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
