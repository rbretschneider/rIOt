package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/commands"
	"github.com/DesyncTheThird/rIOt/internal/server/handlers"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	routeSecret = "route-test-secret"
	routeCmdID  = "00000000-0000-4000-8000-000000000001"
)

func adminCookie(t *testing.T) *http.Cookie {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(routeSecret))
	require.NoError(t, err)
	return &http.Cookie{Name: "riot_session", Value: tok}
}

// TestCL_SEC_RouteAuthMatrix pins the auth stack of every command route: the
// agent report route takes the full deviceAuth stack (a marker middleware in
// that stack proves every element runs), the admin routes take the JWT cookie
// and refuse a device key.
func TestCL_SEC_RouteAuthMatrix(t *testing.T) {
	repo := testutil.NewMockCommandRepo()
	now := time.Now().UTC()
	repo.Commands[routeCmdID] = &models.Command{
		ID: routeCmdID, DeviceID: "dev-1", Action: "fetch_logs", Params: map[string]interface{}{},
		Status: "dispatched", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour), ExecDeadlineS: 900,
	}
	devs := testutil.NewMockDeviceRepo()
	devs.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "h1"}
	devs.Devices["dev-2"] = &models.Device{ID: "dev-2", Hostname: "h2"}
	devs.APIKeys["key-1"] = "dev-1"
	devs.APIKeys["key-2"] = "dev-2"

	h := handlers.New(handlers.HandlerDeps{
		Devices: devs, CommandRepo: repo,
		Commands: commands.New(commands.Options{Commands: repo}),
	})

	var markerRuns atomic.Int32
	marker := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			markerRuns.Add(1)
			next.ServeHTTP(w, r)
		})
	}
	deviceAuth := []func(http.Handler) http.Handler{marker, middleware.DeviceAuth(devs)}
	router := chi.NewRouter()
	router.Route("/api/v1/devices/{id}", func(r chi.Router) {
		registerCommandRoutes(r, h, deviceAuth, middleware.AdminAuth([]byte(routeSecret)))
	})

	do := func(method, path, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if mod != nil {
			mod(req)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	withKey := func(key string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("X-rIOt-Key", key) } }
	withJWT := func(r *http.Request) { r.AddCookie(adminCookie(t)) }

	reportPath := "/api/v1/devices/dev-1/commands/" + routeCmdID + "/report"
	reportBody := `{"kind":"ack"}`

	t.Run("report without a key is 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do("POST", reportPath, reportBody, nil).Code)
	})
	t.Run("report with only a JWT cookie is 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do("POST", reportPath, reportBody, withJWT).Code,
			"[C-11] an admin session must not be able to impersonate an agent")
	})
	t.Run("report with another device's key is 403", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do("POST", reportPath, reportBody, withKey("key-2")).Code)
		assert.Equal(t, "dispatched", repo.Commands[routeCmdID].Status, "a refused report must not touch the row")
	})
	t.Run("report with an invalid key is 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do("POST", reportPath, reportBody, withKey("nope")).Code)
	})
	t.Run("report with the device's own key passes the whole stack", func(t *testing.T) {
		before := markerRuns.Load()
		rec := do("POST", reportPath, reportBody, withKey("key-1"))
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Greater(t, markerRuns.Load(), before, "[C-11] every middleware of the deviceAuth stack must run for the report route")
		assert.Equal(t, "acknowledged", repo.Commands[routeCmdID].Status)
	})

	adminRoutes := []struct{ method, path, body string }{
		{"GET", "/api/v1/devices/dev-1/commands/" + routeCmdID, ""},
		{"POST", "/api/v1/devices/dev-1/commands/" + routeCmdID + "/cancel", ""},
		{"GET", "/api/v1/devices/dev-1/commands", ""},
		{"POST", "/api/v1/devices/dev-1/commands", `{"action":"fetch_logs"}`},
		{"GET", "/api/v1/devices/dev-1/commands/" + routeCmdID + "/output", ""},
		{"POST", "/api/v1/devices/dev-1/docker/bulk-update", `{"container_ids":["a"]}`},
	}
	for _, rt := range adminRoutes {
		t.Run(rt.method+" "+rt.path+" with a device key and no JWT is 401", func(t *testing.T) {
			rec := do(rt.method, rt.path, rt.body, withKey("key-1"))
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "[C-11] a device key must not reach an admin route")
		})
		t.Run(rt.method+" "+rt.path+" without credentials is 401", func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, do(rt.method, rt.path, rt.body, nil).Code)
		})
	}

	t.Run("admin GET command with a JWT reaches the handler", func(t *testing.T) {
		rec := do("GET", "/api/v1/devices/dev-1/commands/"+routeCmdID, "", withJWT)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
	t.Run("admin cancel of a non-queued command is 409", func(t *testing.T) {
		rec := do("POST", "/api/v1/devices/dev-1/commands/"+routeCmdID+"/cancel", "", withJWT)
		assert.Equal(t, http.StatusConflict, rec.Code)
	})
}
