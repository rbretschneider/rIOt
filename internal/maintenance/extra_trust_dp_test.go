package maintenance

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

func TestTrustWalk_DP_AC001_StockDietPiPasses(t *testing.T) {
	for _, version := range []string{"9.20.1", "10.8.2"} {
		t.Run("[AC-001] the stock PiCUPS layout passes for installed "+version, func(t *testing.T) {
			if err := trustWalk(stockDietPi9(), dietpiSpec(), installedDietPi(version)); err != nil {
				t.Fatalf("a stock DietPi host must pass: %v", err)
			}
		})
	}
	t.Run("[AC-001] /run/dietpi at 0777 root, a 0664 mirror file and a uid 1000 mirror directory are the very things the walk used to refuse", func(t *testing.T) {
		m := stockDietPi9()
		if st := m.nodes["/run/dietpi"]; st.Mode.Perm() != 0o777 || st.Mode&fs.ModeSticky != 0 || st.UID != 0 {
			t.Fatalf("fixture drifted from F-1: %+v", st)
		}
		if st := m.nodes["/var/tmp/dietpi/logs/dietpi-ramlog_store/wtmp"]; st.Mode.Perm() != 0o664 {
			t.Fatalf("fixture drifted from F-6: %+v", st)
		}
		if st := m.nodes["/var/tmp/dietpi/logs/dietpi-ramlog_store/cups"]; st.UID != 1000 {
			t.Fatalf("fixture drifted from F-6: %+v", st)
		}
	})
	t.Run("[AC-001] /run itself stays a checked ancestor (FR-005)", func(t *testing.T) {
		for name, mutate := range map[string]func(*memTrustFS){
			"owned by uid 1000": func(m *memTrustFS) { m.dir("/run", 0o755, 1000) },
			"group writable":    func(m *memTrustFS) { m.dir("/run", 0o775, 0) },
		} {
			m := stockDietPi9()
			mutate(m)
			if err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1")); err == nil {
				t.Errorf("/run %s must still fail the walk", name)
			}
		}
	})
	t.Run("[AC-001] the walk does not read /run/dietpi or the contents of logs/", func(t *testing.T) {
		spy := &spyTrustFS{inner: stockDietPi9()}
		if err := trustWalk(spy, dietpiSpec(), installedDietPi("9.20.1")); err != nil {
			t.Fatal(err)
		}
		for _, p := range append(append([]string(nil), spy.lstats...), spy.readDirs...) {
			if strings.HasPrefix(p, "/run/dietpi") {
				t.Errorf("the walk touched %s", p)
			}
			if strings.HasPrefix(p, "/var/tmp/dietpi/logs/") {
				t.Errorf("the walk looked below logs/: %s", p)
			}
		}
		for _, p := range spy.readDirs {
			if strings.HasPrefix(p, "/var/tmp/dietpi") {
				t.Errorf("ReadDir on %s: exact roots are lstat-only", p)
			}
		}
	})
}

// spyTrustFS records every call the walk makes.
type spyTrustFS struct {
	inner    trustFS
	lstats   []string
	readDirs []string
}

func (s *spyTrustFS) Lstat(name string) (trustStat, error) {
	s.lstats = append(s.lstats, name)
	return s.inner.Lstat(name)
}
func (s *spyTrustFS) ReadDir(name string) ([]string, error) {
	s.readDirs = append(s.readDirs, name)
	return s.inner.ReadDir(name)
}

func TestTrustWalk_DP_AC002_BootTreeStillStrict(t *testing.T) {
	cases := map[string]func(*memTrustFS){
		"a group-writable file under /boot/dietpi":    func(m *memTrustFS) { m.file("/boot/dietpi/func/dietpi-globals", 0o664, 0) },
		"a world-writable file under /boot/dietpi":    func(m *memTrustFS) { m.file("/boot/dietpi/dietpi-update", 0o666, 0) },
		"a non-root file under /boot/dietpi":          func(m *memTrustFS) { m.file("/boot/dietpi/func/dietpi-globals", 0o755, 1000) },
		"a non-root subdirectory of /boot/dietpi":     func(m *memTrustFS) { m.dir("/boot/dietpi/func", 0o755, 1000) },
		"a group-writable /boot/dietpi.txt":           func(m *memTrustFS) { m.file("/boot/dietpi.txt", 0o664, 0) },
		"a non-root /boot/dietpi.txt":                 func(m *memTrustFS) { m.file("/boot/dietpi.txt", 0o644, 1000) },
		"a group-writable /boot (ancestor)":           func(m *memTrustFS) { m.dir("/boot", 0o775, 0) },
		"a non-root /boot (ancestor)":                 func(m *memTrustFS) { m.dir("/boot", 0o755, 1000) },
		"a world-writable /boot/dietpi directory":     func(m *memTrustFS) { m.dir("/boot/dietpi", 0o777, 0) },
		"a non-root symlink under /boot/dietpi":       func(m *memTrustFS) { m.symlink("/boot/dietpi/evil", 1000) },
		"a world-writable / (ancestor of every root)": func(m *memTrustFS) { m.dir("/", 0o777, 0) },
	}
	for name, mutate := range cases {
		t.Run("[AC-002] "+name+" fails with a path and the reason", func(t *testing.T) {
			m := stockDietPi9()
			mutate(m)
			err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1"))
			if err == nil {
				t.Fatal("must fail")
			}
			if !strings.Contains(err.Error(), "writable by group or others") && !strings.Contains(err.Error(), "not root") && !strings.Contains(err.Error(), "uid ") {
				t.Errorf("err = %q, want an ownership or write-bit reason", err)
			}
		})
	}
}

