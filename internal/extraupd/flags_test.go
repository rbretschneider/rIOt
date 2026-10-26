package extraupd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func requireLinuxOwnership(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("owner, mode and link-count proofs are linux only: DietPi is linux only and Windows mode bits are synthetic")
	}
}

func chmodFlag(t *testing.T, h *hostFixture, rel string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(filepath.Join(h.root, filepath.FromSlash(rel)), mode); err != nil {
		t.Fatal(err)
	}
}

func TestDetect_DP_AC009_UntrustedFlagIsAbsent(t *testing.T) {
	const flag = "run/dietpi/.update_available"
	const live = "run/dietpi/.live_patches"
	absent := func(t *testing.T, fsys FS) {
		t.Helper()
		got, present := detectWith(t, NewDietPi(), fsys)
		if !present {
			t.Fatal("DietPi must still be present")
		}
		if got.Status != "unknown" || got.Reason != "not_checked_since_boot" || got.Pending != 0 || got.Available != "" {
			t.Errorf("got %+v, want the flag treated as absent (unknown/not_checked_since_boot, pending 0)", got)
		}
	}

	t.Run("[AC-009] a flag written by the trusted owner still yields update_available with its version", func(t *testing.T) {
		h := dietpiHost(t, "9", "20", "1").write(flag, "10.8.2\n")
		got, _ := detectOne(t, h)
		if got.Status != "update_available" || got.Available != "10.8.2" || got.Pending != 1 {
			t.Errorf("got %+v, want update_available 10.8.2 pending 1", got)
		}
	})
	t.Run("[AC-009] a live_patches file from the trusted owner still counts", func(t *testing.T) {
		got, _ := detectOne(t, dietpiHost(t, "10", "8", "2").write(live, ""))
		if got.Status != "update_available" || got.Pending != 1 {
			t.Errorf("got %+v, want update_available pending 1", got)
		}
	})
	t.Run("[AC-009] a flag owned by an untrusted uid is absent", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "9", "20", "1").write(flag, "10.8.2\n").write(live, "")
		absent(t, DirFSOwnedBy(h.root, uint32(os.Getuid())+1))
	})
	t.Run("[AC-009] a flag with mode 0666 is absent", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "9", "20", "1").write(flag, "10.8.2\n")
		chmodFlag(t, h, flag, 0o666)
		absent(t, h.fs())
	})
	t.Run("[AC-009] a group-writable flag (0664) is absent", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "9", "20", "1").write(flag, "10.8.2\n")
		chmodFlag(t, h, flag, 0o664)
		absent(t, h.fs())
	})
	t.Run("[AC-009] a world-writable live_patches file is absent", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "10", "8", "2").write(live, "")
		chmodFlag(t, h, live, 0o666)
		absent(t, h.fs())
	})
	t.Run("[DP-SEC-002] a hard-linked flag (link count 2) is absent", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "9", "20", "1").write(flag, "10.8.2\n")
		if err := os.Link(filepath.Join(h.root, "run", "dietpi", ".update_available"), filepath.Join(h.root, "elsewhere")); err != nil {
			t.Skipf("hard links unavailable here: %v", err)
		}
		absent(t, h.fs())
	})
	t.Run("[DP-SEC-002] a hard-linked live_patches file is absent", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "10", "8", "2").write(live, "")
		if err := os.Link(filepath.Join(h.root, "run", "dietpi", ".live_patches"), filepath.Join(h.root, "elsewhere")); err != nil {
			t.Skipf("hard links unavailable here: %v", err)
		}
		absent(t, h.fs())
	})
	t.Run("[AC-009] a symlink flag is absent, never followed", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "9", "20", "1").write("target", "10.8.2\n").mkdir("run/dietpi")
		if err := os.Symlink(filepath.Join(h.root, "target"), filepath.Join(h.root, "run", "dietpi", ".update_available")); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
		absent(t, h.fs())
	})
	t.Run("[AC-009] a directory in place of a flag is absent, not detection_failed", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").mkdir(flag).mkdir(live)
		absent(t, h.fs())
	})
	t.Run("[AC-009] an untrusted flag never reads as an update even with checks disabled", func(t *testing.T) {
		requireLinuxOwnership(t)
		h := dietpiHost(t, "10", "8", "2").write(flag, "99.0.0\n").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=0\n")
		chmodFlag(t, h, flag, 0o666)
		got, _ := detectWith(t, NewDietPi(), h.fs())
		if got.Status != "unknown" || got.Reason != "check_disabled" {
			t.Errorf("got %+v, want unknown/check_disabled", got)
		}
	})
}

