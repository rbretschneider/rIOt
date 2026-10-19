package db

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestDUX_PG_ConflictProbeAndExclusive(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("host-a")
	repo := e.repo()
	ctx := context.Background()
	first := newQueued("00000000-0000-4000-8000-000000000001", "host-a", "docker_restart", models.TargetKeyContainer("web"))
	first.IsDocker = true
	holder, err := repo.CreateIfNoConflict(ctx, first)
	require.NoError(t, err)
	require.Nil(t, holder)
	other := newQueued("00000000-0000-4000-8000-000000000002", "host-a", "docker_restart", models.TargetKeyContainer("db"))
	other.IsDocker = true
	other.ProbeKeys = []string{models.TargetKeyCompose("/srv/stacks/example")}
	holder, err = repo.CreateIfNoConflict(ctx, other)
	require.NoError(t, err)
	require.Nil(t, holder, "two member restarts may proceed")
	prune := newQueued("00000000-0000-4000-8000-000000000003", "host-a", "docker_remove_items", "docker:host")
	prune.IsDocker = true
	prune.DockerExclusive = true
	holder, err = repo.CreateIfNoConflict(ctx, prune)
	require.NoError(t, err)
	require.NotNil(t, holder, "prune excludes every active Docker command")
	e.Exec(`UPDATE commands SET status='succeeded'`)
	holder, err = repo.CreateIfNoConflict(ctx, prune)
	require.NoError(t, err)
	require.Nil(t, holder)
	later := newQueued("00000000-0000-4000-8000-000000000004", "host-a", "docker_start", models.TargetKeyContainer("other"))
	later.IsDocker = true
	holder, err = repo.CreateIfNoConflict(ctx, later)
	require.NoError(t, err)
	require.NotNil(t, holder)
	require.Equal(t, prune.ID, holder.ID)
}
func TestDUX_PG_CommandActorStepsAndAuditSurviveDeviceDeletion(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("host-a")
	repo := e.repo()
	c := newQueued("00000000-0000-4000-8000-000000000011", "host-a", "docker_restart", models.TargetKeyContainer("web"))
	c.ActorKind = "operator"
	c.ActorAuth = "password"
	c.ActorName = "Admin"
	c.ActorSub = "local-admin"
	c.Origin = models.OriginUser
	_, err := repo.CreateIfNoConflict(context.Background(), c)
	require.NoError(t, err)
	row, err := repo.GetByID(context.Background(), c.ID)
	require.NoError(t, err)
	assert.Equal(t, "local-admin", row.ActorSub)
	steps := []models.DockerCommandStep{{Seq: 1, Name: "restart", StartedAt: pgNow, Outcome: "succeeded", Output: "finished"}}
	results := []models.DockerTargetResult{{Container: "web", Outcome: "succeeded"}}
	row, applied, err := repo.Transition(context.Background(), models.CommandTransition{ID: c.ID, DeviceID: c.DeviceID, Actor: models.ActorAgent, To: models.CommandSucceeded, Now: pgNow, Steps: steps, TargetResults: results})
	require.NoError(t, err)
	require.True(t, applied)
	require.Len(t, row.Steps, 1)
	require.Len(t, row.TargetResults, 1)
	e.Exec(`DELETE FROM devices WHERE id='host-a'`)
	var count int
	require.NoError(t, e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM docker_audit WHERE device_id='host-a' AND kind='command'`).Scan(&count))
	assert.Equal(t, 1, count)
}
func TestDUX_PG_InventoryBoundCadenceAndUnchanged(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("host-a")
	repo := NewDockerRepo(&DB{Pool: e.Pool})
	ctx := context.Background()
	images := make([]models.DockerImage, 2100)
	for i := range images {
		images[i] = models.DockerImage{ID: "sha256:" + strings.Repeat("a", 64), Tags: []string{strings.Repeat("a", 900)}, Size: 10}
	}
	v := &models.DockerInventory{Hash: "first", Images: images}
	require.NoError(t, repo.SaveInventory(ctx, "host-a", v))
	got, err := repo.Inventory(ctx, "host-a")
	require.NoError(t, err)
	assert.Len(t, got.Images, 2000)
	assert.LessOrEqual(t, len(got.Images[0].Tags[0]), 512)
	assert.True(t, got.Truncated)
	require.NoError(t, repo.SaveInventory(ctx, "host-a", &models.DockerInventory{Hash: "changed", Images: []models.DockerImage{{ID: "other"}}}))
	got, err = repo.Inventory(ctx, "host-a")
	require.NoError(t, err)
	assert.Equal(t, "first", got.Hash, "changed writes are limited to 14 minute cadence")
	before := got.CollectedAt
	require.NoError(t, repo.SaveInventory(ctx, "host-a", &models.DockerInventory{Hash: "first", Unchanged: true}))
	got, err = repo.Inventory(ctx, "host-a")
	require.NoError(t, err)
	assert.Len(t, got.Images, 2000)
	assert.False(t, got.CollectedAt.Before(before))
	assert.Equal(t, 900, len(v.Images[0].Tags[0]), "normalization must not mutate caller")
}
func TestDUX_PG_StoredHistoryNameKeysetAcrossRecreates(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("host-a")
	repo := NewDockerRepo(&DB{Pool: e.Pool})
	now := time.Now().UTC().Truncate(time.Microsecond)
	e.Exec(`INSERT INTO container_logs(device_id,container_id,container_name,timestamp,stream,line,agent_ts) VALUES('host-a','old','web',$1,'stdout','first',NULL),('host-a','new','web',$1,'stderr','second',$2),('host-a','new','other',$1,'stdout','foreign',NULL)`, now, now.Add(-48*time.Hour))
	rows, err := repo.Logs(context.Background(), "host-a", []string{"web"}, now.Add(-time.Hour), now.Add(time.Minute), time.Time{}, 0, false, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "second", rows[0].Line)
	assert.NotNil(t, rows[0].AgentTS)
	older, err := repo.Logs(context.Background(), "host-a", []string{"web"}, now.Add(-time.Hour), now.Add(time.Minute), rows[0].Timestamp, rows[0].ID, false, 10)
	require.NoError(t, err)
	require.Len(t, older, 1)
	assert.Equal(t, "first", older[0].Line)
}
func TestDUX_PG_AttachAuditStormBoundedAndCoalesced(t *testing.T) {
	e := newPGEnv(t, 0)
	repo := NewDockerRepo(&DB{Pool: e.Pool})
	ctx := context.Background()
	id, err := repo.StartLogAudit(ctx, "host-a", "web", "local-admin", "192.0.2.10")
	require.NoError(t, err)
	require.NoError(t, repo.EndLogAudit(ctx, id))
	again, err := repo.StartLogAudit(ctx, "host-a", "web", "local-admin", "192.0.2.10")
	require.NoError(t, err)
	assert.Equal(t, id, again)
	for i := 0; i < 100; i++ {
		_, err := repo.StartLogAudit(ctx, "host-a", strings.Repeat("x", i+1), "local-admin", "192.0.2.10")
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, e.Pool.QueryRow(ctx, `SELECT count(*) FROM docker_audit WHERE device_id='host-a'`).Scan(&count))
	assert.LessOrEqual(t, count, 33)
}
