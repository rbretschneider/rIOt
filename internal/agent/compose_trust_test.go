package agent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// trustFx is a compose trust fixture over a map-backed filesystem. Paths are
// rooted at the platform's volume root so the same tests run on Windows and
// Linux, and nothing depends on real file ownership.
type trustFx struct {
	t       *testing.T
	fs      *fakeTrustFS
	root    string
	sock    string
	ct      *composeTrust
	snap    daemonSnapshot
	dir     string // the stack directory
	project string
	srcID   string // SourceID sent by AC-017 style requests
}

const (
	defaultPasswd = "root:x:0:0:root:/root:/bin/sh\nriot:x:1000:1000::/var/lib/riot:/bin/false\n" +
		"alice:x:1001:1001::/home/alice:/bin/bash\nmallory:x:2000:2000::/home/mallory:/bin/bash\n"
	defaultGroup = "root:x:0:\ndocker:x:999:riot\nriot:x:1000:\nalice:x:1001:\nmallory:x:2000:\nstaff:x:50:alice,mallory\n"

	uidRiot, uidAlice, uidMallory = 1000, 1001, 2000
	gidDocker, gidStaff           = 999, 50
)

func newTrustFx(t *testing.T) *trustFx {
	t.Helper()
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	x := &trustFx{t: t, fs: newFakeTrustFS(), root: root, project: "app"}
	x.dir = x.p("opt", "stacks", "app")
	x.sock = x.p("var", "run", "docker.sock")

	x.fs.chain(x.dir)
	x.fs.chain(filepath.Dir(x.sock))
	x.fs.file(x.sock, 0, gidDocker, 0660, "").mode |= os.ModeSocket
	x.fs.file(x.dir+string(filepath.Separator)+"compose.yaml", 0, 0, 0644, "services: {}\n")
	x.fs.chain("/etc")
	x.accounts(defaultPasswd, defaultGroup)

	x.ct = &composeTrust{fs: x.fs, goos: "linux", socket: x.sock}
	x.snap = daemonSnapshot{
		DataRoot:      x.p("var", "lib", "docker"),
		excluded:      map[string]string{},
		volumeDevices: map[string]string{},
	}
	x.addStackContainer("c1", "app-web-1", x.project, x.dir)
	return x
}

func (x *trustFx) p(parts ...string) string {
	return filepath.Join(append([]string{x.root}, parts...)...)
}

func (x *trustFx) accounts(passwd, group string) {
	x.fs.file("/etc/passwd", 0, 0, 0644, passwd)
	x.fs.file("/etc/group", 0, 0, 0644, group)
}

func (x *trustFx) addStackContainer(id, name, project, dir string) *container.Summary {
	x.snap.Containers = append(x.snap.Containers, container.Summary{
		ID: id, Names: []string{"/" + name},
		Labels: map[string]string{
			models.LabelComposeProject: project, models.LabelComposeService: "web", models.LabelComposeWorkingDir: dir,
		},
	})
	return &x.snap.Containers[len(x.snap.Containers)-1]
}

// writer adds a container with the given mounts.
func (x *trustFx) writer(name string, mounts ...container.MountPoint) {
	x.snap.Containers = append(x.snap.Containers, container.Summary{
		ID: "w-" + name, Names: []string{"/" + name}, State: "running", Mounts: mounts,
	})
}

func bindRW(src, dst string) container.MountPoint {
	return container.MountPoint{Type: mount.TypeBind, Source: src, Destination: dst, RW: true}
}

func (x *trustFx) file(name string, uid, gid uint32, perm os.FileMode, content string) *fakeNode {
	return x.fs.file(filepath.Join(x.dir, name), uid, gid, perm, content)
}

func (x *trustFx) dirNode() *fakeNode { return x.fs.get(x.dir) }

func (x *trustFx) check() (trustedDir, error) {
	return x.ct.Check(x.snap, trustRequest{Dir: x.dir, Project: x.project, SourceID: x.srcID})
}

func (x *trustFx) accept() trustedDir {
	x.t.Helper()
	td, err := x.check()
	require.NoError(x.t, err, "the directory must be accepted")
	return td
}

func (x *trustFx) refuse(rule string) *composeTrustError {
	x.t.Helper()
	td, err := x.check()
	require.Error(x.t, err, "expected a %s refusal, got an accepted directory %v", rule, td)
	var te *composeTrustError
	require.True(x.t, errors.As(err, &te), "refusals are *composeTrustError, got %T", err)
	assert.Equal(x.t, rule, te.Rule, "refusal rule; message: %s", te.Error())
	assert.False(x.t, td.valid(), "a refused check returns the zero trustedDir")
	return te
}

// ---------------------------------------------------------------------------
// AC-001 / AC-002 path shape
// ---------------------------------------------------------------------------

func TestComposeTrust_AC001_RelativeOrEmptyDirRefused(t *testing.T) {
	x := newTrustFx(t)
	for _, dir := range []string{"", "stack", "./stack", "../stack", "opt/stacks/app"} {
		_, err := x.ct.Check(x.snap, trustRequest{Dir: dir, Project: "app"})
		var te *composeTrustError
		require.True(t, errors.As(err, &te), "[AC-001] %q must be refused", dir)
		assert.Equal(t, ruleNotAbsolute, te.Rule, "[AC-001] %q", dir)
	}
}

func TestComposeTrust_AC002_SymlinkedDirValidatedAtResolvedPath(t *testing.T) {
	x := newTrustFx(t)
	link := x.p("srv", "link")
	x.fs.chain(x.p("srv"))
	x.fs.link(link, x.dir, 0)

	td, err := x.ct.Check(x.snap, trustRequest{Dir: link, Project: "app"})

	require.NoError(t, err, "[AC-002]")
	assert.Equal(t, x.dir, td.Path(), "[AC-002] compose runs at the resolved path, not the symlink")
	assert.Equal(t, []string{filepath.Join(x.dir, "compose.yaml")}, td.Files())
}

func TestComposeTrust_AC002_SymlinkToUntrustedTargetRefused(t *testing.T) {
	x := newTrustFx(t)
	evil := x.p("srv", "evil")
	x.fs.chain(x.p("srv"))
	x.fs.dir(evil, uidMallory, uidMallory, 0755)
	x.fs.file(filepath.Join(evil, "compose.yaml"), 0, 0, 0644, "")
	link := x.p("srv", "link")
	x.fs.link(link, evil, 0)
	x.addStackContainer("c2", "app-evil-1", "app", evil)

	_, err := x.ct.Check(x.snap, trustRequest{Dir: link, Project: "app"})

	var te *composeTrustError
	require.True(t, errors.As(err, &te), "[AC-002] a symlink whose target fails the checks is refused")
	assert.Equal(t, ruleOwner, te.Rule)
}

