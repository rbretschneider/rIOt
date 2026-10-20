package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const dockerTestDevice = "00000000-0000-4000-8000-000000000001"

func newDockerTest(t *testing.T) (*Handlers, *fakeSocket) {
	t.Helper()
	devices := testutil.NewMockDeviceRepo()
	devices.Devices[dockerTestDevice] = &models.Device{ID: dockerTestDevice, Hostname: "host-a"}
	tel := testutil.NewMockTelemetryRepo()
	enabled := true
	tel.Snapshots[dockerTestDevice] = []models.TelemetrySnapshot{{DeviceID: dockerTestDevice, Timestamp: time.Now(), Data: models.FullTelemetryData{Docker: &models.DockerInfo{Available: true, ContainerLogsEnabled: &enabled, Capabilities: []string{models.DockerCapLogsLive, models.DockerCapPrune, models.DockerCapInventory}, Containers: []models.ContainerInfo{{ID: "abcdef0123456789", Name: "web", State: "running", Image: "example/web:latest"}, {ID: "abcdef0123456790", Name: "db", State: "exited", Image: "example/db:latest"}}}}}}
	socket := &fakeSocket{}
	reg := newAgentRegistry()
	reg.register(dockerTestDevice, &AgentConn{DeviceID: dockerTestDevice, Conn: socket})
	h := &Handlers{telemetry: tel, devices: devices, commandRepo: testutil.NewMockCommandRepo(), agents: reg}
	return h, socket
}
func dockerTestViewer() *dockerViewer {
	return &dockerViewer{device: dockerTestDevice, q: make(chan []byte, 64), closed: make(chan struct{}), last: map[string]time.Time{}}
}
func TestDUX_SharedFollowAndBackfillIsolation(t *testing.T) {
	h, socket := newDockerTest(t)
	r := httptest.NewRequest("GET", "/", nil)
	v1, v2 := dockerTestViewer(), dockerTestViewer()
	h.dockerAttach(r, v1, "web", "")
	h.dockerAttach(r, v2, "web", "")
	require.Eventually(t, func() bool { return len(socket.messages("logs_follow")) == 1 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return len(socket.messages("logs_backfill")) == 1 }, time.Second, time.Millisecond, "same boundary coalesces")
	rt := h.dockerState()
	require.Equal(t, 2, rt.viewers)
	for _, b := range rt.backfills {
		require.Len(t, b.viewers, 2)
	}
	h.dockerDetach(v1, nil)
	h.dockerDetach(v2, nil)
	rt.mu.Lock()
	for _, s := range rt.streams {
		if s.linger != nil {
			s.linger.Stop()
		}
	}
	rt.mu.Unlock()
}
func TestDUX_SEC025_AttachDuringLingerKeepsStream(t *testing.T) {
	h, socket := newDockerTest(t)
	r := httptest.NewRequest("GET", "/", nil)
	v := dockerTestViewer()
	h.dockerAttach(r, v, "web", "")
	h.dockerDetach(v, nil)
	h.dockerAttach(r, v, "web", "")
	require.Eventually(t, func() bool { return len(socket.messages("logs_follow")) == 1 }, time.Second, time.Millisecond)
	require.Empty(t, socket.messages("logs_unfollow"))
	h.dockerState().mu.Lock()
	s := h.dockerState().streams[dockerTestDevice+"/web"]
	require.Nil(t, s.linger)
	h.dockerState().mu.Unlock()
}
func TestDUX_SEC014_ServerBoundsIndependentOfCapabilities(t *testing.T) {
	h, _ := newDockerTest(t)
	v := dockerTestViewer()
	rt := h.dockerState()
	stream := &dockerStream{id: strings.Repeat("a", 32), device: dockerTestDevice, name: "web", viewers: map[*dockerViewer]bool{v: true}}
	rt.streams[dockerTestDevice+"/web"] = stream
	raw, _ := json.Marshal(map[string]interface{}{"stream_id": stream.id, "lines": []map[string]interface{}{{"s": "o", "t": time.Now().Add(24 * time.Hour), "l": "password=secret-value"}}})
	h.dockerRelay(h.registry().get(dockerTestDevice), agentWSMessage{Type: "logs_data", Data: raw})
	require.Len(t, v.q, 1)
	out := string(<-v.q)
	assert.NotContains(t, out, "secret-value")
	assert.Contains(t, out, "agent_t")
	assert.Contains(t, out, "masked")
	raw = append(raw, make([]byte, 64<<10)...)
	h.dockerRelay(h.registry().get(dockerTestDevice), agentWSMessage{Type: "logs_data", Data: raw})
	assert.Empty(t, v.q)
}
func TestDUX_SEC025_UnknownStreamFrameTriggersBoundedUnfollow(t *testing.T) {
	h, socket := newDockerTest(t)
	for i := 0; i < 1000; i++ {
		raw, _ := json.Marshal(map[string]interface{}{"stream_id": fmt.Sprintf("%032x", i)})
		h.dockerRelay(h.registry().get(dockerTestDevice), agentWSMessage{Type: "logs_data", Data: raw})
	}
	assert.LessOrEqual(t, len(socket.messages("logs_unfollow")), 4)
	assert.Empty(t, h.dockerState().streams)
	assert.Len(t, h.dockerState().rate, 1)
}
func TestDUX_BackfillConcurrencyAndQueueCap(t *testing.T) {
	h, socket := newDockerTest(t)
	for i := 0; i < 30; i++ {
		h.dockerBackfill(dockerTestViewer(), fmt.Sprintf("container-%d", i), "")
	}
	require.Eventually(t, func() bool { return len(socket.messages("logs_backfill")) == 2 }, time.Second, time.Millisecond)
	assert.Len(t, h.dockerState().backfills, 18)
	for id := range h.dockerState().backfills {
		h.dockerEndBackfill(id)
	}
	assert.Empty(t, h.dockerState().backfills)
}
func TestDUX_BulkConfirmationBeforeAnyCommand(t *testing.T) {
	h, _ := newDockerTest(t)
	tel := h.telemetry.(*testutil.MockTelemetryRepo)
	snap := tel.Snapshots[dockerTestDevice][0]
	targets := []map[string]string{}
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("web-%d", i)
		snap.Data.Docker.Containers = append(snap.Data.Docker.Containers, models.ContainerInfo{ID: name, Name: name, State: "running"})
		targets = append(targets, map[string]string{"device_id": dockerTestDevice, "container": name})
	}
	tel.Snapshots[dockerTestDevice] = []models.TelemetrySnapshot{snap}
	body, _ := json.Marshal(map[string]interface{}{"action": "stop", "targets": targets})
	r := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DockerBulkActions(w, r)
	require.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "confirmation_required")
	assert.Empty(t, h.commandRepo.(*testutil.MockCommandRepo).Commands)
}
func TestDUX_SEC007_PreviewConsumedOnInvalidSelection(t *testing.T) {
	h, _ := newDockerTest(t)
	rt := h.dockerState()
	id := dockerRandom()
	require.Len(t, id, 32)
	rt.previews[id] = dockerPreview{device: dockerTestDevice, expires: time.Now().Add(time.Minute), items: map[string]map[string]interface{}{"item": {}}}
	router := chi.NewRouter()
	router.Post("/devices/{id}/execute", h.DockerPruneExecute)
	for i, want := range []int{400, 409} {
		body := fmt.Sprintf(`{"preview_id":"%s","selected":["foreign"]}`, id)
		r := httptest.NewRequest("POST", "/devices/"+dockerTestDevice+"/execute", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		assert.Equal(t, want, w.Code, "attempt %d", i)
	}
	assert.Empty(t, rt.previews)
}
func TestDUX_SEC015_ForeignOrMalformedCommandIDCleared(t *testing.T) {
	h, _ := newDockerTest(t)
	repo := h.commandRepo.(*testutil.MockCommandRepo)
	id := "00000000-0000-4000-8000-000000000002"
	repo.Commands[id] = &models.Command{ID: id, DeviceID: "other-device", Action: "docker_restart", Status: "running", TargetKeys: []string{models.TargetKeyContainer("web")}}
	router := chi.NewRouter()
	router.Post("/devices/{id}", func(w http.ResponseWriter, r *http.Request) {})
	_ = router
	for _, commandID := range []string{"invalid", id} {
		r := httptest.NewRequest("POST", "/", nil)
		ctx := chi.NewRouteContext()
		ctx.URLParams.Add("id", dockerTestDevice)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
		e := models.DockerEvent{ContainerName: "web", CommandID: commandID}
		h.validateDockerEventCommand(r, &e)
		assert.Empty(t, e.CommandID)
	}
}
func TestDUX_SEC004_RedactionDoesNotMutateTelemetry(t *testing.T) {
	data := &models.FullTelemetryData{Docker: &models.DockerInfo{Containers: []models.ContainerInfo{{Name: "web", Labels: map[string]string{"API_KEY": "secret-value", "com.docker.compose.project": "web"}, Env: []models.EnvVar{{Key: "PASSWORD", Value: "secret-value"}}}}}}
	safe := sanitizeDockerTelemetry(data)
	assert.Equal(t, "secret-value", data.Docker.Containers[0].Labels["API_KEY"])
	assert.NotEqual(t, "secret-value", safe.Docker.Containers[0].Labels["API_KEY"])
	assert.NotEqual(t, "secret-value", safe.Docker.Containers[0].Env[0].Value)
}
