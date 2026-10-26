//go:build linux

package maintenance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the real descriptor code against a temp directory. The test
// user owns the directory, so it is passed as the trusted uid; the production
// wrapper trusts only uid 0.

func writeFlagFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestReconcileFlags_DP_AC007_RemovesOnlyTwoNeverFollows(t *testing.T) {
	uid := uint32(os.Getuid())
	dir := t.TempDir()
	outside := t.TempDir()
	victim := writeFlagFile(t, outside, "victim", "do not touch")
	update := writeFlagFile(t, dir, ".update_available", "10.8.2\n")
	live := writeFlagFile(t, dir, ".live_patches", "")
	apt := writeFlagFile(t, dir, ".apt_updates", "5\n")
	motd := writeFlagFile(t, dir, ".dietpi_motd", "motd")
	decoy := filepath.Join(dir, ".live_patches2")
	if err := os.Symlink("/etc/passwd", decoy); err != nil {
		t.Fatal(err)
	}

	if note := reconcileFlags(osFlagOps{}, dir, uid); note != "" {
		t.Fatalf("note = %q, want none", note)
	}
	t.Run("[AC-007] .update_available and .live_patches are gone", func(t *testing.T) {
		if exists(update) || exists(live) {
			t.Errorf("update exists %v, live exists %v", exists(update), exists(live))
		}
	})
	t.Run("[AC-007] .apt_updates, .dietpi_motd and an unrelated symlink stay", func(t *testing.T) {
		for _, p := range []string{apt, motd, decoy} {
			if !exists(p) {
				t.Errorf("%s was removed", p)
			}
		}
	})
	t.Run("[AC-007] a missing flag is not an error", func(t *testing.T) {
		if note := reconcileFlags(osFlagOps{}, dir, uid); note != "" {
			t.Errorf("second run note = %q", note)
		}
	})
	t.Run("[AC-007] a flag that is a symlink is removed itself and its target is never touched", func(t *testing.T) {
		link := filepath.Join(dir, ".update_available")
		if err := os.Symlink(victim, link); err != nil {
			t.Fatal(err)
		}
		livelink := filepath.Join(dir, ".live_patches")
		if err := os.Symlink(outside, livelink); err != nil {
			t.Fatal(err)
		}
		if note := reconcileFlags(osFlagOps{}, dir, uid); note != "" {
			t.Fatalf("note = %q", note)
		}
		if exists(link) || exists(livelink) {
			t.Error("the symlink flags must be unlinked")
		}
		if b, err := os.ReadFile(victim); err != nil || string(b) != "do not touch" {
			t.Errorf("symlink target damaged: %q %v", b, err)
		}
		if !exists(outside) {
			t.Error("a directory behind a symlink flag must not be removed")
		}
	})
	t.Run("[AC-007] a non-empty directory named like a flag is a note, not a removal, and the other flag is still removed", func(t *testing.T) {
		d2 := t.TempDir()
		blocker := filepath.Join(d2, ".update_available")
		if err := os.Mkdir(blocker, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFlagFile(t, blocker, "inner", "x")
		other := writeFlagFile(t, d2, ".live_patches", "")
		note := reconcileFlags(osFlagOps{}, d2, uid)
		if !strings.HasPrefix(note, "flag cleanup failed: .update_available: ") {
			t.Errorf("note = %q", note)
		}
		if exists(other) {
			t.Error(".live_patches must still be removed")
		}
		if !exists(filepath.Join(blocker, "inner")) {
			t.Error("a directory's contents were removed")
		}
	})
}

func TestReconcileFlags_DP_AC008_UnsafeDirUntouched(t *testing.T) {
	uid := uint32(os.Getuid())
	t.Run("[AC-008] /run/dietpi that is a symlink to a directory is not touched and a note is recorded", func(t *testing.T) {
		real := t.TempDir()
		flag := writeFlagFile(t, real, ".update_available", "10.8.2\n")
		link := filepath.Join(t.TempDir(), "dietpi")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		note := reconcileFlags(osFlagOps{}, link, uid)
		if !strings.HasPrefix(note, link+" not opened safely; flags left") {
			t.Errorf("note = %q", note)
		}
		if !exists(flag) {
			t.Error("a flag behind a symlinked directory was removed")
		}
	})
	t.Run("[AC-008] a directory owned by another uid is not touched and a note is recorded", func(t *testing.T) {
		dir := t.TempDir()
		flag := writeFlagFile(t, dir, ".update_available", "10.8.2\n")
		note := reconcileFlags(osFlagOps{}, dir, uid+1)
		if note != dir+" not root-owned; flags left" {
			t.Errorf("note = %q", note)
		}
		if !exists(flag) {
			t.Error("a flag in a foreign-owned directory was removed")
		}
	})
	t.Run("[AC-008] a regular file in place of the directory is a note", func(t *testing.T) {
		f := writeFlagFile(t, t.TempDir(), "dietpi", "")
		if note := reconcileFlags(osFlagOps{}, f, uid); !strings.Contains(note, "not opened safely") {
			t.Errorf("note = %q", note)
		}
	})
	t.Run("[AC-008] an absent directory is silent", func(t *testing.T) {
		if note := reconcileFlags(osFlagOps{}, filepath.Join(t.TempDir(), "missing"), uid); note != "" {
			t.Errorf("note = %q", note)
		}
	})
	t.Run("[AC-008] the production wrapper trusts only uid 0", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("running as root: the temp directory is root-owned")
		}
		dir := t.TempDir()
		flag := writeFlagFile(t, dir, ".update_available", "10.8.2\n")
		if note := ReconcileDietPiFlags(dir); note != dir+" not root-owned; flags left" {
			t.Errorf("note = %q", note)
		}
		if !exists(flag) {
			t.Error("the production wrapper removed a flag from a non-root directory")
		}
	})
}
