package extraupd

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// SC-001 / SC-004: fixtures are built in temp directories. The dietpi-update
// file is a tripwire that is never executable: mode 0644, and no test starts a
// process.
const tripwireScript = "#!/bin/sh\necho DIETPI TRIPWIRE >&2\nexit 97\n"

// hostFixture is a fake root filesystem. Every file is written with explicit
// bytes so the fixtures do not depend on the checkout's line-ending policy.
type hostFixture struct {
	t    *testing.T
	root string
}

func newHost(t *testing.T) *hostFixture {
	t.Helper()
	return &hostFixture{t: t, root: t.TempDir()}
}

func (h *hostFixture) write(rel, content string) *hostFixture {
	h.t.Helper()
	p := filepath.Join(h.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return h
}

func (h *hostFixture) mkdir(rel string) *hostFixture {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Join(h.root, filepath.FromSlash(rel)), 0o755); err != nil {
		h.t.Fatal(err)
	}
	return h
}

// fs trusts flags owned by the test's own user: a temp tree is never root-owned
// (DirFSOwnedBy is the fixture seam of FR-011).
func (h *hostFixture) fs() FS { return DirFSOwnedByCaller(h.root) }

// versionText renders a .version file the way DietPi writes it.
func versionText(core, sub, rc string, extra ...string) string {
	lines := []string{
		"G_DIETPI_VERSION_CORE=" + core,
		"G_DIETPI_VERSION_SUB=" + sub,
		"G_DIETPI_VERSION_RC=" + rc,
		"G_GITBRANCH='master'",
		"G_GITOWNER='MichaIng'",
		"G_LIVE_PATCH_STATUS[0]='not applicable'",
	}
	return strings.Join(append(lines, extra...), "\n") + "\n"
}

// dietpiHost builds a DietPi host at version core.sub.rc with
// CONFIG_CHECK_DIETPI_UPDATES=1 and a running kernel with modules present.
func dietpiHost(t *testing.T, core, sub, rc string) *hostFixture {
	t.Helper()
	h := newHost(t)
	h.write("boot/dietpi/.version", versionText(core, sub, rc))
	h.write("boot/dietpi/dietpi-update", tripwireScript)
	h.write("boot/dietpi.txt", "CONFIG_CHECK_DIETPI_UPDATES=1\nCONFIG_CHECK_APT_UPDATES=1\n")
	h.write("proc/sys/kernel/osrelease", "6.12.47+rpt-rpi-v8\n")
	h.mkdir("lib/modules/6.12.47+rpt-rpi-v8")
	return h
}

// countingFS records every read request so a test can assert the 64 KiB bound.
type countingFS struct {
	inner FS
	mu    sync.Mutex
	reads []readCall
}

type readCall struct {
	name string
	max  int64
}

func (c *countingFS) ReadFile(name string, max int64) ([]byte, error) {
	c.mu.Lock()
	c.reads = append(c.reads, readCall{name, max})
	c.mu.Unlock()
	return c.inner.ReadFile(name, max)
}

func (c *countingFS) Lstat(name string) (fs.FileInfo, error) { return c.inner.Lstat(name) }

func (c *countingFS) ReadTrustedFlag(name string, max int64) ([]byte, error) {
	c.mu.Lock()
	c.reads = append(c.reads, readCall{name, max})
	c.mu.Unlock()
	return c.inner.ReadTrustedFlag(name, max)
}

// blockingFS never returns from ReadFile until released.
type blockingFS struct {
	inner   FS
	release chan struct{}
	mu      sync.Mutex
	started int
}

func (b *blockingFS) ReadFile(name string, max int64) ([]byte, error) {
	b.mu.Lock()
	b.started++
	b.mu.Unlock()
	<-b.release
	return b.inner.ReadFile(name, max)
}

func (b *blockingFS) Lstat(name string) (fs.FileInfo, error) { return b.inner.Lstat(name) }

func (b *blockingFS) ReadTrustedFlag(name string, max int64) ([]byte, error) {
	b.mu.Lock()
	b.started++
	b.mu.Unlock()
	<-b.release
	return b.inner.ReadTrustedFlag(name, max)
}

func (b *blockingFS) startedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.started
}

func detectOne(t *testing.T, h *hostFixture) (entryStatus, bool) {
	t.Helper()
	return detectWith(t, NewDietPi(), h.fs())
}

type entryStatus struct {
	ID, Status, Reason, Installed, Available string
	Pending                                  int
	Notices                                  []string
	NoticeValues                             map[string][2]string
}

func detectWith(t *testing.T, d *DietPi, fsys FS) (entryStatus, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e, present := d.Detect(ctx, fsys)
	out := entryStatus{ID: e.ID, Status: string(e.Status), Reason: e.UnknownReason, Installed: e.InstalledVersion, Available: e.AvailableVersion, Pending: e.PendingCount, NoticeValues: map[string][2]string{}}
	for _, n := range e.Notices {
		out.Notices = append(out.Notices, n.Code)
		out.NoticeValues[n.Code] = [2]string{n.Branch, n.Owner}
	}
	return out, present
}

func (e entryStatus) hasNotice(code string) bool {
	for _, n := range e.Notices {
		if n == code {
			return true
		}
	}
	return false
}
