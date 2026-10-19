package agent

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func summaryWith(id, name, netMode string, labels map[string]string, state string) container.Summary {
	s := container.Summary{ID: id, Names: []string{"/" + name}, State: state, Labels: labels}
	s.HostConfig.NetworkMode = netMode
	return s
}

func depNames(deps []depRecord) []string {
	out := make([]string, len(deps))
	for i, d := range deps {
		out[i] = d.Name
	}
	return out
}

// AC-032 (FR-028): a dependent whose network mode is container:<full parent ID>
// is identified.
func TestCL_AC032_DependentByFullParentID(t *testing.T) {
	parentID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	parent := summaryWith(parentID, "gluetun", "bridge", nil, "running")
	dep := summaryWith("dep1", "qbit", "container:"+parentID, nil, "running")
	other := summaryWith("other1", "unrelated", "container:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", nil, "running")

	got := findDependents([]container.Summary{parent, dep, other}, []container.Summary{parent})

	assert.Equal(t, []string{"qbit"}, depNames(got), "[AC-032] only the container attached to the parent's ID is a dependent")
	assert.Equal(t, "gluetun", got[0].ParentName)
}

// AC-033 (FR-028): name, /name and compose service: references are dependents.
func TestCL_AC033_DependentByNameSlashNameService(t *testing.T) {
	proj := map[string]string{models.LabelComposeProject: "stack", models.LabelComposeService: "vpn"}
	parent := summaryWith("p1", "vpn-1", "bridge", proj, "running")
	byName := summaryWith("d1", "by-name", "container:vpn-1", nil, "running")
	bySlash := summaryWith("d2", "by-slash", "container:/vpn-1", nil, "running")
	byService := summaryWith("d3", "by-service", "service:vpn",
		map[string]string{models.LabelComposeProject: "stack", models.LabelComposeService: "app"}, "running")
	otherProject := summaryWith("d4", "other-project", "service:vpn",
		map[string]string{models.LabelComposeProject: "elsewhere", models.LabelComposeService: "app"}, "running")
	bridge := summaryWith("d5", "plain", "bridge", nil, "running")

	got := findDependents([]container.Summary{parent, byName, bySlash, byService, otherProject, bridge}, []container.Summary{parent})

	assert.ElementsMatch(t, []string{"by-name", "by-slash", "by-service"}, depNames(got),
		"[AC-033] name, /name and same-project service: modes are dependents; other projects and plain networks are not")
}

// AC-032: a short ID prefix of at least 12 characters also matches, a shorter
// one does not.
func TestCL_AC032_ShortIDPrefixNeedsTwelveCharacters(t *testing.T) {
	parent := summaryWith("0123456789abcdef0123", "gluetun", "bridge", nil, "running")
	long := summaryWith("d1", "long", "container:0123456789ab", nil, "running")
	short := summaryWith("d2", "short", "container:01234567", nil, "running")

	got := findDependents([]container.Summary{parent, long, short}, []container.Summary{parent})

	assert.Equal(t, []string{"long"}, depNames(got), "[AC-032] a 12-character ID prefix identifies the parent, 8 characters do not")
}

// SEC C-9: composeArgs validates service names, requires an absolute working
// directory, and puts names after "--".
func TestCL_SEC011_ComposeArgsHygiene(t *testing.T) {
	wd := composeDir(t)

	args, err := composeArgs(wd, "", []string{"up", "-d", "--no-deps", "--force-recreate"}, "web-1.a_b")
	require.NoError(t, err)
	assert.Equal(t, []string{"compose", "--project-directory=" + wd, "up", "-d", "--no-deps", "--force-recreate", "--", "web-1.a_b"}, args,
		"[SEC-011] directory is one token, names come after --")

	withFile, err := composeArgs(wd, wd+"/.compose.riot-sanitized.yml", []string{"pull"}, "web")
	require.NoError(t, err)
	assert.Contains(t, withFile, "-f="+wd+"/.compose.riot-sanitized.yml", "[SEC-011] the file is one -f= token")

	stack, err := composeArgs(wd, "", []string{"pull"})
	require.NoError(t, err)
	assert.NotContains(t, stack, "--", "[SEC-011] no -- when there are no service names")

	for _, bad := range []string{"--file=/x", "-f", "", "a b", "web;rm", "../x", ".hidden", "-d"} {
		_, err := composeArgs(wd, "", []string{"up"}, bad)
		assert.Error(t, err, "[SEC-011] service name %q must be rejected", bad)
	}
	_, err = composeArgs("relative/dir", "", []string{"up"}, "web")
	assert.Error(t, err, "[SEC-011] a relative working directory must be rejected")
	_, err = composeArgs("--project-directory=/x", "", []string{"up"}, "web")
	assert.Error(t, err, "[SEC-011] a working directory that looks like a flag must be rejected")
}

// SEC C-9: a stale compose dependent whose service label looks like a flag is
// never passed to compose; a valid one appears only after "--".
func TestCL_SEC011_DependentServiceNameValidatedOrSeparated(t *testing.T) {
	setup := func(t *testing.T, service string) (*dockerHarness, depRecord) {
		h := newDockerHarness(t)
		dir := composeDir(t)
		h.docker.add(fakeContainer{Name: "vpn", Running: true})
		dep := h.docker.add(fakeContainer{Name: "app", Running: true, NetworkMode: "container:stale-parent-id",
			Labels: composeLabels(dir, service)})
		return h, depRecord{ID: dep.ID, Name: "app", ParentName: "vpn", Running: true}
	}

	t.Run("flag-looking label is rejected", func(t *testing.T) {
		captureLogs(t)
		h, dep := setup(t, "--file=/x")

		failed := h.a.restoreDependents(context.Background(), h.docker, []depRecord{dep})

		require.Len(t, failed, 1, "[SEC-011] the dependent cannot be restored")
		assert.Contains(t, failed[0].Reason, "invalid compose service name")
		assert.Empty(t, h.procs.invocations(), "[SEC-011] compose must never be invoked with a hostile service label")
	})

	t.Run("valid label is separated by --", func(t *testing.T) {
		captureLogs(t)
		h, dep := setup(t, "app")
		h.procs.configure = func(_ []string, _ *fakeProcess) {
			// the recreate "creates" a fresh dependent attached to the parent by name
			h.docker.add(fakeContainer{Name: "app-new", Running: true})
		}

		h.a.restoreDependents(context.Background(), h.docker, []depRecord{dep})

		calls := h.procs.invocations()
		require.Len(t, calls, 1, "[SEC-011] one compose fallback is expected")
		argv := calls[0]
		sep := -1
		for i, tok := range argv {
			if tok == "--" {
				sep = i
			}
		}
		require.Greater(t, sep, 0, "[SEC-011] the argv must contain --")
		assert.Equal(t, "app", argv[sep+1], "[SEC-011] the service name follows --")
		assert.NotContains(t, argv[:sep], "app", "[SEC-011] the service name never appears before --")
		assert.Contains(t, argv, "--force-recreate")
	})
}
