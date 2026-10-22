package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Skips unless RIOT_TEST_PG_URL names a test database (see pgTestURL).
func TestLatestFinishedOSUpdates_PG_NewestFinishedPerDevice(t *testing.T) {
	env := newPGEnv(t, 0)
	env.seedDevice("dev-1")
	env.seedDevice("dev-2")
	env.seedDevice("dev-3")
	repo := env.repo()
	ctx := withDeadline(t, 30*time.Second)

	env.insertRaw(rawCommand{ID: "a1", DeviceID: "dev-1", Action: "os_update", Status: "failed", CreatedAt: pgNow.Add(-4 * time.Hour)})
	env.insertRaw(rawCommand{ID: "a2", DeviceID: "dev-1", Action: "os_update", Status: "succeeded", CreatedAt: pgNow.Add(-3 * time.Hour)})
	env.insertRaw(rawCommand{ID: "a3", DeviceID: "dev-1", Action: "os_update", Status: "expired", CreatedAt: pgNow.Add(-2 * time.Hour)})
	env.insertRaw(rawCommand{ID: "a4", DeviceID: "dev-1", Action: "os_update", Status: "cancelled", CreatedAt: pgNow.Add(-time.Hour)})
	env.insertRaw(rawCommand{ID: "a5", DeviceID: "dev-1", Action: "os_update", Status: "running", CreatedAt: pgNow})
	env.insertRaw(rawCommand{ID: "b1", DeviceID: "dev-2", Action: "os_update", Status: "timed_out", CreatedAt: pgNow})
	env.insertRaw(rawCommand{ID: "b2", DeviceID: "dev-2", Action: "reboot", Status: "succeeded", CreatedAt: pgNow.Add(time.Minute)})

	got, err := repo.LatestFinishedOSUpdates(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2, "dev-3 has no os_update, so it is absent")
	assert.Equal(t, "a2", got["dev-1"].ID, "expired, cancelled and running runs are ignored")
	assert.Equal(t, "b1", got["dev-2"].ID, "other actions are ignored")
}