// ---------------------------------------------------------------------------
// AC-003 ownership
// ---------------------------------------------------------------------------

func TestComposeTrust_AC003_UntrustedOwnerRefused(t *testing.T) {
	x := newTrustFx(t)
	x.dirNode().uid = uidMallory
	te := x.refuse(ruleOwner)
	assert.Contains(t, te.Error(), "uid 2000", "[AC-003] the message carries the numeric uid")
}

func TestComposeTrust_AC003_UntrustedAncestorOwnerRefused(t *testing.T) {
	x := newTrustFx(t)
	x.fs.get(x.p("opt", "stacks")).uid = uidMallory
	x.refuse(ruleOwner)
}

func TestComposeTrust_AC003_UnknownUIDRefused(t *testing.T) {
	x := newTrustFx(t)
	x.dirNode().uid = 4242
	x.refuse(ruleOwner)
}

func TestComposeTrust_AC003_SocketUnavailableTrustsRootOnly(t *testing.T) {
	x := newTrustFx(t)
	delete(x.fs.nodes, filepath.Clean(x.sock))
	x.accounts(defaultPasswd+"bob:x:1500:1500::/home/bob:/bin/sh\n", strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,bob", 1))

	x.dirNode().uid = 1500
	x.refuse(ruleOwner) // a docker member is not trusted without a socket to prove it
	x.dirNode().uid = uidRiot
	x.accept() // root and the euid stay trusted
}

func TestComposeTrust_AC003_AccountDBUnreadableFailsClosed(t *testing.T) {
	x := newTrustFx(t)
	x.accounts(defaultPasswd+"bob:x:1500:1500::/home/bob:/bin/sh\n", strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,bob", 1))
	x.fs.get("/etc/group").readErr = errors.New("boom")

	x.dirNode().uid = 1500
	x.refuse(ruleOwner)
	x.dirNode().uid = 0
	x.accept()
}

func TestComposeTrust_AC003_MalformedPasswdFailsClosed(t *testing.T) {
	x := newTrustFx(t)
	x.accounts("root:x:0:0\nthis line is not a passwd entry\n"+"bob:x:1500:1500::/h:/sh\n", strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,bob", 1))
	x.dirNode().uid = 1500
	x.refuse(ruleOwner)
}

// ---------------------------------------------------------------------------
// AC-004 mode and sticky
// ---------------------------------------------------------------------------

func TestComposeTrust_AC004_WorldWritableDirRefused(t *testing.T) {
	x := newTrustFx(t)
	x.dirNode().mode = os.ModeDir | 0777
	te := x.refuse(ruleWorldWritable)
	assert.Contains(t, te.Error(), "0777", "[AC-004] the message carries the octal mode")
}

func TestComposeTrust_AC004_StickyLeafStillRefused(t *testing.T) {
	x := newTrustFx(t)
	x.dirNode().mode = os.ModeDir | os.ModeSticky | 0777
	x.refuse(ruleWorldWritable)
}

func TestComposeTrust_AC004_WorldWritableStickyAncestorAccepted(t *testing.T) {
	x := newTrustFx(t)
	x.fs.get(x.p("opt", "stacks")).mode = os.ModeDir | os.ModeSticky | 0777
	x.accept()
}

func TestComposeTrust_AC004_WorldWritableNonStickyAncestorRefused(t *testing.T) {
	x := newTrustFx(t)
	x.fs.get(x.p("opt", "stacks")).mode = os.ModeDir | 0777
	x.refuse(ruleWorldWritable)
}

func TestComposeTrust_AC004_GroupWritableUntrustedGroupRefused(t *testing.T) {
	x := newTrustFx(t)
	n := x.dirNode()
	n.mode, n.gid = os.ModeDir|0775, gidStaff
	x.refuse(ruleGroupWritable)
}

func TestComposeTrust_AC004_GroupWritableSocketGroupAccepted(t *testing.T) {
	x := newTrustFx(t)
	n := x.dirNode()
	n.mode, n.gid = os.ModeDir|0775, gidDocker
	x.accept()
}

// ---------------------------------------------------------------------------
// AC-005 trusted accounts
// ---------------------------------------------------------------------------

func TestComposeTrust_AC005_RootOwnedAccepted(t *testing.T) {
	x := newTrustFx(t)
	td := x.accept()
	assert.Equal(t, x.dir, td.Path(), "[AC-005]")
}

func TestComposeTrust_AC005_SupplementarySocketMemberAccepted(t *testing.T) {
	x := newTrustFx(t)
	x.accounts(defaultPasswd+"bob:x:1500:1500::/home/bob:/bin/sh\n", strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,bob", 1))
	x.dirNode().uid = 1500
	x.file("compose.yaml", 1500, 0, 0644, "")
	x.accept()
}

func TestComposeTrust_AC005_PrimaryGroupSocketMemberAccepted(t *testing.T) {
	x := newTrustFx(t)
	x.accounts(defaultPasswd+"carol:x:1600:999::/home/carol:/bin/sh\n", defaultGroup)
	x.dirNode().uid = 1600
	x.file("compose.yaml", 1600, 0, 0644, "")
	x.accept()
}

func TestComposeTrust_AC005_SocketOwnerAccepted(t *testing.T) {
	x := newTrustFx(t)
	x.fs.get(x.sock).uid = 7
	x.dirNode().uid = 7
	x.file("compose.yaml", 7, 0, 0644, "")
	x.accept()
}

// ---------------------------------------------------------------------------
// AC-006 compose files must be in the directory
// ---------------------------------------------------------------------------

func TestComposeTrust_AC006_NoDefaultFileNoComposeFileRefused(t *testing.T) {
	x := newTrustFx(t)
	delete(x.fs.nodes, filepath.Join(x.dir, "compose.yaml"))
	x.refuse(ruleNoComposeFile)
}

func TestComposeTrust_AC006_OverrideOnlyRefused(t *testing.T) {
	x := newTrustFx(t)
	delete(x.fs.nodes, filepath.Join(x.dir, "compose.yaml"))
	x.file("compose.override.yml", 0, 0, 0644, "")
	x.refuse(ruleNoComposeFile)
}

func TestComposeTrust_AC006_ComposeFileWithoutDefaultFileAccepted(t *testing.T) {
	x := newTrustFx(t)
	delete(x.fs.nodes, filepath.Join(x.dir, "compose.yaml"))
	x.file("base.yml", 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "COMPOSE_FILE=base.yml\n")
	td := x.accept()
	assert.Equal(t, []string{filepath.Join(x.dir, "base.yml")}, td.Files(), "[AC-006]")
}

// ---------------------------------------------------------------------------
// AC-007 file rules
// ---------------------------------------------------------------------------

func TestComposeTrust_AC007_UntrustedOwnedFileRefused(t *testing.T) {
	names := append(append(append([]string(nil), composeDefaultNames...), composeOverrideNames...), ".env")
	require.Len(t, names, 9)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			x := newTrustFx(t)
			x.file(name, uidMallory, uidMallory, 0644, "")
			te := x.refuse(ruleOwner)
			assert.Contains(t, te.Error(), name, "[AC-007] the message names the file")
		})
	}
}

func TestComposeTrust_AC007_UntrustedWritableFileRefused(t *testing.T) {
	x := newTrustFx(t)
	x.file("compose.override.yml", 0, 0, 0666, "")
	x.refuse(ruleWorldWritable)

	y := newTrustFx(t)
	y.file("compose.override.yml", 0, gidStaff, 0664, "")
	y.refuse(ruleGroupWritable)
}

func TestComposeTrust_AC007_FileSymlinkEscapeRefused(t *testing.T) {
	x := newTrustFx(t)
	elsewhere := x.p("srv", "elsewhere")
	x.fs.chain(elsewhere)
	x.fs.file(filepath.Join(elsewhere, "c.yml"), 0, 0, 0644, "")
	delete(x.fs.nodes, filepath.Join(x.dir, "compose.yaml"))
	x.fs.link(filepath.Join(x.dir, "compose.yaml"), filepath.Join(elsewhere, "c.yml"), 0)
	x.refuse(ruleSymlinkEscape)
}

func TestComposeTrust_AC007_FileSymlinkInsideAccepted(t *testing.T) {
	x := newTrustFx(t)
	delete(x.fs.nodes, filepath.Join(x.dir, "compose.yaml"))
	x.file("real.yml", 0, 0, 0644, "")
	x.fs.link(filepath.Join(x.dir, "compose.yaml"), filepath.Join(x.dir, "real.yml"), 0)
	td := x.accept()
	assert.Equal(t, []string{filepath.Join(x.dir, "real.yml")}, td.Files(), "[AC-007] the resolved target is what compose loads")
}

func TestComposeTrust_AC007_NonRegularFileRefused(t *testing.T) {
	x := newTrustFx(t)
	x.fs.dir(filepath.Join(x.dir, ".env"), 0, 0, 0755)
	x.refuse(ruleNotRegular)
}

// ---------------------------------------------------------------------------
// AC-008 compose roots
// ---------------------------------------------------------------------------

func TestComposeTrust_AC008_OutsideRootsRefused(t *testing.T) {
	x := newTrustFx(t)
	x.fs.chain(x.p("srv", "stacks"))
	x.ct.roots = []string{x.p("srv", "stacks")}
	x.refuse(ruleOutsideRoots)
}

func TestComposeTrust_AC008_InsideRootAccepted(t *testing.T) {
	x := newTrustFx(t)
	x.ct.roots = []string{x.p("opt", "stacks")}
	x.accept()
}

func TestComposeTrust_AC008_RootsUnsetNotEnforced(t *testing.T) {
	x := newTrustFx(t)
	x.ct.roots = nil
	x.accept()
}

func TestComposeTrust_AC008_SymlinkedRootResolved(t *testing.T) {
	x := newTrustFx(t)
	x.fs.chain(x.p("srv"))
	x.fs.link(x.p("srv", "rootlink"), x.p("opt", "stacks"), 0)
	x.ct.roots = []string{x.p("srv", "rootlink")}
	x.accept()
}

func TestComposeTrust_AC008_InvalidRootsFailClosed(t *testing.T) {
	x := newTrustFx(t)
	x.ct.rootsErr = errors.New("bad root")
	x.refuse(ruleRootsInvalid)
}

func TestComposeTrust_AC008_RootPrefixIsSeparatorAware(t *testing.T) {
	assert.False(t, within(filepath.Join("/", "opt", "stacks"), filepath.Join("/", "opt", "stacks-x"), false),
		"[AC-008] a sibling that shares a prefix is not inside the root")
	assert.True(t, within(filepath.Join("/", "opt", "stacks"), filepath.Join("/", "opt", "stacks", "..foo"), false),
		"[AC-008] a name that merely starts with .. is inside the root")
	assert.True(t, within(filepath.Join("/", "opt", "stacks"), filepath.Join("/", "opt", "stacks"), false), "the root itself")
	assert.False(t, within(filepath.Join("/", "opt", "stacks"), filepath.Join("/", "opt"), false), "the parent of the root")

	x := newTrustFx(t)
	sibling := x.p("opt", "stacks-x", "app")
	x.fs.chain(sibling)
	x.fs.file(filepath.Join(sibling, "compose.yaml"), 0, 0, 0644, "")
	x.addStackContainer("c9", "app-x-1", "app", sibling)
	x.ct.roots = []string{x.p("opt", "stacks")}
	_, err := x.ct.Check(x.snap, trustRequest{Dir: sibling, Project: "app"})
	var te *composeTrustError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, ruleOutsideRoots, te.Rule, "[AC-008] /opt/stacks-x is outside /opt/stacks")
}

