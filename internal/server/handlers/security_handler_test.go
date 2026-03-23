package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers for building test snapshots

func makeSecuritySnap(deviceID string, sec *models.SecurityInfo) models.TelemetrySnapshot {
	return models.TelemetrySnapshot{
		DeviceID: deviceID,
		Data:     models.FullTelemetryData{Security: sec},
	}
}

func makeDevice(id, hostname string) *models.Device {
	return &models.Device{ID: id, Hostname: hostname, Status: models.DeviceStatusOnline}
}

// --- SecurityDevices ---

func TestSecurityDevices_AC006_PendingSecurityCount(t *testing.T) {
	// AC-006: SecurityDevices handler populates pending_security_count from Updates telemetry.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			Updates:  &models.UpdateInfo{PendingSecurityCount: 5, UnattendedUpgrades: false},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/devices", nil)
	rec := httptest.NewRecorder()
	h.SecurityDevices(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var result []deviceSecurityInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	require.Len(t, result, 1)
	assert.Equal(t, 5, result[0].PendingSecurityCount, "pending_security_count must reflect Updates.PendingSecurityCount")
}

func TestSecurityDevices_AC007_UnattendedUpgradesTrue(t *testing.T) {
	// AC-007: SecurityDevices handler populates unattended_upgrades as *bool true when enabled.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			Updates:  &models.UpdateInfo{PendingSecurityCount: 0, UnattendedUpgrades: true},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/devices", nil)
	rec := httptest.NewRecorder()
	h.SecurityDevices(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var result []deviceSecurityInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	require.Len(t, result, 1)
	require.NotNil(t, result[0].UnattendedUpgrades, "unattended_upgrades must not be nil when update telemetry exists")
	assert.True(t, *result[0].UnattendedUpgrades, "unattended_upgrades must be true when UnattendedUpgrades is true")
}

func TestSecurityDevices_AC007_UnattendedUpgradesFalse(t *testing.T) {
	// AC-007: SecurityDevices handler populates unattended_upgrades as *bool false when disabled.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			Updates:  &models.UpdateInfo{PendingSecurityCount: 2, UnattendedUpgrades: false},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/devices", nil)
	rec := httptest.NewRecorder()
	h.SecurityDevices(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var result []deviceSecurityInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	require.Len(t, result, 1)
	require.NotNil(t, result[0].UnattendedUpgrades, "unattended_upgrades must not be nil when update telemetry exists")
	assert.False(t, *result[0].UnattendedUpgrades, "unattended_upgrades must be false when UnattendedUpgrades is false")
}

func TestSecurityDevices_AC007_UnattendedUpgradesNullWhenNoUpdateData(t *testing.T) {
	// AC-007: SecurityDevices handler returns null unattended_upgrades when no update telemetry.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := makeSecuritySnap("dev-1", &models.SecurityInfo{FirewallStatus: "active"})
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/devices", nil)
	rec := httptest.NewRecorder()
	h.SecurityDevices(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var result []deviceSecurityInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	require.Len(t, result, 1)
	assert.Nil(t, result[0].UnattendedUpgrades, "unattended_upgrades must be null when no update telemetry")
	assert.Equal(t, 0, result[0].PendingSecurityCount, "pending_security_count must be 0 when no update telemetry")
}

func TestSecurityDevices_AC006_CertsExpiringSoon(t *testing.T) {
	// AC-006 / AC-008: SecurityDevices handler counts certs expiring within 30 days per device.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			WebServers: &models.WebServerInfo{
				Servers: []models.ProxyServer{
					{
						Name: "nginx",
						Certs: []models.ProxyCert{
							{Subject: "example.com", DaysLeft: 10},  // expiring
							{Subject: "other.com", DaysLeft: 90},    // not expiring
							{Subject: "expired.com", DaysLeft: -5},  // expired — still <= 30
						},
					},
				},
			},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/devices", nil)
	rec := httptest.NewRecorder()
	h.SecurityDevices(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var result []deviceSecurityInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	require.Len(t, result, 1)
	assert.Equal(t, 2, result[0].CertsExpiringSoon, "must count certs with DaysLeft <= 30 (including expired)")
}

func TestSecurityDevices_AC006_NoCertsWhenNoWebServerData(t *testing.T) {
	// AC-006 / AC-009: SecurityDevices handler returns 0 certs_expiring_soon when no web server data.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := makeSecuritySnap("dev-1", &models.SecurityInfo{FirewallStatus: "active"})
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/devices", nil)
	rec := httptest.NewRecorder()
	h.SecurityDevices(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var result []deviceSecurityInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&result))
	require.Len(t, result, 1)
	assert.Equal(t, 0, result[0].CertsExpiringSoon, "certs_expiring_soon must be 0 when no web server telemetry")
}

// --- SecurityOverview ---

func TestSecurityOverview_AC008_CertsExpiringSoonCount(t *testing.T) {
	// AC-008: SecurityOverview handler counts fleet-wide certs expiring within 30 days.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")
	deviceRepo.Devices["dev-2"] = makeDevice("dev-2", "host2")

	snap1 := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			WebServers: &models.WebServerInfo{
				Servers: []models.ProxyServer{
					{Certs: []models.ProxyCert{
						{DaysLeft: 5},   // expiring
						{DaysLeft: 100}, // not expiring
					}},
				},
			},
		},
	}
	snap2 := models.TelemetrySnapshot{
		DeviceID: "dev-2",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			WebServers: &models.WebServerInfo{
				Servers: []models.ProxyServer{
					{Certs: []models.ProxyCert{
						{DaysLeft: 29}, // expiring (boundary: <= 30)
						{DaysLeft: 31}, // not expiring
					}},
				},
			},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap1)
	_ = telemetry.StoreSnapshot(nil, &snap2)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/overview", nil)
	rec := httptest.NewRecorder()
	h.SecurityOverview(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var overview securityOverview
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&overview))
	assert.Equal(t, 2, overview.CertsExpiringSoon, "must count certs with DaysLeft <= 30 across all devices")
	assert.Equal(t, 4, overview.TotalCerts, "must count all certs across all devices")
}

