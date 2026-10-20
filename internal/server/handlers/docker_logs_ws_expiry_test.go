package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	ws "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestDUX_SEC026_LiveSocketClosesAtJWTExpiry(t *testing.T) {
	h, _ := newDockerTest(t)
	expires := time.Now().Add(1200 * time.Millisecond)
	router := chi.NewRouter()
	router.Get("/logs/{deviceId}", func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.ActorClaimsKey, jwt.MapClaims{"exp": float64(expires.Unix())})
		h.DockerLogsWS(w, r.WithContext(ctx))
	})
	srv := httptest.NewServer(router)
	defer srv.Close()
	conn, _, err := ws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/logs/"+dockerTestDevice, http.Header{"Origin": []string{srv.URL}})
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(4*time.Second)))
	var frame map[string]interface{}
	require.NoError(t, conn.ReadJSON(&frame))
	require.Equal(t, "session", frame["type"])
	require.NoError(t, conn.ReadJSON(&frame))
	require.Equal(t, "session_expired", frame["code"])
	_, _, err = conn.ReadMessage()
	require.Error(t, err, "expiry must close the connection after notifying the viewer")
}
