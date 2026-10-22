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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func TestParseDpkgHolds_OnlyHoldState(t *testing.T) {
	out := "nginx\t\t\t\t\thold\nbash\t\t\t\t\tinstall\nlibc6:amd64\t\thold\nold\t\tdeinstall\n"
	assert.Equal(t, []string{"nginx", "libc6"}, parseDpkgHolds(out))
}

func TestAptOtherHolds_SubtractsManagedHolds(t *testing.T) {
	out := "linux-image-generic hold\nnginx hold\n"
	got := aptOtherHolds(out, []string{"linux-image-generic"}, nil)
	assert.Equal(t, models.FactStatusOK, got.Status)
	assert.Equal(t, []string{"nginx"}, got.Packages)
	assert.Equal(t, 1, got.Total)
}

func TestAptOtherHolds_NoHoldsIsOKAndEmpty(t *testing.T) {
	got := aptOtherHolds("bash install\n", nil, nil)
	assert.Equal(t, models.FactStatusOK, got.Status)
	assert.Empty(t, got.Packages)
	assert.Equal(t, 0, got.Total)
}

func TestAptOtherHolds_UnreadableManagedStateIsUnavailable(t *testing.T) {
	got := aptOtherHolds("nginx hold\n", nil, errors.New("permission denied"))
	assert.Equal(t, models.FactStatusUnavailable, got.Status)
	assert.Empty(t, got.Packages)
}

func TestBoundFactNames_CapsCountAndReportsTotal(t *testing.T) {
	var names []string
	for i := 0; i < 80; i++ {
		names = append(names, fmt.Sprintf("pkg%03d", i))
	}
	kept, total := boundFactNames(names, nil)
	assert.Len(t, kept, maxFactPackages)
	assert.Equal(t, 80, total)
	assert.Equal(t, "pkg000", kept[0])
}

func TestBoundFactNames_DropsUnsafeLongAndDuplicateNames(t *testing.T) {
	names := []string{
		"ok-pkg", "ok-pkg", "bad name", "bad;rm", "\x1b[31mred", "-leading", "", "<b>x</b>",
		strings.Repeat("a", maxFactNameLen+1), strings.Repeat("a", maxFactNameLen),
	}
	kept, total := boundFactNames(names, nil)
	assert.Equal(t, []string{strings.Repeat("a", maxFactNameLen), "ok-pkg"}, kept)
	assert.Equal(t, 2, total)
}

func TestParseDnfExcludes_BothKeysAndSeparators(t *testing.T) {
	conf := "[main]\n# exclude=ignored\nexclude=kernel* foo\nexcludepkgs = bar, baz\ngpgcheck=1\n"
	assert.Equal(t, []string{"kernel*", "foo", "bar", "baz"}, parseDnfExcludes(conf))
}

func TestParseVersionlockList_ExtractsNames(t *testing.T) {
	content := "# comment\nkernel-0:5.14.0-1.el9.*\nopenssl-libs-1:3.0.7-1.el9.*\n"
	assert.Equal(t, []string{"kernel", "openssl-libs"}, parseVersionlockList(content))
}

type fakeFiles map[string]string

func (f fakeFiles) fs(globs map[string][]string, failRead map[string]error) factFS {
	return factFS{
		ReadFile: func(p string) ([]byte, error) {
			if err, ok := failRead[p]; ok {
				return nil, err
			}
			if c, ok := f[p]; ok {
				return []byte(c), nil
			}
			return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
		},
		Glob: func(p string) ([]string, error) { return globs[p], nil },
	}
}

func TestDnfOtherHolds_SkipsRiotFragmentAndReadsOthers(t *testing.T) {
	files := fakeFiles{
		"/etc/dnf/dnf.conf":                    "[main]\nexclude=httpd\n",
		DNFFragmentPath:                        "excludepkgs=kernel-core\n",
		"/etc/dnf/libdnf5.conf.d/50-site.conf": "excludepkgs=mariadb\n",
	}
	globs := map[string][]string{
		"/etc/dnf/libdnf5.conf.d/*.conf": {"/etc/dnf/libdnf5.conf.d/50-site.conf", DNFFragmentPath},
	}
	got := dnfOtherHolds(files.fs(globs, nil), nil)
	assert.Equal(t, models.FactStatusOK, got.Status)
	assert.Equal(t, []string{"httpd", "mariadb"}, got.Packages, "the rIOt fragment must not be reported")
	assert.Equal(t, 2, got.Total)
}

func TestDnfOtherHolds_MissingFilesAreNotAnError(t *testing.T) {
	got := dnfOtherHolds(fakeFiles{}.fs(nil, nil), nil)
	assert.Equal(t, models.FactStatusOK, got.Status)
	assert.Empty(t, got.Packages)
}

