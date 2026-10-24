package collectors

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// kernelTree builds a fake root with /proc/sys/kernel/osrelease and one
// /boot/vmlinuz-<release> file per installed release.
func kernelTree(t *testing.T, running string, installed ...string) string {
	t.Helper()
	root := t.TempDir()
	if running != "" {
		writeTestFile(t, filepath.Join(root, osReleaseRel), running+"\n")
	}
	if err := os.MkdirAll(filepath.Join(root, bootDirRel), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, release := range installed {
		writeTestFile(t, filepath.Join(root, bootDirRel, kernelImagePrefix+release), "")
	}
	return root
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestKernelReboot_PF_AC005_NewerInstalledKernelIsRequired(t *testing.T) {
	root := kernelTree(t, "6.12.48+deb13-amd64", "6.12.48+deb13-amd64", "7.2.9+deb13-amd64")

	newer, installed, running, ok := kernelNewerThanRunning(root)

	if !ok || !newer {
		t.Fatalf("[AC-005] newer=%v ok=%v, want a newer installed kernel", newer, ok)
	}
	if installed != "7.2.9+deb13-amd64" || running != "6.12.48+deb13-amd64" {
		t.Errorf("[AC-005] installed=%q running=%q", installed, running)
	}
}

func TestKernelReboot_PF_AC005_UbuntuAbiBumpIsRequired(t *testing.T) {
	root := kernelTree(t, "6.8.0-45-generic", "6.8.0-45-generic", "6.8.0-47-generic")
	newer, installed, _, ok := kernelNewerThanRunning(root)
	if !ok || !newer || installed != "6.8.0-47-generic" {
		t.Errorf("[AC-005] newer=%v installed=%q ok=%v", newer, installed, ok)
	}
}

func TestKernelReboot_PF_AC005_RunningIsNewestNotRequired(t *testing.T) {
	root := kernelTree(t, "7.2.9+deb13-amd64", "6.12.48+deb13-amd64", "7.2.9+deb13-amd64")
	newer, installed, _, ok := kernelNewerThanRunning(root)
	if !ok || newer || installed != "" {
		t.Errorf("[AC-005] running is the newest: newer=%v installed=%q ok=%v, want false/empty/true", newer, installed, ok)
	}
}

func TestKernelReboot_PF_AC005_OtherFlavorIgnored(t *testing.T) {
	root := kernelTree(t, "6.8.0-45-generic", "6.8.0-45-generic", "6.8.0-47-lowlatency", "6.8.0-47")
	newer, _, _, ok := kernelNewerThanRunning(root)
	if !ok || newer {
		t.Errorf("[AC-005] a newer kernel of another flavor must not count: newer=%v ok=%v", newer, ok)
	}
}

func TestKernelReboot_PF_AC005_NonKernelAndHostileNamesIgnored(t *testing.T) {
	root := kernelTree(t, "6.12.48+deb13-amd64", "6.12.48+deb13-amd64")
	boot := filepath.Join(root, bootDirRel)
	writeTestFile(t, filepath.Join(boot, "vmlinuz"), "")
	writeTestFile(t, filepath.Join(boot, "vmlinuz.old"), "")
	writeTestFile(t, filepath.Join(boot, "vmlinuz-9.9.9;touch-x-amd64"), "")
	writeTestFile(t, filepath.Join(boot, "vmlinuz-"+strings.Repeat("9", 70)+"-amd64"), "")
	writeTestFile(t, filepath.Join(boot, "initrd.img-9.9.9-amd64"), "")
	if err := os.Mkdir(filepath.Join(boot, "vmlinuz-9.9.9-amd64"), 0o755); err != nil {
		t.Fatal(err)
	}

	newer, installed, _, ok := kernelNewerThanRunning(root)

	if !ok || newer {
		t.Errorf("[AC-005] only regular vmlinuz-<release> files with a clean release count: newer=%v installed=%q", newer, installed)
	}
}

func TestKernelReboot_PF_AC005_UnreadableSourcesCountAsNotRequired(t *testing.T) {
	t.Run("[AC-005] missing osrelease", func(t *testing.T) {
		root := kernelTree(t, "", "7.2.9+deb13-amd64")
		if _, _, _, ok := kernelNewerThanRunning(root); ok {
			t.Error("ok = true, want false when the running release cannot be read")
		}
		if (rebootProbe{root: root, markerPath: filepath.Join(root, "none")}).required(context.Background(), "apt") {
			t.Error("unreadable source must count as not required")
		}
	})
	t.Run("[AC-005] missing /boot", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, osReleaseRel), "6.12.48+deb13-amd64\n")
		if _, _, _, ok := kernelNewerThanRunning(root); ok {
			t.Error("ok = true, want false when /boot cannot be listed")
		}
	})
	t.Run("[AC-005] invalid running release", func(t *testing.T) {
		root := kernelTree(t, "6.1;rm -rf", "7.2.9+deb13-amd64")
		if _, _, _, ok := kernelNewerThanRunning(root); ok {
			t.Error("ok = true, want false for a release that fails the charset filter")
		}
	})
}

