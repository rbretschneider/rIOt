package extraupd

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func TestDietPiDetect_EU_AC001_PresenceNeedsBothFiles(t *testing.T) {
	both := dietpiHost(t, "9", "20", "1")
	onlyVersion := newHost(t).write("boot/dietpi/.version", versionText("9", "20", "1"))
	onlyUpdate := newHost(t).write("boot/dietpi/dietpi-update", tripwireScript)
	neither := newHost(t)

	t.Run("[AC-001] both files report a dietpi entry", func(t *testing.T) {
		got := DetectAll(context.Background(), both.fs())
		if len(got) != 1 || got[0].ID != "dietpi" {
			t.Fatalf("DetectAll = %+v, want exactly one dietpi entry", got)
		}
	})
	for name, h := range map[string]*hostFixture{"only .version": onlyVersion, "only dietpi-update": onlyUpdate, "neither": neither} {
		t.Run("[AC-001] "+name+" reports an empty list", func(t *testing.T) {
			if got := DetectAll(context.Background(), h.fs()); len(got) != 0 {
				t.Fatalf("DetectAll = %+v, want an empty list", got)
			}
			if _, present := detectOne(t, h); present {
				t.Error("DietPi must not be present without both files")
			}
		})
	}
	t.Run("[AC-001] a directory in place of dietpi-update does not count", func(t *testing.T) {
		h := newHost(t).write("boot/dietpi/.version", versionText("9", "20", "1")).mkdir("boot/dietpi/dietpi-update")
		if _, present := detectOne(t, h); present {
			t.Error("a directory is not a regular file")
		}
	})
	t.Run("[AC-001] a symlinked .version does not count", func(t *testing.T) {
		h := newHost(t).write("elsewhere", versionText("9", "20", "1")).write("boot/dietpi/dietpi-update", tripwireScript)
		if err := os.Symlink(filepath.Join(h.root, "elsewhere"), filepath.Join(h.root, "boot", "dietpi", ".version")); err != nil {
			t.Skipf("symlinks unavailable here: %v", err)
		}
		if _, present := detectOne(t, h); present {
			t.Error("a symlink is not a regular file (FR-008)")
		}
	})
}

// treeSnapshot lists every path with its size and mtime, so a test can prove
// that detection wrote nothing.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Directory times lag on some filesystems; the set of paths and the
			// files' own size and mtime prove nothing was written.
			out[path] = "dir"
			return nil
		}
		out[path] = fmt.Sprintf("%s/%d", info.ModTime(), info.Size())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDietPiDetect_EU_AC002_VersionParsedAsDataNoProcess(t *testing.T) {
	h := newHost(t)
	h.write("boot/dietpi/.version", versionText("9", "20", "1", "touch "+filepath.ToSlash(filepath.Join(h.root, "PWNED")), "$(touch "+filepath.ToSlash(filepath.Join(h.root, "PWNED2"))+")", "G_DIETPI_VERSION_CORE_EXTRA=`id`"))
	h.write("boot/dietpi/dietpi-update", tripwireScript)
	h.write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\n")
	before := treeSnapshot(t, h.root)

	got, present := detectOne(t, h)

	t.Run("[AC-002] the installed version is 9.20.1", func(t *testing.T) {
		if !present || got.Installed != "9.20.1" {
			t.Fatalf("installed = %q (present %v), want 9.20.1", got.Installed, present)
		}
	})
	t.Run("[AC-002] no file was written and nothing was executed", func(t *testing.T) {
		after := treeSnapshot(t, h.root)
		if len(after) != len(before) {
			t.Fatalf("tree changed: %d entries before, %d after", len(before), len(after))
		}
		for path, sig := range before {
			if after[path] != sig {
				t.Errorf("%s changed during detection", path)
			}
		}
		for _, marker := range []string{"PWNED", "PWNED2"} {
			if _, err := os.Stat(filepath.Join(h.root, marker)); err == nil {
				t.Errorf("%s exists: the version file was executed", marker)
			}
		}
	})
	t.Run("[AC-002] the package imports no process, network or shell facility", func(t *testing.T) {
		forbidden := map[string]bool{"os/exec": true, "net": true, "net/http": true, "plugin": true, "os/signal": true}
		files, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				if path := strings.Trim(imp.Path.Value, `"`); forbidden[path] {
					t.Errorf("%s imports %s: detection must start no process and use no network (FR-003)", name, path)
				}
			}
		}
	})
}

