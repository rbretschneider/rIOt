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
