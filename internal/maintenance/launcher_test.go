package maintenance

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
)

// fakeNS scripts every effect of the launcher and records the call sequence. It
// never touches a kernel, a mount table or a process (SC-001).
type fakeNS struct {
	calls []string

	euid       int
	env        map[string]string
	fifoErr    error
	parentExe  string
	parentErr  error
	unshareErr error
	mountErr   map[string]error
	files      map[string][]byte
	readErr    map[string]error
	statfs     int64
	statID     map[string]FileID
	statErr    map[string]error
	entries    int
	execErr    error
	status     []string

	// afterTmpMount is the self mountinfo once the tmpfs is stacked.
	afterTmpMount string
	tmpMounted    bool

	// The /run/dietpi scenario (DIETPI-PATH-001). With runDirHost unset the
	// host has no /run/dietpi and the launcher must not touch /run at all.
	runDirHost      *FileID
	runDirStatErr   error
	runDirAfter     FileID
	afterRunMount   string
	runDirMounted   bool
	runDirTmpfsType int64
	runDirEntries   int
	execPath        string
	execArgv        []string
	execEnv         []string
}

const hostInit = `22 1 179:2 / / rw,noatime shared:1 - ext4 /dev/mmcblk0p2 rw
24 22 0:21 / /tmp rw,nosuid,nodev,noatime shared:3 - tmpfs tmpfs rw
23 22 179:1 / /boot rw,noatime shared:7 - vfat /dev/mmcblk0p1 rw
`

const selfBefore = hostInit

const selfAfter = hostInit + `60 24 0:50 / /tmp rw,nosuid,nodev,relatime - tmpfs tmpfs rw,size=1000k,mode=1777
`

func newFakeNS() *fakeNS {
	return &fakeNS{
		euid: 0, env: map[string]string{LauncherEnv: "1"}, parentExe: HelperPath,
		mountErr: map[string]error{}, readErr: map[string]error{}, statErr: map[string]error{},
		files: map[string][]byte{
			initMountinfo: []byte(hostInit),
		},
		statfs:        tmpfsMagic,
		statID:        map[string]FileID{"/tmp": {Dev: 50, Ino: 2, UID: 0}, initTmpPath: {Dev: 21, Ino: 2, UID: 0}},
		afterTmpMount: selfAfter,

		runDirTmpfsType: tmpfsMagic,
	}
}

// Host layouts with a separate /run mount, as on every systemd host.
const hostInitRun = hostInit + "25 22 0:22 / /run rw,nosuid,nodev,noexec,relatime shared:4 - tmpfs tmpfs rw,size=400000k,mode=755\n"

// selfAfterRunTmp is the table once /tmp is stacked, with /run still shared.
const selfAfterRunTmp = hostInitRun + "60 24 0:50 / /tmp rw,nosuid,nodev,relatime - tmpfs tmpfs rw,size=1000k,mode=1777\n"

// selfAfterRunDir is the table once /run is private and /run/dietpi is stacked.
const selfAfterRunDir = "22 1 179:2 / / rw,noatime shared:1 - ext4 /dev/mmcblk0p2 rw\n" +
	"24 22 0:21 / /tmp rw,nosuid,nodev,noatime shared:3 - tmpfs tmpfs rw\n" +
	"23 22 179:1 / /boot rw,noatime shared:7 - vfat /dev/mmcblk0p1 rw\n" +
	"25 22 0:22 / /run rw,nosuid,nodev,noexec,relatime - tmpfs tmpfs rw,size=400000k,mode=755\n" +
	"60 24 0:50 / /tmp rw,nosuid,nodev,relatime - tmpfs tmpfs rw,size=1000k,mode=1777\n" +
	"61 25 0:51 / /run/dietpi rw,nosuid,nodev,noexec,relatime - tmpfs tmpfs rw,size=1024k,nr_inodes=64,mode=755\n"