func TestDietPiDetect_EU_AC003_InvalidVersionIsUnknown(t *testing.T) {
	cases := map[string]string{
		"missing key":       "G_DIETPI_VERSION_CORE=9\nG_DIETPI_VERSION_SUB=20\n",
		"non-integer core":  versionText("nine", "20", "1"),
		"empty value":       versionText("9", "", "1"),
		"duplicate key":     versionText("9", "20", "1", "G_DIETPI_VERSION_SUB=21"),
		"ten digit value":   versionText("1234567890", "20", "1"),
		"empty file":        "",
		"only other lines":  "G_GITBRANCH='master'\n",
		"quoted bad number": versionText("'9x'", "20", "1"),
	}
	for name, body := range cases {
		t.Run("[AC-003] "+name, func(t *testing.T) {
			h := newHost(t).write("boot/dietpi/.version", body).write("boot/dietpi/dietpi-update", tripwireScript).write("run/dietpi/.update_available", "10.8.2\n")
			got, present := detectOne(t, h)
			if !present {
				t.Fatal("host must still count as DietPi")
			}
			if got.Installed != "" || got.Status != "unknown" || got.Reason != "version_unreadable" {
				t.Errorf("got installed=%q status=%q reason=%q, want empty / unknown / version_unreadable", got.Installed, got.Status, got.Reason)
			}
		})
	}
	t.Run("[AC-003] quoted version numbers are accepted", func(t *testing.T) {
		h := newHost(t).write("boot/dietpi/.version", versionText("'9'", `"20"`, "1")).write("boot/dietpi/dietpi-update", tripwireScript)
		if got, _ := detectOne(t, h); got.Installed != "9.20.1" {
			t.Errorf("installed = %q, want 9.20.1", got.Installed)
		}
	})
}

func TestDietPiDetect_EU_AC004_UpdateAvailableMajor(t *testing.T) {
	h := dietpiHost(t, "9", "20", "1").write("run/dietpi/.update_available", "10.8.2\n")
	got, _ := detectOne(t, h)
	if got.Status != "update_available" || got.Available != "10.8.2" || got.Pending != 1 {
		t.Fatalf("got status=%q available=%q pending=%d, want update_available / 10.8.2 / 1", got.Status, got.Available, got.Pending)
	}
	if !got.hasNotice("major_upgrade") {
		t.Errorf("notices = %v, want major_upgrade", got.Notices)
	}
	t.Run("[AC-004] a minor update is not a major upgrade", func(t *testing.T) {
		h := dietpiHost(t, "10", "7", "0").write("run/dietpi/.update_available", "10.8.2\n")
		got, _ := detectOne(t, h)
		if got.Status != "update_available" || got.hasNotice("major_upgrade") {
			t.Errorf("got status=%q notices=%v, want update_available without major_upgrade", got.Status, got.Notices)
		}
	})
	t.Run("[AC-004] a flag with surrounding blanks and extra lines still parses", func(t *testing.T) {
		h := dietpiHost(t, "9", "20", "1").write("run/dietpi/.update_available", "  10.8.2  \nignored\n")
		if got, _ := detectOne(t, h); got.Available != "10.8.2" {
			t.Errorf("available = %q, want 10.8.2", got.Available)
		}
	})
}

