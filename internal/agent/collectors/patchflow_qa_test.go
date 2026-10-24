package collectors

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer (PATCH-FLOW-001).

// vermontZone is the host's local time zone on 2026-10-23 (EDT, UTC-4), the
// incident the story is about.
var vermontZone = time.FixedZone("EDT", -4*60*60)

func vermont(hour, minute, second, nanos int) time.Time {
	return time.Date(2026, 10, 23, hour, minute, second, nanos, vermontZone)
}

// vermontAptTree reproduces, file for file, the apt configuration and lists
// directory read from the real host `vermont` (Debian 13 + OpenMediaVault) with
// a read-only SSH session: the names, the modification times that matter, and
// the backup file that apt ignores. listsMTime is the lists directory mtime.
func vermontAptTree(t *testing.T, listsMTime time.Time) string {
	t.Helper()
	root := t.TempDir()
	feb := time.Date(2026, 2, 2, 22, 24, 3, 0, vermontZone)
	files := map[string]time.Time{
		"etc/apt/sources.list":                                                         feb,
		"etc/apt/sources.list~":                                                        time.Date(2026, 2, 2, 16, 48, 1, 0, vermontZone),
		"etc/apt/sources.list.d/omvextras.sources":                                     vermont(22, 46, 32, 811277642),
		"etc/apt/sources.list.d/openmediavault-kernel-backports.list":                  feb,
		"etc/apt/sources.list.d/openmediavault-local.list":                             time.Date(2025, 12, 30, 2, 3, 52, 0, vermontZone),
		"etc/apt/sources.list.d/openmediavault-os-security.list":                       feb,
		"etc/apt/sources.list.d/openmediavault.list":                                   feb,
		"etc/apt/preferences.d/omvextras.pref":                                         vermont(22, 46, 32, 983106852),
		"etc/apt/preferences.d/openmediavault-kernel-backports.pref":                   feb,
		"etc/apt/preferences.d/openmediavault-local.pref":                              time.Date(2022, 11, 1, 6, 31, 45, 0, vermontZone),
		aptListsRel + "/deb.debian.org_debian_dists_trixie_main_binary-amd64_Packages": time.Date(2026, 9, 12, 3, 29, 52, 0, vermontZone),
		aptListsRel + "/_var_cache_openmediavault_archives_._Packages":                 vermont(22, 46, 32, 0),
		aptListsRel + "/lock":                                                          time.Date(2026, 2, 2, 22, 24, 21, 0, vermontZone),
	}
	for rel, mtime := range files {
		path := filepath.Join(root, rel)
		writeTestFile(t, path, "x")
		setMTime(t, path, mtime)
	}
	setMTime(t, filepath.Join(root, aptListsRel), listsMTime)
	return root
}

// Covers AC-025, AC-038 (real-host facts): at 22:44 the lists directory was last
// refreshed, then the omvextras postinst rewrote omvextras.sources (22:46:32.811)
// and omvextras.pref (22:46:32.983). The new rule must call that state stale.
func TestListsFreshness_QA_AC025_VermontOmvextrasRewriteAfterRefreshIsStale(t *testing.T) {
	root := vermontAptTree(t, vermont(22, 44, 30, 0))
	state, reason := listsFreshness(root, vermont(22, 53, 0, 0), time.Now().Add(freshnessDeadline))
	if state != models.ListsStale {
		t.Fatalf("[AC-025] state = %q (%s), want stale for the 22:46:32 rewrite after a 22:44 refresh", state, reason)
	}
}

// Covers AC-025: only the rewritten .pref (not the .sources) is newer. This is the
// half of the incident that the old rule could never see.
func TestListsFreshness_QA_AC025_VermontPreferencesAloneMakeItStale(t *testing.T) {
	root := vermontAptTree(t, vermont(22, 44, 30, 0))
	older := vermont(22, 40, 0, 0)
	setMTime(t, filepath.Join(root, "etc/apt/sources.list.d/omvextras.sources"), older)
	state, _ := listsFreshness(root, vermont(22, 53, 0, 0), time.Now().Add(freshnessDeadline))
	if state != models.ListsStale {
		t.Errorf("[AC-025] state = %q, want stale when only omvextras.pref is newer than the lists", state)
	}
}

