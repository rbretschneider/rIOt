package maintenance

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// memTrustFS is an in-memory tree for the trust walk.
type memTrustFS struct {
	nodes map[string]trustStat
}

func newMemTrustFS() *memTrustFS { return &memTrustFS{nodes: map[string]trustStat{}} }

func (m *memTrustFS) dir(path string, mode fs.FileMode, uid uint32) *memTrustFS {
	m.nodes[path] = trustStat{Mode: fs.ModeDir | mode, UID: uid}
	return m
}
func (m *memTrustFS) file(path string, mode fs.FileMode, uid uint32) *memTrustFS {
	m.nodes[path] = trustStat{Mode: mode, UID: uid}
	return m
}
func (m *memTrustFS) symlink(path string, uid uint32) *memTrustFS {
	m.nodes[path] = trustStat{Mode: fs.ModeSymlink | 0o777, UID: uid}
	return m
}

func (m *memTrustFS) Lstat(name string) (trustStat, error) {
	st, ok := m.nodes[name]
	if !ok {
		return trustStat{}, fmt.Errorf("lstat %s: %w", name, fs.ErrNotExist)
	}
	return st, nil
}

func (m *memTrustFS) ReadDir(name string) ([]string, error) {
	var out []string
	prefix := strings.TrimRight(name, "/") + "/"
	for p := range m.nodes {
		if p != name && strings.HasPrefix(p, prefix) && !strings.Contains(p[len(prefix):], "/") {
			out = append(out, p[len(prefix):])
		}
	}
	sort.Strings(out)
	return out, nil
}

// ext4Host is a healthy layout: DietPi on the ext4 root with real permissions.
func ext4Host() *memTrustFS {
	m := newMemTrustFS()
	m.dir("/", 0o755, 0).dir("/boot", 0o755, 0).dir("/run", 0o755, 0).dir("/var", 0o755, 0)
	m.nodes["/var/tmp"] = trustStat{Mode: fs.ModeDir | fs.ModeSticky | 0o777, UID: 0}
	m.dir("/boot/dietpi", 0o755, 0).file("/boot/dietpi/dietpi-update", 0o755, 0).file("/boot/dietpi/.version", 0o644, 0)
	m.dir("/boot/dietpi/func", 0o755, 0).file("/boot/dietpi/func/dietpi-globals", 0o755, 0)
	m.file("/boot/dietpi.txt", 0o644, 0)
	m.dir("/run/dietpi", 0o755, 0).file("/run/dietpi/.update_available", 0o644, 0)
	m.dir("/var/tmp/dietpi", 0o755, 0).dir("/var/tmp/dietpi/logs", 0o755, 0)
	return m
}

func dietpiSpec() extraupd.ApplySpec { return extraupd.NewDietPi().Apply() }

func installedDietPi(version string) models.ExtraUpdater {
	return models.ExtraUpdater{ID: "dietpi", InstalledVersion: version}
}

