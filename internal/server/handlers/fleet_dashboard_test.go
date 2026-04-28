package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFleetDashHandlers creates a minimal Handlers instance for fleet dashboard tests.
func newFleetDashHandlers(t *testing.T) (*Handlers, *testutil.MockTelemetryRepo, *testutil.MockDeviceRepo) {
	t.Helper()
	telRepo := testutil.NewMockTelemetryRepo()
	devRepo := testutil.NewMockDeviceRepo()
	h := &Handlers{
		telemetry: telRepo,
		devices:   devRepo,
	}
	return h, telRepo, devRepo
}

// --- [AD-013] window parameter validation ---

func TestFleetHeartbeats_WindowParam(t *testing.T) {
	t.Run("[AD-013] window parameter validation", func(t *testing.T) {
		type testCase struct {
			name       string
			url        string
			wantStatus int
			wantOK     bool // true if we expect a 200
		}

		cases := []testCase{
			// Valid: key absent → default 60m
			{name: "missing → default 60m", url: "/api/v1/fleet/heartbeats", wantStatus: http.StatusOK, wantOK: true},
			// Valid inputs
			{name: "60m → 60m OK", url: "/api/v1/fleet/heartbeats?window=60m", wantStatus: http.StatusOK, wantOK: true},
			{name: "1m → 1m OK", url: "/api/v1/fleet/heartbeats?window=1m", wantStatus: http.StatusOK, wantOK: true},
			{name: "3600s → 60m OK", url: "/api/v1/fleet/heartbeats?window=3600s", wantStatus: http.StatusOK, wantOK: true},
			{name: "1s → 1s OK", url: "/api/v1/fleet/heartbeats?window=1s", wantStatus: http.StatusOK, wantOK: true},
			// Invalid: present-but-empty → 400
			{name: "empty string → 400", url: "/api/v1/fleet/heartbeats?window=", wantStatus: http.StatusBadRequest},
			// Invalid: zero duration
			{name: "0m → 400", url: "/api/v1/fleet/heartbeats?window=0m", wantStatus: http.StatusBadRequest},
			{name: "0s → 400", url: "/api/v1/fleet/heartbeats?window=0s", wantStatus: http.StatusBadRequest},
			// Invalid: leading zero
			{name: "01m leading zero → 400", url: "/api/v1/fleet/heartbeats?window=01m", wantStatus: http.StatusBadRequest},
			// Invalid: wrong case / suffix
			{name: "60M uppercase → 400", url: "/api/v1/fleet/heartbeats?window=60M", wantStatus: http.StatusBadRequest},
			{name: "60h hour suffix → 400", url: "/api/v1/fleet/heartbeats?window=60h", wantStatus: http.StatusBadRequest},
			{name: "60min multi-char suffix → 400", url: "/api/v1/fleet/heartbeats?window=60min", wantStatus: http.StatusBadRequest},
			// Invalid: missing suffix/prefix
			{name: "60 no unit → 400", url: "/api/v1/fleet/heartbeats?window=60", wantStatus: http.StatusBadRequest},
			{name: "m no prefix → 400", url: "/api/v1/fleet/heartbeats?window=m", wantStatus: http.StatusBadRequest},
			// Invalid: over cap
			{name: "61m over cap → 400", url: "/api/v1/fleet/heartbeats?window=61m", wantStatus: http.StatusBadRequest},
			{name: "3601s over cap → 400", url: "/api/v1/fleet/heartbeats?window=3601s", wantStatus: http.StatusBadRequest},
			// Invalid: prefix too long (>5 digits)
			{name: "100000m 6 digits → 400", url: "/api/v1/fleet/heartbeats?window=100000m", wantStatus: http.StatusBadRequest},
			{name: "9999999999m absurd → 400", url: "/api/v1/fleet/heartbeats?window=9999999999m", wantStatus: http.StatusBadRequest},
			// Invalid: sign
			{name: "-1m negative → 400", url: "/api/v1/fleet/heartbeats?window=-1m", wantStatus: http.StatusBadRequest},
			{name: "+1m plus sign → 400", url: "/api/v1/fleet/heartbeats?window=%2B1m", wantStatus: http.StatusBadRequest},
			// Invalid: decimal / exponent
			{name: "1.5m decimal → 400", url: "/api/v1/fleet/heartbeats?window=1.5m", wantStatus: http.StatusBadRequest},
			{name: "1e2m exponent → 400", url: "/api/v1/fleet/heartbeats?window=1e2m", wantStatus: http.StatusBadRequest},
			// Invalid: whitespace (use %20 to avoid httptest panic)
			{name: "' 60m' leading space → 400", url: "/api/v1/fleet/heartbeats?window=%2060m", wantStatus: http.StatusBadRequest},
			{name: "'60m ' trailing space → 400", url: "/api/v1/fleet/heartbeats?window=60m%20", wantStatus: http.StatusBadRequest},
			// Invalid: comma
			{name: "60m,30m comma → 400", url: "/api/v1/fleet/heartbeats?window=60m%2C30m", wantStatus: http.StatusBadRequest},
			// Invalid: non-ASCII
			{name: "abc non-numeric → 400", url: "/api/v1/fleet/heartbeats?window=abc", wantStatus: http.StatusBadRequest},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				h, _, _ := newFleetDashHandlers(t)
				req := httptest.NewRequest("GET", tc.url, nil)
				rec := httptest.NewRecorder()
				h.FleetHeartbeats(rec, req)
				assert.Equal(t, tc.wantStatus, rec.Code,
					"expected HTTP %d for url=%q, got %d (body: %s)",
					tc.wantStatus, tc.url, rec.Code, rec.Body.String())

				if tc.wantStatus == http.StatusBadRequest {
					var body map[string]string
					err := json.NewDecoder(rec.Body).Decode(&body)
					assert.NoError(t, err, "400 response should be valid JSON")
					assert.Equal(t, "invalid window parameter", body["error"],
						"400 body should contain error=invalid window parameter")
				}
			})
		}
	})
}