// ---------------------------------------------------------------------------
// AC-017 / AC-027 messages
// ---------------------------------------------------------------------------

func TestComposeTrust_AC017_MessageNamesPathAndRule(t *testing.T) {
	type tc struct {
		rule  string
		setup func(x *trustFx)
		dir   func(x *trustFx) string
	}
	cases := []tc{
		{ruleNotAbsolute, nil, func(*trustFx) string { return "rel/dir" }},
		{ruleWindowsRoots, func(x *trustFx) { x.ct.goos = "windows" }, nil},
		{ruleRootsInvalid, func(x *trustFx) { x.ct.rootsErr = errors.New("x") }, nil},
		{ruleResolve, func(x *trustFx) { delete(x.fs.nodes, filepath.Clean(x.dir)) }, nil},
		{ruleNotDirectory, func(x *trustFx) { x.dirNode().mode = 0644 }, nil},
		{ruleOutsideRoots, func(x *trustFx) { x.ct.roots = []string{x.p("nowhere")} }, nil},
		{ruleInvalidProject, func(x *trustFx) { x.project = "Bad Project" }, nil},
		{ruleProjectNotFound, func(x *trustFx) { x.snap.Containers = nil }, nil},
		{ruleAmbiguousProject, func(x *trustFx) { x.addStackContainer("c2", "o-1", "other", x.dir) }, nil},
		{ruleProjectMismatch, func(x *trustFx) { x.project = "other" }, nil},
		{ruleLabelProvenance, func(x *trustFx) { x.snap.excluded["c1"] = excludedImageLabel; x.srcID = "c1" }, nil},
		{ruleDockerDataRoot, func(x *trustFx) { x.snap.DataRoot = x.p("opt") }, nil},
		{ruleContainerWritable, func(x *trustFx) { x.writer("w", bindRW(x.dir, "/d")) }, nil},
		{ruleOwner, func(x *trustFx) { x.dirNode().uid = uidMallory }, nil},
		{ruleGroupWritable, func(x *trustFx) { x.dirNode().mode, x.dirNode().gid = os.ModeDir|0775, gidStaff }, nil},
		{ruleACL, func(x *trustFx) {
			x.dirNode().mode, x.dirNode().gid, x.dirNode().acl = os.ModeDir|0775, gidDocker, true
		}, nil},
		{ruleWorldWritable, func(x *trustFx) { x.dirNode().mode = os.ModeDir | 0777 }, nil},
		{ruleSymlinkEscape, func(x *trustFx) {
			x.fs.chain(x.p("srv"))
			x.fs.file(x.p("srv", "c.yml"), 0, 0, 0644, "")
			x.fs.link(filepath.Join(x.dir, "compose.yml"), x.p("srv", "c.yml"), 0)
		}, nil},
		{ruleNotRegular, func(x *trustFx) { x.fs.dir(filepath.Join(x.dir, ".env"), 0, 0, 0755) }, nil},
		{ruleNoComposeFile, func(x *trustFx) { delete(x.fs.nodes, filepath.Join(x.dir, "compose.yaml")) }, nil},
		{ruleEnvUnreadable, func(x *trustFx) { x.file(".env", 0, 0, 0644, "").readErr = os.ErrPermission }, nil},
		{ruleEnvUnparsed, func(x *trustFx) { x.file(".env", 0, 0, 0644, "COMPOSE_FILE=$X\n") }, nil},
		{ruleStat, func(x *trustFx) { x.fs.statErr[filepath.Clean(x.p("opt"))] = errors.New("EIO") }, nil},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			x := newTrustFx(t)
			if c.setup != nil {
				c.setup(x)
			}
			dir := x.dir
			if c.dir != nil {
				dir = c.dir(x)
			}
			_, err := x.ct.Check(x.snap, trustRequest{Dir: dir, Project: x.project, SourceID: x.srcID})
			var te *composeTrustError
			require.True(t, errors.As(err, &te), "[AC-017] %s: expected a refusal, got %v", c.rule, err)
			require.Equal(t, c.rule, te.Rule, "[AC-017] %s: %s", c.rule, te.Error())
			msg := err.Error()
			assert.Contains(t, msg, "("+c.rule+")", "[AC-017] the message names the rule")
			assert.Contains(t, msg, strings.TrimSuffix(strings.TrimPrefix(quoteForTest(dir), `"`), `"`), "[AC-017] the message names the path")
			assert.LessOrEqual(t, len(msg), maxRefusalBytes, "[AC-017] the message is bounded")
		})
	}
}

