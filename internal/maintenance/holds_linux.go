//go:build linux

package maintenance

import (
	"context"
	"errors"
	"os"
	"reflect"
	"regexp"
	"time"
)

var holdOperand = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:~_-]*$`)

// HoldCommandAllowed is an argv-shape defense. The collectors adapter is
// responsible for managed ownership and classification before this sink.
func HoldCommandAllowed(path string, args []string) bool {
	if path == "/usr/bin/dpkg-query" {
		return reflect.DeepEqual(args, []string{"-W", "-f=${Package}\t${Version}\t${db:Status-Status}\n"})
	}
	if path != "/usr/bin/apt-mark" || len(args) == 0 {
		return false
	}
	if len(args) == 1 && args[0] == "showhold" {
		return true
	}
	if len(args) < 2 || len(args) > 257 || (args[0] != "hold" && args[0] != "unhold") {
		return false
	}
	for _, name := range args[1:] {
		if len(name) > 256 || !holdOperand.MatchString(name) {
			return false
		}
	}
	return true
}
func RunHoldCommand(ctx context.Context, path string, args ...string) (CommandResult, error) {
	if !HoldCommandAllowed(path, args) {
		return CommandResult{ExitCode: 1}, errors.New("command is outside the fixed hold plan")
	}
	// A hold-cleanup call must finish, including TERM/KILL/drain grace,
	// inside the outer independent two-minute cleanup budget.
	deadline := time.Now().Add(CleanupBudget)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return CommandResult{ExitCode: 1, Interrupted: true}, context.DeadlineExceeded
	}
	grace := TerminationGrace
	if remaining < grace+10*time.Second {
		grace = remaining / 2
	}
	drain := 5 * time.Second
	if remaining < 10*time.Second {
		drain = remaining / 10
	}
	work, stop := context.WithDeadline(ctx, deadline.Add(-grace-drain))
	defer stop()
	result, err := runProcessBounded(work, path, args, grace, os.Stdout, os.Stderr, 1<<20)
	if result.Truncated {
		return result, errors.Join(err, errors.New("hold command exceeded bounded output; refusing partial query"))
	}
	return result, err
}
