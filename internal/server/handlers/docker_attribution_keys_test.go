package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The target keys of a Docker event feed the expected-activity lookup. They are
// computed from the container's own name and its compose project, never from
// anything the agent claims about a command (EXPECT-001 AC-044).

func keysFor(t *testing.T, h *Handlers, device string, evt *models.DockerEvent) {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/docker/events", func(w http.ResponseWriter, req *http.Request) {
		h.validateDockerEventCommand(req, evt)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+device+"/docker/events", nil))
}

func composeSnapshot(device, name, workDir string) *testutil.MockTelemetryRepo {
	tel := testutil.NewMockTelemetryRepo()
	tel.Snapshots[device] = []models.TelemetrySnapshot{{DeviceID: device, Data: models.FullTelemetryData{
		Docker: &models.DockerInfo{Containers: []models.ContainerInfo{{
			Name: name, Labels: map[string]string{models.LabelComposeWorkingDir: workDir},
		}}},
	}}}
	return tel
}

func TestEX_AC044_KeysWithoutAgentCommandID(t *testing.T) {
	h := &Handlers{commandRepo: testutil.NewMockCommandRepo(), telemetry: composeSnapshot("dev-1", "web", "/srv/stack")}
	evt := &models.DockerEvent{ContainerName: "web", Action: "die"} // an older agent: no command ID at all

	keysFor(t, h, "dev-1", evt)

	assert.Equal(t, []string{models.TargetKeyCompose("/srv/stack"), models.TargetKeyContainer("web")}, evt.TargetKeys,
		"[AC-044] the container key and the compose key come from the server's own data, sorted")
	assert.Empty(t, evt.CommandID, "no agent-supplied attribution is trusted")
}

func TestEX_AC044_KeysAreFilledWhateverTheAgentClaims(t *testing.T) {
	h := &Handlers{commandRepo: testutil.NewMockCommandRepo(), telemetry: testutil.NewMockTelemetryRepo()}
	evt := &models.DockerEvent{ContainerName: "/web", Action: "die", CommandID: "11111111-1111-4111-8111-111111111111", CommandAttr: "agent"}

	keysFor(t, h, "dev-1", evt)

	assert.Equal(t, []string{models.TargetKeyContainer("web")}, evt.TargetKeys, "[AC-044] a leading slash is trimmed; no snapshot means no compose key")
	assert.Empty(t, evt.CommandID, "an unknown command ID is dropped")
	assert.Empty(t, evt.CommandAttr)
}

func TestEX_AC044_KeysAreFilledWithoutACommandRepository(t *testing.T) {
	h := &Handlers{telemetry: composeSnapshot("dev-1", "web", "/srv/stack")}
	evt := &models.DockerEvent{ContainerName: "web", Action: "die"}

	keysFor(t, h, "dev-1", evt)

	assert.Len(t, evt.TargetKeys, 2, "[AC-044] the keys are computed before the missing-repository return")
}

func TestEX_AC044_ComposeKeyOnlyFromTheEventsOwnContainer(t *testing.T) {
	h := &Handlers{commandRepo: testutil.NewMockCommandRepo(), telemetry: composeSnapshot("dev-1", "db", "/srv/other")}
	evt := &models.DockerEvent{ContainerName: "web", Action: "die"}

	keysFor(t, h, "dev-1", evt)

	assert.Equal(t, []string{models.TargetKeyContainer("web")}, evt.TargetKeys, "another container's compose directory is not this event's key")
}

func TestEX_AC044_AgentCannotSupplyTargetKeys(t *testing.T) {
	// DockerEvent.TargetKeys is json:"-": a body that names it is ignored.
	var evt models.DockerEvent
	require.NoError(t, json.Unmarshal([]byte(`{"container_name":"web","action":"die","TargetKeys":["host"],"target_keys":["host"]}`), &evt))
	assert.Empty(t, evt.TargetKeys, "the keys are computed by the server only")
	out, err := json.Marshal(models.DockerEvent{ContainerName: "web", TargetKeys: []string{"host"}})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "host", "and they are never sent back out")
}
