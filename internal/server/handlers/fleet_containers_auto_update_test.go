package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

func getFleetContainers(t *testing.T, h *Handlers) (int, []map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	h.FleetContainers(w, httptest.NewRequest(http.MethodGet, "/api/v1/fleet/containers", nil))
	var rows []map[string]interface{}
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rows))
	}
	return w.Code, rows
}

func putAutoUpdate(h *Handlers, device, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/devices/"+device+"/auto-updates", strings.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", device)
	w := httptest.NewRecorder()
	h.SetAutoUpdate(w, r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc)))
	return w
}

func rowByName(rows []map[string]interface{}, name string) map[string]interface{} {
	for _, r := range rows {
		if r["container_name"] == name {
			return r
		}
	}
	return nil
}

// fleetEnv is an autoEnv whose latest telemetry for dev-1 is data.
func fleetEnv(t *testing.T, data *models.FullTelemetryData) *autoEnv {
	t.Helper()
	e := newAutoEnv(t, anytime(), anytime())
	e.h.dockerAudit = func(context.Context, string, string, string, string, interface{}) error { return nil }
	e.tel.Snapshots["dev-1"] = []models.TelemetrySnapshot{{DeviceID: "dev-1", Data: *data}}
	return e
}

func TestAC001_ContainerRowsCarryPolicyStateFromOneQuery(t *testing.T) {
	updateAvail := true
	data := stackTelemetry("media", [2]string{"sonarr", "/srv/media"})
	data.Docker.Containers = append(data.Docker.Containers,
		models.ContainerInfo{ID: "id-web", Name: "web", State: "running", UpdateAvailable: &updateAvail},
		models.ContainerInfo{ID: "id-db", Name: "db", State: "running"})
	e := fleetEnv(t, data)
	e.tel.Snapshots["dev-2"] = []models.TelemetrySnapshot{{DeviceID: "dev-2", Data: models.FullTelemetryData{Docker: &models.DockerInfo{
		Containers: []models.ContainerInfo{{ID: "id-x", Name: "web", State: "running"}},
	}}}}
	e.policy(t, "web", false, nil)
	e.policy(t, "media", true, nil)
	e.policies.ListByDeviceCalls = 0

	code, rows := getFleetContainers(t, e.h)

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 1, e.policies.ListAllCalls, "[AC-001] one policy query for the whole fleet")
	assert.Zero(t, e.policies.ListByDeviceCalls, "no per-host policy query")
	require.Len(t, rows, 4)

	var web, other map[string]interface{}
	for _, r := range rows {
		if r["container_name"] == "web" && r["device_id"] == "dev-1" {
			web = r
		}
		if r["device_id"] == "dev-2" {
			other = r
		}
	}
	require.NotNil(t, web)
	assert.Equal(t, map[string]interface{}{"target": "web", "enabled": true}, web["auto_update_policy"],
		"[AC-001] an enabled container policy shows on")
	assert.Equal(t, map[string]interface{}{"target": "web", "enabled": false}, other["auto_update_policy"],
		"a policy on one host never shows on another host's container of the same name")
	assert.Equal(t, map[string]interface{}{"target": "db", "enabled": false}, rowByName(rows, "db")["auto_update_policy"],
		"[AC-001] no policy shows off")
	assert.Equal(t, map[string]interface{}{"target": "sonarr", "enabled": false, "covered_by": "media"}, rowByName(rows, "sonarr")["auto_update_policy"],
		"[AC-010] a member with both labels and an enabled stack policy is covered")
}

func TestAC001_PoliciesUnavailableStillListsContainers(t *testing.T) {
	buf := captureServerLog(t)
	e := fleetEnv(t, makeTelemetryWithUpdates("web"))
	e.policy(t, "web", false, nil)
	e.policies.ListAllErr = errors.New("policies table locked")

	code, rows := getFleetContainers(t, e.h)

	require.Equal(t, http.StatusOK, code, "a policy failure does not fail the list")
	require.Len(t, rows, 1)
	assert.NotContains(t, rows[0], "auto_update_policy", "the field is omitted, never a guessed off")
	assert.Contains(t, buf.String(), "auto-update policies unavailable")
	assert.NotContains(t, buf.String(), "target=", "the warning logs only the error")
}