func TestFleetHeartbeats_SinceTimestamp(t *testing.T) {
	t.Run("[AD-013] resolved since timestamp equals now minus duration within epsilon", func(t *testing.T) {
		h, _, _ := newFleetDashHandlers(t)

		before := time.Now().UTC()
		req := httptest.NewRequest("GET", "/api/v1/fleet/heartbeats?window=30m", nil)
		rec := httptest.NewRecorder()
		h.FleetHeartbeats(rec, req)
		after := time.Now().UTC()

		require.Equal(t, http.StatusOK, rec.Code)

		var resp FleetHeartbeatsResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

		expectedSinceMin := before.Add(-30 * time.Minute)
		expectedSinceMax := after.Add(-30 * time.Minute)

		assert.True(t, !resp.Since.Before(expectedSinceMin) && !resp.Since.After(expectedSinceMax.Add(time.Second)),
			"since=%v should be within epsilon of now-30m (range [%v, %v])",
			resp.Since, expectedSinceMin, expectedSinceMax)
	})
}

// --- FleetHeartbeats data tests ---

func TestFleetHeartbeats_EmptyFleet(t *testing.T) {
	t.Run("[AC-030] empty fleet returns empty devices map", func(t *testing.T) {
		h, _, _ := newFleetDashHandlers(t)
		req := httptest.NewRequest("GET", "/api/v1/fleet/heartbeats", nil)
		rec := httptest.NewRecorder()
		h.FleetHeartbeats(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var resp FleetHeartbeatsResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.NotNil(t, resp.Devices, "devices map should not be nil")
		assert.Empty(t, resp.Devices, "empty fleet should return empty devices map")
		assert.NotNil(t, resp.DevicesWithGPU, "devices_with_gpu should not be nil")
	})
}

func TestFleetHeartbeats_GroupsByDevice(t *testing.T) {
	t.Run("[AC-030] heartbeats grouped by device ID", func(t *testing.T) {
		h, telRepo, _ := newFleetDashHandlers(t)

		now := time.Now().UTC()
		telRepo.Heartbeats["dev-a"] = []models.Heartbeat{
			{DeviceID: "dev-a", Timestamp: now.Add(-10 * time.Minute), Data: models.HeartbeatData{CPUPercent: 20.0}},
			{DeviceID: "dev-a", Timestamp: now.Add(-5 * time.Minute), Data: models.HeartbeatData{CPUPercent: 25.0}},
		}
		telRepo.Heartbeats["dev-b"] = []models.Heartbeat{
			{DeviceID: "dev-b", Timestamp: now.Add(-3 * time.Minute), Data: models.HeartbeatData{CPUPercent: 50.0}},
		}

		req := httptest.NewRequest("GET", "/api/v1/fleet/heartbeats?window=60m", nil)
		rec := httptest.NewRecorder()
		h.FleetHeartbeats(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var resp FleetHeartbeatsResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

		assert.Len(t, resp.Devices, 2, "should have 2 device entries")
		assert.Len(t, resp.Devices["dev-a"], 2, "dev-a should have 2 heartbeats")
		assert.Len(t, resp.Devices["dev-b"], 1, "dev-b should have 1 heartbeat")
	})
}

func TestFleetHeartbeats_DBError(t *testing.T) {
	t.Run("[AC-030] DB error returns 500", func(t *testing.T) {
		h, telRepo, _ := newFleetDashHandlers(t)
		telRepo.Err = assert.AnError

		req := httptest.NewRequest("GET", "/api/v1/fleet/heartbeats", nil)
		rec := httptest.NewRecorder()
		h.FleetHeartbeats(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		var body map[string]string
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
		assert.Equal(t, "failed to fetch heartbeats", body["error"])
	})
}

// --- FleetContainers tests ---

func TestFleetContainers_AD011_DoesNotCallGetAllLatestSnapshots(t *testing.T) {
	t.Run("[AD-011] FleetContainers does NOT call GetAllLatestSnapshots", func(t *testing.T) {
		h, telRepo, _ := newFleetDashHandlers(t)

		// Reset the spy flag before the call.
		telRepo.GetAllLatestSnapshotsCalled = false

		req := httptest.NewRequest("GET", "/api/v1/fleet/containers", nil)
		rec := httptest.NewRecorder()
		h.FleetContainers(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code,
			"handler should succeed")
		assert.False(t, telRepo.GetAllLatestSnapshotsCalled,
			"[AD-011] FleetContainers must NOT call GetAllLatestSnapshots — only GetFleetContainerLeaderboard is allowed")
	})
}

func TestFleetContainers_EmptyFleet(t *testing.T) {
	t.Run("[AC-022] empty fleet returns empty array", func(t *testing.T) {
		h, _, _ := newFleetDashHandlers(t)
		req := httptest.NewRequest("GET", "/api/v1/fleet/containers", nil)
		rec := httptest.NewRecorder()
		h.FleetContainers(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var rows []FleetContainerRow
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&rows))
		assert.NotNil(t, rows, "should return empty array, not null")
		assert.Empty(t, rows)
	})
}

func TestFleetContainers_FlattenAndHostname(t *testing.T) {
	t.Run("[AC-020] containers are flattened with hostname join", func(t *testing.T) {
		h, telRepo, devRepo := newFleetDashHandlers(t)

		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "homelab-01"}

		updateTrue := true
		telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{
			{
				DeviceID:  "dev-1",
				Timestamp: time.Now(),
				Data: models.FullTelemetryData{
					Docker: &models.DockerInfo{
						Containers: []models.ContainerInfo{
							{
								ID:              "abc123",
								Name:            "plex",
								Image:           "linuxserver/plex:latest",
								State:           "running",
								CPUPercent:      12.5,
								MemUsage:        1048576,
								MemLimit:        4294967296,
								RestartCount:    0,
								UpdateAvailable: &updateTrue,
								Labels: map[string]string{
									"com.docker.compose.project": "media",
									"traefik.enable":             "true",
								},
							},
						},
					},
				},
			},
		}

		req := httptest.NewRequest("GET", "/api/v1/fleet/containers", nil)
		rec := httptest.NewRecorder()
		h.FleetContainers(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var rows []FleetContainerRow
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&rows))
		require.Len(t, rows, 1, "should have one container row")

		row := rows[0]
		assert.Equal(t, "dev-1", row.DeviceID)
		assert.Equal(t, "homelab-01", row.Hostname, "hostname should be joined from devices")
		assert.Equal(t, "plex", row.ContainerName)
		assert.Equal(t, "media", row.Stack, "stack should come from compose project label")
		assert.Equal(t, "running", row.State)
		assert.InDelta(t, 12.5, row.CPUPercent, 0.01)
		assert.Equal(t, true, *row.UpdateAvailable)
	})
}

