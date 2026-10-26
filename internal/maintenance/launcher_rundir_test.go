package maintenance

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// The /run/dietpi isolation of the launcher (DIETPI-PATH-001 AD-2, FR-006,
// FR-007, DP-SEC-003). Everything runs against the scripted fakeNS: no mount,
// no namespace and no process is touched.

func effectsOf(ns *fakeNS) []string {
	var effects []string
	for _, c := range ns.calls {
		switch {
		case strings.HasPrefix(c, "unshare"), strings.HasPrefix(c, "mount"), strings.HasPrefix(c, "chdir"), strings.HasPrefix(c, "cloexec"), strings.HasPrefix(c, "status"), strings.HasPrefix(c, "exec"):
			effects = append(effects, c)
		}
	}
	return effects
}

func TestLauncher_DP_AC004_PrivateRunDir(t *testing.T) {
	ns := newFakeNS().withRunDir()
	if err := RunExtra(ns, launchArgs); err != nil {
		t.Fatalf("launcher failed: %v", err)
	}
	t.Run("[AC-004] /run is made private, a 0755 tmpfs is mounted on /run/dietpi, and only then the step execs", func(t *testing.T) {
		want := []string{
			fmt.Sprintf("unshare %#x", cloneNewNS),
			fmt.Sprintf("mount %q %q %q %#x %q", "", "/tmp", "", msRec|msPrivate, ""),
			fmt.Sprintf("mount %q %q %q %#x %q", "/tmp", "/tmp", "", msRemount|msBind|msRdonly|msNosuid|msNodev, ""),
			fmt.Sprintf("mount %q %q %q %#x %q", "tmpfs", "/tmp", "tmpfs", msNosuid|msNodev, "mode=1777,size=50%"),
			fmt.Sprintf("mount %q %q %q %#x %q", "", "/run", "", msRec|msPrivate, ""),
			fmt.Sprintf("mount %q %q %q %#x %q", "tmpfs", "/run/dietpi", "tmpfs", msNosuid|msNodev|msNoexec, "mode=0755,size=1m,nr_inodes=64"),
			"chdir /",
			"cloexec",
			fmt.Sprintf("status %q", "ok\n"),
			"exec /boot/dietpi/dietpi-update",
		}
		if got := effectsOf(ns); !reflect.DeepEqual(got, want) {
			t.Errorf("effects:\n got %q\nwant %q", got, want)
		}
	})
	t.Run("[AC-004] the proof checks tmpfs, root, 0755, empty, not the host inode and not shared", func(t *testing.T) {
		for _, want := range []string{"statfs /run/dietpi", "stat /run/dietpi", "count /run/dietpi", "stat /proc/1/root/run/dietpi"} {
			if !ns.did(want) {
				t.Errorf("proof step %q missing; calls %q", want, ns.calls)
			}
		}
	})
	t.Run("[AC-004] the proof runs before the ok line and the exec", func(t *testing.T) {
		mountAt, proofAt, okAt := -1, -1, -1
		for i, c := range ns.calls {
			switch {
			case strings.Contains(c, `"tmpfs" "/run/dietpi"`):
				mountAt = i
			case c == "count /run/dietpi":
				proofAt = i
			case c == fmt.Sprintf("status %q", "ok\n"):
				okAt = i
			}
		}
		if !(mountAt >= 0 && mountAt < proofAt && proofAt < okAt) {
			t.Errorf("mount at %d, proof at %d, ok at %d", mountAt, proofAt, okAt)
		}
	})
	t.Run("[DP-SEC-003] propagation is verified again after the /run mounts, then the step execs", func(t *testing.T) {
		lastMount, reverify := -1, -1
		for i, c := range ns.calls {
			if strings.HasPrefix(c, "mount") {
				lastMount = i
			}
		}
		reads := 0
		for i, c := range ns.calls {
			if c == "read "+initMountinfo && i > lastMount {
				reverify = i
			}
			if c == "read "+selfMountinfo && i > lastMount {
				reads++
			}
		}
		if reverify < 0 || reads == 0 {
			t.Errorf("no propagation re-check after the last mount (init read at %d, self reads %d); calls %q", reverify, reads, ns.calls)
		}
	})
	t.Run("[AC-004] no mount touches / or /boot, and nothing is mounted outside /tmp, /run and /run/dietpi", func(t *testing.T) {
		for _, c := range ns.calls {
			if !strings.HasPrefix(c, "mount") {
				continue
			}
			if !strings.Contains(c, `"/tmp"`) && !strings.Contains(c, `"/run"`) && !strings.Contains(c, `"/run/dietpi"`) {
				t.Errorf("a mount outside the allowed targets: %s", c)
			}
		}
	})
}

