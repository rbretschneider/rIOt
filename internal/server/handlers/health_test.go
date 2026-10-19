package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDBChecker is an injectable DBHealthChecker.
type fakeDBChecker func(ctx context.Context) bool

func (f fakeDBChecker) Healthy(ctx context.Context) bool { return f(ctx) }

var (
	dbUp   = fakeDBChecker(func(context.Context) bool { return true })
	dbDown = fakeDBChecker(func(context.Context) bool { return false })
)

func migrated(v bool) func() bool { return func() bool { return v } }

type healthResponse struct {
	code        int
	contentType string
	body        []byte
	fields      map[string]interface{}
}

func serveHealth(t *testing.T, hh *HealthHandler, endpoint string) healthResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", endpoint, nil)
	if endpoint == "/readyz" {
		hh.Readyz(rec, req)
	} else {
		hh.Health(rec, req)
	}
	var fields map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &fields), "%s body must be a JSON object, got %q", endpoint, rec.Body.String())
	return healthResponse{code: rec.Code, contentType: rec.Header().Get("Content-Type"), body: rec.Body.Bytes(), fields: fields}
}

func keysOf(m map[string]interface{}) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func assertRFC3339UTC(t *testing.T, v interface{}, what string) {
	t.Helper()
	s, ok := v.(string)
	require.True(t, ok, "%s must be a string, got %T", what, v)
	ts, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err, "%s must parse as RFC 3339: %q", what, s)
	_, offset := ts.Zone()
	assert.Equal(t, 0, offset, "%s must be UTC: %q", what, s)
}

func TestHARDEN002_AC045_HealthOK(t *testing.T) {
	fixed := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	hh := &HealthHandler{DB: dbUp, Migrated: migrated(true), Now: func() time.Time { return fixed }}
	r := serveHealth(t, hh, "/health")

	assert.Equal(t, http.StatusOK, r.code, "healthy DB must return 200")
	assert.Equal(t, "application/json", r.contentType)
	assert.Equal(t, []string{"database", "status", "time"}, keysOf(r.fields), "body must keep exactly the pre-HARDEN-002 keys")
	assert.Equal(t, "ok", r.fields["status"])
	assert.Equal(t, true, r.fields["database"])
	assertRFC3339UTC(t, r.fields["time"], "time")
	assert.Equal(t, "2026-10-17T12:00:00Z", r.fields["time"], "the injected clock must be used")
}

func TestHARDEN002_AC046_HealthDegradedReturns503(t *testing.T) {
	hh := &HealthHandler{DB: dbDown, Migrated: migrated(true)}
	r := serveHealth(t, hh, "/health")

	assert.Equal(t, http.StatusServiceUnavailable, r.code, "BR-004: a degraded database must not report 2xx")
	assert.Equal(t, "application/json", r.contentType)
	assert.Equal(t, []string{"database", "status", "time"}, keysOf(r.fields))
	assert.Equal(t, "degraded", r.fields["status"])
	assert.Equal(t, false, r.fields["database"])
	assertRFC3339UTC(t, r.fields["time"], "time")
}

func TestHARDEN002_AC046_HealthNilDBCountsAsDown(t *testing.T) {
	r := serveHealth(t, &HealthHandler{Migrated: migrated(true)}, "/health")
	assert.Equal(t, http.StatusServiceUnavailable, r.code, "a nil DB must be reported as down, not panic")
	assert.Equal(t, false, r.fields["database"])
}

func TestHARDEN002_AC047_ReadyzReady(t *testing.T) {
	hh := &HealthHandler{DB: dbUp, Migrated: migrated(true)}
	r := serveHealth(t, hh, "/readyz")

	assert.Equal(t, http.StatusOK, r.code)
	assert.Equal(t, "application/json", r.contentType)
	assert.Equal(t, []string{"database", "migrations", "status", "time"}, keysOf(r.fields))
	assert.Equal(t, "ready", r.fields["status"])
	assert.Equal(t, true, r.fields["database"])
	assert.Equal(t, true, r.fields["migrations"])
	assertRFC3339UTC(t, r.fields["time"], "time")
}

func TestHARDEN002_AC048_ReadyzDBDown503(t *testing.T) {
	hh := &HealthHandler{DB: dbDown, Migrated: migrated(true)}
	r := serveHealth(t, hh, "/readyz")

	assert.Equal(t, http.StatusServiceUnavailable, r.code)
	assert.Equal(t, "not_ready", r.fields["status"])
	assert.Equal(t, false, r.fields["database"])
	assert.Equal(t, true, r.fields["migrations"])
}