func TestFleetContainers_AD012_OnlyComposeProjectLabelInStack(t *testing.T) {
	t.Run("[AD-012] only com.docker.compose.project label is used; other labels are not in response", func(t *testing.T) {
		h, telRepo, devRepo := newFleetDashHandlers(t)

		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "traefik-host"}

		// Simulate the Traefik basicauth label scenario from the security review.
		telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{
			{
				DeviceID:  "dev-1",
				Timestamp: time.Now(),
				Data: models.FullTelemetryData{
					Docker: &models.DockerInfo{
						Containers: []models.ContainerInfo{
							{
								ID:    "traefik123",
								Name:  "traefik",
								Image: "traefik:v3",
								State: "running",
								Labels: map[string]string{
									"com.docker.compose.project":                           "proxy",
									"traefik.http.middlewares.auth.basicauth.users":         "admin:$apr1$secret$hash",
									"org.opencontainers.image.description":                 "build metadata",
								},
							},
						},
					},
				},
			},
		}

		req := httptest.NewRequest("GET", "/api/v1/fleet/containers", nil)
		rec := httptest.NewRecorder()
		h.FleetContainers(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		// The raw response body must not contain sensitive label key or value.
		body := rec.Body.String()
		assert.NotContains(t, body, "traefik.http.middlewares.auth.basicauth.users",
			"[AD-012] sensitive label key must not appear in response body")
		assert.NotContains(t, body, "apr1$secret",
			"[AD-012] sensitive label value (htpasswd) must not appear in response body")
		assert.NotContains(t, body, "org.opencontainers.image.description",
			"[AD-012] OCI metadata label must not appear in response body")

		var rows []FleetContainerRow
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&rows))
		require.Len(t, rows, 1)
		assert.Equal(t, "proxy", rows[0].Stack, "stack derived from compose project label should still be present")
	})
}

