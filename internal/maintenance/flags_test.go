package maintenance

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// fakeFlagOps scripts the directory handle so the reconcile logic is tested on
// every platform; flags_linux_test.go runs the real descriptor code.
type fakeFlagOps struct {
	openErr   error
	uid       uint32
	unlinkErr map[string]error
	unlinked  []string
	closed    bool
	opened    []string
}

func (f *fakeFlagOps) OpenDir(path string) (flagDir, error) {
	f.opened = append(f.opened, path)
	if f.openErr != nil {
		return nil, f.openErr
	}
	return fakeFlagDir{f}, nil
}

type fakeFlagDir struct{ f *fakeFlagOps }

func (d fakeFlagDir) OwnerUID() uint32 { return d.f.uid }
func (d fakeFlagDir) Unlink(name string) error {
	d.f.unlinked = append(d.f.unlinked, name)
	return d.f.unlinkErr[name]
}
func (d fakeFlagDir) Close() error { d.f.closed = true; return nil }

func TestReconcileFlagsLogic_DP_AC007_OnlyTheTwoFlagsAreUnlinked(t *testing.T) {
	ops := &fakeFlagOps{}
	if note := reconcileFlags(ops, "/run/dietpi", 0); note != "" {
		t.Fatalf("note = %q, want none", note)
	}
	t.Run("[AC-007] exactly .update_available and .live_patches, in that order", func(t *testing.T) {
		if want := []string{".update_available", ".live_patches"}; !reflect.DeepEqual(ops.unlinked, want) {
			t.Errorf("unlinked %q, want %q", ops.unlinked, want)
		}
	})
	t.Run("[AC-007] .apt_updates and .dietpi_motd are never touched", func(t *testing.T) {
		for _, n := range ops.unlinked {
			if n == ".apt_updates" || n == ".dietpi_motd" {
				t.Errorf("unlinked %s", n)
			}
		}
	})
	t.Run("[AC-007] the directory handle is closed", func(t *testing.T) {
		if !ops.closed {
			t.Error("directory descriptor leaked")
		}
	})
	t.Run("[AC-007] a missing file is not an error", func(t *testing.T) {
		ops := &fakeFlagOps{unlinkErr: map[string]error{
			".update_available": &fs.PathError{Op: "unlinkat", Path: ".update_available", Err: syscall.ENOENT},
			".live_patches":     &fs.PathError{Op: "unlinkat", Path: ".live_patches", Err: syscall.ENOENT},
		}}
		if note := reconcileFlags(ops, "/run/dietpi", 0); note != "" {
			t.Errorf("note = %q, want none", note)
		}
	})
	t.Run("[AC-007] a missing directory is not an error", func(t *testing.T) {
		ops := &fakeFlagOps{openErr: &fs.PathError{Op: "open", Path: "/run/dietpi", Err: syscall.ENOENT}}
		if note := reconcileFlags(ops, "/run/dietpi", 0); note != "" || len(ops.unlinked) != 0 {
			t.Errorf("note %q unlinked %q", note, ops.unlinked)
		}
	})
	t.Run("[AC-007] any other unlink error is one bounded note and the other flag is still tried", func(t *testing.T) {
		ops := &fakeFlagOps{unlinkErr: map[string]error{
			".update_available": errors.New(strings.Repeat("x", 600)),
		}}
		note := reconcileFlags(ops, "/run/dietpi", 0)
		if !strings.HasPrefix(note, "flag cleanup failed: .update_available: ") || len(note) > maxFlagNote {
			t.Errorf("note = %q (%d bytes)", note, len(note))
		}
		if len(ops.unlinked) != 2 {
			t.Errorf("unlinked %q, want both attempted", ops.unlinked)
		}
	})
	t.Run("[AC-007] both failing yields one note naming both", func(t *testing.T) {
		ops := &fakeFlagOps{unlinkErr: map[string]error{".update_available": errors.New("EIO"), ".live_patches": errors.New("EROFS")}}
		note := reconcileFlags(ops, "/run/dietpi", 0)
		if !strings.Contains(note, ".update_available: EIO") || !strings.Contains(note, ".live_patches: EROFS") {
			t.Errorf("note = %q", note)
		}
	})
}

func TestReconcileFlagsLogic_DP_AC008_UnsafeDirUntouched(t *testing.T) {
	t.Run("[AC-008] a directory not owned by the trusted uid is left alone with a note", func(t *testing.T) {
		ops := &fakeFlagOps{uid: 1000}
		note := reconcileFlags(ops, "/run/dietpi", 0)
		if note != "/run/dietpi not root-owned; flags left" {
			t.Errorf("note = %q", note)
		}
		if len(ops.unlinked) != 0 {
			t.Errorf("unlinked %q in a directory root does not own", ops.unlinked)
		}
		if !ops.closed {
			t.Error("directory descriptor leaked")
		}
	})
	t.Run("[AC-008] a directory that cannot be opened safely (a symlink, a file) is left alone with a note", func(t *testing.T) {
		for name, err := range map[string]error{
			"symlink": &fs.PathError{Op: "open", Path: "/run/dietpi", Err: syscall.ELOOP},
			"file":    &fs.PathError{Op: "open", Path: "/run/dietpi", Err: errors.New("not a directory")},
			"denied":  &fs.PathError{Op: "open", Path: "/run/dietpi", Err: syscall.EACCES},
		} {
			ops := &fakeFlagOps{openErr: err}
			note := reconcileFlags(ops, "/run/dietpi", 0)
			if !strings.HasPrefix(note, "/run/dietpi not opened safely; flags left") || len(ops.unlinked) != 0 {
				t.Errorf("%s: note %q unlinked %q", name, note, ops.unlinked)
			}
		}
	})
	t.Run("[AC-008] the production directory is /run/dietpi", func(t *testing.T) {
		if DietPiRunDir != "/run/dietpi" {
			t.Errorf("DietPiRunDir = %q", DietPiRunDir)
		}
	})
}