// withRunDir makes the host have a stock 0777 root-owned /run/dietpi on its own
// /run mount, and scripts the namespace the launcher ends up with.
func (f *fakeNS) withRunDir() *fakeNS {
	f.runDirHost = &FileID{Dev: 22, Ino: 40, UID: 0, Mode: 0o040777}
	f.files[selfMountinfo] = []byte(hostInitRun)
	f.afterTmpMount = selfAfterRunTmp
	f.afterRunMount = selfAfterRunDir
	f.runDirAfter = FileID{Dev: 51, Ino: 1, UID: 0, Mode: 0o040755}
	return f
}

func (f *fakeNS) rec(format string, a ...any) { f.calls = append(f.calls, fmt.Sprintf(format, a...)) }

func (f *fakeNS) Euid() int { f.rec("euid"); return f.euid }
func (f *fakeNS) Getenv(k string) string {
	f.rec("getenv %s", k)
	return f.env[k]
}
func (f *fakeNS) StatusPipeIsFIFO() error { f.rec("fifo"); return f.fifoErr }
func (f *fakeNS) ParentExe() (string, error) {
	f.rec("parent-exe")
	return f.parentExe, f.parentErr
}
func (f *fakeNS) LockThread() { f.rec("lock-thread") }
func (f *fakeNS) Unshare(flags int) error {
	f.rec("unshare %#x", flags)
	return f.unshareErr
}
func (f *fakeNS) Mount(source, target, fstype string, flags uintptr, data string) error {
	f.rec("mount %q %q %q %#x %q", source, target, fstype, flags, data)
	if err := f.mountErr[target+"|"+fstype]; err != nil {
		return err
	}
	if fstype == "tmpfs" && target == "/run/dietpi" {
		f.runDirMounted = true
	} else if fstype == "tmpfs" {
		f.tmpMounted = true
	}
	return nil
}
func (f *fakeNS) ReadFile(path string, max int) ([]byte, error) {
	f.rec("read %s", path)
	if err := f.readErr[path]; err != nil {
		return nil, err
	}
	if path == selfMountinfo {
		if f.runDirMounted && f.afterRunMount != "" {
			return []byte(f.afterRunMount), nil
		}
		if f.tmpMounted {
			return []byte(f.afterTmpMount), nil
		}
		if data, ok := f.files[selfMountinfo]; ok {
			return data, nil
		}
		return []byte(selfBefore), nil
	}
	return f.files[path], nil
}
func (f *fakeNS) StatfsType(path string) (int64, error) {
	f.rec("statfs %s", path)
	if path == "/run/dietpi" {
		return f.runDirTmpfsType, f.statErr["statfs-run"]
	}
	return f.statfs, f.statErr["statfs"]
}
func (f *fakeNS) StatID(path string) (FileID, error) {
	f.rec("stat %s", path)
	switch path {
	case "/run/dietpi":
		return f.runDirAfter, f.statErr[path]
	case "/proc/1/root/run/dietpi":
		if f.runDirHost == nil {
			return FileID{}, f.statErr[path]
		}
		return *f.runDirHost, f.statErr[path]
	}
	return f.statID[path], f.statErr[path]
}
func (f *fakeNS) StatNoFollow(path string) (FileID, error) {
	f.rec("lstat %s", path)
	if path != "/run/dietpi" {
		return f.statID[path], f.statErr[path]
	}
	if f.runDirStatErr != nil {
		return FileID{}, f.runDirStatErr
	}
	if f.runDirHost == nil {
		return FileID{}, &fs.PathError{Op: "lstat", Path: path, Err: fs.ErrNotExist}
	}
	return *f.runDirHost, nil
}
func (f *fakeNS) EntryCount(path string) (int, error) {
	f.rec("count %s", path)
	if path == "/run/dietpi" {
		return f.runDirEntries, nil
	}
	return f.entries, nil
}
func (f *fakeNS) Chdir(path string) error { f.rec("chdir %s", path); return nil }
func (f *fakeNS) WriteStatus(line string) error {
	f.rec("status %q", line)
	f.status = append(f.status, line)
	return nil
}
func (f *fakeNS) SetStatusCloexec() error { f.rec("cloexec"); return nil }
func (f *fakeNS) Exec(path string, argv, env []string) error {
	f.rec("exec %s", path)
	f.execPath, f.execArgv, f.execEnv = path, argv, env
	return f.execErr
}