func TestTrustWalk_DP_AC003_VarTmpExactPaths(t *testing.T) {
	const reinstalls = "/var/tmp/dietpi/dietpi-update_reinstalls"
	failing := map[string]func(*memTrustFS){
		"/var/tmp/dietpi owned by uid 1000": func(m *memTrustFS) { m.dir("/var/tmp/dietpi", 0o755, 1000) },
		"/var/tmp/dietpi at 0777":           func(m *memTrustFS) { m.dir("/var/tmp/dietpi", 0o777, 0) },
		"/var/tmp/dietpi at 1777 (sticky)": func(m *memTrustFS) {
			m.nodes["/var/tmp/dietpi"] = trustStat{Mode: fs.ModeDir | fs.ModeSticky | 0o777, UID: 0}
		},
		"/var/tmp/dietpi group writable": func(m *memTrustFS) { m.dir("/var/tmp/dietpi", 0o775, 0) },
		"/var/tmp/dietpi a symlink":      func(m *memTrustFS) { m.symlink("/var/tmp/dietpi", 0) },
		"/var/tmp/dietpi a regular file": func(m *memTrustFS) { m.file("/var/tmp/dietpi", 0o644, 0) },
		"logs owned by uid 1000":         func(m *memTrustFS) { m.dir("/var/tmp/dietpi/logs", 0o755, 1000) },
		"logs at 0777":                   func(m *memTrustFS) { m.dir("/var/tmp/dietpi/logs", 0o777, 0) },
		"logs at 1777 (sticky)": func(m *memTrustFS) {
			m.nodes["/var/tmp/dietpi/logs"] = trustStat{Mode: fs.ModeDir | fs.ModeSticky | 0o777, UID: 0}
		},
		"logs a symlink":                       func(m *memTrustFS) { m.symlink("/var/tmp/dietpi/logs", 0) },
		"logs a regular file":                  func(m *memTrustFS) { m.file("/var/tmp/dietpi/logs", 0o644, 0) },
		"reinstalls owned by uid 1000":         func(m *memTrustFS) { m.file(reinstalls, 0o644, 1000) },
		"reinstalls at 0666":                   func(m *memTrustFS) { m.file(reinstalls, 0o666, 0) },
		"reinstalls group writable":            func(m *memTrustFS) { m.file(reinstalls, 0o664, 0) },
		"reinstalls a symlink":                 func(m *memTrustFS) { m.symlink(reinstalls, 0) },
		"reinstalls hard linked":               func(m *memTrustFS) { m.links(reinstalls, 2) },
		"reinstalls a directory":               func(m *memTrustFS) { m.dir(reinstalls, 0o755, 0) },
		"an ancestor /var/tmp not sticky 0777": func(m *memTrustFS) { m.dir("/var/tmp", 0o777, 0) },
	}
	for name, mutate := range failing {
		t.Run("[AC-003] "+name+" fails the walk for installed 9.20.1", func(t *testing.T) {
			m := stockDietPi9()
			mutate(m)
			if err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1")); err == nil {
				t.Fatal("must fail")
			}
		})
	}
	t.Run("[AC-003] the same failures apply while installed 10.8.2 is current, because the paths exist", func(t *testing.T) {
		for name, mutate := range failing {
			m := stockDietPi9()
			mutate(m)
			if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err == nil {
				t.Errorf("%s: must fail", name)
			}
		}
	})
	t.Run("[AC-003] /var/tmp/dietpi absent with 9.20.1 installed fails (D-10)", func(t *testing.T) {
		m := stockDietPi9()
		for p := range m.nodes {
			if strings.HasPrefix(p, "/var/tmp/dietpi") {
				delete(m.nodes, p)
			}
		}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1")); err == nil || !strings.Contains(err.Error(), "/var/tmp/dietpi") {
			t.Fatalf("err = %v, want a failure naming /var/tmp/dietpi", err)
		}
	})
	t.Run("[AC-003] /var/tmp/dietpi absent with 10.8.2 installed passes", func(t *testing.T) {
		m := stockDietPi9()
		for p := range m.nodes {
			if strings.HasPrefix(p, "/var/tmp/dietpi") {
				delete(m.nodes, p)
			}
		}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("10.8.2")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("[AC-003] logs and the reinstalls file are optional", func(t *testing.T) {
		m := stockDietPi9()
		delete(m.nodes, reinstalls)
		for p := range m.nodes {
			if strings.HasPrefix(p, "/var/tmp/dietpi/logs") {
				delete(m.nodes, p)
			}
		}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1")); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("[AC-003] a huge or deep mirror below logs/ costs nothing and passes", func(t *testing.T) {
		m := stockDietPi9()
		base := "/var/tmp/dietpi/logs/dietpi-ramlog_store"
		for i := 0; i < 6000; i++ {
			m.file(fmt.Sprintf("%s/f%04d", base, i), 0o666, 4242)
		}
		deep := base
		for _, d := range []string{"a", "b", "c", "d", "e", "f"} {
			deep += "/" + d
			m.dir(deep, 0o777, 4242)
		}
		if err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1")); err != nil {
			t.Fatalf("exact roots must not walk below logs/: %v", err)
		}
	})
	t.Run("[AC-003] the error names the path and the cause", func(t *testing.T) {
		m := stockDietPi9()
		m.dir("/var/tmp/dietpi/logs", 0o777, 0)
		err := trustWalk(m, dietpiSpec(), installedDietPi("9.20.1"))
		if err == nil || !strings.Contains(err.Error(), "/var/tmp/dietpi/logs is writable by group or others (mode 0777)") {
			t.Fatalf("err = %v", err)
		}
	})
}
