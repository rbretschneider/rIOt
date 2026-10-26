package agent

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer (SHUTDOWN-DPKG-001). Gap-filling tests found during the QA
// audit. Like every shutdown test they run on the fake clock with a runner seam;
// none reaches a real power process.

// ---------------------------------------------------------------------------
// AC-018: the gate specs are wired to their OWN constants
// ---------------------------------------------------------------------------

// gateBoundSelectors returns, per spec function, the "pkg.Name" selector used as
// the waitBound of its powerGate literal.
func gateBoundSelectors(t *testing.T, src []byte) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "reboot_clearance.go", src, 0)
	require.NoError(t, err)
	out := map[string]string{}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "powerGate" {
				return true
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "waitBound" {
					if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok {
							out[fn.Name.Name] = pkg.Name + "." + sel.Sel.Name
						}
					} else {
						out[fn.Name.Name] = "<not a plain selector>"
					}
				}
			}
			return true
		})
	}
	return out
}

// Covers AC-018 / FR-019 / BR-004. The budgets are equal at launch, so a value
// comparison cannot tell a shutdown gate wired to the reboot bound from a correct
// one: a shutdown gate that read models.DpkgLockWaitBound passed every other test.
// The wiring is therefore read from the declarations.
func TestPowerGate_QA_AC018_GateBoundsAreWiredToTheirOwnConstants(t *testing.T) {
	src, err := os.ReadFile("reboot_clearance.go")
	require.NoError(t, err)

	got := gateBoundSelectors(t, src)

	assert.Equal(t, "models.DpkgLockWaitBound", got["rebootGate"], "[AC-018] the reboot gate keeps the reboot wait bound")
	assert.Equal(t, "models.ShutdownDpkgLockWaitBound", got["shutdownGate"], "[AC-018] the shutdown gate uses its own wait bound, not the reboot one")

	// Self-test: the mis-wired shapes are flagged.
	const crossed = `package agent
func rebootGate() powerGate   { return powerGate{waitBound: models.ShutdownDpkgLockWaitBound} }
func shutdownGate() powerGate { return powerGate{waitBound: models.DpkgLockWaitBound} }`
	bad := gateBoundSelectors(t, []byte(crossed))
	assert.Equal(t, "models.ShutdownDpkgLockWaitBound", bad["rebootGate"], "[AC-018] self-test: the reader sees a crossed reboot gate")
	assert.Equal(t, "models.DpkgLockWaitBound", bad["shutdownGate"], "[AC-018] self-test: the reader sees a crossed shutdown gate")
}

// ---------------------------------------------------------------------------
// AC-016 / FR-018: the agent budget of a real shutdown command
// ---------------------------------------------------------------------------

// Covers AC-016 / FR-018 / FR-020. The policy test pins the constant; this pins
// that the agent actually runs a shutdown under it (the old budget was 1 minute,
// which would have recorded a 2-minute wait as a budget timeout).
func TestShutdown_QA_AC016_AgentCommandContextIsTheShutdownBudget(t *testing.T) {
	r := newShutdownRig(t)

	for action, want := range map[string]time.Duration{
		models.ActionShutdown: 12 * time.Minute,
		models.ActionReboot:   12 * time.Minute,
	} {
		before := time.Now()
		cr := r.a.newCmdRun(models.CommandPayload{CommandID: idA, Action: action})
		dl, ok := cr.ctx.Deadline()
		cr.cancel()
		require.True(t, ok, "[AC-016] %s has a deadline", action)
		left := dl.Sub(before)
		assert.InDelta(t, want.Seconds(), left.Seconds(), 5, "[AC-016] the %s command context lasts %s, got %s", action, want, left)
	}
}

// ---------------------------------------------------------------------------
// AC-003 / AC-004 / FR-003: the exact boundary of the wait
// ---------------------------------------------------------------------------

// Covers AC-004 / AC-016 / FR-003 at the boundary: a lock that is free at exactly
// the 10 minute probe was released in time; one released one second later was not.
func TestShutdown_QA_AC004_ExactBoundary(t *testing.T) {
	t.Run("free at exactly the bound proceeds", func(t *testing.T) {
		r := newShutdownRig(t)
		r.script = heldUntil(models.ShutdownDpkgLockWaitBound, unattendedUpgrHolder)

		res := r.runShutdown()

		assert.Equal(t, models.ResultStatusSuccess, res.Status, "[AC-004] released at the bound is not a timeout")
		assert.Equal(t, "shutdown initiated after waiting 10m for the dpkg lock", res.Message, "[AC-004] the wait is reported")
		assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-004] one poweroff")
	})
	t.Run("held at exactly the bound skips", func(t *testing.T) {
		r := newShutdownRig(t)
		r.script = heldUntil(models.ShutdownDpkgLockWaitBound+time.Second, unattendedUpgrHolder)

		res := r.runShutdown()

		assert.Equal(t, models.ResultStatusError, res.Status, "[AC-004] still held at the bound is a skip")
		assert.Contains(t, res.Message, "dpkg lock held past the timeout (10m)", "[AC-004] the timeout is reported")
		assert.Zero(t, r.poweroffs.Load(), "[AC-004] and never forced")
		assert.Equal(t, models.ShutdownDpkgLockWaitBound, r.sinceFirstProbe(), "[AC-004] the wait stops at the bound, not a poll later")
	})
}