func TestDietPiDetect_EU_AC005_StaleAndGarbageFlag(t *testing.T) {
	t.Run("[AC-005] a flag equal to the installed version is ignored", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("run/dietpi/.update_available", "10.8.2\n")
		got, _ := detectOne(t, h)
		if got.Status == "update_available" || got.Pending != 0 || got.Available != "" {
			t.Errorf("got %+v, want the stale flag treated as absent", got)
		}
		if got.Status != "unknown" || got.Reason != "not_checked_since_boot" {
			t.Errorf("status=%q reason=%q, want the D4 row", got.Status, got.Reason)
		}
	})
	t.Run("[AC-005] a flag older than the installed version is ignored", func(t *testing.T) {
		h := dietpiHost(t, "10", "9", "0").write("run/dietpi/.update_available", "10.8.2\n")
		if got, _ := detectOne(t, h); got.Status == "update_available" {
			t.Errorf("an older flag must not mean an update: %+v", got)
		}
	})
	t.Run("[AC-005] a stale flag with checks disabled follows row D5", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("run/dietpi/.update_available", "10.8.2\n").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=0\n")
		if got, _ := detectOne(t, h); got.Status != "unknown" || got.Reason != "check_disabled" {
			t.Errorf("got %+v, want unknown/check_disabled", got)
		}
	})
	t.Run("[AC-005] an unparseable flag is update_available with an empty version", func(t *testing.T) {
		h := dietpiHost(t, "9", "20", "1").write("run/dietpi/.update_available", "garbage\n")
		got, _ := detectOne(t, h)
		if got.Status != "update_available" || got.Available != "" || got.Pending != 1 {
			t.Errorf("got %+v, want update_available with empty available version and pending 1", got)
		}
	})
	t.Run("[AC-005] an empty flag file is unparseable, not absent", func(t *testing.T) {
		h := dietpiHost(t, "9", "20", "1").write("run/dietpi/.update_available", "")
		if got, _ := detectOne(t, h); got.Status != "update_available" {
			t.Errorf("status = %q, want update_available", got.Status)
		}
	})
}

func TestDietPiDetect_EU_AC006_NeverUpToDateWithoutEvidence(t *testing.T) {
	t.Run("[AC-006] D3: evidence of a completed empty check is up to date", func(t *testing.T) {
		d := NewDietPi()
		d.Evidence = func(FS) bool { return true }
		got, _ := detectWith(t, d, dietpiHost(t, "10", "8", "2").fs())
		if got.Status != "up_to_date" || got.Reason != "" {
			t.Errorf("got %+v, want up_to_date", got)
		}
	})
	t.Run("[AC-006] D4: checks on, no flag, no evidence is not_checked_since_boot", func(t *testing.T) {
		got, _ := detectOne(t, dietpiHost(t, "10", "8", "2"))
		if got.Status != "unknown" || got.Reason != "not_checked_since_boot" {
			t.Errorf("got %+v, want unknown/not_checked_since_boot", got)
		}
	})
	t.Run("[AC-006] D5: checks off is check_disabled", func(t *testing.T) {
		for _, body := range []string{"CONFIG_CHECK_DIETPI_UPDATES=0\n", "CONFIG_CHECK_DIETPI_UPDATES=2\n", "# only a comment\n", "CONFIG_CHECK_DIETPI_UPDATES=\n", "#CONFIG_CHECK_DIETPI_UPDATES=1\n"} {
			h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", body)
			if got, _ := detectOne(t, h); got.Status != "unknown" || got.Reason != "check_disabled" {
				t.Errorf("dietpi.txt %q: got %+v, want unknown/check_disabled", body, got)
			}
		}
	})
	t.Run("[AC-006] D6: an unreadable dietpi.txt is config_unreadable", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2")
		if err := os.Remove(filepath.Join(h.root, "boot", "dietpi.txt")); err != nil {
			t.Fatal(err)
		}
		if got, _ := detectOne(t, h); got.Status != "unknown" || got.Reason != "config_unreadable" {
			t.Errorf("got %+v, want unknown/config_unreadable", got)
		}
	})
	t.Run("[AC-006] production never carries check evidence", func(t *testing.T) {
		for _, u := range Registry() {
			if d, ok := u.(*DietPi); ok && d.Evidence != nil {
				t.Error("the registered DietPi updater must have no evidence function: DietPi leaves no marker (OQ-A1)")
			}
		}
	})
	t.Run("[AC-006] trailing blanks and a carriage return after the 1 still enable the check", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1 \r\n")
		if got, _ := detectOne(t, h); got.Reason != "not_checked_since_boot" {
			t.Errorf("got %+v, want not_checked_since_boot", got)
		}
	})
	t.Run("[AC-006] an unreadable update flag is detection_failed, never up to date", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").mkdir("run/dietpi/.update_available")
		d := NewDietPi()
		d.Evidence = func(FS) bool { return true }
		got, _ := detectWith(t, d, h.fs())
		if got.Status != "unknown" || got.Reason != "detection_failed" {
			t.Errorf("got %+v, want unknown/detection_failed", got)
		}
	})
}