func TestDnfOtherHolds_UnreadableFileIsUnavailable(t *testing.T) {
	files := fakeFiles{"/etc/dnf/dnf.conf": "exclude=httpd\n"}
	failing := map[string]error{"/etc/dnf/dnf.conf": &os.PathError{Op: "open", Path: "/etc/dnf/dnf.conf", Err: os.ErrPermission}}
	got := dnfOtherHolds(files.fs(nil, failing), nil)
	assert.Equal(t, models.FactStatusUnavailable, got.Status)
	assert.Empty(t, got.Packages)
}

func TestDnfOtherHolds_IncludesVersionlock(t *testing.T) {
	files := fakeFiles{dnfVersionlockListPath: "kernel-0:5.14.0-1.el9.*\n"}
	got := dnfOtherHolds(files.fs(nil, nil), nil)
	assert.Equal(t, []string{"kernel"}, got.Packages)
}

func TestManagedHolds_ReadsStateReadOnly(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "holds.json")
	hm := NewHoldManager(true, state, filepath.Join(dir, "staged"))

	names, err := hm.ManagedHolds()
	require.NoError(t, err, "a missing state file means no managed holds")
	assert.Empty(t, names)

	require.NoError(t, os.WriteFile(state, []byte(`{"version":1,"pm":"apt","apt_holds":["linux-image-generic"]}`), 0o600))
	names, err = hm.ManagedHolds()
	require.NoError(t, err)
	assert.Equal(t, []string{"linux-image-generic"}, names)

	require.NoError(t, os.WriteFile(state, []byte(`{not json`), 0o600))
	_, err = hm.ManagedHolds()
	assert.Error(t, err, "an unparsable state file must be surfaced")
}

type checkerFixture struct {
	files map[string]string
	runs  int
	code  int
	out   string
	err   error
	now   time.Time
}

func (f *checkerFixture) checker() *releaseUpgradeChecker {
	return &releaseUpgradeChecker{
		now: func() time.Time { return f.now },
		readFile: func(p string) ([]byte, error) {
			if c, ok := f.files[p]; ok {
				return []byte(c), nil
			}
			return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
		},
		run: func(context.Context) (int, string, error) {
			f.runs++
			return f.code, f.out, f.err
		},
	}
}

var ubuntuRelease = map[string]string{osReleasePath: "NAME=\"Ubuntu\"\nID=ubuntu\n"}

func TestReleaseUpgrade_MarkerFileReportsAvailable(t *testing.T) {
	f := &checkerFixture{files: map[string]string{
		osReleasePath:      "ID=ubuntu\n",
		releaseUpgradeFile: "New release '24.04.1 LTS' available.\nRun 'do-release-upgrade' to upgrade to it.\n",
	}}
	got := f.checker().Check(context.Background())
	assert.Equal(t, models.FactStatusAvailable, got.Status)
	assert.Equal(t, "24.04.1 LTS", got.Name)
	assert.Equal(t, 0, f.runs, "the marker file makes the command unnecessary")
}

func TestReleaseUpgrade_ExitCodesMapToStatus(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		err    error
		status string
	}{
		{"exit 0 is available", 0, nil, models.FactStatusAvailable},
		{"exit 1 is none", 1, nil, models.FactStatusNone},
		{"exit 2 is unavailable", 2, nil, models.FactStatusUnavailable},
		{"run error is unavailable", -1, errors.New("timeout"), models.FactStatusUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &checkerFixture{files: ubuntuRelease, code: tc.code, err: tc.err, out: "New release '26.04 LTS' available."}
			assert.Equal(t, tc.status, f.checker().Check(context.Background()).Status)
		})
	}
}

func TestReleaseUpgrade_CheckRunsAtMostOncePer12Hours(t *testing.T) {
	f := &checkerFixture{files: ubuntuRelease, code: 1, now: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)}
	c := f.checker()
	c.Check(context.Background())
	f.now = f.now.Add(11 * time.Hour)
	c.Check(context.Background())
	assert.Equal(t, 1, f.runs, "second check within 12h must use the cache")
	f.now = f.now.Add(2 * time.Hour)
	c.Check(context.Background())
	assert.Equal(t, 2, f.runs, "check after 12h must run again")
}

func TestReleaseUpgrade_NonUbuntuIsUnavailableWithoutRunning(t *testing.T) {
	f := &checkerFixture{files: map[string]string{osReleasePath: "ID=debian\n"}, code: 0}
	got := f.checker().Check(context.Background())
	assert.Equal(t, models.FactStatusUnavailable, got.Status)
	assert.Equal(t, 0, f.runs)
}

func TestParseReleaseName_RejectsUnsafeLabels(t *testing.T) {
	assert.Equal(t, "", parseReleaseName("New release '<script>' available"))
	assert.Equal(t, "", parseReleaseName("nothing here"))
	assert.Equal(t, "24.04 LTS", parseReleaseName("New release '24.04 LTS' available."))
}