// ---------------------------------------------------------------------------
// FR-010 / FR-012: an agent that is already stopping
// ---------------------------------------------------------------------------

// Covers AC-013 / FR-010 / FR-012: a shutdown that arrives after the agent was told
// to stop must not power off, even when the lock is free and every config flag is on.
func TestShutdown_QA_AC013_AlreadyShuttingDownNeverPowersOff(t *testing.T) {
	r := newShutdownRig(t)
	r.a.shuttingDown.Store(true)

	c := r.a.awaitShutdownClearance(context.Background(), idA)
	res := shutdownSkippedResult(c)

	assert.False(t, c.Cleared, "[AC-013] a stopping agent never clears")
	assert.Equal(t, "shutdown skipped: agent shutting down", res.Message, "[AC-013] outcome (e)")
	assert.Equal(t, models.ReasonShutdown, res.Reason, "[AC-013] reason shutdown")
	assert.EqualValues(t, 0, r.a.powerPermitSeq.Load(), "[AC-013] no permit was minted")
	assert.Zero(t, r.poweroffs.Load(), "[AC-013] no poweroff")
}

// ---------------------------------------------------------------------------
// NFR-003: no leaked goroutine, no stuck child flag
// ---------------------------------------------------------------------------

// Covers AC-030 / NFR-003 / AD-007: every way a poweroff child can end leaves no
// goroutine behind and clears the one-child flag, so repeated shutdowns do not
// accumulate.
func TestExecShutdown_QA_AC030_NoGoroutineOrFlagLeakAcrossEveryExit(t *testing.T) {
	exits := []struct {
		name string
		out  string
		err  error
	}{
		{"exit 0", "", nil},
		{"refused", sudoNoTTY, fakeExit{code: 1}},
		{"signal", "", fakeExit{code: -1}},
		{"launch error", "", errors.New("exec: not found")},
	}
	r := newShutdownRig(t)
	runtime.GC()
	base := runtime.NumGoroutine()

	for i := 0; i < 25; i++ {
		for _, e := range exits {
			r.setPoweroff(e.out, e.err)
			_ = r.a.execShutdown(r.a.mintPowerPermit(powerActionShutdown))
			// The reaper clears the one-child flag just after it hands the exit over, so
			// the next call must wait for it (see the QA report: a microsecond window).
			require.Eventually(t, func() bool { return !r.a.poweroffRunning.Load() }, 5*time.Second, 50*time.Microsecond,
				"[AC-030] the one-child flag must clear after each exit (%s)", e.name)
		}
	}

	require.Eventually(t, func() bool { return !r.a.poweroffRunning.Load() }, 5*time.Second, time.Millisecond,
		"[AC-030] the one-child flag must be clear after the last exit")
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= base+2 }, 5*time.Second, 5*time.Millisecond,
		"[AC-030] %d goroutines after 100 poweroffs, started with %d", runtime.NumGoroutine(), base)
	assert.EqualValues(t, 100, r.poweroffs.Load(), "[AC-030] every call launched exactly one child")
}

// Covers AC-030 / NFR-003: a child that never returns holds exactly one goroutine
// and the flag; once it exits both are released.
func TestExecShutdown_QA_AC030_HungChildHoldsOneGoroutineThenReleasesIt(t *testing.T) {
	r := newShutdownRig(t)
	r.poweroffBlock = make(chan struct{})
	runtime.GC()
	base := runtime.NumGoroutine()

	done := make(chan error, 1)
	go func() { done <- r.a.execShutdown(r.a.mintPowerPermit(powerActionShutdown)) }()
	require.Eventually(t, r.launchedAndArmed, 5*time.Second, time.Millisecond, "[AC-030] launched and armed")
	r.clk.Advance(models.ShutdownStartCheckBound)
	select {
	case err := <-done:
		assert.NoError(t, err, "[AC-030] still running at the bound is initiated")
	case <-time.After(5 * time.Second):
		t.Fatal("[AC-030] execShutdown did not return at the start-check bound")
	}

	// Ten more attempts while the child hangs: all refused, none adds a goroutine.
	for i := 0; i < 10; i++ {
		err := r.a.execShutdown(r.a.mintPowerPermit(powerActionShutdown))
		assert.EqualError(t, err, "a previous poweroff command is still running", "[AC-030] refused while a child is alive")
	}
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-030] still one child")
	assert.LessOrEqual(t, runtime.NumGoroutine(), base+3, "[AC-030] a hung child costs one goroutine, not one per attempt")

	close(r.poweroffBlock)
	require.Eventually(t, func() bool { return !r.a.poweroffRunning.Load() }, 5*time.Second, time.Millisecond, "[AC-030] released")
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= base+2 }, 5*time.Second, 5*time.Millisecond,
		"[AC-030] the reaper goroutine ends with the child")
}

