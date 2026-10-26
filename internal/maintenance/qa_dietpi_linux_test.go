//go:build linux

package maintenance

import (
	"os"
	"path/filepath"
	"testing"
)

// Added by QA Engineer. The in-memory trustFS and the fake nsOps are what the
// DIETPI-PATH-001 tests drive; these run the production implementations on a
// real kernel, so a regression in what they report (link count, no-follow stat,
// mode) cannot hide behind the fakes.

// Covers AC-003 (a hard-linked dietpi-update_reinstalls fails the walk): the
// production trustFS must report the real link count.
func TestOSTrustFS_DP_QA_AC003_LstatReportsRealLinkCountAndNeverFollows(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "reinstalls")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := osTrustFS{}.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	if st.Nlink != 1 {
		t.Errorf("a fresh file reports %d links, want 1", st.Nlink)
	}
	if err = os.Link(file, filepath.Join(dir, "second")); err != nil {
		t.Skipf("hard links unavailable here: %v", err)
	}
	if st, err = (osTrustFS{}).Lstat(file); err != nil || st.Nlink != 2 {
		t.Errorf("a hard-linked file reports %d links (%v), want 2", st.Nlink, err)
	}
	link := filepath.Join(dir, "link")
	if err = os.Symlink(file, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if st, err = (osTrustFS{}).Lstat(link); err != nil || !st.isSymlink() {
		t.Errorf("Lstat followed a symlink: %+v %v", st, err)
	}
}

// Covers AC-005 / DP-SEC-003(b): the launcher's probe of /run/dietpi must not
// follow a symlink, and the proof must see the real permission bits.
func TestLinuxOps_DP_QA_SEC003_StatNoFollowAndModeAreReal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	ops := linuxOps{}
	viaLink, err := ops.StatNoFollow(link)
	if err != nil {
		t.Fatal(err)
	}
	if viaLink.IsDir() {
		t.Errorf("StatNoFollow followed the symlink: mode %#o", viaLink.Mode)
	}
	followed, err := ops.StatID(link)
	if err != nil || !followed.IsDir() || followed.Perm() != 0o755 {
		t.Errorf("StatID = %+v (%v), want a 0755 directory through the link", followed, err)
	}
	direct, err := ops.StatNoFollow(target)
	if err != nil || !direct.IsDir() || direct.Perm() != 0o755 || direct.UID != uint32(os.Getuid()) {
		t.Errorf("StatNoFollow(dir) = %+v (%v), want a 0755 directory of this user", direct, err)
	}
	if _, err = ops.StatNoFollow(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Errorf("a missing path must report not-exist, got %v", err)
	}
}
