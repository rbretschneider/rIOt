package handlers

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"net/http/httptest"
	"testing"
)

type dockerHostProjectionSpy struct {
	*testutil.MockTelemetryRepo
	calls int
}

func (s *dockerHostProjectionSpy) GetDockerHostProjection(context.Context) ([]db.DockerHostProjection, error) {
	s.calls++
	return []db.DockerHostProjection{{DeviceID: "host", Available: true, Capabilities: []string{"docker_meta_v1"}}}, nil
}
func (s *dockerHostProjectionSpy) GetLatestSnapshot(context.Context, string) (*models.TelemetrySnapshot, error) {
	panic("host listing decoded full telemetry")
}
func TestDUX_AC064_HostsUseProjection(t *testing.T) {
	devices := testutil.NewMockDeviceRepo()
	devices.Devices["host"] = &models.Device{ID: "host", Hostname: "host-a"}
	telemetry := &dockerHostProjectionSpy{MockTelemetryRepo: testutil.NewMockTelemetryRepo()}
	h := &Handlers{devices: devices, telemetry: telemetry}
	rec := httptest.NewRecorder()
	h.DockerHosts(rec, httptest.NewRequest("GET", "/api/v1/docker/hosts", nil))
	if rec.Code != 200 || telemetry.calls != 1 {
		t.Fatalf("projection not used: %d %d", rec.Code, telemetry.calls)
	}
}
func TestDUX_SEC011_PrimaryIPRejectsHostnamesAndMarkup(t *testing.T) {
	for _, ip := range []string{"javascript:alert(1)", "host.example.com", "192.0.2.10:8080", "127.0.0.8", "0.0.0.0"} {
		data := &models.FullTelemetryData{Network: &models.NetworkInfo{Interfaces: []models.NetworkInterface{{Name: "eth0", State: "UP", IPv4: []string{ip}}}}}
		if got := extractPrimaryIP(data); got != "" {
			t.Errorf("invalid IP stored: %s", got)
		}
	}
}