// ---------------------------------------------------------------------------
// AC-029: hostile error text
// ---------------------------------------------------------------------------
// u returns the string of one rune, so the invisible characters stay escaped in the source.
func u(r rune) string { return string(r) }

// Covers AC-029 / FR-028: invisible and direction-changing characters, line
// separators and a long run of them are neutralized, not just ASCII controls.
func TestSanitizePowerErr_QA_AC029_UnicodeControlAndFormatCharacters(t *testing.T) {
	cases := map[string]string{
		"bidi override":      "exit status 1: " + u(0x202e) + "evil" + u(0x202c),
		"zero width space":   "a" + u(0x200b) + "b" + u(0x200c) + "c" + u(0x200d) + "d" + u(0xfeff) + "e",
		"line separator":     "one" + u(0x2028) + "two" + u(0x2029) + "three" + u(0x85) + "four",
		"C1 controls":        "x" + u(0x80) + u(0x9f) + "y",
		"DEL":                "a\x7fb",
		"ANSI OSC link":      "\x1b]8;;https://evil.example\x07click\x1b]8;;\x07",
		"only format chars":  u(0x200b) + u(0x200c) + u(0x202e),
		"truncated mid-rune": strings.Repeat("\u00e9\u20ac\U0001F600", 120),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitizePowerErr(in)
			assert.LessOrEqual(t, len(got), 200, "[AC-029] at most 200 bytes, got %d", len(got))
			assert.True(t, utf8.ValidString(got), "[AC-029] valid UTF-8: %q", got)
			for _, r := range got {
				assert.True(t, unicode.IsPrint(r), "[AC-029] rune %U in %q is not printable", r, got)
			}
			assert.NotContains(t, got, "\x1b", "[AC-029] no escape character survives")
		})
	}
}

// Covers AC-029 / FR-028 end to end: the result for a refusal whose output is all
// control characters stays in the form `shutdown: <err>` with a printable err.
func TestShutdown_QA_AC029_ControlOnlyOutputKeepsTheForm(t *testing.T) {
	r := newShutdownRig(t)
	r.setPoweroff("\x1b[2J\x07\r\n\x00", fakeExit{code: 1})

	res := r.runShutdown()

	assert.Equal(t, models.ResultStatusError, res.Status, "[AC-029] failed")
	// The ESC byte is removed; the printable remainder of the sequence ("[2J") is
	// kept, which is within FR-028 (printable characters only).
	assert.Equal(t, "shutdown: exit status 1: [2J", res.Message, "[AC-029] the controls are gone, the printable part survives")
	for _, ch := range res.Message {
		assert.True(t, unicode.IsPrint(ch), "[AC-029] rune %U is not printable", ch)
	}
}

// ---------------------------------------------------------------------------
// AC-028 / BR-006: a refusal must not leave a usable permit or a stuck flag
// ---------------------------------------------------------------------------

// Covers AC-028 / BR-006 / FR-008: after a refused poweroff the same cleared
// attempt cannot be replayed into a second poweroff, and a later shutdown works.
func TestShutdown_QA_AC028_RefusalIsFinalForTheAttemptAndFreesTheAgent(t *testing.T) {
	r := newShutdownRig(t)
	r.setPoweroff(sudoNoTTY, fakeExit{code: 1})
	c := r.a.awaitShutdownClearance(context.Background(), idA)
	require.True(t, c.Cleared, "[AC-028] precondition: cleared")

	require.Error(t, r.a.execShutdown(c.Permit), "[AC-028] the refusal is reported")
	assert.ErrorIs(t, r.a.execShutdown(c.Permit), errShutdownNotCleared, "[AC-028] the used permit cannot be replayed")
	assert.EqualValues(t, 1, r.poweroffs.Load(), "[AC-028] one launch")
	assert.EqualValues(t, 0, r.a.powerPermitLive.Load(), "[AC-028] no live permit is left behind")
	require.Eventually(t, func() bool { return !r.a.poweroffRunning.Load() }, 5*time.Second, time.Millisecond,
		"[AC-028] a refused child frees the agent for the next shutdown")

	r.setPoweroff("", nil)
	assert.NoError(t, r.a.execShutdown(r.a.mintPowerPermit(powerActionShutdown)), "[AC-028] the next shutdown works")
}

// Covers AC-004 / NFR-003: a skipped or unknown attempt leaves no live permit.
func TestShutdown_QA_AC004_SkippedAttemptsLeaveNoLivePermit(t *testing.T) {
	scripts := map[string]func(time.Duration, int) dpkgLockStatus{
		"timeout": alwaysHeld(unattendedUpgrHolder),
		"unknown": alwaysHeld(dpkgLockStatus{State: dpkgLockUnknown, UID: -1, Cause: "boom"}),
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			r := newShutdownRig(t)
			r.script = script

			r.runShutdown()

			assert.EqualValues(t, 0, r.a.powerPermitLive.Load(), "[AC-004] no live permit after a skip")
			assert.EqualValues(t, 0, r.a.powerPermitSeq.Load(), "[AC-004] no permit was ever minted")
			assert.Zero(t, r.poweroffs.Load(), "[AC-004] no poweroff")
		})
	}
}
