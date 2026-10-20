package db

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDUX_AC064_ProjectionWhitelistBeforeTransfer(t *testing.T) {
	raw, err := os.ReadFile("telemetry_repo.go")
	require.NoError(t, err)
	body := methodBody(t, string(raw), "GetFleetContainerLeaderboard")
	assert.Contains(t, body, "jsonb_build_object")
	assert.NotContains(t, body, "jsonb_path_query_array")
	for _, forbidden := range []string{"'env',", "'mounts',", "'networks',", "'labels', c->'labels'"} {
		assert.NotContains(t, body, forbidden)
	}
}
func TestDUX_PG_FleetProjectionDropsHeavyFieldsAndKeepsMetadata(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("host-a")
	ctx := context.Background()
	now := time.Now().UTC()
	data := map[string]interface{}{"docker": map[string]interface{}{"available": true, "containers": []interface{}{map[string]interface{}{"id": strings.Repeat("a", 64), "name": "web", "image": "example/web:latest", "state": "running", "status": "Up", "health_status": "healthy", "mem_limit_configured": 1234, "update_available": true, "image_id": "sha256:example", "ports": []interface{}{map[string]interface{}{"container_port": "80", "host_port": "8080", "protocol": "tcp", "host_ip": "192.0.2.10", "private": "must-not-transfer"}}, "labels": map[string]interface{}{"com.docker.compose.project": "example", "com.docker.compose.project.working_dir": "/srv/stacks/example", "riot.name": "Web", "API_KEY": "must-not-transfer", "unrelated": strings.Repeat("x", 1<<20)}, "env": strings.Repeat("secret-env", 1<<16), "mounts": []interface{}{map[string]interface{}{"source": "must-not-transfer"}}, "networks": []interface{}{map[string]interface{}{"name": "must-not-transfer"}}}}}}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	env.Exec(`INSERT INTO telemetry_snapshots(device_id,timestamp,data) VALUES('host-a',$1,$2)`, now.Add(-time.Minute), raw)
	env.Exec(`INSERT INTO telemetry_snapshots(device_id,timestamp,data) VALUES('host-a',$1,'{"trimmed":true,"dropped_sections":["docker"]}')`, now)
	rows, err := NewTelemetryRepo(&DB{Pool: env.Pool}).GetFleetContainerLeaderboard(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Containers, 1)
	c := rows[0].Containers[0]
	assert.Equal(t, "web", c.Name)
	assert.Equal(t, "healthy", c.HealthStatus)
	assert.EqualValues(t, 1234, c.MemLimitConfigured)
	assert.Equal(t, "example", c.Labels["com.docker.compose.project"])
	assert.Equal(t, "/srv/stacks/example", c.Labels["com.docker.compose.project.working_dir"])
	assert.NotContains(t, c.Labels, "API_KEY")
	assert.NotContains(t, c.Labels, "unrelated")
	require.Len(t, c.Ports, 1)
	assert.Equal(t, "8080", c.Ports[0].HostPort)
	encoded, err := json.Marshal(rows)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "must-not-transfer")
	assert.Less(t, len(encoded), 4096)
}