func TestFleetContainers_DBError(t *testing.T) {
	t.Run("[AC-030] DB error returns 500", func(t *testing.T) {
		h, telRepo, _ := newFleetDashHandlers(t)
		telRepo.Err = assert.AnError

		req := httptest.NewRequest("GET", "/api/v1/fleet/containers", nil)
		rec := httptest.NewRecorder()
		h.FleetContainers(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		var body map[string]string
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
		assert.Equal(t, "failed to fetch fleet containers", body["error"])
	})
}

func TestFleetContainers_MultipleDevices(t *testing.T) {
	t.Run("[AC-019] containers flattened across multiple devices", func(t *testing.T) {
		h, telRepo, devRepo := newFleetDashHandlers(t)

		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "node-a"}
		devRepo.Devices["dev-2"] = &models.Device{ID: "dev-2", Hostname: "node-b"}

		telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{
			{DeviceID: "dev-1", Timestamp: time.Now(), Data: models.FullTelemetryData{
				Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
					{ID: "c1", Name: "nginx", State: "running"},
					{ID: "c2", Name: "redis", State: "running"},
				}},
			}},
		}
		telRepo.Snapshots["dev-2"] = []models.TelemetrySnapshot{
			{DeviceID: "dev-2", Timestamp: time.Now(), Data: models.FullTelemetryData{
				Docker: &models.DockerInfo{Containers: []models.ContainerInfo{
					{ID: "c3", Name: "postgres", State: "running"},
				}},
			}},
		}

		req := httptest.NewRequest("GET", "/api/v1/fleet/containers", nil)
		rec := httptest.NewRecorder()
		h.FleetContainers(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var rows []FleetContainerRow
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&rows))
		assert.Len(t, rows, 3, "should have 3 containers across 2 devices")
	})
}

