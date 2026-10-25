package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SHUTDOWN-DPKG-001 AD-009: the poweroff exit mapping shared by the test seam and
// the production path. None of these tests starts a power tool: the helper-process
// pattern re-executes this test binary.

// fakeExit is an error with an exit code, like *exec.ExitError, for the seam.
type fakeExit struct{ code int }

func (e fakeExit) Error() string {
	if e.code < 0 {
		return "signal: terminated"
	}
	return fmt.Sprintf("exit status %d", e.code)
}
func (e fakeExit) ExitCode() int { return e.code }

const sudoNoTTY = "sudo: a terminal is required to read the password; either use the -S option to read from standard input or configure an askpass helper\n"

func TestPoweroffExitFrom_SD_AC032_Kinds(t *testing.T) {
	cases := []struct {
		name    string
		out     []byte
		err     error
		want    poweroffExitKind
		initial bool
	}{
		{"nil error is exit 0", nil, nil, exitedZero, true},
		{"an exit error with code 0", nil, fakeExit{0}, exitedZero, true},
		{"a normal non-zero exit", []byte(sudoNoTTY), fakeExit{1}, exitedNonZero, false},
		{"a Windows refusal code", nil, fakeExit{1190}, exitedNonZero, false},
		{"a signal has code -1", nil, fakeExit{-1}, signaled, true},
		{"a wrapped exit error", nil, fmt.Errorf("run: %w", fakeExit{2}), exitedNonZero, false},
		{"exit 0 with output pending at WaitDelay", nil, exec.ErrWaitDelay, exitedZero, true},
		{"program not found", nil, exec.ErrNotFound, launchFailed, false},
		{"a plain error", nil, errors.New("boom"), launchFailed, false},
		{"the test-binary guard error", nil, errPowerProcessInTest, launchFailed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := poweroffExitFrom(tc.out, tc.err)
			assert.Equal(t, tc.want, x.Kind, "[AC-032] kind")
			assert.Equal(t, tc.initial, x.initiated(), "[AC-032] initiated")
			assert.Equal(t, tc.initial, classifyPoweroff(x) == nil, "[AC-032] classify must agree with initiated")
		})
	}
}

func TestClassifyPoweroff_SD_AC028_NonZeroCarriesExitAndOutput(t *testing.T) {
	err := classifyPoweroff(poweroffExitFrom([]byte(sudoNoTTY), fakeExit{1}))

	require.Error(t, err, "[AC-028] a refused poweroff is an error")
	assert.Equal(t, "exit status 1: "+strings.TrimSpace(sudoNoTTY), err.Error(), "[AC-028] exit status then the trimmed output")

	bare := classifyPoweroff(poweroffExitFrom(nil, fakeExit{5}))
	assert.EqualError(t, bare, "exit status 5", "[AC-028] no output, no colon")
}

func TestClassifyPoweroff_SD_AC015_LaunchErrorIsReturnedAsIs(t *testing.T) {
	boom := errors.New("exec: \"sudo\": executable file not found in $PATH")
	assert.Same(t, boom, classifyPoweroff(poweroffExitFrom(nil, boom)), "[AC-015] a launch error passes through unchanged")
	assert.Error(t, classifyPoweroff(poweroffExit{}), "[AC-015] the zero value never reads as success")
}

func TestPoweroffExitFrom_SD_AC030_OutputIsCapped(t *testing.T) {
	x := poweroffExitFrom([]byte(strings.Repeat("x", 3*poweroffOutputCap)), fakeExit{1})
	assert.Len(t, x.Output, poweroffOutputCap, "[AC-030] captured output is bounded")
}

func TestCappedWriter_SD_AC030_KeepsHeadAndNeverFails(t *testing.T) {
	w := &cappedWriter{max: 10}
	for i := 0; i < 5; i++ {
		n, err := w.Write([]byte("abcdef"))
		require.NoError(t, err, "[AC-030] Write must never fail, or the child would block on a full pipe")
		require.Equal(t, 6, n, "[AC-030] Write reports every byte as taken")
	}
	assert.Equal(t, "abcdefabcd", string(w.Bytes()), "[AC-030] the first max bytes are kept")
}

