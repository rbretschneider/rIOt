package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fleet-wide dispatches record the logged-in operator (EXPECT-001 AD-016,
// FR-018), so the activity label and the event messages can say "by <name>".

func (e *pathEnv) postAs(claims jwt.MapClaims, url, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	e.t.Helper()
	r := chi.NewRouter()
	r.Post(url, handler)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.ActorClaimsKey, claims))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

var ryanClaims = jwt.MapClaims{"auth_method": "password", "actor_sub": "admin-sub", "display_name": "ryan"}

func TestEX_AC024_FleetAgentUpdateRecordsActor(t *testing.T) {
	e := newPathEnv(t, true)

	rec := e.postAs(ryanClaims, "/api/v1/fleet/bulk-update", `{"version":"1.0.0"}`, e.h.BulkUpdateAgents)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	cmds := e.commandsOf(models.ActionAgentUpdate)
	require.Len(t, cmds, 1, "one agent_update command")
	assert.Equal(t, "ryan", cmds[0].ActorName, "[AC-024] the command records who started it")
	assert.Equal(t, "operator", cmds[0].ActorKind)
	assert.Equal(t, "password", cmds[0].ActorAuth)
	assert.Equal(t, "admin-sub", cmds[0].ActorSub)
	assert.Equal(t, models.OriginUser, cmds[0].Origin, "an operator dispatch is user-origin")
}

func TestEX_AC024_FleetPatchRecordsActor(t *testing.T) {
	e := newPathEnv(t, true)

	rec := e.postAs(ryanClaims, "/api/v1/fleet/bulk-patch", `{"mode":"full"}`, e.h.BulkPatchDevices)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	cmds := e.commandsOf(models.ActionOSUpdate)
	require.Len(t, cmds, 1, "one os_update command")
	assert.Equal(t, "ryan", cmds[0].ActorName, "[AC-024] the patch records who started it")
	assert.Equal(t, "operator", cmds[0].ActorKind)
	assert.Equal(t, "full", cmds[0].Params["mode"], "the mode parameter is unchanged")
}

func TestEX_AC024_FleetDispatchWithoutSessionIsUnknownActor(t *testing.T) {
	e := newPathEnv(t, true)

	rec := e.postAs(nil, "/api/v1/fleet/bulk-update", `{"version":"1.0.0"}`, e.h.BulkUpdateAgents)

	require.Equal(t, http.StatusOK, rec.Code)
	cmds := e.commandsOf(models.ActionAgentUpdate)
	require.Len(t, cmds, 1)
	assert.Equal(t, "unknown", cmds[0].ActorKind, "no session claims: the actor kind is unknown, as for the Docker dispatches")
	assert.Empty(t, cmds[0].ActorName)
}
