package handlers

import (
	"context"
	"net/http"
	"time"
)

// DBHealthChecker is the database liveness probe behind /health and /readyz.
// *db.DB satisfies it; tests inject a fake (HARDEN-002 AD-011, NFR-009).
type DBHealthChecker interface {
	Healthy(ctx context.Context) bool
}

// DefaultHealthProbeTimeout bounds the database probe so an unreachable
// database cannot hang an orchestrator's health check (NFR-005).
const DefaultHealthProbeTimeout = 2 * time.Second

// HealthHandler serves /health (liveness-of-data) and /readyz (readiness).
//
// A nil DB counts as "database down"; a nil Migrated counts as "migrations
// incomplete". Never assign a possibly-nil *db.DB to the DB field: a typed nil
// inside the interface is non-nil and would panic on probe.
type HealthHandler struct {
	DB           DBHealthChecker
	Migrated     func() bool
	ProbeTimeout time.Duration // <= 0 means DefaultHealthProbeTimeout
	Now          func() time.Time
}

// healthBody is the fixed /health response. It deliberately carries no error
// text, host names, versions or connection details (FR-040).
type healthBody struct {
	Status   string    `json:"status"`
	Database bool      `json:"database"`
	Time     time.Time `json:"time"`
}

// readyBody is the fixed /readyz response (FR-040).
type readyBody struct {
	Status     string    `json:"status"`
	Database   bool      `json:"database"`
	Migrations bool      `json:"migrations"`
	Time       time.Time `json:"time"`
}

func (hh *HealthHandler) now() time.Time {
	if hh.Now != nil {
		return hh.Now().UTC()
	}
	return time.Now().UTC()
}

// dbHealthy runs the probe in a goroutine and waits for it or the timeout. The
// result channel is buffered so a checker that ignores its context still
// finishes and exits after the handler has returned.
func (hh *HealthHandler) dbHealthy(ctx context.Context) bool {
	if hh.DB == nil {
		return false
	}
	timeout := hh.ProbeTimeout
	if timeout <= 0 {
		timeout = DefaultHealthProbeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := make(chan bool, 1)
	go func() { result <- hh.DB.Healthy(ctx) }()

	select {
	case ok := <-result:
		return ok
	case <-ctx.Done():
		return false
	}
}

// Health handles GET /health: 200 "ok" when the database is reachable, 503
// "degraded" otherwise. The body shape is unchanged from before HARDEN-002.
func (hh *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	dbOK := hh.dbHealthy(r.Context())
	status, code := "ok", http.StatusOK
	if !dbOK {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	writeJSON(w, code, healthBody{Status: status, Database: dbOK, Time: hh.now()})
}

// Readyz handles GET /readyz: 200 "ready" only when the database is reachable
// and migrations have completed in this process, 503 "not_ready" otherwise.
func (hh *HealthHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	dbOK := hh.dbHealthy(r.Context())
	migrated := hh.Migrated != nil && hh.Migrated()
	status, code := "ready", http.StatusOK
	if !dbOK || !migrated {
		status, code = "not_ready", http.StatusServiceUnavailable
	}
	writeJSON(w, code, readyBody{Status: status, Database: dbOK, Migrations: migrated, Time: hh.now()})
}