// Covers AC-025, AC-032, AC-038: after the second Patch (22:57:39, the real lists
// mtime on the host) the same configuration is fresh, and the zero-byte
// _Packages copy that carries a 22:46:32 mtime does not count as configuration.
func TestListsFreshness_QA_AC025_VermontAfterSecondPatchIsFresh(t *testing.T) {
	root := vermontAptTree(t, vermont(22, 57, 39, 379210091))
	state, reason := listsFreshness(root, vermont(23, 0, 0, 0), time.Now().Add(freshnessDeadline))
	if state != models.ListsFresh {
		t.Errorf("[AC-025] state = %q (%s), want fresh once the lists are newer than every config file", state, reason)
	}
}

// Covers AC-026, NFR-004: the skew guard is 5 minutes, not "some large number".
// An apt file 4 minutes ahead of the agent clock still counts as a newer file; one
// 6 minutes ahead cannot be trusted and reports unknown rather than a false stale.
func TestListsFreshness_QA_AC026_ClockSkewGuardIsFiveMinutes(t *testing.T) {
	now := hours(3)
	for _, tc := range []struct {
		name   string
		ahead  time.Duration
		expect models.ListsFreshness
	}{
		{"4 minutes ahead is a real newer file", 4 * time.Minute, models.ListsStale},
		{"6 minutes ahead is clock skew", 6 * time.Minute, models.ListsUnknown},
	} {
		t.Run("[AC-026] "+tc.name, func(t *testing.T) {
			root := aptTree(t)
			setMTime(t, filepath.Join(root, "etc/apt/preferences.d/omvextras.pref"), now.Add(tc.ahead))
			if state, reason := listsFreshness(root, now, time.Now().Add(freshnessDeadline)); state != tc.expect {
				t.Errorf("state = %q (%s), want %q", state, reason, tc.expect)
			}
		})
	}
}

// Covers AC-005 (real-host facts): the exact /boot listing of vermont, 19 kernels
// from 6.18.5 to 7.2.9 with the running kernel 7.2.9+deb13-amd64, is not a
// reboot; the same listing while still running the previous 7.2.6 kernel is.
func TestKernelReboot_QA_AC005_VermontBootListing(t *testing.T) {
	installed := []string{
		"6.18.12+deb13-amd64", "6.18.15+deb13-amd64", "6.18.5+deb13-amd64", "6.18.9+deb13-amd64",
		"6.19.10+deb13-amd64", "6.19.11+deb13-amd64", "6.19.13+deb13-amd64", "6.19.14+deb13-amd64",
		"6.19.6+deb13-amd64", "6.19.8+deb13-amd64", "7.0.10+deb13-amd64", "7.0.12+deb13-amd64",
		"7.0.13+deb13-amd64", "7.0.4+deb13-amd64", "7.0.7+deb13-amd64", "7.0.9+deb13-amd64",
		"7.1.3+deb13-amd64", "7.2.6+deb13-amd64", "7.2.9+deb13-amd64",
	}
	t.Run("[AC-005] running the newest kernel is not a reboot", func(t *testing.T) {
		root := kernelTree(t, "7.2.9+deb13-amd64", installed...)
		for _, other := range []string{"System.map-7.2.9+deb13-amd64", "config-7.2.9+deb13-amd64", "initrd.img-7.2.9+deb13-amd64"} {
			writeTestFile(t, filepath.Join(root, bootDirRel, other), "")
		}
		if err := os.MkdirAll(filepath.Join(root, bootDirRel, "grub"), 0o755); err != nil {
			t.Fatal(err)
		}
		newer, installedNewest, _, ok := kernelNewerThanRunning(root)
		if !ok || newer {
			t.Errorf("[AC-005] newer=%v installed=%q ok=%v, want not newer", newer, installedNewest, ok)
		}
	})
	t.Run("[AC-005] running 7.2.6 while 7.2.9 is installed is a reboot", func(t *testing.T) {
		root := kernelTree(t, "7.2.6+deb13-amd64", installed...)
		newer, installedNewest, _, ok := kernelNewerThanRunning(root)
		if !ok || !newer || installedNewest != "7.2.9+deb13-amd64" {
			t.Errorf("[AC-005] newer=%v installed=%q ok=%v, want 7.2.9 newer", newer, installedNewest, ok)
		}
	})
}
