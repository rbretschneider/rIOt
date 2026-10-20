package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDUX_AC064_HostProjectionRetainsOmittedDockerCapabilities(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("projection-fixture")
	e.Exec(`INSERT INTO telemetry_snapshots(device_id,timestamp,data) VALUES
	('projection-fixture',now()-interval '1 minute','{"docker":{"available":true,"capabilities":["docker_meta_v1"],"container_logs_enabled":false,"containers":[{"name":"riot","image":"riot-server:latest"}]}}'),
	('projection-fixture',now(),'{"trimmed":true,"dropped_sections":["docker"]}')`)
	rows, err := NewTelemetryRepo(&DB{Pool: e.Pool}).GetDockerHostProjection(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Available)
	require.Equal(t, []string{"docker_meta_v1"}, rows[0].Capabilities)
	require.NotNil(t, rows[0].ContainerLogsEnabled)
	require.False(t, *rows[0].ContainerLogsEnabled)
	require.Equal(t, "server", rows[0].SelfRole)
}

func TestDockerProjectionsLatestValidPerHost(t *testing.T) {
	e := newPGEnv(t, 0)
	for _, id := range []string{"docker-host", "cleared-host", "no-history"} {
		e.seedDevice(id)
	}
	e.Exec(`INSERT INTO telemetry_snapshots(device_id,timestamp,data) VALUES
	('docker-host',now()-interval '2 minutes','{"docker":{"available":true,"capabilities":["current"],"containers":[{"name":"current","state":"running"}]}}'),
	('docker-host',now()-interval '1 minute','{"trimmed":true,"dropped_sections":["docker"]}'),
	('docker-host',now()+interval '1 day','{"docker":{"available":true,"capabilities":["future"],"containers":[{"name":"future","state":"running"}]}}'),
	('cleared-host',now()-interval '2 minutes','{"docker":{"available":true,"containers":[{"name":"old","state":"running"}]}}'),
	('cleared-host',now()-interval '1 minute','{"docker":{"available":false,"containers":[]}}')`)
	r := NewTelemetryRepo(&DB{Pool: e.Pool})
	hosts, err := r.GetDockerHostProjection(context.Background())
	require.NoError(t, err)
	require.Len(t, hosts, 2)
	require.Equal(t, "cleared-host", hosts[0].DeviceID)
	require.False(t, hosts[0].Available)
	require.Equal(t, "docker-host", hosts[1].DeviceID)
	require.Equal(t, []string{"current"}, hosts[1].Capabilities)
	fleet, err := r.GetFleetContainerLeaderboard(context.Background())
	require.NoError(t, err)
	require.Len(t, fleet, 2)
	require.Empty(t, fleet[0].Containers)
	require.Len(t, fleet[1].Containers, 1)
	require.Equal(t, "current", fleet[1].Containers[0].Name)
}
