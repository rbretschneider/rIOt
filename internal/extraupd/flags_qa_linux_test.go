//go:build linux

package extraupd

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Added by QA Engineer. Covers AC-009 and DP-SEC-002.
//
// A local user can plant a named pipe where DietPi's flag belongs. The no-follow
// open uses O_NONBLOCK, so it must neither block nor believe the pipe.
func TestReadTrustedFlag_DP_QA_AC009_FIFOFlagIsAbsentAndNeverBlocks(t *testing.T) {
	h := dietpiHost(t, "9", "20", "1").mkdir("run/dietpi")
	for _, name := range []string{".update_available", ".live_patches"} {
		if err := syscall.Mkfifo(filepath.Join(h.root, "run", "dietpi", name), 0o644); err != nil {
			t.Skipf("fifo unavailable here: %v", err)
		}
	}
	done := make(chan struct{})
	var readErr error
	var got entryStatus
	go func() {
		defer close(done)
		_, readErr = h.fs().ReadTrustedFlag("run/dietpi/.update_available", 64)
		got, _ = detectOne(t, h)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("reading a named pipe flag blocked")
	}
	if !errors.Is(readErr, ErrUntrustedFlag) {
		t.Errorf("ReadTrustedFlag on a FIFO = %v, want ErrUntrustedFlag", readErr)
	}
	if got.Status != "unknown" || got.Reason != "not_checked_since_boot" || got.Pending != 0 {
		t.Errorf("detect = %+v, want both pipe flags treated as absent", got)
	}
	if _, err := os.Lstat(filepath.Join(h.root, "run", "dietpi", ".update_available")); err != nil {
		t.Errorf("the pipe must be left alone: %v", err)
	}
}