func TestTrustWalk_EU_SEC004_Layouts(t *testing.T) {
	t.Run("[SEC-004] an ext4 layout with root-owned files passes", func(t *testing.T) {
		if err := trustWalk(ext4Host(), dietpiSpec(), installedDietPi("10.8.2")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[SEC-004] a vfat boot partition with umask 0777 fails like any world-writable tree", func(t *testing.T) {
		m := ext4Host()
		m.file("/boot/dietpi/dietpi-update", 0o777, 0).dir("/boot/dietpi", 0o777, 0)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil || !strings.Contains(err.Error(), "writable by group or others") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[SEC-004] a pre-migration FAT /boot with 0755 root ownership passes", func(t *testing.T) {
		m := ext4Host()
		m.dir("/boot", 0o755, 0)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[SEC-004] group-writable globals fail", func(t *testing.T) {
		m := ext4Host().file("/boot/dietpi/func/dietpi-globals", 0o775, 0)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a group-writable dietpi-globals must fail")
		}
	})
	t.Run("[SEC-004] a non-root /run/dietpi fails", func(t *testing.T) {
		m := ext4Host().dir("/run/dietpi", 0o755, 1000)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil || !strings.Contains(err.Error(), "uid 1000") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[SEC-004] a non-root flag file under a root-owned /run/dietpi fails", func(t *testing.T) {
		m := ext4Host().file("/run/dietpi/.update_available", 0o644, 1000)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a forged flag file must fail")
		}
	})
	t.Run("[SEC-004] a non-root entry in /var/tmp/dietpi fails", func(t *testing.T) {
		m := ext4Host().file("/var/tmp/dietpi/logs/dietpi-update.log", 0o644, 1000)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("riot-planted content under /var/tmp/dietpi must fail")
		}
	})
	t.Run("[SEC-004] /var/tmp/dietpi absent with installed 9.20.1 fails because Patch_10_4 is pending", func(t *testing.T) {
		m := ext4Host()
		delete(m.nodes, "/var/tmp/dietpi")
		delete(m.nodes, "/var/tmp/dietpi/logs")
		if err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1")); err == nil {
			t.Fatal("a missing /var/tmp/dietpi must fail while the migration is pending")
		}
	})
	t.Run("[SEC-004] /var/tmp/dietpi absent with installed 10.8.2 passes", func(t *testing.T) {
		m := ext4Host()
		delete(m.nodes, "/var/tmp/dietpi")
		delete(m.nodes, "/var/tmp/dietpi/logs")
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[SEC-004] 4097 entries fail", func(t *testing.T) {
		m := ext4Host()
		for i := 0; i < 4100; i++ {
			m.file(fmt.Sprintf("/boot/dietpi/f%04d", i), 0o644, 0)
		}
		err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2"))
		if err == nil || !strings.Contains(err.Error(), "4096 entries") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[SEC-004] exactly 4096 entries pass", func(t *testing.T) {
		m := ext4Host()
		base := 0
		for p := range m.nodes {
			if strings.HasPrefix(p, "/boot/dietpi") || strings.HasPrefix(p, "/run/dietpi") || strings.HasPrefix(p, "/var/tmp/dietpi") {
				base++
			}
		}
		// /boot/dietpi.txt is also a trust root and counts once.
		for i := 0; base < 4096; i++ {
			m.file(fmt.Sprintf("/boot/dietpi/f%04d", i), 0o644, 0)
			base++
		}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[SEC-004] a tree deeper than 4 levels fails", func(t *testing.T) {
		m := ext4Host()
		path := "/boot/dietpi"
		for _, name := range []string{"a", "b", "c", "d", "e"} {
			path += "/" + name
			m.dir(path, 0o755, 0)
		}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil || !strings.Contains(err.Error(), "deeper") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[SEC-004] a tree exactly 4 levels deep passes", func(t *testing.T) {
		m := ext4Host()
		path := "/boot/dietpi"
		for _, name := range []string{"a", "b", "c", "d"} {
			path += "/" + name
			m.dir(path, 0o755, 0)
		}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[SEC-004] a world-writable ancestor fails, a sticky root-owned /var/tmp does not", func(t *testing.T) {
		m := ext4Host().dir("/boot", 0o777, 0)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a world-writable /boot must fail")
		}
		m = ext4Host()
		m.nodes["/var/tmp"] = trustStat{Mode: fs.ModeDir | fs.ModeSticky | 0o777, UID: 1000}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a non-root sticky /var/tmp must fail")
		}
		m = ext4Host()
		m.nodes["/var/tmp"] = trustStat{Mode: fs.ModeDir | 0o777, UID: 0}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a world-writable /var/tmp without the sticky bit must fail")
		}
	})
	t.Run("[SEC-004] sticky is not accepted inside a trust tree", func(t *testing.T) {
		m := ext4Host()
		m.nodes["/var/tmp/dietpi/logs"] = trustStat{Mode: fs.ModeDir | fs.ModeSticky | 0o777, UID: 0}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a world-writable directory inside the tree must fail even when sticky")
		}
	})
	t.Run("[SEC-004] only root-owned symlinks are allowed", func(t *testing.T) {
		m := ext4Host().symlink("/boot/dietpi/link", 0)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err != nil {
			t.Fatalf("a root-owned symlink must pass: %v", err)
		}
		m.symlink("/boot/dietpi/evil", 1000)
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a riot-owned symlink must fail")
		}
	})
	t.Run("[SEC-004] a missing /boot or /run fails, a missing optional root does not", func(t *testing.T) {
		m := ext4Host()
		delete(m.nodes, "/run")
		delete(m.nodes, "/run/dietpi")
		delete(m.nodes, "/run/dietpi/.update_available")
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("a missing /run must fail")
		}
		m = ext4Host()
		delete(m.nodes, "/run/dietpi")
		delete(m.nodes, "/run/dietpi/.update_available")
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err != nil {
			t.Fatalf("an absent /run/dietpi is optional: %v", err)
		}
	})
	t.Run("[SEC-004] a read error fails closed", func(t *testing.T) {
		if err := trustWalk(errFS{ext4Host()}, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
			t.Fatal("an unreadable directory must fail")
		}
	})
}

type errFS struct{ *memTrustFS }

func (errFS) ReadDir(string) ([]string, error) { return nil, errors.New("input/output error") }
