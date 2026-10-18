package db

import (
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRowScan returns a scan function that fills the destinations in the order
// of commandColumns, so the test also pins that order.
func fakeRowScan(t *testing.T, status string, targetKeys []string) func(dest ...interface{}) error {
	t.Helper()
	cols := strings.Split(commandColumns, ",")
	return func(dest ...interface{}) error {
		require.Len(t, dest, len(cols), "scanCommand must scan exactly the commandColumns")
		now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
		*dest[0].(*string) = "c-1"
		*dest[1].(*string) = "dev-1"
		*dest[2].(*string) = "reboot"
		*dest[3].(*[]byte) = []byte(`{"mode":"full"}`)
		*dest[4].(*string) = status
		*dest[5].(*string) = "msg"
		*dest[8].(*time.Time) = now
		*dest[9].(*time.Time) = now
		*dest[10].(*time.Time) = now.Add(15 * time.Minute)
		*dest[11].(*int) = 600
		*dest[18].(*string) = ""
		*dest[20].(*string) = ""
		*dest[21].(*string) = ""
		*dest[22].(*string) = ""
		*dest[24].(*[]string) = targetKeys
		return nil
	}
}

func TestCL_AC101_LegacyStatusesCanonical(t *testing.T) {
	cases := []struct{ stored, want string }{
		{"pending", "queued"},
		{"sent", "dispatched"},
		{"success", "succeeded"},
		{"error", "failed"},
		{"queued", "queued"},
		{"timed_out", "timed_out"},
	}
	for _, tc := range cases {
		t.Run(tc.stored, func(t *testing.T) {
			cmd, err := scanCommand(fakeRowScan(t, tc.stored, nil))
			require.NoError(t, err)
			assert.Equal(t, tc.want, cmd.Status, "[AC-101] stored %q must read as %q", tc.stored, tc.want)
			assert.Equal(t, models.OriginUser, cmd.Origin, "an empty stored origin reads as user")
			assert.NotNil(t, cmd.TargetKeys, "target_keys must read as [] not null")
			assert.Equal(t, "full", cmd.Params["mode"])
		})
	}
}

func TestScanCommand_DerivesTargetKeyFromKeys(t *testing.T) {
	cmd, err := scanCommand(fakeRowScan(t, "queued", []string{"docker:compose:/srv/app", "host"}))
	require.NoError(t, err)
	assert.Equal(t, "docker:compose:/srv/app", cmd.TargetKey)
}

func TestScanCommand_BadParamsJSONIsAnError(t *testing.T) {
	scan := fakeRowScan(t, "queued", nil)
	_, err := scanCommand(func(dest ...interface{}) error {
		if err := scan(dest...); err != nil {
			return err
		}
		*dest[3].(*[]byte) = []byte(`{not json`)
		return nil
	})
	require.Error(t, err, "malformed params must surface, not be silently dropped")
}

func TestCL_SEC006_FilteredQueryUsesOnlyPlaceholders(t *testing.T) {
	// statuses are the alias-expanded output of models.ParseStatusFilter.
	sql, args := buildFilteredQuery("dev-1", 50, 10, []string{"queued", "pending"}, "os_update", "docker:container:web")

	assert.NotContains(t, sql, "'", "[SEC-006] the filtered query must contain no string literal at all")
	assert.Contains(t, sql, "status = ANY($2::text[])", "statuses must be bound as one array")
	assert.Contains(t, sql, "action=$3")
	assert.Contains(t, sql, "$4 = ANY(target_keys)")
	assert.Contains(t, sql, "LIMIT $5 OFFSET $6")
	require.Len(t, args, 6)
	assert.Equal(t, "dev-1", args[0])
	assert.Equal(t, []string{"queued", "pending"}, args[1])
	assert.Equal(t, "os_update", args[2])
	assert.Equal(t, "docker:container:web", args[3])
	assert.Equal(t, 50, args[4])
	assert.Equal(t, 10, args[5])
}

func TestCL_SEC006_FilteredQueryOmitsAbsentFilters(t *testing.T) {
	sql, args := buildFilteredQuery("dev-1", 50, 0, nil, "", "")

	assert.NotContains(t, sql, "ANY(", "no filter means no filter clause")
	assert.Equal(t, []interface{}{"dev-1", 50, 0}, args)
	assert.Contains(t, sql, "LIMIT $2 OFFSET $3")
}

func TestCL_SEC006_TargetKeyFilterIsBound(t *testing.T) {
	hostile := []string{"x'; DROP TABLE commands;--", "100%", "docker:container:web' OR '1'='1", `a\b_c`}
	for _, key := range hostile {
		t.Run(key, func(t *testing.T) {
			sql, args := buildFilteredQuery("dev-1", 50, 0, nil, "", key)
			assert.NotContains(t, sql, key, "[SEC-006] the target key must never be interpolated into SQL")
			assert.Contains(t, args, key, "the target key must travel as a bind parameter")

			activeSQL, activeArgs := buildActiveQuery("dev-1", key)
			assert.NotContains(t, activeSQL, key, "[SEC-006] the active query must not interpolate the target key")
			assert.Contains(t, activeArgs, key)
		})
	}
}

func TestBuildActiveQuery_StatusListIsTypedLiteralOnly(t *testing.T) {
	sql, args := buildActiveQuery("dev-1", "")

	assert.Contains(t, sql, "status IN ('queued','pending','dispatched','sent','acknowledged','running','verifying')",
		"the list must match the partial index predicate of migration 000023")
	assert.NotContains(t, sql, "ANY(target_keys)", "no target key means no key clause")
	assert.Equal(t, []interface{}{"dev-1"}, args)
}

func TestBuildTransitionSQL_DisallowedEdgeIsNotQueried(t *testing.T) {
	// succeeded -> running is not an edge for any actor.
	_, _, ok := buildTransitionSQL(models.CommandTransition{
		ID: "c", Actor: models.ActorAgent, To: models.CommandRunning, From: []models.CommandState{models.CommandSucceeded},
		Now: time.Now(),
	})
	assert.False(t, ok, "an empty guard must short-circuit without a query")
}

func TestBuildTransitionSQL_RequiresNow(t *testing.T) {
	_, _, ok := buildTransitionSQL(models.CommandTransition{ID: "c", Actor: models.ActorAgent, To: models.CommandAcknowledged})
	assert.False(t, ok, "the repository never reads the clock: a zero Now is not applicable")
}

func TestBuildTransitionSQL_ScopesDeviceAndGuardsStatus(t *testing.T) {
	step := "pull"
	query, args, ok := buildTransitionSQL(models.CommandTransition{
		ID: "c-1", DeviceID: "dev-1", Actor: models.ActorAgent, To: models.CommandRunning,
		Step: &step, ProgressSet: true, Progress: nil, Now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	})
	require.True(t, ok)

	assert.Contains(t, query, "WHERE id=$1 AND device_id=$")
	assert.Contains(t, query, "status IN ('queued','pending','dispatched','sent','acknowledged','running')",
		"agent -> running is legal from queued, dispatched, acknowledged and running only")
	assert.Contains(t, query, "progress=$")
	assert.Contains(t, query, "started_at=COALESCE(started_at,$3)")
	assert.Contains(t, query, "deadline_at=COALESCE(deadline_at,",
		"an agent edge must fill the deadline so no active row escapes the sweeper")
	assert.NotContains(t, query, "'pull'", "values are bound, never interpolated")
	assert.Contains(t, args, "pull")
	assert.Contains(t, args, "dev-1")
}

func TestBuildTransitionSQL_ServerEdgeDoesNotTouchDeadlineCoalesce(t *testing.T) {
	query, _, ok := buildTransitionSQL(models.CommandTransition{
		ID: "c-1", Actor: models.ActorServer, To: models.CommandTimedOut, Now: time.Now(),
	})
	require.True(t, ok)

	assert.Contains(t, query, "finished_at=$3")
	assert.NotContains(t, query, "COALESCE(deadline_at", "only agent edges backfill the deadline")
	assert.NotContains(t, query, "device_id=", "an unscoped server transition has no device clause")
}

func TestBuildTransitionSQL_RevertClearsClaimAndFlagsAmbiguous(t *testing.T) {
	query, _, ok := buildTransitionSQL(models.CommandTransition{
		ID: "c-1", Actor: models.ActorServer, To: models.CommandQueued,
		From: []models.CommandState{models.CommandDispatched}, Now: time.Now(),
	})
	require.True(t, ok)

	assert.Contains(t, query, "dispatched_at=NULL")
	assert.Contains(t, query, "deadline_at=NULL")
	assert.Contains(t, query, "send_ambiguous=true", "[SEC-016] a revert marks the send ambiguous")
	assert.Contains(t, query, "status IN ('dispatched','sent')", "only a claimed row can be reverted")
}

func TestListQueriesNeverWriteStatus(t *testing.T) {
	// DoD: the only places that write commands.status are Transition and the two
	// claim methods. The builders for reads must not contain an UPDATE.
	filtered, _ := buildFilteredQuery("d", 1, 0, []string{"queued"}, "a", "k")
	active, _ := buildActiveQuery("d", "k")
	assert.True(t, strings.HasPrefix(filtered, "SELECT "), "a list query must be a plain SELECT")
	assert.True(t, strings.HasPrefix(active, "SELECT "), "an active query must be a plain SELECT")
}
