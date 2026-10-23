package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

func composeLabels(project, workDir string) map[string]string {
	labels := map[string]string{}
	if project != "" {
		labels[models.LabelComposeProject] = project
		labels[models.LabelComposeService] = "svc"
	}
	if workDir != "" {
		labels[models.LabelComposeWorkingDir] = workDir
	}
	return labels
}

func TestAutoUpdateCoverage_ContainerState(t *testing.T) {
	ix := indexAutoUpdatePolicies([]models.AutoUpdatePolicy{
		{DeviceID: "dev-1", Target: "web", Enabled: true},
		{DeviceID: "dev-1", Target: "off", Enabled: false},
		{DeviceID: "dev-1", Target: "media", IsStack: true, Enabled: true},
		{DeviceID: "dev-1", Target: "paused", IsStack: true, Enabled: false},
		{DeviceID: "dev-2", Target: "other", IsStack: true, Enabled: true},
	})
	long := strings.Repeat("x", 128)
	cases := []struct {
		name   string
		device string
		cname  string
		labels map[string]string
		want   FleetAutoUpdatePolicy
	}{
		{"enabled container policy", "dev-1", "web", nil, FleetAutoUpdatePolicy{Target: "web", Enabled: true}},
		{"disabled container policy", "dev-1", "off", nil, FleetAutoUpdatePolicy{Target: "off"}},
		{"no policy", "dev-1", "db", nil, FleetAutoUpdatePolicy{Target: "db"}},
		{"name verbatim with a leading slash", "dev-1", "/web", nil, FleetAutoUpdatePolicy{Target: "/web"}},
		{"[AC-010] both labels and an enabled stack policy", "dev-1", "sonarr", composeLabels("media", "/srv/media"), FleetAutoUpdatePolicy{Target: "sonarr", CoveredBy: "media"}},
		{"[AC-010] covered and own policy kept", "dev-1", "web", composeLabels("media", "/srv/media"), FleetAutoUpdatePolicy{Target: "web", Enabled: true, CoveredBy: "media"}},
		{"[AC-010] project label only is not covered", "dev-1", "sonarr", composeLabels("media", ""), FleetAutoUpdatePolicy{Target: "sonarr"}},
		{"[AC-010] disabled stack policy does not cover", "dev-1", "sonarr", composeLabels("paused", "/srv/paused"), FleetAutoUpdatePolicy{Target: "sonarr"}},
		{"[AC-010] stack policy on another host does not cover", "dev-1", "sonarr", composeLabels("other", "/srv/other"), FleetAutoUpdatePolicy{Target: "sonarr"}},
		{"[AC-011] a stack policy under the container's name is not the container's", "dev-1", "media", nil, FleetAutoUpdatePolicy{Target: "media"}},
		{"[AC-011] a container policy under the project name does not cover", "dev-1", "api", composeLabels("web", "/srv/web"), FleetAutoUpdatePolicy{Target: "api"}},
		{"empty name has no target", "dev-1", "", nil, FleetAutoUpdatePolicy{}},
		{"name at the projection limit has no target", "dev-1", long, nil, FleetAutoUpdatePolicy{}},
		{"name just under the limit keeps its target", "dev-1", long[:127], nil, FleetAutoUpdatePolicy{Target: long[:127]}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ix.container(tc.device, tc.cname, tc.labels))
		})
	}
}

func TestAutoUpdateCoverage_ContainerOutOfDate(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		status string
		legacy *bool
		want   bool
	}{
		{"update_available", nil, true},
		{"update_available", &no, true},
		{"up_to_date", &yes, false},
		{"pinned", &yes, false},
		{"check_failed", &yes, false},
		{"", &yes, true},
		{"", &no, false},
		{"", nil, false},
	}
	for _, tc := range cases {
		got := containerOutOfDate(models.ContainerInfo{UpdateStatus: tc.status, UpdateAvailable: tc.legacy})
		assert.Equal(t, tc.want, got, "[AC-006] status %q legacy %v", tc.status, tc.legacy)
	}
}