func TestDietPiDetect_EU_AC007_LivePatchesOnly(t *testing.T) {
	h := dietpiHost(t, "10", "8", "2").write("run/dietpi/.live_patches", "")
	got, _ := detectOne(t, h)
	if got.Status != "update_available" || got.Pending != 1 || !got.hasNotice("live_patches_available") || got.Available != "" {
		t.Fatalf("got %+v, want update_available, pending 1, live_patches_available, no available version", got)
	}
	t.Run("[AC-007] a version update plus live patches counts two items", func(t *testing.T) {
		h := dietpiHost(t, "9", "20", "1").write("run/dietpi/.live_patches", "").write("run/dietpi/.update_available", "10.8.2\n")
		got, _ := detectOne(t, h)
		if got.Pending != 2 || !got.hasNotice("live_patches_available") {
			t.Errorf("got %+v, want pending 2 with the live patches notice", got)
		}
	})
	t.Run("[AC-007] live patches override check_disabled", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("run/dietpi/.live_patches", "").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=0\n")
		if got, _ := detectOne(t, h); got.Status != "update_available" {
			t.Errorf("status = %q, want update_available (row D2 precedes D5)", got.Status)
		}
	})
}

func TestDietPiDetect_EU_AC008_SettingsNotices(t *testing.T) {
	t.Run("[AC-008] CONFIG_CHECK_APT_UPDATES=2 reports dietpi_auto_apt_upgrade", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nCONFIG_CHECK_APT_UPDATES=2\n")
		got, _ := detectOne(t, h)
		if !got.hasNotice("dietpi_auto_apt_upgrade") {
			t.Errorf("notices = %v, want dietpi_auto_apt_upgrade", got.Notices)
		}
	})
	t.Run("[AC-008] other apt values add no notice", func(t *testing.T) {
		for _, v := range []string{"0", "1"} {
			h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_APT_UPDATES="+v+"\n")
			if got, _ := detectOne(t, h); got.hasNotice("dietpi_auto_apt_upgrade") {
				t.Errorf("CONFIG_CHECK_APT_UPDATES=%s must not raise the notice", v)
			}
		}
	})
	t.Run("[AC-008] DEV_GITBRANCH=dev reports non_default_update_source with branch dev", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITBRANCH=dev\n")
		got, _ := detectOne(t, h)
		if !got.hasNotice("non_default_update_source") {
			t.Fatalf("notices = %v, want non_default_update_source", got.Notices)
		}
		if v := got.NoticeValues["non_default_update_source"]; v[0] != "dev" || v[1] != "MichaIng" {
			t.Errorf("branch/owner = %v, want dev / MichaIng", v)
		}
	})
	t.Run("[AC-008] a 200-character owner is reported as unreadable", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITOWNER="+strings.Repeat("o", 200)+"\n")
		got, _ := detectOne(t, h)
		if v := got.NoticeValues["non_default_update_source"]; v[1] != "unreadable" {
			t.Errorf("owner = %q, want unreadable", v[1])
		}
	})
	t.Run("[AC-008] a non-printable value is unreadable", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITBRANCH=dev\r\n")
		got, _ := detectOne(t, h)
		if v := got.NoticeValues["non_default_update_source"]; v[0] != "unreadable" {
			t.Errorf("branch with a carriage return = %q, want unreadable", v[0])
		}
	})
	t.Run("[AC-008] the default source adds no notice", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nDEV_GITBRANCH=master\nDEV_GITOWNER=MichaIng\n")
		if got, _ := detectOne(t, h); got.hasNotice("non_default_update_source") {
			t.Errorf("notices = %v, want none for the default source", got.Notices)
		}
	})
	t.Run("[AC-008] a source recorded in .version is reported when dietpi.txt is default", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi/.version", strings.Replace(versionText("10", "8", "2"), "G_GITBRANCH='master'", "G_GITBRANCH='beta'", 1))
		got, _ := detectOne(t, h)
		if v := got.NoticeValues["non_default_update_source"]; v[0] != "beta" || v[1] != "MichaIng" {
			t.Errorf("branch/owner = %v, want beta / MichaIng", v)
		}
	})
	t.Run("[AC-008] notices keep their documented order", func(t *testing.T) {
		h := dietpiHost(t, "9", "20", "1").
			write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nCONFIG_CHECK_APT_UPDATES=2\nDEV_GITBRANCH=dev\n").
			write("run/dietpi/.update_available", "10.8.2\n").write("run/dietpi/.live_patches", "")
		got, _ := detectOne(t, h)
		want := []string{"live_patches_available", "dietpi_auto_apt_upgrade", "non_default_update_source", "major_upgrade"}
		if strings.Join(got.Notices, ",") != strings.Join(want, ",") {
			t.Errorf("notices = %v, want %v", got.Notices, want)
		}
	})
}