func quoteForTest(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`)
}

func TestComposeTrust_AC027_ControlCharsQuotedAndErrnoCollapsed(t *testing.T) {
	x := newTrustFx(t)
	evil := x.dir + "\nINJECTED\x1b[31m\x00"
	_, err := x.ct.Check(x.snap, trustRequest{Dir: evil, Project: "app"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "\n", "[AC-027] a newline in a label is quoted")
	assert.NotContains(t, err.Error(), "\x1b", "[AC-027] an escape character is quoted")
	assert.Contains(t, err.Error(), `\n`, "[AC-027] shown in escaped form")

	// A raw errno never reaches the message: Lstat failures collapse to "cannot stat".
	x2 := newTrustFx(t)
	x2.fs.statErr[filepath.Clean(x2.p("opt", "stacks"))] = syscall.EACCES
	_, err = x2.check()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot stat", "[AC-027]")
	assert.NotContains(t, strings.ToLower(err.Error()), "permission denied", "[AC-027] no raw errno text")
	assert.NotContains(t, strings.ToLower(err.Error()), "access", "[AC-027]")

	// Over-long label values are cut, whatever their length.
	long := x.p(strings.Repeat("a", 5000))
	_, err = x.ct.Check(x.snap, trustRequest{Dir: long, Project: "app"})
	require.Error(t, err)
	assert.LessOrEqual(t, len(err.Error()), maxRefusalBytes, "[AC-027] bounded message")
}

// ---------------------------------------------------------------------------
// AC-028 component types
// ---------------------------------------------------------------------------

func TestComposeTrust_AC028_SymlinkComponentRefused(t *testing.T) {
	x := newTrustFx(t)
	// The resolver (wrongly) returns a path through a symlink: the walk still
	// refuses it instead of assuming it cannot happen.
	x.fs.link(x.p("opt", "stacks", "hop"), x.dir, 0)
	x.ct.fs = &lyingResolver{fakeTrustFS: x.fs, from: x.dir, answer: x.p("opt", "stacks", "hop")}
	x.refuse(ruleNotDirectory)
}

func TestComposeTrust_AC028_NonDirectoryComponentRefused(t *testing.T) {
	x := newTrustFx(t)
	x.fs.get(x.p("opt", "stacks")).mode = 0644
	x.refuse(ruleNotDirectory)
}

// ---------------------------------------------------------------------------
// AC-019 container-writable locations
// ---------------------------------------------------------------------------

func TestComposeTrust_AC019_DirUnderRWBindSourceRefused(t *testing.T) {
	x := newTrustFx(t)
	x.writer("appdata-writer", bindRW(x.p("opt", "stacks"), "/data"))
	te := x.refuse(ruleContainerWritable)
	assert.Contains(t, te.Error(), "appdata-writer", "[AC-019] the container is named")
	assert.Contains(t, te.Error(), "/data", "[AC-019] the mount is named")
}

func TestComposeTrust_AC019_DirEqualsRWBindSourceRefused(t *testing.T) {
	x := newTrustFx(t)
	x.writer("dot-mount", bindRW(x.dir, "/app")) // `.:/app`
	x.refuse(ruleContainerWritable)
}

func TestComposeTrust_AC019_StackDirAncestorOfBindSourceAccepted(t *testing.T) {
	x := newTrustFx(t)
	x.writer("config-mount", bindRW(filepath.Join(x.dir, "config"), "/config")) // `./config:/config`
	x.accept()
}

func TestComposeTrust_AC019_ReadOnlyBindIgnored(t *testing.T) {
	x := newTrustFx(t)
	x.writer("ro", container.MountPoint{Type: mount.TypeBind, Source: x.dir, Destination: "/app", RW: false})
	x.accept()
}

func TestComposeTrust_AC019_StoppedContainerMountCounts(t *testing.T) {
	x := newTrustFx(t)
	x.writer("stopped", bindRW(x.dir, "/app"))
	x.snap.Containers[len(x.snap.Containers)-1].State = "exited"
	x.refuse(ruleContainerWritable)
}

func TestComposeTrust_AC019_VolumeMountpointRefused(t *testing.T) {
	x := newTrustFx(t)
	mp := x.p("srv", "vols", "v1", "_data")
	x.writer("vol", container.MountPoint{Type: mount.TypeVolume, Name: "v1", Source: mp, Destination: "/v", RW: true})
	x.snap.DataRoot = x.p("nowhere")
	x.ct.roots = nil
	x.dir = filepath.Join(mp, "stack")
	x.fs.chain(x.dir)
	x.fs.file(filepath.Join(x.dir, "compose.yaml"), 0, 0, 0644, "")
	x.snap.Containers[0].Labels[models.LabelComposeWorkingDir] = x.dir
	x.refuse(ruleContainerWritable)
}

func TestComposeTrust_AC019_BindDeviceVolumeRefused(t *testing.T) {
	x := newTrustFx(t)
	x.snap.volumeDevices["v1"] = x.p("opt", "stacks")
	x.writer("dev", container.MountPoint{Type: mount.TypeVolume, Name: "v1", Source: x.p("var", "lib", "docker", "volumes", "v1", "_data"), Destination: "/v", RW: true})
	x.refuse(ruleContainerWritable)
}

func TestComposeTrust_AC019_DockerDataRootRefused(t *testing.T) {
	x := newTrustFx(t)
	x.snap.DataRoot = x.p("opt", "stacks")
	te := x.refuse(ruleDockerDataRoot)
	assert.Contains(t, te.Error(), "data root")
}

func TestComposeTrust_AC019_SocketMountingContainerExempt(t *testing.T) {
	x := newTrustFx(t)
	x.writer("stack-manager", bindRW(x.p("opt", "stacks"), "/stacks"), bindRW(x.sock, "/var/run/docker.sock"))
	x.accept()
}

func TestComposeTrust_AC019_ComposeFileEntryUnderBindRefused(t *testing.T) {
	x := newTrustFx(t)
	shared := x.p("srv", "shared")
	x.fs.chain(shared)
	x.fs.file(filepath.Join(shared, "base.yml"), 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "COMPOSE_FILE=compose.yaml|"+x.relTo(filepath.Join(shared, "base.yml"))+"\nCOMPOSE_PATH_SEPARATOR=|\n")
	x.writer("w", bindRW(shared, "/shared"))
	te := x.refuse(ruleContainerWritable)
	assert.Contains(t, te.Error(), "base.yml")
}

func TestComposeTrust_AC019_UnresolvableMountSourceRefused(t *testing.T) {
	x := newTrustFx(t)
	src := x.p("srv", "hidden", "data")
	x.fs.evalErr[filepath.Clean(src)] = syscall.EACCES
	x.writer("w", bindRW(src, "/d"))
	te := x.refuse(ruleContainerWritable)
	assert.Contains(t, te.Error(), "cannot resolve", "[AC-019] the message reports an unresolvable source, with no lexical fallback")
}

func TestComposeTrust_AC019_NonexistentMountSourceFallsBackToLexical(t *testing.T) {
	x := newTrustFx(t)
	x.writer("w", bindRW(x.p("srv", "not-yet-created"), "/d")) // ENOENT: compared lexically
	x.accept()
}

// ---------------------------------------------------------------------------
// AC-020 explicit -f set and .env
// ---------------------------------------------------------------------------

func TestComposeTrust_AC020_DefaultAndOverrideBecomeExplicitF(t *testing.T) {
	x := newTrustFx(t)
	x.file("compose.yml", 0, 0, 0644, "") // later in compose's order: not loaded
	x.file("docker-compose.yml", 0, 0, 0644, "")
	x.file("compose.override.yaml", 0, 0, 0644, "")
	x.file("docker-compose.override.yml", 0, 0, 0644, "")
	td := x.accept()
	assert.Equal(t, []string{filepath.Join(x.dir, "compose.yaml"), filepath.Join(x.dir, "compose.override.yaml")}, td.Files(),
		"[AC-020] the first default name and the first override name, in compose's own order")

	y := newTrustFx(t)
	delete(y.fs.nodes, filepath.Join(y.dir, "compose.yaml"))
	y.file("docker-compose.yaml", 0, 0, 0644, "")
	y.file("docker-compose.yml", 0, 0, 0644, "")
	assert.Equal(t, []string{filepath.Join(y.dir, "docker-compose.yml")}, y.accept().Files(),
		"[AC-020] compose prefers docker-compose.yml over docker-compose.yaml (verified on compose 2.26.1 and 2.40.3)")
}

func TestComposeTrust_AC020_ComposeFileEntriesValidatedInOrder(t *testing.T) {
	x := newTrustFx(t)
	x.file("a.yml", 0, 0, 0644, "")
	x.file("b.yml", 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "# comment\nFOO=bar\nCOMPOSE_FILE=b.yml"+x.listSep()+"compose.yaml"+x.listSep()+"a.yml\n")
	td := x.accept()
	assert.Equal(t, []string{filepath.Join(x.dir, "b.yml"), filepath.Join(x.dir, "compose.yaml"), filepath.Join(x.dir, "a.yml")}, td.Files(), "[AC-020]")
}

// listSep is the COMPOSE_FILE separator for the fixture's platform semantics.
func (x *trustFx) listSep() string { return ":" }

func TestComposeTrust_AC020_ComposeFileEntryUntrustedRefused(t *testing.T) {
	x := newTrustFx(t)
	x.file("extra.yml", uidMallory, uidMallory, 0644, "")
	x.file(".env", 0, 0, 0644, "COMPOSE_FILE=compose.yaml:extra.yml\n")
	x.refuse(ruleOwner)
}

func TestComposeTrust_AC020_ComposeFileEntryOutsideDirChainChecked(t *testing.T) {
	x := newTrustFx(t)
	shared := x.p("srv", "shared")
	x.fs.chain(shared)
	x.fs.file(filepath.Join(shared, "base.yml"), 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "COMPOSE_FILE=compose.yaml|"+x.relTo(filepath.Join(shared, "base.yml"))+"\nCOMPOSE_PATH_SEPARATOR=|\n")

	td := x.accept()
	assert.Equal(t, filepath.Join(shared, "base.yml"), td.Files()[1], "[AC-020] an entry outside the project directory is allowed when its chain is clean")

	x.fs.get(shared).uid = uidMallory
	x.refuse(ruleOwner)
	x.fs.get(shared).uid = 0
	x.fs.get(x.p("srv")).mode = os.ModeDir | 0777
	x.refuse(ruleWorldWritable)
}

func TestComposeTrust_AC020_CustomPathSeparator(t *testing.T) {
	x := newTrustFx(t)
	x.file("a.yml", 0, 0, 0644, "")
	x.file(".env", 0, 0, 0644, "COMPOSE_PATH_SEPARATOR=';'\nCOMPOSE_FILE=\"compose.yaml;a.yml\"\n")
	td := x.accept()
	assert.Equal(t, []string{filepath.Join(x.dir, "compose.yaml"), filepath.Join(x.dir, "a.yml")}, td.Files(), "[AC-020] quoted values and a custom separator")
}

func TestComposeTrust_AC020_UnparseableComposeFileRefused(t *testing.T) {
	cases := map[string]string{
		"interpolation":     "COMPOSE_FILE=$BASE/compose.yaml\n",
		"braced":            "COMPOSE_FILE=${BASE}.yaml\n",
		"export":            "export COMPOSE_FILE=compose.yaml\n",
		"space before =":    "COMPOSE_FILE =compose.yaml\n",
		"backslash":         "COMPOSE_FILE=a\\b.yml\n",
		"inline comment":    "COMPOSE_FILE=compose.yaml # prod\n",
		"unterminated":      "COMPOSE_FILE=\"compose.yaml\n",
		"duplicate":         "COMPOSE_FILE=a.yml\nCOMPOSE_FILE=b.yml\n",
		"empty value":       "COMPOSE_FILE=\n",
		"empty quoted":      "COMPOSE_FILE=''\n",
		"empty entry":       "COMPOSE_FILE=compose.yaml::a.yml\n",
		"separator garbage": "COMPOSE_PATH_SEPARATOR=$X\n",
		"backtick":          "COMPOSE_FILE=`id`\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			x := newTrustFx(t)
			x.file(".env", 0, 0, 0644, "SECRET_TOKEN=xyz123hunter\n"+content)
			te := x.refuse(ruleEnvUnparsed)
			assert.NotContains(t, te.Error(), "xyz123hunter", "[AC-020] the .env content is never echoed")
		})
	}
}

func TestComposeTrust_AC020_EnvUnreadableRefusedWithGuidance(t *testing.T) {
	x := newTrustFx(t)
	x.file(".env", 0, gidDocker, 0640, "A=b\n").readErr = &os.PathError{Op: "open", Path: "x", Err: syscall.EACCES}
	te := x.refuse(ruleEnvUnreadable)
	assert.Contains(t, te.Error(), "grant the agent user read access", "[AC-020] the operator is told what to fix")
	assert.Contains(t, te.Error(), ".env")
}

func TestComposeTrust_AC020_MessageDoesNotEchoEnvContent(t *testing.T) {
	x := newTrustFx(t)
	x.file(".env", 0, 0, 0644, "DB_PASSWORD=hunter2-SECRET\nCOMPOSE_FILE=$X\n")
	te := x.refuse(ruleEnvUnparsed)
	assert.NotContains(t, te.Error(), "hunter2", "[AC-020]")
	assert.NotContains(t, te.Error(), "SECRET", "[AC-020]")
}

func TestComposeTrust_AC020_UnrelatedEnvKeysIgnored(t *testing.T) {
	x := newTrustFx(t)
	x.file(".env", 0, 0, 0644, "export TOKEN=a b c\nCOMPOSE_PROJECT_NAME=foo\nCOMPOSE_PROFILES=dev\n")
	td := x.accept()
	assert.Equal(t, []string{filepath.Join(x.dir, "compose.yaml")}, td.Files(), "[AC-020] only COMPOSE_FILE and COMPOSE_PATH_SEPARATOR matter")
}

// ---------------------------------------------------------------------------
// AC-021 Windows
// ---------------------------------------------------------------------------

func TestComposeTrust_AC021_WindowsWithoutRootsRefused(t *testing.T) {
	x := newTrustFx(t)
	x.ct.goos = "windows"
	te := x.refuse(ruleWindowsRoots)
	assert.Contains(t, te.Error(), "docker.compose_roots", "[AC-021]")
}

func TestComposeTrust_AC021_WindowsRootsCaseInsensitive(t *testing.T) {
	// Fixed by QA Engineer: the drive-letter paths only mean something to
	// filepath on Windows; on Linux the backslash is an ordinary character, so
	// the original assertion failed on the ubuntu CI runner. The fold itself is
	// platform independent and is checked with slash paths everywhere.
	if runtime.GOOS == "windows" {
		assert.True(t, within(`C:\Stacks`, `c:\stacks\App`, true), "[AC-021] roots compare case-insensitively on Windows")
	}
	root, below := filepath.Join(string(filepath.Separator), "Srv", "Stacks"), filepath.Join(string(filepath.Separator), "srv", "stacks", "App")
	assert.True(t, within(root, below, true), "[AC-021] roots compare case-insensitively when the fold is on")
	if runtime.GOOS != "windows" { // filepath.Rel is already case-insensitive there
		assert.False(t, within(root, below, false), "and case-sensitively without the fold")
	}
}

func TestComposeTrust_AC021_WindowsSkipsOwnershipKeepsPlacement(t *testing.T) {
	x := newTrustFx(t)
	x.ct.goos = "windows"
	x.ct.roots = []string{x.p("opt", "stacks")}
	x.dirNode().uid = uidMallory // no owner on Windows: not checked
	x.dirNode().mode = os.ModeDir | 0777
	x.accept()

	x.writer("w", bindRW(x.dir, "/app"))
	x.refuse(ruleContainerWritable) // placement rules still apply
}

// ---------------------------------------------------------------------------
// AC-022 ACLs
// ---------------------------------------------------------------------------

func TestComposeTrust_AC022_GroupWritableWithACLRefused(t *testing.T) {
	x := newTrustFx(t)
	n := x.dirNode()
	n.mode, n.gid, n.acl = os.ModeDir|0775, gidDocker, true
	x.refuse(ruleACL)
}

func TestComposeTrust_AC022_ACLReadErrorFailsClosed(t *testing.T) {
	x := newTrustFx(t)
	n := x.dirNode()
	n.mode, n.gid, n.aclErr = os.ModeDir|0775, gidDocker, errors.New("EIO")
	x.refuse(ruleACL)
}

func TestComposeTrust_AC022_NoGroupWriteSkipsACLLookup(t *testing.T) {
	x := newTrustFx(t)
	x.dirNode().acl = true // would refuse if it were consulted
	x.accept()
	assert.Empty(t, x.fs.aclCalls, "[AC-022] a path without group-write needs no ACL inspection")
}

// ---------------------------------------------------------------------------
// AC-023 socket group
// ---------------------------------------------------------------------------

func TestComposeTrust_AC023_SocketWithoutGroupRWDoesNotTrustMembers(t *testing.T) {
	x := newTrustFx(t)
	x.accounts(defaultPasswd+"carol:x:1600:999::/h:/sh\n", strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,bob", 1)+"")
	x.dirNode().uid = 1600
	x.file("compose.yaml", 1600, 0, 0644, "")
	x.fs.get(x.sock).mode = os.ModeSocket | 0640 // group may connect read-only: no Docker rights
	x.refuse(ruleOwner)

	x.fs.get(x.sock).mode = os.ModeSocket | 0660
	x.accept()
}

// ---------------------------------------------------------------------------
// AC-024 user-private groups and account enumeration
// ---------------------------------------------------------------------------

func (x *trustFx) daveAccounts() {
	x.accounts(defaultPasswd+"dave:x:1700:1700::/home/dave:/bin/sh\n",
		strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,dave", 1)+"dave:x:1700:\n")
}

func TestComposeTrust_AC024_UserPrivateGroupWritableAccepted(t *testing.T) {
	x := newTrustFx(t)
	x.daveAccounts()
	n := x.dirNode()
	n.uid, n.gid, n.mode = 1700, 1700, os.ModeDir|0775
	x.file("compose.yaml", 1700, 1700, 0664, "")
	x.accept()
}

func TestComposeTrust_AC024_SharedGroupWithUntrustedMemberRefused(t *testing.T) {
	x := newTrustFx(t)
	n := x.dirNode()
	n.gid, n.mode = gidStaff, os.ModeDir|0775
	x.refuse(ruleGroupWritable)
}

func TestComposeTrust_AC024_GroupWithUnknownMemberNameRefused(t *testing.T) {
	x := newTrustFx(t)
	x.accounts(defaultPasswd, defaultGroup+"ghosts:x:1800:riot,nobody-here\n")
	n := x.dirNode()
	n.gid, n.mode = 1800, os.ModeDir|0775
	x.refuse(ruleGroupWritable)
}

func TestComposeTrust_AC024_GroupWithoutFileEntryRefused(t *testing.T) {
	x := newTrustFx(t)
	n := x.dirNode()
	n.gid, n.mode = 4242, os.ModeDir|0775
	x.refuse(ruleGroupWritable)
}

func TestComposeTrust_AC024_NonFilesNSSDisablesMemberRule(t *testing.T) {
	x := newTrustFx(t)
	x.daveAccounts()
	x.fs.file("/etc/nsswitch.conf", 0, 0, 0644, "passwd: files sss\ngroup: files sss\n")
	x.dirNode().uid = 1700
	x.file("compose.yaml", 1700, 0, 0644, "")
	x.refuse(ruleOwner) // dave is only trusted through enumeration

	x.fs.get("/etc/nsswitch.conf").data = []byte("passwd: files systemd\ngroup: compat [NOTFOUND=return] files\n")
	x.accept() // systemd with no userdb records and no homed keeps enumeration on
}

func TestComposeTrust_AC024_SystemdUserdbRecordsDisableMemberRule(t *testing.T) {
	for _, record := range []string{"dave.user", "wheel.group", "dave:wheel.membership"} {
		t.Run(record, func(t *testing.T) {
			x := newTrustFx(t)
			x.daveAccounts()
			x.fs.file("/etc/nsswitch.conf", 0, 0, 0644, "passwd: files systemd\ngroup: files systemd\n")
			x.fs.chain("/etc/userdb")
			x.fs.file("/etc/userdb/"+record, 0, 0, 0644, "{}")
			x.dirNode().uid = 1700
			x.file("compose.yaml", 1700, 0, 0644, "")
			x.refuse(ruleOwner)
		})
	}
}

func TestComposeTrust_AC024_HomedSocketDisablesMemberRule(t *testing.T) {
	x := newTrustFx(t)
	x.daveAccounts()
	x.fs.file("/etc/nsswitch.conf", 0, 0, 0644, "passwd: files systemd\ngroup: files systemd\n")
	x.fs.chain("/run/systemd/userdb")
	x.fs.file("/run/systemd/userdb/io.systemd.Home", 0, 0, 0600, "").mode |= os.ModeSocket
	x.dirNode().uid = 1700
	x.file("compose.yaml", 1700, 0, 0644, "")
	x.refuse(ruleOwner)
}

func TestComposeTrust_AC024_UnreadableUserdbDirDisablesMemberRule(t *testing.T) {
	x := newTrustFx(t)
	x.daveAccounts()
	x.fs.file("/etc/nsswitch.conf", 0, 0, 0644, "passwd: files systemd\ngroup: files systemd\n")
	x.fs.file("/run/userdb", 0, 0, 0644, "") // not a directory: listing it fails
	x.fs.chain("/run")
	x.dirNode().uid = 1700
	x.file("compose.yaml", 1700, 0, 0644, "")
	x.refuse(ruleOwner)
}

func TestComposeTrust_AC024_CompatPlusAndMinusLinesMarkDBUnavailable(t *testing.T) {
	for _, line := range []string{"+::::::\n", "+@netgroup\n", "-mallory\n"} {
		t.Run(strings.TrimSpace(line), func(t *testing.T) {
			x := newTrustFx(t)
			x.accounts(defaultPasswd+"dave:x:1700:1700::/h:/sh\n"+line, strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,dave", 1)+"dave:x:1700:\n")
			x.dirNode().uid = 1700
			x.file("compose.yaml", 1700, 0, 0644, "")
			x.refuse(ruleOwner)

			y := newTrustFx(t)
			y.accounts(defaultPasswd, strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,dave", 1)+line)
			y.accounts(defaultPasswd+"dave:x:1700:1700::/h:/sh\n", strings.Replace(defaultGroup, "docker:x:999:riot", "docker:x:999:riot,dave", 1)+line)
			y.dirNode().uid = 1700
			y.file("compose.yaml", 1700, 0, 0644, "")
			y.refuse(ruleOwner)
		})
	}
}

// ---------------------------------------------------------------------------
// AC-025 project mapping
// ---------------------------------------------------------------------------

func TestComposeTrust_AC025_DirMappedToOtherProjectRefused(t *testing.T) {
	x := newTrustFx(t)
	x.project = "evil"
	te := x.refuse(ruleProjectMismatch)
	assert.Contains(t, te.Error(), `"app"`, "[AC-025] the message names the project the directory belongs to")
}

func TestComposeTrust_AC025_RecoveryDerivesTheUniqueProject(t *testing.T) {
	x := newTrustFx(t)
	_, err := x.ct.Check(x.snap, trustRequest{Dir: x.dir})
	require.NoError(t, err, "[AC-025] Project \"\" uses the unique accepted project")

	x.addStackContainer("c2", "other-1", "other", x.dir)
	_, err = x.ct.Check(x.snap, trustRequest{Dir: x.dir})
	var te *composeTrustError
	require.True(t, errors.As(err, &te))
	assert.Equal(t, ruleAmbiguousProject, te.Rule, "[AC-025] two projects for one directory are ambiguous")
}

func TestComposeTrust_AC025_TrailingSlashLabelStillMatches(t *testing.T) {
	x := newTrustFx(t)
	x.snap.Containers[0].Labels[models.LabelComposeWorkingDir] = x.dir + "/"
	x.accept()
}

// lyingResolver answers EvalSymlinks for one path with a fixed result.
type lyingResolver struct {
	*fakeTrustFS
	from, answer string
}

func (l *lyingResolver) EvalSymlinks(p string) (string, error) {
	if filepath.Clean(p) == filepath.Clean(l.from) {
		return l.answer, nil
	}
	return l.fakeTrustFS.EvalSymlinks(p)
}

// relTo is target as a slash-separated path relative to the stack directory, so
// a COMPOSE_FILE entry can name it on any platform (the .env grammar has no
// backslash).
func (x *trustFx) relTo(target string) string {
	rel, err := filepath.Rel(x.dir, target)
	require.NoError(x.t, err)
	return filepath.ToSlash(rel)
}

// ---------------------------------------------------------------------------
// QA-D2 (security ruling): a read-write mount source below a directory the agent
// user cannot enter is compared through its readable prefix, under conditions.
// ---------------------------------------------------------------------------

// sandboxSource sets up /home/sandbox with mode 0750 (root-owned) and a bind
// source below it that the agent user cannot resolve (EACCES).
func (x *trustFx) sandboxSource() (home, src string) {
	home = x.p("home", "sandbox")
	x.fs.chain(home)
	x.fs.get(home).mode = os.ModeDir | 0750
	src = filepath.Join(home, "data")
	x.fs.evalErr[filepath.Clean(src)] = syscall.EACCES
	x.writer("sandbox", bindRW(src, "/data"))
	return home, src
}

func TestComposeTrust_AC019_UnreadableTrustedHomeSourceAccepted(t *testing.T) {
	x := newTrustFx(t)
	home, src := x.sandboxSource()

	x.accept()

	fb := x.ct.FallbackSources(x.snap)
	require.Len(t, fb, 1, "[AC-019] the source was compared through its prefix")
	assert.Equal(t, src, fb[0].source)
	assert.Equal(t, home, fb[0].prefix)
	assert.Equal(t, src, fb[0].path, "[AC-019] resolved prefix plus the cleaned remainder")
}

func TestComposeTrust_AC019_FallbackComparesPrefixPlusRemainder(t *testing.T) {
	x := newTrustFx(t)
	home, src := x.sandboxSource()
	// The stack lives below the unreadable source: it is a container-writable
	// location and must be refused, wherever the daemon spelled the source.
	x.dir = filepath.Join(src, "stack")
	x.fs.chain(x.dir)
	x.fs.file(filepath.Join(x.dir, "compose.yaml"), 0, 0, 0644, "")
	x.snap.Containers[0].Labels[models.LabelComposeWorkingDir] = x.dir
	x.fs.get(home).mode = os.ModeDir | 0750

	te := x.refuse(ruleContainerWritable)
	assert.Contains(t, te.Error(), "sandbox", "[AC-019] the container is named")
}

func TestComposeTrust_AC019_FallbackUntrustedOwnedPrefixRefused(t *testing.T) {
	x := newTrustFx(t)
	home, _ := x.sandboxSource()
	x.fs.get(home).uid = uidMallory // an untrusted account owns the last readable directory

	te := x.refuse(ruleContainerWritable)
	assert.Contains(t, te.Error(), "cannot resolve", "[AC-019]")
	assert.Contains(t, te.Error(), "owner", "[AC-019] the failed host-account rule is named")
}

func TestComposeTrust_AC019_FallbackGroupWritablePrefixWithACLRefused(t *testing.T) {
	x := newTrustFx(t)
	home, _ := x.sandboxSource()
	n := x.fs.get(home)
	n.mode, n.gid, n.acl = os.ModeDir|0775, gidDocker, true

	te := x.refuse(ruleContainerWritable)
	assert.Contains(t, te.Error(), "acl", "[AC-019]")
}

func TestComposeTrust_AC019_FallbackRefusedWhenUntrustedAncestorOfPrefix(t *testing.T) {
	x := newTrustFx(t)
	x.sandboxSource()
	x.fs.get(x.p("home")).mode = os.ModeDir | 0777 // every ancestor of the prefix is checked too

	x.refuse(ruleContainerWritable)
}

func TestComposeTrust_AC019_FallbackRefusedWhenAnotherWritableSourceIsAbove(t *testing.T) {
	t.Run("lexical ancestor", func(t *testing.T) {
		x := newTrustFx(t)
		x.sandboxSource()
		x.writer("whole-home", bindRW(x.p("home"), "/home")) // a container could plant a symlink below it

		te := x.refuse(ruleContainerWritable)
		assert.Contains(t, te.Error(), "above it", "[AC-019]")
	})
	t.Run("ancestor through a symlink to the readable prefix", func(t *testing.T) {
		x := newTrustFx(t)
		home, _ := x.sandboxSource()
		x.fs.chain(x.p("alias"))
		x.fs.link(x.p("alias", "home-link"), home, 0)
		x.writer("via-link", bindRW(x.p("alias", "home-link"), "/h")) // resolves to the prefix itself

		x.refuse(ruleContainerWritable)
	})
	t.Run("a sibling source is not above it", func(t *testing.T) {
		x := newTrustFx(t)
		x.sandboxSource()
		x.writer("other", bindRW(x.p("srv", "elsewhere"), "/o"))

		x.accept()
	})
}

func TestComposeTrust_AC019_FallbackOnlyForPermissionErrors(t *testing.T) {
	for name, errno := range map[string]error{
		"ELOOP":   syscall.ELOOP,
		"ENOTDIR": syscall.ENOTDIR,
		"EIO":     syscall.EIO,
	} {
		t.Run(name, func(t *testing.T) {
			if name == "ENOTDIR" && runtime.GOOS == "windows" {
				t.Skip("syscall.ENOTDIR is ERROR_PATH_NOT_FOUND on Windows, which means not-exist")
			}
			x := newTrustFx(t)
			_, src := x.sandboxSource()
			x.fs.evalErr[filepath.Clean(src)] = errno

			te := x.refuse(ruleContainerWritable)
			assert.Contains(t, te.Error(), "cannot resolve", "[AC-019] %s refuses", name)
		})
	}
}

func TestComposeTrust_AC019_FallbackENOENTKeepsLexicalComparison(t *testing.T) {
	x := newTrustFx(t)
	_, src := x.sandboxSource()
	x.fs.evalErr[filepath.Clean(src)] = fakeNotExist("lstat", src)

	x.accept()
	assert.Empty(t, x.ct.FallbackSources(x.snap), "[AC-019] a missing source is the old lexical case, not a fallback")
}

func TestComposeTrust_AC019_FallbackNeverOnWindows(t *testing.T) {
	x := newTrustFx(t)
	x.sandboxSource()
	x.ct.goos = "windows"
	x.ct.roots = []string{x.p("opt", "stacks")}

	x.refuse(ruleContainerWritable)
}
