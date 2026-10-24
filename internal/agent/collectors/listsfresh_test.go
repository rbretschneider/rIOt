package collectors

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// freshnessBase anchors every mtime in these tests so none of them depends on
// the wall clock; only the 2 s deadline uses real time.
var freshnessBase = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func hours(h int) time.Time { return freshnessBase.Add(time.Duration(h) * time.Hour) }

func setMTime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// aptTree builds a fake apt tree whose lists directory was refreshed at hour 0
// and whose configuration files are older (hour -5).
func aptTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, aptListsRel, "deb.debian.org_debian_dists_trixie_main_binary-amd64_Packages"), "x")
	writeTestFile(t, filepath.Join(root, "etc/apt/sources.list"), "deb http://deb.debian.org/debian trixie main")
	writeTestFile(t, filepath.Join(root, "etc/apt/sources.list.d/debian.sources"), "Types: deb")
	writeTestFile(t, filepath.Join(root, "etc/apt/preferences.d/omvextras.pref"), "Pin: x")
	for _, rel := range []string{"etc/apt/sources.list", "etc/apt/sources.list.d/debian.sources", "etc/apt/preferences.d/omvextras.pref"} {
		setMTime(t, filepath.Join(root, rel), hours(-5))
	}
	setMTime(t, filepath.Join(root, aptListsRel), hours(0))
	return root
}

func freshness(t *testing.T, root string) (models.ListsFreshness, string) {
	t.Helper()
	return listsFreshness(root, hours(3), time.Now().Add(freshnessDeadline))
}

func TestListsFreshness_PF_AC025_ConfigNewerThanListsIsStale(t *testing.T) {
	for _, rel := range []string{"etc/apt/sources.list.d/debian.sources", "etc/apt/preferences.d/omvextras.pref", "etc/apt/sources.list"} {
		t.Run("[AC-025] "+rel, func(t *testing.T) {
			root := aptTree(t)
			setMTime(t, filepath.Join(root, rel), hours(1))
			if state, reason := freshness(t, root); state != models.ListsStale {
				t.Errorf("state = %q (%s), want stale when %s is newer than the lists", state, reason, rel)
			}
		})
	}
}

func TestListsFreshness_PF_AC025_ConfigOlderIsFresh(t *testing.T) {
	root := aptTree(t)
	if state, reason := freshness(t, root); state != models.ListsFresh {
		t.Errorf("state = %q (%s), want fresh when no configuration file is newer", state, reason)
	}
}

func TestListsFreshness_PF_AC025_ConfigEqualToListsIsFresh(t *testing.T) {
	root := aptTree(t)
	setMTime(t, filepath.Join(root, "etc/apt/preferences.d/omvextras.pref"), hours(0))
	if state, _ := freshness(t, root); state != models.ListsFresh {
		t.Errorf("state = %q, want fresh at the exact refresh instant", state)
	}
}

func TestListsFreshness_PF_AC025_BackupFilesIgnored(t *testing.T) {
	root := aptTree(t)
	for _, rel := range []string{
		"etc/apt/sources.list.d/debian.sources.dpkg-old",
		"etc/apt/sources.list.d/extra.list.save",
		"etc/apt/sources.list.d/extra.list~",
		"etc/apt/preferences.d/omvextras.pref.dpkg-dist",
		"etc/apt/preferences.d/README.txt",
		"etc/apt/apt.conf.d/50unattended",
	} {
		path := filepath.Join(root, rel)
		writeTestFile(t, path, "x")
		setMTime(t, path, hours(2))
	}
	if state, reason := freshness(t, root); state != models.ListsFresh {
		t.Errorf("state = %q (%s), want fresh: backups and apt.conf.d never count (OQ-007)", state, reason)
	}
}

func TestListsFreshness_PF_AC025_UbuntuStampCountsAsRefresh(t *testing.T) {
	root := aptTree(t)
	setMTime(t, filepath.Join(root, "etc/apt/sources.list.d/debian.sources"), hours(1))
	writeTestFile(t, filepath.Join(root, aptStampRel), "")
	setMTime(t, filepath.Join(root, aptStampRel), hours(2))
	if state, reason := freshness(t, root); state != models.ListsFresh {
		t.Errorf("state = %q (%s), want fresh: the update-success-stamp is newer than the config", state, reason)
	}
}

func TestListsFreshness_PF_AC032_StampAfterRewriteReportsFresh(t *testing.T) {
	root := aptTree(t)
	omv := filepath.Join(root, "etc/apt/preferences.d/omvextras.pref")
	setMTime(t, omv, hours(1))
	if state, _ := freshness(t, root); state != models.ListsStale {
		t.Fatalf("precondition: config rewritten after refresh must be stale, got %q", state)
	}
	setMTime(t, filepath.Join(root, aptListsRel), hours(2)) // helper's lists stamp
	if state, reason := freshness(t, root); state != models.ListsFresh {
		t.Errorf("state = %q (%s), want fresh after the helper stamps the lists directory", state, reason)
	}
}

func TestListsFreshness_PF_AC025_RawCountStillReported(t *testing.T) {
	root := aptTree(t)
	setMTime(t, filepath.Join(root, "etc/apt/preferences.d/omvextras.pref"), hours(1))
	var listing strings.Builder
	listing.WriteString("Listing...\n")
	for i := 0; i < 38; i++ {
		fmt.Fprintf(&listing, "pkg%02d/trixie-backports 2.%d amd64 [upgradable from: 1.%d]\n", i, i, i)
	}
	info := &models.UpdateInfo{}
	parseAptUpgradable(listing.String(), info)
	c := &UpdatesCollector{aptRoot: root, listsNow: func() time.Time { return hours(3) }}

	c.applyListsFreshness(info)

	if info.ListsFreshness != models.ListsStale {
		t.Errorf("freshness = %q, want stale", info.ListsFreshness)
	}
	if info.PendingUpdates != 38 || len(info.Updates) != 38 {
		t.Errorf("raw count = %d (%d listed), want 38 reported alongside the stale flag", info.PendingUpdates, len(info.Updates))
	}
}