// The switch saves the row's target. The dispatcher must then match that
// exact string against the telemetry name, so the saved policy really fires.
func TestAC001_SavedTargetIsTheNameTheDispatcherMatches(t *testing.T) {
	for _, name := range []string{"web", "/legacy-web", "my_app.1-blue"} {
		t.Run(name, func(t *testing.T) {
			updateAvail := true
			data := &models.FullTelemetryData{Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
				{ID: "id-1", Name: name, State: "running", UpdateAvailable: &updateAvail},
			}}}
			e := fleetEnv(t, data)

			_, rows := getFleetContainers(t, e.h)
			require.Len(t, rows, 1)
			policy, _ := rows[0]["auto_update_policy"].(map[string]interface{})
			require.NotNil(t, policy)
			target, _ := policy["target"].(string)
			require.Equal(t, name, target, "[AC-001] the target is the telemetry name verbatim")

			body, _ := json.Marshal(map[string]interface{}{"target": target, "is_stack": false, "compose_work_dir": "", "enabled": true})
			require.Equal(t, http.StatusOK, putAutoUpdate(e.h, "dev-1", string(body)).Code)

			e.h.checkAutoUpdates(context.Background(), "dev-1", data)

			cmds := e.dockerUpdates()
			require.Len(t, cmds, 1, "[AC-001] the saved policy dispatches")
			assert.Equal(t, name, cmds[0].Params["container"], "[AC-001] the dispatcher matched the saved name exactly")

			_, rows = getFleetContainers(t, e.h)
			assert.Equal(t, true, rows[0]["auto_update_policy"].(map[string]interface{})["enabled"], "the list shows the saved policy on")
		})
	}
}

func TestAC010_TurningStackOffShowsOwnPolicyAgain(t *testing.T) {
	e := fleetEnv(t, stackTelemetry("media", [2]string{"sonarr", "/srv/media"}))
	e.policy(t, "sonarr", false, func(p *models.AutoUpdatePolicy) { p.Enabled = false })
	e.policy(t, "media", true, nil)

	_, rows := getFleetContainers(t, e.h)
	assert.Equal(t, "media", rows[0]["auto_update_policy"].(map[string]interface{})["covered_by"])

	require.Equal(t, http.StatusOK, putAutoUpdate(e.h, "dev-1", `{"target":"media","is_stack":true,"compose_work_dir":"/srv/media","enabled":false}`).Code)

	_, rows = getFleetContainers(t, e.h)
	assert.Equal(t, map[string]interface{}{"target": "sonarr", "enabled": false}, rows[0]["auto_update_policy"],
		"[AC-010] with the stack off the container's own (off) policy shows again")
}

// A policy saved under a trimmed form of the name never fires: the dispatcher
// compares the telemetry name verbatim, which is why the list hands the client
// that exact string as the target.
func TestAC001_TrimmedNameFormDoesNotDispatch(t *testing.T) {
	updateAvail := true
	data := &models.FullTelemetryData{Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
		{ID: "id-1", Name: "/legacy-web", State: "running", UpdateAvailable: &updateAvail},
	}}}
	e := fleetEnv(t, data)
	require.Equal(t, http.StatusOK,
		putAutoUpdate(e.h, "dev-1", `{"target":"legacy-web","is_stack":false,"compose_work_dir":"","enabled":true}`).Code)

	e.h.checkAutoUpdates(context.Background(), "dev-1", data)

	assert.Empty(t, e.dockerUpdates(), "[AC-001] a policy under the trimmed name does not match /legacy-web")
	_, rows := getFleetContainers(t, e.h)
	assert.Equal(t, map[string]interface{}{"target": "/legacy-web", "enabled": false}, rows[0]["auto_update_policy"],
		"the row shows off, matching what the dispatcher does")
}