func TestSanitizePowerErr_SD_AC029_PrintableAndAtMost200Bytes(t *testing.T) {
	allPrintable := func(t *testing.T, s string) {
		t.Helper()
		assert.True(t, utf8.ValidString(s), "[AC-029] valid UTF-8: %q", s)
		for _, r := range s {
			assert.True(t, unicode.IsPrint(r), "[AC-029] rune %U in %q is not printable", r, s)
		}
	}
	cases := []struct {
		name string
		in   string
	}{
		{"ESC sequences", "exit status 1: \x1b[31mred\x1b[0m text"},
		{"CR LF TAB", "line one\r\nline two\tend"},
		{"NUL", "before\x00after"},
		{"invalid UTF-8", "bad \xff\xfe bytes"},
		{"a 4 KiB input", strings.Repeat("sudo: refused. ", 300)},
		{"a multi-byte rune at byte 200", strings.Repeat("a", 199) + "éé tail"},
		{"only control characters", "\x01\x02\x03\n\r"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizePowerErr(tc.in)
			assert.LessOrEqual(t, len(got), 200, "[AC-029] at most 200 bytes, got %d", len(got))
			allPrintable(t, got)
		})
	}

	assert.Equal(t, "line one line two end", sanitizePowerErr("line one\r\nline two\tend"), "[AC-029] control characters become single spaces")
	assert.Equal(t, "", sanitizePowerErr("\x01\x02\n"), "[AC-029] nothing printable leaves an empty string")
	long := sanitizePowerErr(strings.Repeat("a", 199) + "éé")
	assert.Equal(t, 199, len(long), "[AC-029] a rune that would straddle byte 200 is dropped whole")
}

// ---------------------------------------------------------------------------
// AC-032: real exec errors map like the seam's
// ---------------------------------------------------------------------------

// TestPoweroffHelperProcess is the child of the helper-process pattern: it does
// nothing unless RIOT_POWEROFF_HELPER is set, so a normal run passes through it.
func TestPoweroffHelperProcess(t *testing.T) {
	mode := os.Getenv("RIOT_POWEROFF_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "exit0":
		os.Exit(0)
	case "exit1":
		fmt.Fprint(os.Stderr, sudoNoTTY)
		os.Exit(1)
	case "sigterm":
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGTERM)
		time.Sleep(30 * time.Second)
		os.Exit(3) // not reached: the signal ends the process
	}
	os.Exit(4)
}

// runHelper runs the helper the way execShutdown runs the poweroff child: Start,
// then Wait, with a capped writer and a WaitDelay.
func runHelper(t *testing.T, mode string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestPoweroffHelperProcess$")
	cmd.Env = append(os.Environ(), "RIOT_POWEROFF_HELPER="+mode)
	buf := &cappedWriter{max: poweroffOutputCap}
	cmd.Stdout, cmd.Stderr = buf, buf
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	err := cmd.Wait()
	return buf.Bytes(), err
}

func TestPoweroffExit_SD_AC032_SeamAndProductionErrorsMapAlike(t *testing.T) {
	missing := "riot-no-such-program-" + fmt.Sprint(time.Now().UnixNano())
	_, startErr := exec.Command(missing).Output()
	require.Error(t, startErr, "precondition: a missing program fails to start")

	type pair struct {
		name       string
		realOut    func() ([]byte, error)
		seamOut    []byte
		seamErr    error
		wantKind   poweroffExitKind
		skipOnGOOS string
	}
	cases := []pair{
		{"exit 0", func() ([]byte, error) { return runHelper(t, "exit0") }, nil, nil, exitedZero, ""},
		{"exit 1", func() ([]byte, error) { return runHelper(t, "exit1") }, []byte(sudoNoTTY), fakeExit{1}, exitedNonZero, ""},
		{"killed by SIGTERM", func() ([]byte, error) { return runHelper(t, "sigterm") }, nil, fakeExit{-1}, signaled, "windows"},
		{"program not found", func() ([]byte, error) { return nil, startErr }, nil, errors.New("exec: not found"), launchFailed, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipOnGOOS == runtime.GOOS {
				t.Skipf("[AC-032] a signal exit cannot be produced on %s", runtime.GOOS)
			}
			realOut, realErr := tc.realOut()
			prod := poweroffExitFrom(realOut, realErr)
			seam := poweroffExitFrom(tc.seamOut, tc.seamErr)

			assert.Equal(t, tc.wantKind, prod.Kind, "[AC-032] production kind for a real %s", tc.name)
			assert.Equal(t, prod.Kind, seam.Kind, "[AC-032] the seam and the production path must classify %s alike", tc.name)
			assert.Equal(t, classifyPoweroff(prod) == nil, classifyPoweroff(seam) == nil,
				"[AC-032] both paths must give the same initiated/failed outcome for %s", tc.name)
		})
	}
}