func TestHARDEN002_AC049_ReadyzMigrationsIncomplete503(t *testing.T) {
	t.Run("flag false", func(t *testing.T) {
		r := serveHealth(t, &HealthHandler{DB: dbUp, Migrated: migrated(false)}, "/readyz")
		assert.Equal(t, http.StatusServiceUnavailable, r.code)
		assert.Equal(t, "not_ready", r.fields["status"])
		assert.Equal(t, true, r.fields["database"])
		assert.Equal(t, false, r.fields["migrations"])
	})
	t.Run("nil flag counts as incomplete", func(t *testing.T) {
		r := serveHealth(t, &HealthHandler{DB: dbUp}, "/readyz")
		assert.Equal(t, http.StatusServiceUnavailable, r.code)
		assert.Equal(t, false, r.fields["migrations"])
	})
	t.Run("health ignores the migrations flag", func(t *testing.T) {
		r := serveHealth(t, &HealthHandler{DB: dbUp, Migrated: migrated(false)}, "/health")
		assert.Equal(t, http.StatusOK, r.code, "/health reflects the database only (readiness table)")
	})
}

func TestHARDEN002_AC051_HungProbeBounded(t *testing.T) {
	hangUntilCancelled := fakeDBChecker(func(ctx context.Context) bool {
		<-ctx.Done()
		return false
	})
	release := make(chan struct{})
	defer close(release)
	ignoresContext := fakeDBChecker(func(context.Context) bool {
		<-release // a checker that never honours its context
		return true
	})

	for name, checker := range map[string]DBHealthChecker{
		"honours context": hangUntilCancelled,
		"ignores context": ignoresContext,
	} {
		for _, endpoint := range []string{"/health", "/readyz"} {
			name, checker, endpoint := name, checker, endpoint
			t.Run(name+" "+endpoint, func(t *testing.T) {
				hh := &HealthHandler{DB: checker, Migrated: migrated(true), ProbeTimeout: 50 * time.Millisecond}
				start := time.Now()
				r := serveHealth(t, hh, endpoint)
				elapsed := time.Since(start)

				assert.Less(t, elapsed, 3*time.Second, "NFR-005: response must arrive within 3s, took %s", elapsed)
				assert.Equal(t, http.StatusServiceUnavailable, r.code, "a timed-out probe is a 503")
				assert.Equal(t, false, r.fields["database"], "a timed-out probe means database:false")
			})
		}
	}
}

func TestHARDEN002_AC051_DefaultProbeTimeoutIsTwoSeconds(t *testing.T) {
	assert.Equal(t, 2*time.Second, DefaultHealthProbeTimeout, "V-004: probe bound is 2 s")

	var seen time.Duration
	hh := &HealthHandler{DB: fakeDBChecker(func(ctx context.Context) bool {
		dl, ok := ctx.Deadline()
		require.True(t, ok, "probe context must carry a deadline")
		seen = time.Until(dl)
		return true
	})}
	serveHealth(t, hh, "/health")
	assert.Greater(t, seen, time.Second, "default deadline should be about 2s, got %s remaining", seen)
	assert.LessOrEqual(t, seen, 2*time.Second, "default deadline must not exceed 2s, got %s", seen)
}

func TestHARDEN002_AC052_HealthBodiesLeakNothing(t *testing.T) {
	secretErr := `dial tcp riot-db:5432: password authentication failed for user "riot"`
	failing := fakeDBChecker(func(context.Context) bool {
		_ = secretErr // the probe reduces any failure to a bool
		return false
	})
	hh := &HealthHandler{DB: failing, Migrated: migrated(true)}

	for _, endpoint := range []string{"/health", "/readyz"} {
		r := serveHealth(t, hh, endpoint)
		body := string(r.body)
		for _, leak := range []string{"riot-db", "5432", "password", "authentication", "dial tcp", "riot\""} {
			assert.NotContains(t, body, leak, "%s body must not leak %q", endpoint, leak)
		}
		want := []string{"database", "status", "time"}
		if endpoint == "/readyz" {
			want = []string{"database", "migrations", "status", "time"}
		}
		assert.Equal(t, want, keysOf(r.fields), "%s must carry only the documented keys", endpoint)
	}
}

// *db.DB must keep satisfying the checker interface the router injects.
var _ DBHealthChecker = (*db.DB)(nil)
