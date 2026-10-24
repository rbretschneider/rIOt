package maintenance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func writeAptFile(t *testing.T, root, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func entryBaseNames(entries []AptSourceEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, filepath.Base(e.Path))
	}
	sort.Strings(names)
	return names
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAptSourceEntries_PF_AC025_OnlyApptEffectiveFilesCount(t *testing.T) {
	root := t.TempDir()
	writeAptFile(t, root, "etc/apt/sources.list", "deb x")
	writeAptFile(t, root, "etc/apt/sources.list.d/debian.sources", "x")
	writeAptFile(t, root, "etc/apt/sources.list.d/extra.list", "x")
	writeAptFile(t, root, "etc/apt/sources.list.d/extra.list.dpkg-old", "x")
	writeAptFile(t, root, "etc/apt/sources.list.d/extra.list.save", "x")
	writeAptFile(t, root, "etc/apt/sources.list.d/extra.list~", "x")
	writeAptFile(t, root, "etc/apt/preferences.d/omvextras.pref", "x")
	writeAptFile(t, root, "etc/apt/preferences.d/nodot", "x")
	writeAptFile(t, root, "etc/apt/preferences.d/readme.txt", "x")
	writeAptFile(t, root, "etc/apt/preferences.d/omvextras.pref.dpkg-dist", "x")
	writeAptFile(t, root, "etc/apt/apt.conf.d/50unattended", "x")
	if err := os.MkdirAll(filepath.Join(root, "etc/apt/sources.list.d/sub.list"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := AptSourceEntries(root, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"debian.sources", "extra.list", "nodot", "omvextras.pref", "sources.list"}
	if got := entryBaseNames(entries); !equalStrings(got, want) {
		t.Errorf("apt-effective files = %v, want %v (backups, subdirs, apt.conf.d must be ignored)", got, want)
	}
}

func TestAptSourceEntries_PF_AC026_MissingDirsAreNotAnError(t *testing.T) {
	entries, err := AptSourceEntries(t.TempDir(), 1000)
	if err != nil {
		t.Fatalf("missing apt tree must not be an error, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0", len(entries))
	}
}

func TestAptSourceEntries_PF_AC026_BudgetReachedIsReported(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 10; i++ {
		writeAptFile(t, root, fmt.Sprintf("etc/apt/sources.list.d/f%02d.list", i), "x")
	}
	if _, err := AptSourceEntries(root, 5); !errors.Is(err, ErrAptBudget) {
		t.Errorf("err = %v, want ErrAptBudget when more entries exist than the budget", err)
	}
	if _, err := AptSourceEntries(root, 11); err != nil {
		t.Errorf("budget covering sources.list + 10 entries must succeed, got %v", err)
	}
}

func TestAptSourceEntries_PF_Bound_ChunkedReadStopsAtBudget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "etc/apt/sources.list.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < aptDirReadChunk*3; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.list", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AptSourceEntries(root, aptDirReadChunk+10); !errors.Is(err, ErrAptBudget) {
		t.Errorf("err = %v, want ErrAptBudget for a directory larger than the budget", err)
	}
}

func TestAptSourceEntriesUntil_PF_AC026_ExpiryCheckedBetweenChunks(t *testing.T) {
	root := t.TempDir()
	writeAptFile(t, root, "etc/apt/sources.list.d/a.list", "x")
	_, err := AptSourceEntriesUntil(root, 1000, func() bool { return true })
	if !errors.Is(err, ErrAptDeadline) {
		t.Errorf("err = %v, want ErrAptDeadline when the expiry callback fires", err)
	}
}

func TestAptSourceEntries_PF_AC025_SymlinkFollowedAndDanglingSkipped(t *testing.T) {
	root := t.TempDir()
	target := writeAptFile(t, root, "elsewhere/real.sources", "x")
	dir := filepath.Join(root, "etc/apt/sources.list.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.sources")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "nope"), filepath.Join(dir, "dangling.list")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	entries, err := AptSourceEntries(root, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := entryBaseNames(entries); !equalStrings(got, []string{"link.sources"}) {
		t.Errorf("entries = %v, want only the resolvable symlink", got)
	}
}

func TestAptSourcesFingerprint_PF_AC032_ChangesOnTouchAddAndRemove(t *testing.T) {
	root := t.TempDir()
	path := writeAptFile(t, root, "etc/apt/sources.list.d/omvextras.sources", "one")
	fingerprint := func() string {
		entries, err := AptSourceEntries(root, 1000)
		if err != nil {
			t.Fatal(err)
		}
		return AptSourcesFingerprint(entries)
	}

	base := fingerprint()
	if again := fingerprint(); again != base {
		t.Fatal("fingerprint must be stable when nothing changed")
	}

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	touched := fingerprint()
	if touched == base {
		t.Error("an mtime-only change (identical content rewrite) must change the fingerprint")
	}

	added := writeAptFile(t, root, "etc/apt/preferences.d/omvextras.pref", "p")
	withAdd := fingerprint()
	if withAdd == touched {
		t.Error("adding a preferences file must change the fingerprint")
	}

	if err := os.Remove(added); err != nil {
		t.Fatal(err)
	}
	if fingerprint() != touched {
		t.Error("removing the added file must restore the previous fingerprint")
	}
}