func TestKernelReboot_PF_AC005_OsreleaseReadIsCapped(t *testing.T) {
	root := kernelTree(t, strings.Repeat("a", 100000), "7.2.9+deb13-amd64")
	if _, _, _, ok := kernelNewerThanRunning(root); ok {
		t.Error("an oversized osrelease must fail the length filter, not be read whole")
	}
}

func TestKernelReboot_PF_AC005_EntryBoundReachedNotOK(t *testing.T) {
	t.Run("[AC-005] exactly the cap is read", func(t *testing.T) {
		root := kernelTree(t, "6.12.48+deb13-amd64")
		for i := 0; i < bootEntryCap; i++ {
			writeTestFile(t, filepath.Join(root, bootDirRel, fmt.Sprintf("file-%03d", i)), "")
		}
		if _, _, _, ok := kernelNewerThanRunning(root); !ok {
			t.Error("a /boot with exactly the cap entries must still be evaluated")
		}
	})
	t.Run("[AC-005] one past the cap is not ok", func(t *testing.T) {
		root := kernelTree(t, "6.12.48+deb13-amd64", "7.2.9+deb13-amd64")
		for i := 0; i < bootEntryCap+1; i++ {
			writeTestFile(t, filepath.Join(root, bootDirRel, fmt.Sprintf("file-%03d", i)), "")
		}
		newer, _, _, ok := kernelNewerThanRunning(root)
		if ok || newer {
			t.Errorf("[AC-005] bound reached must count as not required: newer=%v ok=%v", newer, ok)
		}
	})
}

func TestRebootProbe_PF_AC005_AptMarkerOrKernel(t *testing.T) {
	noMarker := func(root string) string { return filepath.Join(root, "no-marker") }

	t.Run("[AC-005] marker only", func(t *testing.T) {
		root := kernelTree(t, "7.2.9+deb13-amd64", "7.2.9+deb13-amd64")
		marker := filepath.Join(root, "reboot-required")
		writeTestFile(t, marker, "")
		if !(rebootProbe{root: root, markerPath: marker}).required(context.Background(), "apt") {
			t.Error("marker file must require a reboot")
		}
	})
	t.Run("[AC-005] newer kernel only", func(t *testing.T) {
		root := kernelTree(t, "6.12.48+deb13-amd64", "7.2.9+deb13-amd64")
		if !(rebootProbe{root: root, markerPath: noMarker(root)}).required(context.Background(), "apt") {
			t.Error("a newer installed kernel must require a reboot")
		}
	})
	t.Run("[AC-005] neither", func(t *testing.T) {
		root := kernelTree(t, "7.2.9+deb13-amd64", "7.2.9+deb13-amd64")
		if (rebootProbe{root: root, markerPath: noMarker(root)}).required(context.Background(), "apt") {
			t.Error("no marker and the newest kernel running must not require a reboot")
		}
	})
	t.Run("[AC-005] unknown package manager", func(t *testing.T) {
		if (rebootProbe{}).required(context.Background(), "pacman") {
			t.Error("a package manager without a detection source is never required")
		}
	})
}