func TestDietPiDetect_EU_AC009_BoundsAndBlockingReader(t *testing.T) {
	t.Run("[AC-009] a 1 MiB dietpi.txt is never read past 64 KiB and its facts are unknown", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\n"+strings.Repeat("# padding\n", 100000))
		cfs := &countingFS{inner: h.fs()}
		d := NewDietPi()
		d.Evidence = func(FS) bool { return true }
		got, _ := detectWith(t, d, cfs)
		if got.Status == "up_to_date" {
			t.Fatal("an oversized config must never yield up_to_date")
		}
		if got.Status != "unknown" || got.Reason != "config_unreadable" {
			t.Errorf("got %+v, want unknown/config_unreadable", got)
		}
		for _, call := range cfs.reads {
			if call.max != MaxReadBytes {
				t.Errorf("read of %s asked for up to %d bytes, want %d", call.name, call.max, MaxReadBytes)
			}
		}
		if len(cfs.reads) == 0 {
			t.Error("no reads recorded")
		}
	})
	t.Run("[AC-009] an oversized .version is unreadable", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2").write("boot/dietpi/.version", versionText("10", "8", "2")+strings.Repeat("# pad\n", 20000))
		if got, _ := detectOne(t, h); got.Reason != "version_unreadable" {
			t.Errorf("got %+v, want version_unreadable", got)
		}
	})
	t.Run("[AC-009] a blocking reader returns within the deadline as detection_failed", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2")
		bfs := &blockingFS{inner: h.fs(), release: make(chan struct{})}
		defer close(bfs.release)
		d := NewDietPi()
		d.Timeout = 100 * time.Millisecond
		start := time.Now()
		got, present := detectWith(t, d, bfs)
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("detection took %s, want about the deadline", elapsed)
		}
		if !present || got.Status != "unknown" || got.Reason != "detection_failed" {
			t.Errorf("got %+v (present %v), want unknown/detection_failed", got, present)
		}
	})
	t.Run("[AC-009] a second call while the first is stuck starts no new read", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2")
		bfs := &blockingFS{inner: h.fs(), release: make(chan struct{})}
		d := NewDietPi()
		d.Timeout = 50 * time.Millisecond
		detectWith(t, d, bfs)
		first := bfs.startedCount()
		for i := 0; i < 5; i++ {
			got, _ := detectWith(t, d, bfs)
			if got.Reason != "detection_failed" {
				t.Errorf("call %d: reason = %q, want detection_failed", i, got.Reason)
			}
		}
		if after := bfs.startedCount(); after != first {
			t.Errorf("reads started grew from %d to %d: a stuck detector must not leak goroutines", first, after)
		}
		close(bfs.release)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && d.inflight.Load() {
			time.Sleep(10 * time.Millisecond)
		}
		if d.inflight.Load() {
			t.Error("the in-flight flag must clear once the stuck read returns")
		}
	})
	t.Run("[AC-009] a cancelled context returns detection_failed without reading", func(t *testing.T) {
		h := dietpiHost(t, "10", "8", "2")
		cfs := &countingFS{inner: h.fs()}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		e, _ := NewDietPi().Detect(ctx, cfs)
		if e.UnknownReason != "detection_failed" || len(cfs.reads) != 0 {
			t.Errorf("got %+v after %d reads, want detection_failed with no reads", e, len(cfs.reads))
		}
	})
}

