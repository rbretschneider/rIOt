package db

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type storedPolicy struct {
	IsStack             bool
	WorkDir             string
	Enabled             bool
	Triggered           bool
	ConsecutiveFailures int
}

func readPolicy(t *testing.T, e *pgEnv, device, target string) storedPolicy {
	t.Helper()
	var p storedPolicy
	err := e.Pool.QueryRow(context.Background(),
		`SELECT is_stack, compose_work_dir, enabled, last_triggered_at IS NOT NULL, consecutive_failures
		 FROM auto_update_policies WHERE device_id=$1 AND target=$2`, device, target).
		Scan(&p.IsStack, &p.WorkDir, &p.Enabled, &p.Triggered, &p.ConsecutiveFailures)
	require.NoError(t, err)
	return p
}

func TestAutoUpdateRepo_ListAllReturnsEveryDeviceInOneQuery(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("dev-b")
	e.seedDevice("dev-a")
	e.Exec(`INSERT INTO auto_update_policies (device_id, target, is_stack, compose_work_dir, enabled) VALUES
		('dev-b','web',false,'',true),
		('dev-a','media',true,'/srv/media',false),
		('dev-a','db',false,'',true)`)
	repo := NewAutoUpdateRepo(&DB{Pool: e.Pool})

	all, err := repo.ListAll(context.Background())
	require.NoError(t, err)
	require.Len(t, all, 3)
	got := []string{}
	for _, p := range all {
		got = append(got, p.DeviceID+"/"+p.Target)
	}
	assert.Equal(t, []string{"dev-a/db", "dev-a/media", "dev-b/web"}, got, "ordered by device, then target")
	assert.True(t, all[1].IsStack)
	assert.Equal(t, "/srv/media", all[1].ComposeWorkDir)
	assert.False(t, all[1].Enabled)
}

func TestAutoUpdateRepo_UpsertUnlessKindClashMatrix(t *testing.T) {
	cases := []struct {
		name           string
		existingStack  bool
		existingOn     bool
		saveStack      bool
		saveOn         bool
		wantStored     bool
		wantStackAfter bool
		wantOnAfter    bool
	}{
		{"same kind container replaces", false, true, false, false, true, false, false},
		{"same kind stack replaces", true, true, true, false, true, true, false},
		{"enabled stack refuses container on", true, true, false, true, false, true, true},
		{"enabled stack refuses container off", true, true, false, false, false, true, true},
		{"enabled container refuses stack on", false, true, true, true, false, false, true},
		{"disabled stack is replaced by container", true, false, false, true, true, false, true},
		{"disabled container is replaced by stack", false, false, true, true, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newPGEnv(t, 0)
			e.seedDevice("dev-1")
			e.Exec(`INSERT INTO auto_update_policies (device_id, target, is_stack, compose_work_dir, enabled, last_triggered_at, consecutive_failures)
				VALUES ('dev-1','app',$1,'/srv/app',$2, now(), 2)`, tc.existingStack, tc.existingOn)
			repo := NewAutoUpdateRepo(&DB{Pool: e.Pool})
			before := readPolicy(t, e, "dev-1", "app")

			workDir := ""
			if tc.saveStack {
				workDir = "/srv/app"
			}
			stored, err := repo.UpsertUnlessKindClash(context.Background(), &models.AutoUpdatePolicy{
				DeviceID: "dev-1", Target: "app", IsStack: tc.saveStack, ComposeWorkDir: workDir, Enabled: tc.saveOn,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantStored, stored)
			after := readPolicy(t, e, "dev-1", "app")
			if !tc.wantStored {
				assert.Equal(t, before, after, "a refused save must leave the row byte-identical")
				return
			}
			assert.Equal(t, tc.wantStackAfter, after.IsStack)
			assert.Equal(t, tc.wantOnAfter, after.Enabled)
			assert.Equal(t, workDir, after.WorkDir)
		})
	}
}

func TestAutoUpdateRepo_UpsertUnlessKindClashInsertsNewRow(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("dev-1")
	repo := NewAutoUpdateRepo(&DB{Pool: e.Pool})
	p := &models.AutoUpdatePolicy{DeviceID: "dev-1", Target: "web", Enabled: true}

	stored, err := repo.UpsertUnlessKindClash(context.Background(), p)

	require.NoError(t, err)
	assert.True(t, stored)
	assert.NotZero(t, p.ID, "the stored id is returned")
	assert.Equal(t, storedPolicy{Enabled: true}, readPolicy(t, e, "dev-1", "web"))
}

// Two saves of opposite kinds race for one target that has no row yet. One of
// them inserts; the other either replaces it (only possible while the first is
// disabled) or is refused. Neither may ever overwrite an enabled row of the
// other kind.
func TestAutoUpdateRepo_UpsertUnlessKindClashIsRaceFree(t *testing.T) {
	e := newPGEnv(t, 0)
	e.seedDevice("dev-1")
	repo := NewAutoUpdateRepo(&DB{Pool: e.Pool})
	ctx := withDeadline(t, 30*time.Second)

	for round := 0; round < 20; round++ {
		e.Exec(`DELETE FROM auto_update_policies`)
		var wg sync.WaitGroup
		results := make([]bool, 2)
		errs := make([]error, 2)
		for i, isStack := range []bool{false, true} {
			wg.Add(1)
			go func(i int, isStack bool) {
				defer wg.Done()
				results[i], errs[i] = repo.UpsertUnlessKindClash(ctx, &models.AutoUpdatePolicy{
					DeviceID: "dev-1", Target: "app", IsStack: isStack, Enabled: true,
				})
			}(i, isStack)
		}
		wg.Wait()
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		assert.True(t, results[0] != results[1], "round %d: exactly one of two enabled saves of opposite kinds is stored", round)
		final := readPolicy(t, e, "dev-1", "app")
		assert.Equal(t, results[1], final.IsStack, "round %d: the stored row is the save that won", round)
	}
}