func TestOSRoot_DP_AC009_TrustsOnlyUIDZero(t *testing.T) {
	for name, fsys := range map[string]FS{"OSRoot": OSRoot(), "DirFS": DirFS(t.TempDir())} {
		d, ok := fsys.(dirFS)
		if !ok {
			t.Fatalf("%s is %T, want dirFS", name, fsys)
		}
		if d.owner != 0 {
			t.Errorf("%s trusts uid %d, want only uid 0", name, d.owner)
		}
	}
}

func TestReadTrustedFlag_DP_SEC002_ChecksTheDescriptor(t *testing.T) {
	requireLinuxOwnership(t)
	h := newHost(t).write("run/dietpi/.update_available", "10.8.2\n")
	fsys := h.fs()
	flagPath := filepath.Join(h.root, "run", "dietpi", ".update_available")

	t.Run("[DP-SEC-002] a probe that looked fine does not vouch for a file swapped in before the read", func(t *testing.T) {
		info, err := fsys.Lstat("run/dietpi/.update_available")
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
			t.Fatalf("the probe must see a benign flag: %v %v", info, err)
		}
		forged := filepath.Join(h.root, "forged")
		if err := os.WriteFile(forged, []byte("99.0.0\n"), 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(forged, 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(forged, flagPath); err != nil {
			t.Fatal(err)
		}
		if data, err := fsys.ReadTrustedFlag("run/dietpi/.update_available", 64); !errors.Is(err, ErrUntrustedFlag) {
			t.Errorf("read %q, %v after the swap, want ErrUntrustedFlag", data, err)
		}
	})
	t.Run("[DP-SEC-002] a flag swapped for a symlink is refused by the no-follow open", func(t *testing.T) {
		if err := os.Remove(flagPath); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(h.root, "target")
		if err := os.WriteFile(target, []byte("99.0.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, flagPath); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if data, err := fsys.ReadTrustedFlag("run/dietpi/.update_available", 64); !errors.Is(err, ErrUntrustedFlag) {
			t.Errorf("read %q, %v through a symlink, want ErrUntrustedFlag", data, err)
		}
	})
	t.Run("[DP-SEC-002] a missing flag is ErrNotExist and an oversized trusted one ErrTooLarge", func(t *testing.T) {
		if _, err := fsys.ReadTrustedFlag("run/dietpi/.nothing", 64); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("err = %v, want not exist", err)
		}
		big := newHost(t).write("run/dietpi/.update_available", strings.Repeat("x", 65))
		if _, err := big.fs().ReadTrustedFlag("run/dietpi/.update_available", 64); !errors.Is(err, ErrTooLarge) {
			t.Errorf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("[DP-SEC-002] names that escape the root are refused", func(t *testing.T) {
		for _, name := range []string{"", "/etc/passwd", "../x", "a\x00b"} {
			if _, err := fsys.ReadTrustedFlag(name, 10); err == nil {
				t.Errorf("ReadTrustedFlag(%q) must fail", name)
			}
		}
	})
}

// DP-SEC-006: DirFSOwnedBy can make a non-root owner trusted, so no production
// code may reference it. The only non-test file allowed to mention it is the
// file that defines it.
func TestSeam_DP_SEC006_NoProductionReferenceToDirFSOwnedBy(t *testing.T) {
	var offenders []string
	for _, top := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(top, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.Base(path) == "testseam.go" && filepath.Base(filepath.Dir(path)) == "extraupd" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), "DirFSOwnedBy") {
				offenders = append(offenders, filepath.ToSlash(path))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", top, err)
		}
	}
	if len(offenders) != 0 {
		t.Errorf("production code references the test-only DirFSOwnedBy seam: %v", offenders)
	}
}

func TestSeam_DP_SEC006_TheGuardSeesTheDefiningFile(t *testing.T) {
	data, err := os.ReadFile("testseam.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "func DirFSOwnedBy(") {
		t.Error("testseam.go must define DirFSOwnedBy: the guard's exemption is by that file name")
	}
	if !strings.Contains(string(data), "test fixtures only") {
		t.Error("the seam's doc comment must say it is for test fixtures only")
	}
}