func TestDietPiSource_EU_SEC003_MatchesDietPiSedRule(t *testing.T) {
	src := func(t *testing.T, body string) Source {
		return NewDietPi().Source(newHost(t).write("boot/dietpi.txt", body).fs())
	}
	t.Run("[SEC-003] absent keys resolve to the defaults", func(t *testing.T) {
		if got := src(t, "CONFIG_X=1\n"); got != (Source{Owner: "MichaIng", Branch: "master", Readable: true}) {
			t.Errorf("source = %+v", got)
		}
	})
	t.Run("[SEC-003] empty values resolve to the defaults", func(t *testing.T) {
		if got := src(t, "DEV_GITOWNER=\nDEV_GITBRANCH=\n"); got.Owner != "MichaIng" || got.Branch != "master" {
			t.Errorf("source = %+v", got)
		}
	})
	t.Run("[SEC-003] the first matching line wins, even if a later one differs", func(t *testing.T) {
		if got := src(t, "DEV_GITOWNER=First\nDEV_GITOWNER=Second\n"); got.Owner != "First" {
			t.Errorf("owner = %q, want First", got.Owner)
		}
	})
	t.Run("[SEC-003] leading blanks are allowed and a comment never matches", func(t *testing.T) {
		if got := src(t, "#DEV_GITOWNER=Evil\n \t DEV_GITOWNER=Spaced\n"); got.Owner != "Spaced" {
			t.Errorf("owner = %q, want Spaced", got.Owner)
		}
		if got := src(t, "#DEV_GITOWNER=Evil\n"); got.Owner != "MichaIng" {
			t.Errorf("a commented line must not count, owner = %q", got.Owner)
		}
	})
	t.Run("[SEC-003] the value is verbatim: a trailing blank or CR is part of it", func(t *testing.T) {
		if got := src(t, "DEV_GITOWNER=MichaIng \n"); got.Owner != "MichaIng " {
			t.Errorf("owner = %q, want a trailing blank kept so the owner gate fails closed", got.Owner)
		}
		if got := src(t, "DEV_GITOWNER=MichaIng\r\n"); got.Owner != "MichaIng\r" {
			t.Errorf("owner = %q, want the CR kept", got.Owner)
		}
	})
	t.Run("[SEC-003] an unreadable or oversized dietpi.txt is not readable", func(t *testing.T) {
		if got := NewDietPi().Source(newHost(t).fs()); got.Readable {
			t.Error("a missing file must be unreadable")
		}
		if got := src(t, strings.Repeat("# pad\n", 20000)); got.Readable {
			t.Error("an oversized file must be unreadable")
		}
	})
}

