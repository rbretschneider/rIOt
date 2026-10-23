package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func stackMember(name, project, status string, legacy *bool) models.ContainerInfo {
	return models.ContainerInfo{
		ID: "id-" + name, Name: name, State: "running", UpdateStatus: status, UpdateAvailable: legacy,
		Labels: map[string]string{
			models.LabelComposeProject:                project,
			models.LabelComposeService:                name,
			models.LabelComposeWorkingDir:             "/srv/" + project,
			"com.docker.compose.project.config_files": "/srv/" + project + "/compose.yaml",
		},
	}
}

func stacksByProject(rows []map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	for _, r := range rows {
		out[r["device_id"].(string)+"/"+r["project"].(string)] = r
	}
	return out
}

func stacksEnv(t *testing.T, containers ...models.ContainerInfo) *autoEnv {
	t.Helper()
	return fleetEnv(t, &models.FullTelemetryData{Docker: &models.DockerInfo{Containers: containers}})
}

func TestAC006_StackOutOfDateCountsMembersWithTheContainersRule(t *testing.T) {
	yes, no := true, false
	e := stacksEnv(t,
		stackMember("a", "media", "update_available", nil),
		stackMember("b", "media", "", &yes),             // older agent, true
		stackMember("c", "media", "pinned", &yes),       // pinned never counts
		stackMember("d", "media", "check_failed", &yes), // unknown never counts
		stackMember("e", "media", "up_to_date", &yes),
		stackMember("f", "media", "", &no),
		stackMember("g", "media", "", nil),
		stackMember("x", "fresh", "up_to_date", nil),
	)

	rows, err := e.h.mergeDockerStacks(context.Background(), nil)

	require.NoError(t, err)
	by := stacksByProject(rows)
	assert.Equal(t, 2, by["dev-1/media"]["out_of_date"], "[AC-006] update_available plus the older true field; pinned and check_failed never")
	assert.Equal(t, 0, by["dev-1/fresh"]["out_of_date"])
}

func TestAC006_StoredDownStackHasNoOutOfDateMembers(t *testing.T) {
	e := stacksEnv(t)
	known := []map[string]interface{}{{"device_id": "dev-1", "project": "old", "hostname": "host", "path_state": "agreed", "out_of_date": 3}}

	rows, err := e.h.mergeDockerStacks(context.Background(), known)

	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 0, rows[0]["out_of_date"], "a stack with no running members has none out of date")
	assert.Equal(t, "down", rows[0]["state"])
}

func TestAC002_StackAutoUpdateIsTheEnabledStackPolicyFromOneQuery(t *testing.T) {
	e := stacksEnv(t,
		stackMember("a", "media", "", nil),
		stackMember("b", "paused", "", nil),
		stackMember("c", "clash", "", nil),
		stackMember("d", "none", "", nil),
	)
	e.policy(t, "media", true, nil)
	e.policy(t, "paused", true, func(p *models.AutoUpdatePolicy) { p.Enabled = false })
	e.policy(t, "clash", false, nil) // a container policy under the project's name
	known := []map[string]interface{}{{"device_id": "dev-1", "project": "gone", "hostname": "host"}}
	e.policy(t, "gone", true, nil)
	e.policies.ListByDeviceCalls = 0

	rows, err := e.h.mergeDockerStacks(context.Background(), known)

	require.NoError(t, err)
	assert.Equal(t, 1, e.policies.ListAllCalls, "one policy query for all hosts")
	assert.Zero(t, e.policies.ListByDeviceCalls, "no per-host policy query")
	by := stacksByProject(rows)
	assert.Equal(t, true, by["dev-1/media"]["auto_update"], "[AC-002] an enabled stack policy shows on")
	assert.Equal(t, false, by["dev-1/paused"]["auto_update"], "[AC-003] a disabled stack policy shows off")
	assert.Equal(t, false, by["dev-1/clash"]["auto_update"], "[AC-011] a container policy under the project name is not the stack's")
	assert.Equal(t, false, by["dev-1/none"]["auto_update"], "[AC-002] no policy shows off")
	assert.Equal(t, true, by["dev-1/gone"]["auto_update"], "a stored down stack shows its policy too")
	for key, row := range by {
		assert.NotContains(t, row, "auto_update_unavailable", key)
	}
}

func TestAC002_StackPoliciesUnavailableMarksEveryRow(t *testing.T) {
	buf := captureServerLog(t)
	e := stacksEnv(t, stackMember("a", "media", "", nil), stackMember("b", "other", "", nil))
	e.policy(t, "media", true, nil)
	e.policies.ListAllErr = errors.New("policies table locked")

	rows, err := e.h.mergeDockerStacks(context.Background(), nil)

	require.NoError(t, err, "a policy failure does not fail the list")
	require.Len(t, rows, 2)
	for _, row := range rows {
		assert.Equal(t, true, row["auto_update_unavailable"], "every row is marked unavailable")
		assert.Equal(t, false, row["auto_update"])
	}
	assert.Contains(t, buf.String(), "auto-update policies unavailable")
}

func TestAC002_StackListBodyCarriesTheNewFields(t *testing.T) {
	e := stacksEnv(t, stackMember("a", "media", "update_available", nil))
	e.policy(t, "media", true, nil)

	rows, err := e.h.mergeDockerStacks(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	body := safeStack(rows[0], false)

	assert.Equal(t, 1, body["out_of_date"])
	assert.Equal(t, true, body["auto_update"])
	assert.Equal(t, "/srv/media", body["agreed_working_dir"], "the switch saves the agreed working dir")
	assert.NotContains(t, body, "services", "the list body still has no member details")
}
