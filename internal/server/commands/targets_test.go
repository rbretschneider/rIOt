package commands

import (
	"context"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
)

func composeContainer(id, name, project, svc, wd string) models.ContainerInfo {
	return models.ContainerInfo{ID: id, Name: name, Labels: map[string]string{
		models.LabelComposeProject: project, models.LabelComposeService: svc, models.LabelComposeWorkingDir: wd,
	}}
}

func TestResolveDockerUpdateKeys(t *testing.T) {
	containers := []models.ContainerInfo{
		composeContainer("id-app", "/app", "stack", "app", "/srv/stack/"),
		{ID: "id-solo", Name: "solo"},
		{ID: "id-partial", Name: "partial", Labels: map[string]string{models.LabelComposeProject: "p"}},
	}
	cases := []struct {
		name   string
		params map[string]interface{}
		want   []string
	}{
		{"compose_work_dir wins, trailing slash removed", map[string]interface{}{"compose_work_dir": "/srv/stack/", "container": "solo"}, []string{"docker:compose:/srv/stack"}},
		{"compose container by name", map[string]interface{}{"container": "app"}, []string{"docker:compose:/srv/stack"}},
		{"leading slash ignored", map[string]interface{}{"container": "/app"}, []string{"docker:compose:/srv/stack"}},
		{"standalone by name", map[string]interface{}{"container": "solo"}, []string{"docker:container:solo"}},
		{"standalone by id", map[string]interface{}{"container_id": "id-solo"}, []string{"docker:container:solo"}},
		{"incomplete compose labels fall back to name key", map[string]interface{}{"container": "partial"}, []string{"docker:container:partial"}},
		{"name beats stale id", map[string]interface{}{"container": "solo", "container_id": "old-id"}, []string{"docker:container:solo"}},
		{"unknown name keeps name key", map[string]interface{}{"container": "ghost"}, []string{"docker:container:ghost"}},
		{"unknown id falls back to id key", map[string]interface{}{"container_id": "zzz"}, []string{"docker:id:zzz"}},
		{"nothing to target", map[string]interface{}{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveDockerUpdateKeys(tc.params, containers))
		})
	}
}

func TestFindContainer_ShortIDPrefixNeedsTwelveChars(t *testing.T) {
	containers := []models.ContainerInfo{{ID: "0123456789abcdef0123", Name: "x"}}
	assert.NotNil(t, findContainer(containers, "", "0123456789ab"), "12-char short id matches")
	assert.Nil(t, findContainer(containers, "", "01234567"), "a shorter prefix is ambiguous and must not match")
}

func TestServiceResolveTargetKeys(t *testing.T) {
	h := newHarness(t)
	h.tel.Snapshots[devA] = []models.TelemetrySnapshot{{DeviceID: devA, Data: models.FullTelemetryData{
		Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
			composeContainer("id-app", "app", "stack", "app", "/srv/stack"),
			composeContainer("id-db", "db", "stack", "db", "/srv/stack"),
			{ID: "id-solo", Name: "solo"},
		}}}}}
	ctx := context.Background()

	cases := []struct {
		name, action string
		params       map[string]interface{}
		want         []string
	}{
		{"reboot", "reboot", nil, []string{"host"}},
		{"shutdown", "shutdown", nil, []string{"host"}},
		{"os_update", "os_update", nil, []string{"host"}},
		{"enable_auto_updates", "enable_auto_updates", nil, []string{"host"}},
		{"agent_update", "agent_update", nil, []string{"host"}},
		{"fetch_logs holds nothing", "fetch_logs", nil, nil},
		{"docker_stop holds nothing", "docker_stop", map[string]interface{}{"container_id": "id-app"}, nil},
		{"docker_update compose container", "docker_update", map[string]interface{}{"container": "app"}, []string{"docker:compose:/srv/stack"}},
		{"bulk is the union, deduplicated", "docker_bulk_update",
			map[string]interface{}{"container_ids": []interface{}{"id-app", "id-db", "id-solo", "id-unknown"}},
			[]string{"docker:compose:/srv/stack", "docker:container:solo", "docker:id:id-unknown"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := h.svc.ResolveTargetKeys(ctx, devA, tc.action, tc.params)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestServiceResolveTargetKeys_NoTelemetryStillResolves(t *testing.T) {
	h := newHarness(t)
	got := h.svc.ResolveTargetKeys(context.Background(), "no-such-device", "docker_update", map[string]interface{}{"container": "web"})
	assert.Equal(t, []string{"docker:container:web"}, got, "a missing snapshot must degrade to the name key, not fail")
}

func TestContainerIDParams(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, containerIDParams(map[string]interface{}{"container_ids": []string{"a", "b"}}))
	assert.Equal(t, []string{"a"}, containerIDParams(map[string]interface{}{"container_ids": []interface{}{"a", 5, ""}}))
	assert.Nil(t, containerIDParams(map[string]interface{}{"container_ids": "nope"}))
}

// AUTOUPD-HARDEN-001 AC-014: a stack update that names only a project (the form
// a newer server may send) is keyed by the project's one working directory.
func TestResolveDockerUpdateKeys_AC014_ProjectOnlyMapsViaTelemetry(t *testing.T) {
	containers := []models.ContainerInfo{
		composeContainer("id-app", "app", "stack", "app", "/srv/stack/"),
		composeContainer("id-db", "db", "stack", "db", "/srv/stack"), // same directory, no trailing slash
		composeContainer("id-other", "other", "elsewhere", "o", "/srv/elsewhere"),
	}
	got := resolveDockerUpdateKeys(map[string]interface{}{"compose_project": "stack"}, containers)
	assert.Equal(t, []string{"docker:compose:/srv/stack"}, got, "[AC-014] the unique working directory of the project")
}


func TestResolveDockerUpdateKeys_AC014_WorkDirStillWins(t *testing.T) {
	containers := []models.ContainerInfo{composeContainer("id-a", "a", "stack", "a", "/srv/one")}
	got := resolveDockerUpdateKeys(map[string]interface{}{"compose_project": "stack", "compose_work_dir": "/srv/two/"}, containers)
	assert.Equal(t, []string{"docker:compose:/srv/two"}, got, "[AC-014] a work dir keys the command as before (conflict detection is keyed by work dir)")
}

// A project whose labels disagree (a symlinked directory after a partial
// recreate) holds every reported directory key, so a queued duplicate conflicts.
func TestResolveDockerUpdateKeys_AC014_ProjectOnlyHoldsEveryReportedDir(t *testing.T) {
	containers := []models.ContainerInfo{
		composeContainer("id-a", "a", "stack", "a", "/srv/two"),
		composeContainer("id-b", "b", "stack", "b", "/srv/one/"),
	}
	assert.Equal(t, []string{"docker:compose:/srv/one", "docker:compose:/srv/two"},
		resolveDockerUpdateKeys(map[string]interface{}{"compose_project": "stack"}, containers),
		"[AC-014] both keys, sorted, trailing slash removed")
	assert.Nil(t, resolveDockerUpdateKeys(map[string]interface{}{"compose_project": "unknown"}, containers),
		"[AC-014] an unknown project has no key")
}
