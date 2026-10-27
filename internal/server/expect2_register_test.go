package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXPECT-002 INFO-5 / X-20 with the real offline detector: a re-registration
// counts as a heartbeat, so a restart that registers before the old heartbeat is
// 45 s old is not marked offline by the next detector pass. The handler-level
// check is TestEX2_AC005_StaleHeartbeatAfterFastReturnNoAlert in package
// handlers; this one runs RegisterDevice and offlineDetector.check together.
func TestEX2_AC005_RealDetectorSeesTheRegistrationHeartbeat(t *testing.T) {
	r := newOfflineRig(t, time.Time{})
	hub := websocket.NewHub()
	go hub.Run()
	r.devices.APIKeys["key-hawaii"] = "hawaii"
	h := handlers.New(handlers.HandlerDeps{
		Devices: r.devices, AdminRepo: testutil.NewMockAdminRepo(""), Hub: hub, EventGen: r.gen, Clock: r.clock,
	})

	r.device("hawaii", models.DeviceStatusOnline, 0)  // last heartbeat at T
	r.device("vermont", models.DeviceStatusOnline, 0) // the control: no registration

	// The restarted agent registers at T + 40 s ...
	r.now = offlineBase.Add(40 * time.Second)
	body, _ := json.Marshal(models.DeviceRegistration{DeviceID: "hawaii", Hostname: "hawaii", Arch: "amd64", AgentVersion: "2.80.0"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/register", strings.NewReader(string(body)))
	req.Header.Set("X-rIOt-Key", "key-hawaii")
	req.RemoteAddr = "203.0.113.9:4444"
	rec := httptest.NewRecorder()
	h.RegisterDevice(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// ... the detector passes at T + 46 s, before the new process's first heartbeat.
	r.now = offlineBase.Add(46 * time.Second)
	r.det.check(context.Background())

	assert.Equal(t, models.DeviceStatusOnline, r.status("hawaii"),
		"[AC-005, X-20] the registration refreshed the heartbeat: no stale-heartbeat transition after the return")
	assert.Equal(t, models.DeviceStatusOffline, r.status("vermont"),
		"control: a device with the old heartbeat and no registration is marked offline at the same pass")
	var hosts []string
	for _, e := range r.offlineEvents() {
		hosts = append(hosts, e.DeviceID)
	}
	assert.Equal(t, []string{"vermont"}, hosts, "only the control produced a device_offline event")
}