func TestRebootProbe_PF_AC005_DnfExitCodes(t *testing.T) {
	run := func(code int, err error) exitRunner {
		return func(ctx context.Context, name string, args ...string) (int, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > dnfRestartTimeout {
				t.Errorf("dnf needs-restarting must run under a %v timeout", dnfRestartTimeout)
			}
			return code, err
		}
	}
	cases := []struct {
		name string
		run  exitRunner
		want bool
	}{
		{"exit 1 requires reboot", run(1, nil), true},
		{"exit 0 does not", run(0, nil), false},
		{"other exit code does not", run(2, nil), false},
		{"run error does not", run(-1, errors.New("no dnf")), false},
	}
	for _, tc := range cases {
		t.Run("[AC-005] "+tc.name, func(t *testing.T) {
			if got := (rebootProbe{exit: tc.run}).required(context.Background(), "dnf"); got != tc.want {
				t.Errorf("required = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCompareDebVersion_PF_AC005_Table(t *testing.T) {
	long19 := strings.Repeat("9", 19)
	long30 := strings.Repeat("9", 30)
	cases := []struct {
		a, b string
		want int
	}{
		{"7.2.9+deb13", "6.12.48+deb13", 1},
		{"6.12.48+deb13", "7.2.9+deb13", -1},
		{"6.12.48+deb13", "6.12.48+deb13", 0},
		{"6.8.0-47", "6.8.0-45", 1},
		{"6.8.0-9", "6.8.0-10", -1},
		{"6.10", "6.9", 1},
		{"1.0~rc1", "1.0", -1},
		{"1.0", "1.0~rc1", 1},
		{"1.0~~", "1.0~", -1},
		{"1.0a", "1.0", 1},
		{"1.0+b1", "1.0", 1},
		{"1:0.1", "2.0", 1},
		{"0:2.0", "2.0", 0},
		{"2:1.0", "1:9.9", 1},
		{"1.0-1", "1.0", 1},
		{"007", "7", 0},
		{"1.0.0", "1.0", 1},
		{"", "", 0},
		{"", "1", -1},
		{"1", "", 1},
		{long19 + "1", long19, 1},
		{long30 + "0", long30, 1},
		{long30, long30, 0},
		{"0" + long30, long30, 0},
		{"1." + long30, "1." + long30[:29] + "8", 1},
		{"99999999999999999999999999999998:1", "99999999999999999999999999999999:1", -1},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("[AC-005] %q vs %q", clip(tc.a), clip(tc.b)), func(t *testing.T) {
			if got := compareDebVersion(tc.a, tc.b); got != tc.want {
				t.Errorf("compareDebVersion = %d, want %d", got, tc.want)
			}
			if got := compareDebVersion(tc.b, tc.a); got != -tc.want {
				t.Errorf("reversed compareDebVersion = %d, want %d (antisymmetry)", got, -tc.want)
			}
		})
	}
}

func clip(s string) string {
	if len(s) > 24 {
		return s[:24] + "..."
	}
	return s
}

func FuzzCompareDebVersion(f *testing.F) {
	seeds := [][2]string{
		{"7.2.9+deb13", "6.12.48+deb13"}, {"1.0~rc1", "1.0"}, {"1:2", "3"}, {"", ""},
		{strings.Repeat("9", 64), strings.Repeat("9", 63)}, {"~", ""}, {"a-b-c", "a-b"}, {"0:0", ":"},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		ab := compareDebVersion(a, b)
		ba := compareDebVersion(b, a)
		if ab != -ba {
			t.Fatalf("not antisymmetric: cmp(%q,%q)=%d but cmp(%q,%q)=%d", a, b, ab, b, a, ba)
		}
		if ab < -1 || ab > 1 {
			t.Fatalf("result %d outside -1..1", ab)
		}
		if compareDebVersion(a, a) != 0 {
			t.Fatalf("cmp(%q,%q) != 0", a, a)
		}
	})
}

func TestSplitKernelFlavor_PF_AC005(t *testing.T) {
	cases := []struct{ release, base, flavor string }{
		{"6.12.48+deb13-amd64", "6.12.48+deb13", "amd64"},
		{"6.8.0-45-generic", "6.8.0-45", "generic"},
		{"6.8.0-47", "6.8.0-47", ""},
		{"5.15.0-rpi-v8", "5.15.0-rpi", "v8"},
		{"6.12.48+deb13", "6.12.48+deb13", ""},
	}
	for _, tc := range cases {
		base, flavor := splitKernelFlavor(tc.release)
		if base != tc.base || flavor != tc.flavor {
			t.Errorf("[AC-005] splitKernelFlavor(%q) = (%q,%q), want (%q,%q)", tc.release, base, flavor, tc.base, tc.flavor)
		}
	}
}
