package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

func TestExtractPrimaryIP(t *testing.T) {
	tests := []struct {
		name string
		data *models.FullTelemetryData
		want string
	}{
		{
			name: "nil network",
			data: &models.FullTelemetryData{Network: nil},
			want: "",
		},
		{
			name: "no interfaces",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{Interfaces: nil},
			},
			want: "",
		},
		{
			name: "loopback only",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "lo", State: "UP", IPv4: []string{"127.0.0.1/8"}},
					},
				},
			},
			want: "",
		},
		{
			name: "interface down",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "DOWN", IPv4: []string{"192.168.1.10/24"}},
					},
				},
			},
			want: "",
		},
		{
			name: "CIDR stripping",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"10.0.0.5/24"}},
					},
				},
			},
			want: "10.0.0.5",
		},
		{
			name: "bare IP without CIDR",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"192.168.1.100"}},
					},
				},
			},
			want: "192.168.1.100",
		},
		{
			name: "first valid IP wins",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "lo", State: "UP", IPv4: []string{"127.0.0.1"}},
						{Name: "eth0", State: "UP", IPv4: []string{"10.0.0.1/24"}},
						{Name: "eth1", State: "UP", IPv4: []string{"172.16.0.1/16"}},
					},
				},
			},
			want: "10.0.0.1",
		},
		{
			name: "skip empty strings",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"", "10.0.0.2/24"}},
					},
				},
			},
			want: "10.0.0.2",
		},
		{
			name: "skip 127.0.0.1 on non-lo interface",
			data: &models.FullTelemetryData{
				Network: &models.NetworkInfo{
					Interfaces: []models.NetworkInterface{
						{Name: "eth0", State: "UP", IPv4: []string{"127.0.0.1", "192.168.1.5"}},
					},
				},
			},
			want: "192.168.1.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPrimaryIP(tt.data)
			assert.Equal(t, tt.want, got)
		})
	}
}

// [AC-024] Old agents and old servers remain compatible: a pre-PATCH-GATE
// telemetry payload (no classification / held-package / reboot-required
// keys) is accepted with 2xx and decodes to zero values, and a PATCH-GATE
// UpdateInfo marshals into a shape an old-server decoder (permissive,
// unknown fields ignored) still accepts.
func TestTelemetry_AC024_OldAgentCompat(t *testing.T) {
	t.Run("[AC-024] pre-PATCH-GATE payload accepted with zero-value decode", func(t *testing.T) {
		telRepo := testutil.NewMockTelemetryRepo()
		devRepo := testutil.NewMockDeviceRepo()
		devRepo.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "old-host"}

		hub := websocket.NewHub()
		go hub.Run()
		gen := events.NewGenerator(testutil.NewMockEventRepo(), hub,
			testutil.NewMockAlertRuleRepo(), testutil.NewMockDispatcher(), testutil.NewMockCommandRepo())

		h := &Handlers{telemetry: telRepo, devices: devRepo, hub: hub, eventGen: gen}

		r := chi.NewRouter()
		r.Post("/api/v1/devices/{id}/telemetry", h.Telemetry)

		// Exactly the update keys a pre-PATCH-GATE agent sends.
		payload := `{
			"device_id": "dev-1",
			"data": {
				"updates": {
					"package_manager": "apt",
					"total_installed": 1200,
					"pending_updates": 3,
					"pending_security_count": 1,
					"pending_kernel_update": false,
					"unattended_upgrades": true,
					"updates": [{"name":"curl","current_ver":"8.4","new_ver":"8.5","is_security":false}]
				}
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-1/telemetry",
			strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "[AC-024] old-agent push must be accepted")

		stored := telRepo.Snapshots["dev-1"]
		require.Len(t, stored, 1)
		upd := stored[0].Data.Updates
		require.NotNil(t, upd)
		assert.Empty(t, upd.HeldPackages, "[AC-024] absent held_packages decodes empty")
		assert.Empty(t, upd.HoldEnforcement)
		assert.False(t, upd.RebootRequired)
		assert.Zero(t, upd.PendingRebootClassCount)
		assert.Empty(t, upd.Updates[0].Class, "[AC-024] absent class decodes empty")
	})

	t.Run("[AC-024] new UpdateInfo decodes into an old-shape struct without error", func(t *testing.T) {
		newInfo := models.UpdateInfo{
			PackageManager:          "apt",
			PendingUpdates:          2,
			PendingRebootClassCount: 1,
			HeldPackages:            []string{"nvidia-driver-550"},
			HoldEnforcement:         "active",
			RebootRequired:          true,
			RebootRequiredReasons:   []string{"linux-image-6.8.0-45-generic"},
			Updates: []models.PendingUpdate{
				{Name: "nvidia-driver-550", NewVer: "550.95", Class: "gpu_driver"},
			},
		}
		data, err := json.Marshal(newInfo)
		require.NoError(t, err)

		// The pre-PATCH-GATE UpdateInfo shape (permissive decoder drops
		// unknown keys — the ingest path has no DisallowUnknownFields).
		var oldShape struct {
			PackageManager      string `json:"package_manager"`
			PendingUpdates      int    `json:"pending_updates"`
			PendingKernelUpdate bool   `json:"pending_kernel_update"`
			Updates             []struct {
				Name   string `json:"name"`
				NewVer string `json:"new_ver"`
			} `json:"updates"`
		}
		require.NoError(t, json.Unmarshal(data, &oldShape),
			"[AC-024] old servers must decode new-agent payloads without error")
		assert.Equal(t, "apt", oldShape.PackageManager)
		assert.Len(t, oldShape.Updates, 1)
	})
}