// TestParseWindowParam_FullMatrix is the dedicated table-driven test for the
// AD-013 window parser matching the concrete table in the addendum.
func TestParseWindowParam_FullMatrix(t *testing.T) {
	t.Run("[AD-013] full input matrix from addendum concrete table", func(t *testing.T) {
		type row struct {
			name     string
			raw      string // raw query value after ?window= (URL-encoded)
			hasKey   bool   // true if ?window= key is present
			wantOK   bool
			wantDur  time.Duration
		}

		cases := []row{
			// Valid
			{name: "absent", raw: "", hasKey: false, wantOK: true, wantDur: 60 * time.Minute},
			{name: "60m", raw: "60m", hasKey: true, wantOK: true, wantDur: 60 * time.Minute},
			{name: "1m", raw: "1m", hasKey: true, wantOK: true, wantDur: 1 * time.Minute},
			{name: "3600s", raw: "3600s", hasKey: true, wantOK: true, wantDur: 60 * time.Minute},
			{name: "1s", raw: "1s", hasKey: true, wantOK: true, wantDur: time.Second},
			// Invalid
			{name: "empty present", raw: "", hasKey: true, wantOK: false},
			{name: "0m", raw: "0m", hasKey: true, wantOK: false},
			{name: "0s", raw: "0s", hasKey: true, wantOK: false},
			{name: "01m leading-zero", raw: "01m", hasKey: true, wantOK: false},
			{name: "60M uppercase", raw: "60M", hasKey: true, wantOK: false},
			{name: "60h hour", raw: "60h", hasKey: true, wantOK: false},
			{name: "60min multi-char", raw: "60min", hasKey: true, wantOK: false},
			{name: "60 no-unit", raw: "60", hasKey: true, wantOK: false},
			{name: "m no-prefix", raw: "m", hasKey: true, wantOK: false},
			{name: "61m over-cap", raw: "61m", hasKey: true, wantOK: false},
			{name: "3601s over-cap", raw: "3601s", hasKey: true, wantOK: false},
			{name: "100000m 6digits", raw: "100000m", hasKey: true, wantOK: false},
			{name: "9999999999m absurd", raw: "9999999999m", hasKey: true, wantOK: false},
			{name: "-1m negative", raw: "-1m", hasKey: true, wantOK: false},
			{name: "+1m plus", raw: "%2B1m", hasKey: true, wantOK: false},
			{name: "1.5m decimal", raw: "1.5m", hasKey: true, wantOK: false},
			{name: "1e2m exponent", raw: "1e2m", hasKey: true, wantOK: false},
			// Spaces: URL-encode as %20 so httptest.NewRequest does not panic
			{name: "leading-space", raw: "%2060m", hasKey: true, wantOK: false},
			{name: "trailing-space", raw: "60m%20", hasKey: true, wantOK: false},
			// Comma (URL-encoded)
			{name: "60m,30m comma", raw: "60m%2C30m", hasKey: true, wantOK: false},
			{name: "abc non-numeric", raw: "abc", hasKey: true, wantOK: false},
			// Added by QA Engineer — AD-013 mandates this case explicitly
			// Covers AD-013: full-width Unicode digits must be rejected
			// "６０ｍ" uses full-width characters U+FF16 U+FF10 U+FF4D — the regex
			// ^[1-9][0-9]{0,4}[ms]$ uses explicit ASCII character classes so these
			// are correctly rejected, but the test was missing from the mandatory matrix.
			{name: "full-width unicode 60m", raw: "%EF%BC%96%EF%BC%90%EF%BD%8D", hasKey: true, wantOK: false},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				var rawURL string
				if tc.hasKey {
					rawURL = "/api/v1/fleet/heartbeats?window=" + tc.raw
				} else {
					rawURL = "/api/v1/fleet/heartbeats"
				}
				req := httptest.NewRequest("GET", rawURL, nil)
				rec := httptest.NewRecorder()

				dur, ok := parseWindowParam(rec, req)
				assert.Equal(t, tc.wantOK, ok, "ok mismatch for name=%q raw=%q", tc.name, tc.raw)
				if tc.wantOK {
					assert.Equal(t, tc.wantDur, dur, "duration mismatch for name=%q raw=%q", tc.name, tc.raw)
				} else {
					assert.Equal(t, http.StatusBadRequest, rec.Code,
						"expected 400 for name=%q raw=%q", tc.name, tc.raw)
				}
			})
		}
	})
}

// TestFleetHeartbeats_DevicesWithGPU verifies the GPU device list is present.
func TestFleetHeartbeats_DevicesWithGPU(t *testing.T) {
	t.Run("[AC-005] devices_with_gpu is present in response", func(t *testing.T) {
		h, telRepo, _ := newFleetDashHandlers(t)

		gpuCount := 1
		telRepo.Snapshots["dev-gpu"] = []models.TelemetrySnapshot{
			{
				DeviceID:  "dev-gpu",
				Timestamp: time.Now(),
				Data: models.FullTelemetryData{
					GPUTelemetry: &models.GPUTelemetry{
						GPUs: make([]models.GPUDeviceMetrics, gpuCount),
					},
				},
			},
		}

		req := httptest.NewRequest("GET", "/api/v1/fleet/heartbeats", nil)
		rec := httptest.NewRecorder()
		h.FleetHeartbeats(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var resp FleetHeartbeatsResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.Contains(t, resp.DevicesWithGPU, "dev-gpu",
			"device with GPU telemetry should appear in devices_with_gpu")
	})
}

// TestFleetContainers_NoDockerDeviceSkipped ensures devices without docker data
// are skipped gracefully.
func TestFleetContainers_NoDockerDeviceSkipped(t *testing.T) {
	t.Run("[AC-022] devices without docker data are excluded", func(t *testing.T) {
		h, telRepo, devRepo := newFleetDashHandlers(t)

		devRepo.Devices["dev-no-docker"] = &models.Device{ID: "dev-no-docker", Hostname: "bare-metal"}
		telRepo.Snapshots["dev-no-docker"] = []models.TelemetrySnapshot{
			{DeviceID: "dev-no-docker", Timestamp: time.Now(), Data: models.FullTelemetryData{
				// No Docker field.
			}},
		}

		req := httptest.NewRequest("GET", "/api/v1/fleet/containers", nil)
		rec := httptest.NewRecorder()
		h.FleetContainers(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var rows []FleetContainerRow
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&rows))
		assert.Empty(t, rows, "device without docker data should contribute no rows")
	})
}

// Compile-time check: FleetContainerRow is defined in this package.
var _ db.FleetContainerProjection
