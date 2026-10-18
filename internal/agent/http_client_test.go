package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// SEC C-7: the device and command IDs are path-escaped, so a hostile command ID
// cannot re-target the authenticated POST to another endpoint.
func TestCL_SEC007_ReportURLPathEscaped(t *testing.T) {
	var gotURI, gotKey, gotKind string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI, gotKey = r.RequestURI, r.Header.Get("X-rIOt-Key")
		var rep models.CommandReport
		_ = json.NewDecoder(r.Body).Decode(&rep)
		gotKind = string(rep.Kind)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"command_id":"x","result":"accepted","status":"succeeded"}`))
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, "secret-key")

	reply, err := c.PostCommandReport(context.Background(), "dev/1", "../../telemetry",
		models.CommandReport{Kind: models.ReportKindAck, CommandID: "../../telemetry"})

	require.NoError(t, err)
	assert.Equal(t, "/api/v1/devices/dev%2F1/commands/..%2F..%2Ftelemetry/report", gotURI,
		"[SEC-007] slashes in either ID must be escaped so the request path keeps its shape")
	assert.Equal(t, "secret-key", gotKey, "[SEC-007] the device key header must be sent")
	assert.Equal(t, "ack", gotKind)
	assert.Equal(t, 200, reply.StatusCode)
	assert.Equal(t, models.ReportAccepted, reply.Response.Result)
}

// AC-025: a legacy server's plain 404 page yields a reply with no code, which
// the outbox reads as "no report endpoint".
func TestCL_AC025_NonJSONReplyYieldsEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, "k")

	reply, err := c.PostCommandReport(context.Background(), idA, idB, models.CommandReport{Kind: models.ReportKindResult})

	require.NoError(t, err)
	assert.Equal(t, 404, reply.StatusCode)
	assert.Empty(t, reply.Response.Code, "[AC-025] a legacy 404 carries no error code")
	assert.True(t, isLegacyReply(reply), "[AC-025] 404 without a code is the legacy-server signature")
}
