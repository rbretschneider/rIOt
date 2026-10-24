//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClosedArguments(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--request", "/etc/shadow"}, {"repair"}, {"--env=APT_CONFIG=/attacker"}, {";sh"}} {
		t.Run("[MAINT-RECOVERY AC-001] rejects caller-selected production arguments", func(t *testing.T) {
			if err := run(args); err == nil {
				t.Fatalf("accepted %q", args)
			}
		})
	}
}

func TestTouchDirNoFollow_PF_AC032_StampsARealDirectory(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := touchDirNoFollow(dir, now, uint32(os.Getuid())); err != nil {
		t.Fatalf("touch of a real owned directory failed: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if delta := info.ModTime().Sub(now); delta < -2*time.Second || delta > 2*time.Second {
		t.Errorf("mtime = %v, want within 2s of %v", info.ModTime(), now)
	}
}

func TestTouchDirNoFollow_PF_C5_RefusesSymlinkAndLeavesTargetAlone(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(target, old, old); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "lists")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := touchDirNoFollow(link, time.Now(), uint32(os.Getuid())); err == nil {
		t.Fatal("[C-5] a symlinked lists path must be refused")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("[C-5] symlink target mtime changed to %v, want untouched %v", info.ModTime(), old)
	}
}

func TestTouchDirNoFollow_PF_C5_RefusesNonDirectoryAndWrongOwner(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "plain")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := touchDirNoFollow(file, time.Now(), uint32(os.Getuid())); err == nil {
		t.Error("[C-5] a regular file must be refused")
	}
	if err := touchDirNoFollow(root, time.Now(), uint32(os.Getuid())+1); err == nil {
		t.Error("[C-5] a directory with an unexpected owner must be refused")
	}
	if err := touchDirNoFollow(filepath.Join(root, "missing"), time.Now(), uint32(os.Getuid())); err == nil {
		t.Error("[C-5] a missing path must be an error")
	}
}
