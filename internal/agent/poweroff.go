package agent

import (
	"errors"
	"os/exec"
	"strings"
	"unicode"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SHUTDOWN-DPKG-001 AD-009: the truthful poweroff result. execShutdown watches
// the poweroff command's exit for models.ShutdownStartCheckBound. Both the real
// process and the test runner seam produce an (output, error) pair, and both go
// through the two functions below, so the seam and production agree (FR-032).

const (
	// poweroffOutputCap bounds the output kept from one poweroff child (NFR-003).
	poweroffOutputCap = 4 << 10
	// poweroffErrMax bounds the <err> text of a failed shutdown result (FR-028).
	poweroffErrMax = 200
)

type poweroffExitKind uint8

const (
	// launchFailed is the zero value, so an unclassified result never reads as
	// success: the command did not start, or failed without an exit code.
	launchFailed poweroffExitKind = iota
	exitedZero
	exitedNonZero // a normal exit with a non-zero code, such as sudo's refusal
	signaled      // killed by a signal: no exit status
)

// poweroffExit is how one poweroff child ended.
type poweroffExit struct {
	Kind   poweroffExitKind
	Code   int
	Err    error
	Output string // at most poweroffOutputCap bytes
}

// initiated reports whether the exit counts as an accepted poweroff. A child
// killed by a signal is what the cgroup stop does to sudo and systemctl once the
// poweroff is underway, so it counts (AD-009, residual R-10).
func (x poweroffExit) initiated() bool { return x.Kind == exitedZero || x.Kind == signaled }

// exitCoder is implemented by *exec.ExitError and by the tests' fake exit errors.
type exitCoder interface{ ExitCode() int }

// poweroffExitFrom sorts the (output, error) pair of one poweroff child.
func poweroffExitFrom(out []byte, err error) poweroffExit {
	x := poweroffExit{Err: err}
	if len(out) > poweroffOutputCap {
		out = out[:poweroffOutputCap]
	}
	x.Output = string(out)
	if err == nil {
		x.Kind = exitedZero
		return x
	}
	var ec exitCoder
	if errors.As(err, &ec) {
		switch code := ec.ExitCode(); {
		case code == 0:
			x.Kind = exitedZero
		case code > 0:
			x.Kind, x.Code = exitedNonZero, code
		default:
			x.Kind, x.Code = signaled, code
		}
		return x
	}
	// The process exited 0 and only its output was still pending at WaitDelay.
	if errors.Is(err, exec.ErrWaitDelay) {
		x.Kind = exitedZero
		return x
	}
	x.Kind = launchFailed
	return x
}

// classifyPoweroff maps an exit to the error execShutdown returns; nil means the
// shutdown was initiated.
func classifyPoweroff(x poweroffExit) error {
	switch x.Kind {
	case exitedZero, signaled:
		return nil
	case exitedNonZero:
		msg := x.Err.Error()
		if out := strings.TrimSpace(x.Output); out != "" {
			msg += ": " + out
		}
		return errors.New(msg)
	default:
		if x.Err == nil {
			return errors.New("shutdown command did not start")
		}
		return x.Err
	}
}

// sanitizePowerErr makes the text of a failed shutdown safe for the result: valid
// UTF-8 without NUL, every non-printable rune turned into a space, runs of spaces
// collapsed, and at most poweroffErrMax bytes (FR-028).
func sanitizePowerErr(s string) string {
	s = models.SanitizeText(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return ' '
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(models.TruncateHead(s, poweroffErrMax))
}

// cappedWriter keeps the first max bytes written and discards the rest, so a
// chatty child cannot grow the agent's memory. Write never fails, so the child
// is never blocked on a full pipe.
type cappedWriter struct {
	max int
	buf []byte
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if room := w.max - len(w.buf); room > 0 {
		w.buf = append(w.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (w *cappedWriter) Bytes() []byte { return w.buf }