func TestDietPiApply_EU_AC019_FixedSpec(t *testing.T) {
	spec := NewDietPi().Apply()
	t.Run("[AC-019] the spec is exactly /boot/dietpi/dietpi-update 1", func(t *testing.T) {
		if spec.Path != "/boot/dietpi/dietpi-update" || len(spec.Args) != 1 || spec.Args[0] != "1" || spec.Dir != "/" {
			t.Errorf("spec = %q %q dir %q", spec.Path, spec.Args, spec.Dir)
		}
	})
	t.Run("[AC-019] the environment is the fixed environment plus G_INTERACTIVE=0", func(t *testing.T) {
		want := "PATH=/usr/sbin:/usr/bin:/sbin:/bin,HOME=/root,LANG=C,LC_ALL=C,DEBIAN_FRONTEND=noninteractive,DEBCONF_NONINTERACTIVE_SEEN=true,NEEDRESTART_MODE=l,G_INTERACTIVE=0"
		if got := strings.Join(spec.Env, ","); got != want {
			t.Errorf("env = %s, want %s", got, want)
		}
	})
	t.Run("[AC-019] each call returns a fresh copy no caller can use to change the next", func(t *testing.T) {
		a := NewDietPi().Apply()
		a.Args[0] = "2"
		a.Env[0] = "PATH=/tmp"
		if b := NewDietPi().Apply(); b.Args[0] != "1" || b.Env[0] != "PATH=/usr/sbin:/usr/bin:/sbin:/bin" {
			t.Errorf("a mutated spec leaked into the next call: %q %q", b.Args, b.Env[0])
		}
	})
	t.Run("[AC-023] the limits are the stated ones", func(t *testing.T) {
		if spec.StepLimit != 40*time.Minute || spec.OutputCap != 128<<10 || spec.DescendantSettle != 10*time.Second || spec.LogPath != "/var/lib/dietpi/logs/dietpi-update.log" {
			t.Errorf("limits = %s %d %s %s", spec.StepLimit, spec.OutputCap, spec.DescendantSettle, spec.LogPath)
		}
	})
	t.Run("[SEC-003] only owner MichaIng is trusted and the step is launched with a private /tmp", func(t *testing.T) {
		if spec.TrustedOwner != "MichaIng" || !spec.PrivateTmp {
			t.Errorf("trusted owner %q private tmp %v", spec.TrustedOwner, spec.PrivateTmp)
		}
	})
}

func TestDietPiApply_EU_SEC004_RequiredRootsFollowVersion(t *testing.T) {
	spec := NewDietPi().Apply()
	for version, want := range map[string]bool{"9.20.1": true, "10.0.0": true, "10.3.9": true, "10.4.0": false, "10.8.2": false, "11.0.0": false, "": false, "garbage": false} {
		t.Run("[SEC-004] installed "+version, func(t *testing.T) {
			got := spec.RequiredRoots(models.ExtraUpdater{InstalledVersion: version})
			if (len(got) == 1 && got[0] == "/var/tmp/dietpi") != want || (!want && len(got) != 0) {
				t.Errorf("RequiredRoots = %v, want /var/tmp/dietpi required = %v", got, want)
			}
		})
	}
	t.Run("[SEC-004] the trust roots cover the DietPi tree, config, flags and /var/tmp/dietpi", func(t *testing.T) {
		if strings.Join(spec.TrustRoots, ",") != "/boot/dietpi,/boot/dietpi.txt,/run/dietpi,/var/tmp/dietpi" {
			t.Errorf("trust roots = %v", spec.TrustRoots)
		}
	})
}

func TestDietPiBusy_EU_SEC001_MatchRule(t *testing.T) {
	busy := NewDietPi().Apply().BusyMatch
	cases := []struct {
		argv0, argv1 string
		want         bool
	}{
		{"/bin/bash", "/boot/dietpi/dietpi-update", true},
		{"/boot/dietpi/dietpi-software", "", true},
		{"/bin/dash", "/etc/cron.daily/dietpi", true},
		{"/boot/dietpi/func/dietpi-globals", "", true},
		{"/bin/bash", "/boot/dietpi/dietpi-cloudshell", false},
		{"/usr/local/lib/riot/riot-maintenance", "", false},
		{"/usr/bin/apt-get", "update", false},
		{"/boot/dietpi.txt", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := busy(tc.argv0, tc.argv1); got != tc.want {
			t.Errorf("BusyMatch(%q, %q) = %v, want %v", tc.argv0, tc.argv1, got, tc.want)
		}
	}
}