func (f *fakeNS) did(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (f *fakeNS) lastStatus() string {
	if len(f.status) == 0 {
		return ""
	}
	return f.status[len(f.status)-1]
}

var launchArgs = []string{LauncherArg, "dietpi"}

func TestRunExtra_EU_SEC001_PrivateTmpSequence(t *testing.T) {
	ns := newFakeNS()
	if err := RunExtra(ns, launchArgs); err != nil {
		t.Fatalf("launcher failed: %v", err)
	}
	t.Run("[SEC-001] the effects happen in exactly the documented order", func(t *testing.T) {
		var effects []string
		for _, c := range ns.calls {
			switch {
			case strings.HasPrefix(c, "unshare"), strings.HasPrefix(c, "mount"), strings.HasPrefix(c, "chdir"), strings.HasPrefix(c, "cloexec"), strings.HasPrefix(c, "status"), strings.HasPrefix(c, "exec"):
				effects = append(effects, c)
			}
		}
		want := []string{
			fmt.Sprintf("unshare %#x", cloneNewNS),
			fmt.Sprintf("mount %q %q %q %#x %q", "", "/tmp", "", msRec|msPrivate, ""),
			fmt.Sprintf("mount %q %q %q %#x %q", "/tmp", "/tmp", "", msRemount|msBind|msRdonly|msNosuid|msNodev, ""),
			fmt.Sprintf("mount %q %q %q %#x %q", "tmpfs", "/tmp", "tmpfs", msNosuid|msNodev, "mode=1777,size=50%"),
			"chdir /",
			"cloexec",
			fmt.Sprintf("status %q", "ok\n"),
			"exec /boot/dietpi/dietpi-update",
		}
		if !reflect.DeepEqual(effects, want) {
			t.Errorf("effects:\n got %q\nwant %q", effects, want)
		}
	})
	t.Run("[SEC-001] no propagation change is made on / and no bind mount creates a new mount", func(t *testing.T) {
		for _, c := range ns.calls {
			if strings.HasPrefix(c, "mount") && strings.Contains(c, `"/" `) && !strings.Contains(c, `"/tmp"`) {
				t.Errorf("mount touching /: %s", c)
			}
		}
		for _, c := range ns.calls {
			if strings.HasPrefix(c, "mount") && !strings.Contains(c, `"/tmp"`) {
				t.Errorf("a mount outside /tmp: %s", c)
			}
		}
	})
	t.Run("[SEC-001] the unshare is the raw CLONE_NEWNS and the thread is locked before it", func(t *testing.T) {
		lock, unshare := -1, -1
		for i, c := range ns.calls {
			if c == "lock-thread" {
				lock = i
			}
			if strings.HasPrefix(c, "unshare") {
				unshare = i
			}
		}
		if lock < 0 || unshare < 0 || lock > unshare {
			t.Errorf("lock at %d, unshare at %d", lock, unshare)
		}
		if !ns.did("unshare 0x20000") {
			t.Error("unshare must pass exactly CLONE_NEWNS")
		}
	})
	t.Run("[AC-019] the exec is the registry spec: fixed path, args and environment", func(t *testing.T) {
		spec := extraupd.NewDietPi().Apply()
		wantArgv := append([]string{spec.Path}, spec.Args...)
		if ns.execPath != "/boot/dietpi/dietpi-update" || !reflect.DeepEqual(ns.execArgv, wantArgv) || !reflect.DeepEqual(ns.execEnv, spec.Env) {
			t.Errorf("exec %q %q %q", ns.execPath, ns.execArgv, ns.execEnv)
		}
		if ns.execArgv[1] != "1" || len(ns.execArgv) != 2 {
			t.Errorf("argv = %q, want exactly [path 1]", ns.execArgv)
		}
	})
	t.Run("[SEC-001] the ok line is written only after both proofs and right before the exec", func(t *testing.T) {
		okAt, execAt, proofAt := -1, -1, -1
		reads := 0
		for i, c := range ns.calls {
			switch {
			case c == fmt.Sprintf("status %q", "ok\n"):
				okAt = i
			case strings.HasPrefix(c, "exec"):
				execAt = i
			case c == "read "+selfMountinfo:
				reads++
				if reads == 2 {
					proofAt = i
				}
			}
		}
		if !(proofAt >= 0 && proofAt < okAt && okAt < execAt && execAt == len(ns.calls)-1) {
			t.Errorf("proof at %d, ok at %d, exec at %d of %d", proofAt, okAt, execAt, len(ns.calls))
		}
	})
	t.Run("[SEC-001] noexec on the host /tmp is carried into the read-only remount", func(t *testing.T) {
		ns := newFakeNS()
		ns.files[selfMountinfo] = []byte(strings.Replace(hostInit, "nosuid,nodev,noatime", "nosuid,nodev,noexec,noatime", 1))
		if err := RunExtra(ns, launchArgs); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("mount %q %q %q %#x %q", "/tmp", "/tmp", "", msRemount|msBind|msRdonly|msNosuid|msNodev|msNoexec, "")
		if !ns.did(want) {
			t.Errorf("remount lacks the inherited flags; calls %q", ns.calls)
		}
	})
}

func TestRunExtra_EU_SEC001_TmpNotMountPointRefused(t *testing.T) {
	ns := newFakeNS()
	noTmp := strings.Replace(hostInit, "24 22 0:21 / /tmp rw,nosuid,nodev,noatime shared:3 - tmpfs tmpfs rw\n", "", 1)
	ns.files[selfMountinfo] = []byte(noTmp)
	err := RunExtra(ns, launchArgs)
	if err == nil || err.Reason != ReasonTmpNotPrivate {
		t.Fatalf("err = %v, want tmp_not_private", err)
	}
	t.Run("[SEC-001] nothing is mounted and nothing is executed", func(t *testing.T) {
		if ns.did("mount") || ns.did("exec") {
			t.Errorf("calls %q", ns.calls)
		}
		if !strings.HasPrefix(ns.lastStatus(), "err tmp_not_private ") {
			t.Errorf("status = %q", ns.lastStatus())
		}
	})
}

func TestRunExtra_EU_SEC001_SameDevInoRefused(t *testing.T) {
	ns := newFakeNS()
	ns.statID["/tmp"] = FileID{Dev: 21, Ino: 2, UID: 0}
	err := RunExtra(ns, launchArgs)
	if err == nil || err.Reason != ReasonTmpNotPrivate || !strings.Contains(err.Detail, "still the host") {
		t.Fatalf("err = %v", err)
	}
	if ns.did("exec") || ns.did(fmt.Sprintf("status %q", "ok\n")) {
		t.Errorf("a launcher that failed its proof must not report ok or exec: %q", ns.calls)
	}
}

func TestRunExtra_EU_SEC001_NonEmptyOrNonTmpfsRefused(t *testing.T) {
	cases := map[string]func(*fakeNS){
		"not a tmpfs":           func(n *fakeNS) { n.statfs = 0xEF53 },
		"not empty":             func(n *fakeNS) { n.entries = 1 },
		"not owned by root":     func(n *fakeNS) { n.statID["/tmp"] = FileID{Dev: 50, Ino: 2, UID: 1000} },
		"still shared":          func(n *fakeNS) { n.afterTmpMount = hostInit + "60 24 0:50 / /tmp rw shared:44 - tmpfs tmpfs rw\n" },
		"host tmp unidentified": func(n *fakeNS) { n.statErr[initTmpPath] = errors.New("permission denied") },
		"statfs fails":          func(n *fakeNS) { n.statErr["statfs"] = errors.New("boom") },
		"stat fails":            func(n *fakeNS) { n.statErr["/tmp"] = errors.New("boom") },
	}
	for name, mutate := range cases {
		t.Run("[SEC-001] "+name+" is tmp_not_private and never execs", func(t *testing.T) {
			ns := newFakeNS()
			mutate(ns)
			err := RunExtra(ns, launchArgs)
			if err == nil || err.Reason != ReasonTmpNotPrivate {
				t.Fatalf("err = %v", err)
			}
			if ns.did("exec") {
				t.Error("exec was called after a failed proof")
			}
		})
	}
}

func TestRunExtra_EU_OPS001_RequiresSharedPeerOfPid1(t *testing.T) {
	t.Run("[OPS-001] self shared:5 against pid 1 shared:5 passes", func(t *testing.T) {
		ns := newFakeNS()
		table := strings.ReplaceAll(hostInit, "shared:1 ", "shared:5 ")
		ns.files[selfMountinfo] = []byte(table)
		ns.files[initMountinfo] = []byte(table)
		ns.afterTmpMount = table + "60 24 0:50 / /tmp rw - tmpfs tmpfs rw\n"
		if err := RunExtra(ns, launchArgs); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	cases := map[string]struct {
		self, init string
	}{
		"master only, no shared": {strings.Replace(hostInit, "shared:1 ", "master:1 ", 1), hostInit},
		"a different peer group": {strings.Replace(hostInit, "shared:1 ", "shared:9 ", 1), hostInit},
		"/boot not shared":       {strings.Replace(hostInit, "shared:7 ", "", 1), hostInit},
		"/boot peer mismatch":    {hostInit, strings.Replace(hostInit, "shared:7 ", "shared:8 ", 1)},
		"/ missing from pid 1":   {hostInit, strings.Replace(hostInit, "22 1 179:2 / / rw,noatime shared:1 - ext4 /dev/mmcblk0p2 rw\n", "", 1)},
		"/boot/firmware mismatch": {
			hostInit + "70 22 179:1 / /boot/firmware rw shared:11 - vfat /dev/mmcblk0p1 rw\n",
			hostInit + "70 22 179:1 / /boot/firmware rw shared:12 - vfat /dev/mmcblk0p1 rw\n",
		},
		"/boot/firmware not shared": {
			hostInit + "70 22 179:1 / /boot/firmware rw - vfat /dev/mmcblk0p1 rw\n",
			hostInit + "70 22 179:1 / /boot/firmware rw shared:12 - vfat /dev/mmcblk0p1 rw\n",
		},
	}
	for name, tc := range cases {
		t.Run("[OPS-001] "+name+" is mount_propagation_unverified and exec is never called", func(t *testing.T) {
			ns := newFakeNS()
			ns.files[selfMountinfo] = []byte(tc.self)
			ns.files[initMountinfo] = []byte(tc.init)
			err := RunExtra(ns, launchArgs)
			if err == nil || err.Reason != ReasonMountPropagation {
				t.Fatalf("err = %v, want mount_propagation_unverified", err)
			}
			if ns.did("exec") || ns.did("mount") {
				t.Errorf("work continued after the propagation proof failed: %q", ns.calls)
			}
		})
	}
	t.Run("[OPS-001] an unreadable or malformed table is mount_propagation_unverified", func(t *testing.T) {
		for name, mutate := range map[string]func(*fakeNS){
			"self unreadable": func(n *fakeNS) { n.readErr[selfMountinfo] = errors.New("denied") },
			"init unreadable": func(n *fakeNS) { n.readErr[initMountinfo] = errors.New("denied") },
			"self malformed":  func(n *fakeNS) { n.files[selfMountinfo] = []byte("garbage\n") },
			"init malformed":  func(n *fakeNS) { n.files[initMountinfo] = []byte("garbage\n") },
		} {
			ns := newFakeNS()
			mutate(ns)
			if err := RunExtra(ns, launchArgs); err == nil || err.Reason != ReasonMountPropagation || ns.did("exec") {
				t.Errorf("%s: err = %v, calls %q", name, err, ns.calls)
			}
		}
	})
}

func TestRunExtra_EU_SEC007_RefusesWithoutRootFifoParentToken(t *testing.T) {
	cases := map[string]func(*fakeNS){
		"not root":          func(n *fakeNS) { n.euid = 1000 },
		"status not a FIFO": func(n *fakeNS) { n.fifoErr = errors.New("not a FIFO") },
		"foreign parent":    func(n *fakeNS) { n.parentExe = "/bin/sh" },
		"parent unreadable": func(n *fakeNS) { n.parentErr = errors.New("denied") },
		"missing token":     func(n *fakeNS) { n.env = map[string]string{} },
		"wrong token":       func(n *fakeNS) { n.env = map[string]string{LauncherEnv: "yes"} },
		"deleted helper":    func(n *fakeNS) { n.parentExe = HelperPath + " (deleted)" },
	}
	for name, mutate := range cases {
		t.Run("[SEC-007] "+name+" is launcher_refused before any namespace work", func(t *testing.T) {
			ns := newFakeNS()
			mutate(ns)
			err := RunExtra(ns, launchArgs)
			if err == nil || err.Reason != ReasonLauncherRefused {
				t.Fatalf("err = %v", err)
			}
			if ns.did("unshare") || ns.did("mount") || ns.did("exec") || ns.did("lock-thread") {
				t.Errorf("a refused launcher touched the system: %q", ns.calls)
			}
		})
	}
}

func TestRunExtra_EU_AC019_OnlyRegistrySpec(t *testing.T) {
	cases := map[string][]string{
		"no arguments":      nil,
		"unknown id":        {LauncherArg, "snap"},
		"extra argument":    {LauncherArg, "dietpi", "--evil"},
		"wrong flag":        {"--run", "dietpi"},
		"path as id":        {LauncherArg, "/boot/dietpi/dietpi-update"},
		"id with argument":  {LauncherArg, "dietpi 1"},
		"upper-case id":     {LauncherArg, "DIETPI"},
		"empty id":          {LauncherArg, ""},
		"capabilities flag": {"--capabilities"},
		"flag only":         {LauncherArg},
	}
	for name, args := range cases {
		t.Run("[AC-019] "+name+" is launcher_refused and the fake exec is never called", func(t *testing.T) {
			ns := newFakeNS()
			err := RunExtra(ns, args)
			if err == nil || err.Reason != ReasonLauncherRefused {
				t.Fatalf("err = %v", err)
			}
			if ns.did("exec") || ns.did("unshare") {
				t.Errorf("calls %q", ns.calls)
			}
		})
	}
	t.Run("[AC-019] a test-only spec is possible only through the unexported policy", func(t *testing.T) {
		ns := newFakeNS()
		policy := launchPolicy{helperPath: HelperPath, lookup: func(id string) (extraupd.ApplySpec, bool) {
			return extraupd.ApplySpec{Path: "/bin/sh", Args: []string{"-c", "true"}, Env: []string{"A=1"}, PrivateTmp: true}, id == "fixture"
		}}
		if err := runExtra(ns, []string{LauncherArg, "fixture"}, policy); err != nil || ns.execPath != "/bin/sh" {
			t.Fatalf("err = %v exec %q", err, ns.execPath)
		}
		if err := RunExtra(newFakeNS(), []string{LauncherArg, "fixture"}); err == nil {
			t.Error("the production entry must not know the fixture id")
		}
	})
	t.Run("[AC-019] a registered spec without PrivateTmp is not launchable", func(t *testing.T) {
		ns := newFakeNS()
		policy := launchPolicy{helperPath: HelperPath, lookup: func(string) (extraupd.ApplySpec, bool) {
			return extraupd.ApplySpec{Path: "/bin/true"}, true
		}}
		if err := runExtra(ns, launchArgs, policy); err == nil || err.Reason != ReasonLauncherRefused {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRunExtra_EU_SEC009_MountTableOfTheCallingThread(t *testing.T) {
	ns := newFakeNS()
	if err := RunExtra(ns, launchArgs); err != nil {
		t.Fatal(err)
	}
	t.Run("[SEC-009] every self proof reads the thread-scoped table", func(t *testing.T) {
		if selfMountinfo != "/proc/thread-self/mountinfo" {
			t.Fatalf("selfMountinfo = %q", selfMountinfo)
		}
		for _, c := range ns.calls {
			if strings.HasPrefix(c, "read ") && c != "read "+selfMountinfo && c != "read "+initMountinfo {
				t.Errorf("unexpected read: %s", c)
			}
			if strings.Contains(c, "/proc/self/") {
				t.Errorf("a proof used /proc/self, which names the thread-group leader: %s", c)
			}
		}
	})
	t.Run("[SEC-009] both proofs read the table after the unshare", func(t *testing.T) {
		unshare := -1
		for i, c := range ns.calls {
			if strings.HasPrefix(c, "unshare") {
				unshare = i
			}
			if c == "read "+selfMountinfo && (unshare < 0 || i < unshare) {
				t.Errorf("own table read before the unshare at call %d", i)
			}
		}
	})
	t.Run("[SEC-009] the launcher source never names /proc/self", func(t *testing.T) {
		files := []string{"launcher.go", "nsops.go", "nsops_linux.go"}
		fset := token.NewFileSet()
		for _, name := range files {
			f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			for _, lit := range stringLiterals(f) {
				if strings.Contains(lit, "/proc/self") {
					t.Errorf("%s contains the literal %q", name, lit)
				}
			}
		}
	})
}

func TestRunExtra_EU_SEC007_StatusLinesAreBoundedAndOneLine(t *testing.T) {
	ns := newFakeNS()
	ns.unshareErr = errors.New(strings.Repeat("x", 500) + "\nsecond line\x00")
	err := RunExtra(ns, launchArgs)
	if err == nil || err.Reason != ReasonLauncherSetupError {
		t.Fatalf("err = %v", err)
	}
	line := ns.lastStatus()
	if !strings.HasPrefix(line, "err setup_failed ") || strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Errorf("status line = %q", line)
	}
	if len(strings.TrimPrefix(line, "err setup_failed ")) > maxLauncherDetail+1 {
		t.Errorf("detail is %d bytes, want at most %d", len(line), maxLauncherDetail)
	}
	t.Run("[SEC-007] an exec failure after ok reports a setup error", func(t *testing.T) {
		ns := newFakeNS()
		ns.execErr = errors.New("exec format error")
		err := RunExtra(ns, launchArgs)
		if err == nil || err.Reason != ReasonLauncherSetupError || !strings.HasPrefix(ns.lastStatus(), "err setup_failed ") {
			t.Fatalf("err = %v status %q", err, ns.lastStatus())
		}
	})
	t.Run("[SEC-007] each mount failure is a setup error and stops the sequence", func(t *testing.T) {
		for _, key := range []string{"/tmp|", "/tmp|tmpfs"} {
			ns := newFakeNS()
			ns.mountErr[key] = errors.New("EPERM")
			if err := RunExtra(ns, launchArgs); err == nil || ns.did("exec") {
				t.Errorf("%s: err = %v", key, err)
			}
		}
	})
}

func TestNsFlags_EU_OPS001_PinnedToKernelValues(t *testing.T) {
	// The Linux test (nsops_flags_linux_test.go) compares these with x/sys/unix;
	// this one pins the documented values on every host.
	want := map[string]uint64{"CLONE_NEWNS": 0x20000, "MS_RDONLY": 1, "MS_NOSUID": 2, "MS_NODEV": 4, "MS_NOEXEC": 8, "MS_REMOUNT": 32, "MS_BIND": 4096, "MS_REC": 16384, "MS_PRIVATE": 1 << 18, "TMPFS_MAGIC": 0x01021994}
	got := map[string]uint64{"CLONE_NEWNS": cloneNewNS, "MS_RDONLY": msRdonly, "MS_NOSUID": msNosuid, "MS_NODEV": msNodev, "MS_NOEXEC": msNoexec, "MS_REMOUNT": msRemount, "MS_BIND": msBind, "MS_REC": msRec, "MS_PRIVATE": msPrivate, "TMPFS_MAGIC": tmpfsMagic}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flags = %v, want %v", got, want)
	}
}

// stringLiterals returns every string literal of a parsed file, comments not
// included.
func stringLiterals(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if v, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, v)
			}
		}
		return true
	})
	return out
}
