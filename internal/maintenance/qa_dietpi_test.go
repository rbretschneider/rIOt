package maintenance

import (
	"errors"
	"strings"
	"testing"
)

// Added by QA Engineer. Covers AC-007 (a removal problem is a bounded note) and
// the "notes at most 200 bytes" bound of the story. The bound is a literal here:
// the existing test compares against the production constant, so it cannot
// notice the constant growing.
func TestReconcileFlagsLogic_DP_QA_AC007_NoteNeverExceeds200Bytes(t *testing.T) {
	t.Run("[AC-007] one very long unlink error gives a note of at most 200 bytes", func(t *testing.T) {
		ops := &fakeFlagOps{unlinkErr: map[string]error{".update_available": errors.New(strings.Repeat("x", 5000))}}
		if note := reconcileFlags(ops, "/run/dietpi", 0); note == "" || len(note) > 200 {
			t.Errorf("note is %d bytes, want 1..200", len(note))
		}
	})
	t.Run("[AC-007] two very long unlink errors give one note of at most 200 bytes", func(t *testing.T) {
		long := errors.New(strings.Repeat("y", 5000))
		ops := &fakeFlagOps{unlinkErr: map[string]error{".update_available": long, ".live_patches": long}}
		if note := reconcileFlags(ops, "/run/dietpi", 0); len(note) > 200 {
			t.Errorf("note is %d bytes, want at most 200", len(note))
		}
	})
	t.Run("[AC-008] a very long open error gives a note of at most 200 bytes", func(t *testing.T) {
		ops := &fakeFlagOps{openErr: errors.New(strings.Repeat("z", 5000))}
		if note := reconcileFlags(ops, "/run/dietpi", 0); note == "" || len(note) > 200 {
			t.Errorf("note is %d bytes, want 1..200", len(note))
		}
	})
}
