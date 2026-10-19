package db

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The db package has no database test harness, so the SQL halves of AC-026,
// AC-032 and AC-033 are verified by QA against a disposable database. These
// tests pin what can be pinned without one: the SQL guard agrees with the Go
// allowance, and the guard is present, in the right place, in every read query.

func TestFutureSkewSQL_AC030_MatchesAllowance(t *testing.T) {
	m := regexp.MustCompile(`NOW\(\) \+ interval '(\d+) minutes'`).FindStringSubmatch(notFutureDatedSQL)
	require.Len(t, m, 2, "notFutureDatedSQL must bound timestamp by NOW() plus a whole number of minutes: %q", notFutureDatedSQL)
	minutes, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	assert.Equal(t, FutureSkewAllowance, time.Duration(minutes)*time.Minute,
		"the SQL read guard and the ingest clamp must use the same allowance")
	assert.Equal(t, 5*time.Minute, FutureSkewAllowance, "FR-030: the allowance is 5 minutes")
	assert.True(t, strings.HasPrefix(notFutureDatedSQL, "timestamp <= "),
		"the boundary is inclusive: exactly now+allowance is kept")
}

// methodBody returns the source of one TelemetryRepo method.
func methodBody(t *testing.T, src, name string) string {
	t.Helper()
	marker := "func (r *TelemetryRepo) " + name + "("
	start := strings.Index(src, marker)
	require.GreaterOrEqual(t, start, 0, "method %s not found", name)
	rest := src[start+len(marker):]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestReadQueries_AC032_AC033_GuardPlacement(t *testing.T) {
	raw, err := os.ReadFile("telemetry_repo.go")
	require.NoError(t, err)
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")
	guard := "`+notFutureDatedSQL+`"

	// The five "latest snapshot" reads (FR-036) and three history/window reads (FR-037).
	for _, name := range []string{
		"GetLatestSnapshot", "GetAllLatestSnapshots", "GetAllLatestSummaries",
		"GetGPUDeviceIDs", "GetFleetContainerLeaderboard",
		"GetHistory", "GetHeartbeatHistory", "GetFleetHeartbeats",
	} {
		name := name
		t.Run("[AC-032] [AC-033] "+name+" applies the guard", func(t *testing.T) {
			body := methodBody(t, src, name)
			assert.Equal(t, 1, strings.Count(body, guard), "%s must apply the future-dated guard exactly once", name)
		})
	}

	// In the DISTINCT ON queries the guard must be inside the subquery, before
	// the row is chosen: after ORDER BY (or in an outer WHERE) it would drop the
	// device instead of falling back to its newest valid row.
	for _, name := range []string{"GetAllLatestSnapshots", "GetAllLatestSummaries", "GetGPUDeviceIDs", "GetFleetContainerLeaderboard"} {
		name := name
		t.Run("[AC-032] "+name+" guard precedes DISTINCT ON ordering", func(t *testing.T) {
			body := methodBody(t, src, name)
			g := strings.Index(body, guard)
			order := strings.Index(body, "ORDER BY device_id, timestamp DESC")
			require.GreaterOrEqual(t, g, 0)
			require.GreaterOrEqual(t, order, 0)
			assert.Less(t, g, order, "guard must come before ORDER BY in %s", name)
			if latest := strings.Index(body, ") latest"); latest >= 0 {
				assert.Less(t, g, latest, "guard must be inside the subquery in %s, not in the outer WHERE", name)
			}
		})
	}
}

// AC-034: this story adds no statement that rewrites existing snapshot or
// heartbeat rows; the only DELETEs are the pre-existing retention purges.
func TestTelemetryRepo_AC034_NoRowRewrite(t *testing.T) {
	raw, err := os.ReadFile("telemetry_repo.go")
	require.NoError(t, err)
	src := strings.ToUpper(string(raw))
	assert.NotContains(t, src, "UPDATE TELEMETRY_SNAPSHOTS", "no UPDATE of snapshot rows")
	assert.NotContains(t, src, "UPDATE HEARTBEATS", "no UPDATE of heartbeat rows")
	assert.Equal(t, 0, strings.Count(src, "DELETE FROM"), "PART-001 replaces row deletion with whole-partition retention")
}