func TestSecurityOverview_AC009_CertsExpiringSoonZeroWhenNoCertData(t *testing.T) {
	// AC-009: SecurityOverview returns total_certs=0 and certs_expiring_soon=0 when no web server data.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := makeSecuritySnap("dev-1", &models.SecurityInfo{FirewallStatus: "active"})
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/overview", nil)
	rec := httptest.NewRecorder()
	h.SecurityOverview(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var overview securityOverview
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&overview))
	assert.Equal(t, 0, overview.CertsExpiringSoon, "certs_expiring_soon must be 0 when no web server telemetry")
	assert.Equal(t, 0, overview.TotalCerts, "total_certs must be 0 when no web server telemetry")
}

func TestSecurityOverview_AC008_CertsExpiringBoundary30Days(t *testing.T) {
	// AC-008: Cert with exactly 30 days left is counted as expiring (DaysLeft <= 30).
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			WebServers: &models.WebServerInfo{
				Servers: []models.ProxyServer{
					{Certs: []models.ProxyCert{{DaysLeft: 30}}},
				},
			},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/overview", nil)
	rec := httptest.NewRecorder()
	h.SecurityOverview(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var overview securityOverview
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&overview))
	assert.Equal(t, 1, overview.CertsExpiringSoon, "cert with exactly 30 days left must be counted as expiring")
}

func TestSecurityOverview_AC008_CertsExpiringBoundary31Days(t *testing.T) {
	// AC-008: Cert with 31 days left is NOT counted as expiring.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	snap := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: &models.SecurityInfo{FirewallStatus: "active"},
			WebServers: &models.WebServerInfo{
				Servers: []models.ProxyServer{
					{Certs: []models.ProxyCert{{DaysLeft: 31}}},
				},
			},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/overview", nil)
	rec := httptest.NewRecorder()
	h.SecurityOverview(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var overview securityOverview
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&overview))
	assert.Equal(t, 0, overview.CertsExpiringSoon, "cert with 31 days left must NOT be counted as expiring")
	assert.Equal(t, 1, overview.TotalCerts, "total_certs must still count non-expiring certs")
}

// Added by QA Engineer
// Covers AC-008: exposes a defect where SecurityOverview skips cert counting for
// devices that have web server telemetry but no security telemetry (sec == nil guard
// wraps the entire loop body including the cert counting block).
func TestSecurityOverview_AC008_CertsCountedWhenNoSecurityTelemetry(t *testing.T) {
	// A device may run the webservers collector but not the security collector.
	// The overview handler must still count its certificates.
	telemetry := testutil.NewMockTelemetryRepo()
	deviceRepo := testutil.NewMockDeviceRepo()
	deviceRepo.Devices["dev-1"] = makeDevice("dev-1", "host1")

	// Snapshot has web server data with an expiring cert, but Security is nil.
	snap := models.TelemetrySnapshot{
		DeviceID: "dev-1",
		Data: models.FullTelemetryData{
			Security: nil, // no security collector running
			WebServers: &models.WebServerInfo{
				Servers: []models.ProxyServer{
					{Certs: []models.ProxyCert{{DaysLeft: 10}}},
				},
			},
		},
	}
	_ = telemetry.StoreSnapshot(nil, &snap)

	h := &Handlers{telemetry: telemetry, devices: deviceRepo}
	req := httptest.NewRequest("GET", "/api/v1/security/overview", nil)
	rec := httptest.NewRecorder()
	h.SecurityOverview(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var overview securityOverview
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&overview))
	// This test is expected to FAIL because the current implementation skips
	// cert counting when sec == nil. This demonstrates the defect.
	assert.Equal(t, 1, overview.TotalCerts, "total_certs must count certs even when security telemetry is absent")
	assert.Equal(t, 1, overview.CertsExpiringSoon, "certs_expiring_soon must count expiring certs even when security telemetry is absent")
}