// The list's coverage rule must agree with what checkAutoUpdates really does.
// Each case has one container with an update, so the dispatcher's result shows
// which policy (if any) decides for it: covered rows dispatch the stack,
// uncovered rows with an enabled own policy dispatch the container, the rest
// dispatch nothing.
func TestAutoUpdateCoverage_MatchesDispatcher(t *testing.T) {
	type pol struct {
		target  string
		isStack bool
		enabled bool
	}
	cases := []struct {
		name     string
		policies []pol
		cname    string
		labels   map[string]string
	}{
		{"no policies", nil, "web", nil},
		{"enabled container policy", []pol{{"web", false, true}}, "web", nil},
		{"disabled container policy", []pol{{"web", false, false}}, "web", nil},
		{"leading slash matched verbatim", []pol{{"/web", false, true}}, "/web", nil},
		{"stack policy covers a member", []pol{{"media", true, true}}, "sonarr", composeLabels("media", "/srv/media")},
		{"stack policy and own enabled policy", []pol{{"media", true, true}, {"sonarr", false, true}}, "sonarr", composeLabels("media", "/srv/media")},
		{"project label without working dir", []pol{{"media", true, true}}, "sonarr", composeLabels("media", "")},
		{"project label only, own enabled policy", []pol{{"media", true, true}, {"sonarr", false, true}}, "sonarr", composeLabels("media", "")},
		{"disabled stack, own enabled policy", []pol{{"media", true, false}, {"sonarr", false, true}}, "sonarr", composeLabels("media", "/srv/media")},
		{"stack policy under the container name", []pol{{"media", true, true}}, "media", nil},
		{"container policy under the project name", []pol{{"app", false, true}}, "app", composeLabels("app", "/srv/app")},
		{"container policy under the project name, other member", []pol{{"app", false, true}}, "api", composeLabels("app", "/srv/app")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newAutoEnv(t, anytime(), anytime())
			for _, p := range tc.policies {
				enabled := p.enabled
				e.policy(t, p.target, p.isStack, func(sp *models.AutoUpdatePolicy) { sp.Enabled = enabled })
			}
			all, err := e.policies.ListAll(context.Background())
			require.NoError(t, err)
			state := indexAutoUpdatePolicies(all).container("dev-1", tc.cname, tc.labels)

			updateAvail := true
			e.h.checkAutoUpdates(context.Background(), "dev-1", &models.FullTelemetryData{Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
				{ID: "id-1", Name: tc.cname, State: "running", UpdateAvailable: &updateAvail, Labels: tc.labels},
			}}})

			cmds := e.dockerUpdates()
			switch {
			case state.CoveredBy != "":
				require.Len(t, cmds, 1, "a covered row is decided by its stack policy")
				assert.Equal(t, state.CoveredBy, cmds[0].Params["compose_project"], "the stack named in covered_by dispatches")
				assert.NotContains(t, cmds[0].Params, "container")
			case state.Enabled:
				require.Len(t, cmds, 1, "an uncovered row with an enabled own policy dispatches the container")
				assert.Equal(t, state.Target, cmds[0].Params["container"], "the dispatched name is the row's target, verbatim")
			default:
				assert.Empty(t, cmds, "a row shown as off dispatches nothing")
			}
		})
	}
}

// stackCreateFailer makes the command store refuse to create stack updates
// (commands carrying compose_project), so the stack's Dispatch returns an
// error, while every other command goes through the faithful mock and its
// conflict check.
type stackCreateFailer struct {
	*testutil.MockCommandRepo
	refused int
}

func (f *stackCreateFailer) CreateIfNoConflict(ctx context.Context, cmd *models.Command) (*models.Command, error) {
	if _, ok := cmd.Params["compose_project"]; ok {
		f.refused++
		return nil, errStackCreateRefused
	}
	return f.MockCommandRepo.CreateIfNoConflict(ctx, cmd)
}

// The dispatcher's stack branch runs once per project per pass. A
// later member of the same stack then falls through to its own container
// policy. The list shows such a member as "Controlled by stack". This test
// records what really happens, with the real command service and its conflict
// keys, when the stack dispatch succeeds and when it fails.
func TestAutoUpdateCoverage_LaterStackMemberWithOwnPolicy(t *testing.T) {
	setup := func(t *testing.T) (*autoEnv, *models.FullTelemetryData) {
		e := newAutoEnv(t, anytime(), anytime())
		e.policy(t, "media", true, nil)
		e.policy(t, "radarr", false, nil) // enabled own policy on the second member
		data := stackTelemetry("media", [2]string{"sonarr", "/srv/media"}, [2]string{"radarr", "/srv/media"})
		e.tel.Snapshots["dev-1"] = []models.TelemetrySnapshot{{DeviceID: "dev-1", Data: *data}}
		all, err := e.policies.ListAll(context.Background())
		require.NoError(t, err)
		ix := indexAutoUpdatePolicies(all)
		for _, c := range data.Docker.Containers {
			state := ix.container("dev-1", c.Name, c.Labels)
			require.Equal(t, "media", state.CoveredBy, "both members are shown as controlled by the stack")
		}
		require.True(t, ix.container("dev-1", "radarr", data.Docker.Containers[1].Labels).Enabled,
			"the second member's own policy is kept and reported")
		return e, data
	}

	t.Run("stack dispatched", func(t *testing.T) {
		e, data := setup(t)

		e.h.checkAutoUpdates(context.Background(), "dev-1", data)

		cmds := e.dockerUpdates()
		require.Len(t, cmds, 1, "the member's own dispatch conflicts with the stack command's keys")
		assert.Equal(t, "media", cmds[0].Params["compose_project"])
	})

	t.Run("stack dispatch returns an error", func(t *testing.T) {
		e, data := setup(t)
		failing := &stackCreateFailer{MockCommandRepo: e.repo}
		e.h.commandRepo = failing // before the first dispatch builds the command service

		e.h.checkAutoUpdates(context.Background(), "dev-1", data)

		cmds := e.dockerUpdates()
		require.Equal(t, 1, failing.refused, "the stack's Dispatch failed once")
		// Pre-existing dispatcher behaviour, recorded rather than changed: with
		// the stack command missing, nothing holds the member's key, so the
		// member's own enabled container policy dispatches in the same pass.
		require.Len(t, cmds, 1, "one per-container update goes out")
		assert.Equal(t, "radarr", cmds[0].Params["container"])
		assert.NotContains(t, cmds[0].Params, "compose_project")
	})
}

var errStackCreateRefused = errors.New("stack create refused for the test")