func TestLauncher_DP_AC005_RunDirFailsClosed(t *testing.T) {
	cases := map[string]func(*fakeNS){
		"/run is not its own mount": func(n *fakeNS) {
			n.files[selfMountinfo] = []byte(hostInit)
			n.afterTmpMount = selfAfter
		},
		"making /run private fails": func(n *fakeNS) { n.mountErr["/run|"] = errors.New("EPERM") },
		"mounting the tmpfs fails":  func(n *fakeNS) { n.mountErr["/run/dietpi|tmpfs"] = errors.New("ENOSPC") },
		"the proof: not a tmpfs":    func(n *fakeNS) { n.runDirTmpfsType = 0xEF53 },
		"the proof: not root":       func(n *fakeNS) { n.runDirAfter = FileID{Dev: 51, Ino: 1, UID: 1000, Mode: 0o040755} },
		"the proof: mode 0777":      func(n *fakeNS) { n.runDirAfter = FileID{Dev: 51, Ino: 1, UID: 0, Mode: 0o040777} },
		"the proof: mode 1755":      func(n *fakeNS) { n.runDirAfter = FileID{Dev: 51, Ino: 1, UID: 0, Mode: 0o041755} },
		"the proof: not a dir":      func(n *fakeNS) { n.runDirAfter = FileID{Dev: 51, Ino: 1, UID: 0, Mode: 0o100755} },
		"the proof: not empty":      func(n *fakeNS) { n.runDirEntries = 1 },
		"the proof: still the host's inode": func(n *fakeNS) {
			n.runDirAfter = FileID{Dev: 22, Ino: 40, UID: 0, Mode: 0o040755}
		},
		"the proof: host directory unidentified": func(n *fakeNS) { n.statErr["/proc/1/root/run/dietpi"] = errors.New("denied") },
		"the proof: still shared": func(n *fakeNS) {
			n.afterRunMount = strings.Replace(selfAfterRunDir, "61 25 0:51 / /run/dietpi rw,nosuid,nodev,noexec,relatime -", "61 25 0:51 / /run/dietpi rw,nosuid,nodev,noexec,relatime shared:77 -", 1)
		},
		"the proof: statfs fails":   func(n *fakeNS) { n.statErr["statfs-run"] = errors.New("boom") },
		"the proof: stat fails":     func(n *fakeNS) { n.statErr["/run/dietpi"] = errors.New("boom") },
		"the proof: mountinfo gone": func(n *fakeNS) { n.afterRunMount = "garbage\n" },
	}
	for name, mutate := range cases {
		t.Run("[AC-005] "+name+" is rundir_not_private and the step never execs", func(t *testing.T) {
			ns := newFakeNS().withRunDir()
			mutate(ns)
			err := RunExtra(ns, launchArgs)
			if err == nil || err.Reason != ReasonRunDirNotPrivate {
				t.Fatalf("err = %v, want rundir_not_private", err)
			}
			if ns.did("exec") || ns.did(fmt.Sprintf("status %q", "ok\n")) {
				t.Errorf("a failed run-dir setup must not report ok or exec: %q", ns.calls)
			}
			if !strings.HasPrefix(ns.lastStatus(), "err rundir_not_private ") {
				t.Errorf("status = %q, want an err rundir_not_private line", ns.lastStatus())
			}
		})
	}
	t.Run("[AC-005] the reason is the documented skip reason", func(t *testing.T) {
		if ReasonRunDirNotPrivate != "rundir_not_private" || SkipRunDirNotPrivate != "rundir_not_private" {
			t.Errorf("reasons %q %q", ReasonRunDirNotPrivate, SkipRunDirNotPrivate)
		}
	})
	t.Run("[AC-005] a failure before any run-dir mount performs none", func(t *testing.T) {
		ns := newFakeNS().withRunDir()
		ns.files[selfMountinfo] = []byte(hostInit)
		ns.afterTmpMount = selfAfter
		RunExtra(ns, launchArgs)
		if ns.did(`mount "" "/run"`) || ns.did(`mount "tmpfs" "/run/dietpi"`) {
			t.Errorf("a run-dir mount happened although /run is not a mount: %q", ns.calls)
		}
	})
}