func TestListsFreshness_PF_AC026_NoListsIsStale(t *testing.T) {
	t.Run("[AC-026] missing lists directory", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, "etc/apt/sources.list"), "deb x")
		if state, _ := freshness(t, root); state != models.ListsStale {
			t.Errorf("state = %q, want stale", state)
		}
	})
	t.Run("[AC-026] lists directory without a _Packages file", func(t *testing.T) {
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, aptListsRel, "lock"), "")
		if err := os.MkdirAll(filepath.Join(root, aptListsRel, "partial"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(root, aptListsRel, "deb.debian.org_debian_dists_trixie_InRelease"), "x")
		if state, _ := freshness(t, root); state != models.ListsStale {
			t.Errorf("state = %q, want stale when no package index exists", state)
		}
	})
	t.Run("[AC-026] a directory named like an index is not an index", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, aptListsRel, "x_Packages"), 0o755); err != nil {
			t.Fatal(err)
		}
		if state, _ := freshness(t, root); state != models.ListsStale {
			t.Errorf("state = %q, want stale", state)
		}
	})
}

func TestListsFreshness_PF_AC026_StatErrorIsUnknown(t *testing.T) {
	root := t.TempDir()
	// A regular file where the lists directory should be: stat succeeds but the
	// directory cannot be listed.
	writeTestFile(t, filepath.Join(root, aptListsRel), "not a directory")
	state, reason := freshness(t, root)
	if state != models.ListsUnknown || reason == "" {
		t.Errorf("state = %q reason = %q, want unknown with a reason", state, reason)
	}
}

func TestListsFreshness_PF_AC026_EntryBudgetIsUnknown(t *testing.T) {
	root := aptTree(t)
	dir := filepath.Join(root, "etc/apt/sources.list.d")
	for i := 0; i < freshnessEntryBudget+1; i++ {
		writeTestFile(t, filepath.Join(dir, fmt.Sprintf("f%04d.list", i)), "")
	}
	state, reason := freshness(t, root)
	if state != models.ListsUnknown || !strings.Contains(reason, "budget") {
		t.Errorf("state = %q reason = %q, want unknown (entry budget reached)", state, reason)
	}
}

func TestListsFreshness_PF_AC026_DeadlineIsUnknown(t *testing.T) {
	root := aptTree(t)
	state, reason := listsFreshness(root, hours(3), time.Now().Add(-time.Second))
	if state != models.ListsUnknown || !strings.Contains(reason, "deadline") {
		t.Errorf("state = %q reason = %q, want unknown (deadline reached)", state, reason)
	}
}

func TestListsFreshness_PF_AC026_FutureMtimeIsUnknown(t *testing.T) {
	root := aptTree(t)
	// now is hour 3; a file 10 hours ahead is beyond the 5 minute skew allowance.
	setMTime(t, filepath.Join(root, "etc/apt/sources.list.d/debian.sources"), hours(13))
	state, reason := freshness(t, root)
	if state != models.ListsUnknown || !strings.Contains(reason, "clock") {
		t.Errorf("state = %q reason = %q, want unknown for a clock-skewed file, never a permanent stale", state, reason)
	}
}

func treeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		names = append(names, fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}

func TestListsFreshness_PF_AC026_NoFilesCreated(t *testing.T) {
	root := aptTree(t)
	setMTime(t, filepath.Join(root, "etc/apt/preferences.d/omvextras.pref"), hours(1))
	before := treeSnapshot(t, root)

	freshness(t, root)
	(&UpdatesCollector{aptRoot: root, listsNow: func() time.Time { return hours(3) }}).applyListsFreshness(&models.UpdateInfo{})

	after := treeSnapshot(t, root)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("[AC-026] the freshness check changed the filesystem:\nbefore=%v\nafter=%v", before, after)
	}
}

type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func captureLogs(t *testing.T) *captureHandler {
	t.Helper()
	h := &captureHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return h
}

func TestListsFreshness_PF_AC026_AtMostOneDebugLine(t *testing.T) {
	t.Run("[AC-026] unknown logs exactly one debug line", func(t *testing.T) {
		logs := captureLogs(t)
		root := t.TempDir()
		writeTestFile(t, filepath.Join(root, aptListsRel), "not a directory")
		(&UpdatesCollector{aptRoot: root}).applyListsFreshness(&models.UpdateInfo{})
		if len(logs.records) != 1 || logs.records[0].Level != slog.LevelDebug {
			t.Errorf("records = %d, want exactly one debug record", len(logs.records))
		}
	})
	t.Run("[AC-026] stale and fresh log nothing", func(t *testing.T) {
		logs := captureLogs(t)
		root := aptTree(t)
		c := &UpdatesCollector{aptRoot: root, listsNow: func() time.Time { return hours(3) }}
		c.applyListsFreshness(&models.UpdateInfo{})
		setMTime(t, filepath.Join(root, "etc/apt/preferences.d/omvextras.pref"), hours(1))
		c.applyListsFreshness(&models.UpdateInfo{})
		if len(logs.records) != 0 {
			t.Errorf("records = %d, want 0 for stale and fresh results", len(logs.records))
		}
	})
}
