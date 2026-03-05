package probes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteHTTP_SuccessGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe := models.Probe{
		ID:             1,
		Type:           "http",
		Config:         map[string]interface{}{"url": srv.URL},
		TimeoutSeconds: 5,
	}

	result := executeHTTP(context.Background(), probe)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, int64(1), result.ProbeID)
	assert.Greater(t, result.LatencyMs, 0.0)
	assert.NotNil(t, result.StatusCode)
	assert.Equal(t, 200, *result.StatusCode)
}

func TestExecuteHTTP_StatusMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	probe := models.Probe{
		ID:     2,
		Type:   "http",
		Config: map[string]interface{}{"url": srv.URL, "expected_status": float64(200)},
	}

	result := executeHTTP(context.Background(), probe)
	assert.False(t, result.Success)
	assert.Contains(t, result.ErrorMsg, "expected status 200, got 404")
}

func TestExecuteHTTP_CustomMethod(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe := models.Probe{
		ID:     3,
		Type:   "http",
		Config: map[string]interface{}{"url": srv.URL, "method": "post"},
	}

	result := executeHTTP(context.Background(), probe)
	assert.True(t, result.Success)
	assert.Equal(t, "POST", gotMethod, "method should be uppercased")
}

func TestExecuteHTTP_CustomHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	probe := models.Probe{
		ID:   4,
		Type: "http",
		Config: map[string]interface{}{
			"url":     srv.URL,
			"headers": map[string]interface{}{"X-Custom": "hello"},
		},
	}

	result := executeHTTP(context.Background(), probe)
	assert.True(t, result.Success)
	assert.Equal(t, "hello", gotHeader)
}

func TestExecuteHTTP_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context cancelled
		<-r.Context().Done()
	}))
	defer srv.Close()

	probe := models.Probe{
		ID:             5,
		Type:           "http",
		Config:         map[string]interface{}{"url": srv.URL},
		TimeoutSeconds: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 0) // immediate timeout
	defer cancel()

	result := executeHTTP(ctx, probe)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.ErrorMsg)
}

func TestExecuteHTTP_MissingURL(t *testing.T) {
	probe := models.Probe{
		ID:     6,
		Type:   "http",
		Config: map[string]interface{}{},
	}

	result := executeHTTP(context.Background(), probe)
	assert.False(t, result.Success)
	assert.Equal(t, "url not configured", result.ErrorMsg)
}

func TestExecuteHTTP_CustomExpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	probe := models.Probe{
		ID:   7,
		Type: "http",
		Config: map[string]interface{}{
			"url":             srv.URL,
			"expected_status": float64(201),
		},
	}

	result := executeHTTP(context.Background(), probe)
	assert.True(t, result.Success)
}