func TestLauncher_DP_SEC003_ProbeAndPropagationFailClosed(t *testing.T) {
	t.Run("[DP-SEC-003] a probe error other than not-exist fails with rundir_not_private", func(t *testing.T) {
		for name, probeErr := range map[string]error{
			"EACCES": &fs.PathError{Op: "lstat", Path: "/run/dietpi", Err: syscall.EACCES},
			"EIO":    &fs.PathError{Op: "lstat", Path: "/run/dietpi", Err: syscall.EIO},
			"opaque": errors.New("boom"),
		} {
			ns := newFakeNS().withRunDir()
			ns.runDirStatErr = probeErr
			err := RunExtra(ns, launchArgs)
			if err == nil || err.Reason != ReasonRunDirNotPrivate || ns.did("exec") || ns.did(`mount "" "/run"`) {
				t.Errorf("%s: err = %v, calls %q", name, err, ns.calls)
			}
		}
	})
	t.Run("[DP-SEC-003] the probe does not follow a symlink", func(t *testing.T) {
		ns := newFakeNS().withRunDir()
		if err := RunExtra(ns, launchArgs); err != nil {
			t.Fatal(err)
		}
		if !ns.did("lstat /run/dietpi") {
			t.Errorf("the existence probe must be a no-follow stat; calls %q", ns.calls)
		}
		mountAt := -1
		for i, c := range ns.calls {
			if strings.Contains(c, `"tmpfs" "/run/dietpi"`) {
				mountAt = i
			}
		}
		for i, c := range ns.calls {
			if c == "stat /run/dietpi" && i < mountAt {
				t.Errorf("a following stat of /run/dietpi before the mount: %s", c)
			}
		}
	})
	t.Run("[DP-SEC-003] a /run/dietpi that is a symlink, a file or not root-owned is refused before any mount", func(t *testing.T) {
		for name, id := range map[string]FileID{
			"symlink":       {Dev: 22, Ino: 40, UID: 0, Mode: 0o120777},
			"regular file":  {Dev: 22, Ino: 40, UID: 0, Mode: 0o100644},
			"owned by 1000": {Dev: 22, Ino: 40, UID: 1000, Mode: 0o040777},
		} {
			ns := newFakeNS().withRunDir()
			ns.runDirHost = &id
			err := RunExtra(ns, launchArgs)
			if err == nil || err.Reason != ReasonRunDirNotPrivate || ns.did(`mount "" "/run"`) || ns.did("exec") {
				t.Errorf("%s: err = %v, calls %q", name, err, ns.calls)
			}
		}
	})
	t.Run("[DP-SEC-003] a propagation regression after the /run mounts is mount_propagation_unverified", func(t *testing.T) {
		ns := newFakeNS().withRunDir()
		ns.afterRunMount = strings.Replace(selfAfterRunDir, "23 22 179:1 / /boot rw,noatime shared:7", "23 22 179:1 / /boot rw,noatime", 1)
		err := RunExtra(ns, launchArgs)
		if err == nil || err.Reason != ReasonMountPropagation {
			t.Fatalf("err = %v, want mount_propagation_unverified", err)
		}
		if ns.did("exec") {
			t.Error("exec after a failed propagation re-check")
		}
	})
}

func TestLauncher_DP_AC006_NoRunDirNoMount(t *testing.T) {
	ns := newFakeNS()
	if err := RunExtra(ns, launchArgs); err != nil {
		t.Fatalf("launcher failed: %v", err)
	}
	t.Run("[AC-006] with no /run/dietpi nothing is created, mounted or proved there, and the step execs", func(t *testing.T) {
		for _, c := range ns.calls {
			if strings.Contains(c, "/run") && !strings.HasPrefix(c, "lstat /run/dietpi") {
				t.Errorf("the launcher touched /run although /run/dietpi does not exist: %s", c)
			}
		}
		if !ns.did("lstat /run/dietpi") {
			t.Error("the probe did not run")
		}
		if !ns.did("exec /boot/dietpi/dietpi-update") {
			t.Errorf("the step did not exec: %q", ns.calls)
		}
	})
}

func TestLauncher_DP_AC005_FlagsAndConstants(t *testing.T) {
	t.Run("[AC-004] the mount flags and sizes are the documented ones", func(t *testing.T) {
		if runDirTmpfsData != "mode=0755,size=1m,nr_inodes=64" || runDirMode != 0o755 {
			t.Errorf("data %q mode %#o", runDirTmpfsData, runDirMode)
		}
		if msNoexec != 8 {
			t.Errorf("MS_NOEXEC = %d", msNoexec)
		}
	})
	t.Run("[AC-004] FileID mode helpers", func(t *testing.T) {
		d := FileID{Mode: 0o041755}
		if !d.IsDir() || d.Perm() != 0o1755 {
			t.Errorf("dir %v perm %#o", d.IsDir(), d.Perm())
		}
		if (FileID{Mode: 0o120777}).IsDir() || (FileID{Mode: 0o100644}).IsDir() {
			t.Error("a symlink or file is not a directory")
		}
	})
}
