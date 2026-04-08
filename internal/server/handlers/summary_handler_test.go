package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSummaryHandlers creates a minimal Handlers instance for DeviceSummary tests.
func newSummaryHandlers(t *testing.T) (*Handlers, *testutil.MockDeviceRepo, *testutil.MockTelemetryRepo) {
	t.Helper()
	deviceRepo := testutil.NewMockDeviceRepo()
	telRepo := testutil.NewMockTelemetryRepo()
	h := &Handlers{
		devices:   deviceRepo,
		telemetry: telRepo,
	}
	return h, deviceRepo, telRepo
}

// routeSummaryRequest wires up the chi router with the DeviceSummary handler
// and executes the given request, returning the recorder.
func routeSummaryRequest(h *Handlers, method, path string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Get("/api/v1/devices/{id}/summary", h.DeviceSummary)

	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// [AC-008] Device not found → 404 with correct error body.
func TestDeviceSummary_AC008_DeviceNotFound(t *testing.T) {
	h, _, _ := newSummaryHandlers(t)
	// No devices seeded — any ID will return not found.

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/nonexistent-id/summary")

	assert.Equal(t, http.StatusNotFound, rec.Code, "[AC-008] must return 404 when device does not exist")
	assert.Contains(t, rec.Body.String(), "device not found", "[AC-008] error body must say 'device not found'")
}

// [AC-009] Device exists but no telemetry → 404 with correct error body.
func TestDeviceSummary_AC009_NoTelemetryData(t *testing.T) {
	h, deviceRepo, _ := newSummaryHandlers(t)
	deviceRepo.Devices["dev-1"] = &models.Device{
		ID:      "dev-1",
		ShortID: "abc12345",
		Hostname: "host-no-tel",
	}
	// No snapshots seeded for dev-1.

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-1/summary")

	assert.Equal(t, http.StatusNotFound, rec.Code, "[AC-009] must return 404 when device has no telemetry")
	assert.Contains(t, rec.Body.String(), "no telemetry data available", "[AC-009] error body must say 'no telemetry data available'")
}

// [AC-001] Happy path: 200, correct Content-Type, Content-Disposition filename, Markdown body.
func TestDeviceSummary_AC001_SuccessResponse(t *testing.T) {
	h, deviceRepo, telRepo := newSummaryHandlers(t)
	deviceRepo.Devices["dev-1"] = &models.Device{
		ID:      "dev-1",
		ShortID: "abc12345",
		Hostname: "my-server",
		Arch:    "amd64",
	}
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{
		{
			ID:        1,
			DeviceID:  "dev-1",
			Timestamp: time.Now().UTC(),
			Data: models.FullTelemetryData{
				System: &models.SystemInfo{
					Hostname: "my-server",
					Arch:     "amd64",
				},
			},
		},
	}

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-1/summary")

	require.Equal(t, http.StatusOK, rec.Code, "[AC-001] must return 200 for valid device with telemetry")

	// Content-Type
	ct := rec.Header().Get("Content-Type")
	assert.Equal(t, "text/markdown; charset=utf-8", ct, "[AC-001] Content-Type must be text/markdown; charset=utf-8")

	// Content-Disposition filename pattern: {hostname}-summary-{YYYY-MM-DD}.md
	cd := rec.Header().Get("Content-Disposition")
	assert.Contains(t, cd, "attachment;", "[AC-001] Content-Disposition must include attachment directive")
	assert.Contains(t, cd, "my-server-summary-", "[AC-001] Content-Disposition filename must include sanitized hostname")
	assert.Contains(t, cd, ".md", "[AC-001] Content-Disposition filename must end with .md")
	assert.Contains(t, cd, `"`, "[AC-001] Content-Disposition filename must be quoted per RFC 6266")

	// Markdown body must contain expected heading
	body := rec.Body.String()
	assert.Contains(t, body, "# Device Summary: my-server", "[AC-001] body must contain device summary heading")
	assert.Contains(t, body, "**Device ID:** abc12345", "[AC-001] body must contain device short ID")
}

// [AC-001][SEC-001] Hostname with special chars is sanitized in Content-Disposition.
func TestDeviceSummary_AC001_SEC001_HostnameSanitizedInContentDisposition(t *testing.T) {
	h, deviceRepo, telRepo := newSummaryHandlers(t)
	// Hostile hostname with characters that are illegal in Content-Disposition filenames:
	// double-quotes, semicolons, equals, spaces, newlines.
	// The sanitization must strip all of these to prevent header injection.
	deviceRepo.Devices["dev-evil"] = &models.Device{
		ID:      "dev-evil",
		ShortID: "evil1234",
		Hostname: "evil\";inject=\"pwned",
	}
	telRepo.Snapshots["dev-evil"] = []models.TelemetrySnapshot{
		{
			ID:       1,
			DeviceID: "dev-evil",
			Data:     models.FullTelemetryData{},
		},
	}

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-evil/summary")

	require.Equal(t, http.StatusOK, rec.Code)
	cd := rec.Header().Get("Content-Disposition")

	// The Content-Disposition header must be present with the attachment directive and a quoted filename
	assert.Contains(t, cd, `attachment; filename="`, "[SEC-001] Content-Disposition must use quoted filename format")

	// The header must end with .md" — meaning the filename value is self-contained with no
	// injected additional parameters after the closing quote.
	assert.Contains(t, cd, `.md"`, "[SEC-001] filename must terminate at .md with a closing quote")

	// The sanitized value must not contain raw double-quotes or semicolons in the filename portion.
	// Verify that only a single pair of double-quotes wraps the filename (not broken by injection).
	// Count the double-quotes: a well-formed header has exactly 2 (one opening, one closing).
	doubleQuoteCount := strings.Count(cd, `"`)
	assert.Equal(t, 2, doubleQuoteCount, "[SEC-001] Content-Disposition must have exactly 2 double-quotes (quoted filename)")
}

// [AC-001] Filename uses current date in YYYY-MM-DD format.
func TestDeviceSummary_AC001_FilenameDateFormat(t *testing.T) {
	h, deviceRepo, telRepo := newSummaryHandlers(t)
	deviceRepo.Devices["dev-1"] = &models.Device{
		ID:      "dev-1",
		ShortID: "abc12345",
		Hostname: "myhost",
	}
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{
		{ID: 1, DeviceID: "dev-1", Data: models.FullTelemetryData{}},
	}

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-1/summary")
	require.Equal(t, http.StatusOK, rec.Code)

	cd := rec.Header().Get("Content-Disposition")
	today := time.Now().UTC().Format("2006-01-02")
	assert.Contains(t, cd, today, "[AC-001] filename must include current date in YYYY-MM-DD format")
}

// [AC-010] GetLatestSnapshot is called with the device ID (integration check).
func TestDeviceSummary_AC010_GetLatestSnapshotCalled(t *testing.T) {
	h, deviceRepo, telRepo := newSummaryHandlers(t)
	deviceRepo.Devices["dev-1"] = &models.Device{
		ID:      "dev-1",
		ShortID: "abc12345",
		Hostname: "host-for-snap",
	}
	// Seed TWO snapshots; GetLatestSnapshot returns the last one.
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{
		{
			ID:       1,
			DeviceID: "dev-1",
			Data: models.FullTelemetryData{
				System: &models.SystemInfo{Hostname: "host-for-snap", BoardModel: "OldBoard"},
			},
		},
		{
			ID:       2,
			DeviceID: "dev-1",
			Data: models.FullTelemetryData{
				System: &models.SystemInfo{Hostname: "host-for-snap", BoardModel: "NewBoard"},
			},
		},
	}

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-1/summary")
	require.Equal(t, http.StatusOK, rec.Code)

	// The handler calls GetLatestSnapshot which returns the last entry in the slice.
	body := rec.Body.String()
	assert.Contains(t, body, "NewBoard", "[AC-010] handler must use the latest snapshot (NewBoard)")
	assert.NotContains(t, body, "OldBoard", "[AC-010] handler must not use an older snapshot")
}

// sanitizeFilename unit tests.

func TestSanitizeFilename_AlphanumericHyphenUnderscore(t *testing.T) {
	assert.Equal(t, "my-server_01", sanitizeFilename("my-server_01"), "allowed chars must pass through unchanged")
}

func TestSanitizeFilename_SpecialCharsReplaced(t *testing.T) {
	got := sanitizeFilename(`evil"; filename=malware.exe`)
	// Must only contain alphanumeric, hyphens, underscores
	for _, c := range got {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		assert.True(t, ok, "sanitized filename must only contain safe characters, got: %q", string(c))
	}
}

func TestSanitizeFilename_EmptyStringFallback(t *testing.T) {
	// A hostname that sanitizes to empty must return "device" as fallback
	assert.Equal(t, "device", sanitizeFilename(""), "[SEC-001] empty result must fall back to 'device'")
	assert.Equal(t, "device", sanitizeFilename("!!!"), "[SEC-001] all-special-chars result must fall back to 'device'")
}

func TestSanitizeFilename_UnicodeReplaced(t *testing.T) {
	got := sanitizeFilename("héllo-wörld")
	// é and ö must be replaced; a-z and hyphens must survive
	assert.NotContains(t, got, "é", "unicode chars must be replaced")
	assert.NotContains(t, got, "ö", "unicode chars must be replaced")
	assert.Contains(t, got, "h", "ASCII chars must be preserved")

	// All remaining chars must be in the safe set
	for _, c := range got {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			t.Errorf("sanitizeFilename left unsafe character: %q", string(c))
		}
	}
}

// Verify that Content-Disposition is not set on error responses.
func TestDeviceSummary_NoContentDispositionOnError(t *testing.T) {
	h, _, _ := newSummaryHandlers(t)

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/missing/summary")

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, rec.Header().Get("Content-Disposition"), "error responses must not set Content-Disposition")
}

// Verify Markdown body is non-empty and starts with the expected heading.
func TestDeviceSummary_MarkdownBodyFormat(t *testing.T) {
	h, deviceRepo, telRepo := newSummaryHandlers(t)
	deviceRepo.Devices["dev-1"] = &models.Device{
		ID:      "dev-1",
		ShortID: "abc12345",
		Hostname: "format-check",
	}
	telRepo.Snapshots["dev-1"] = []models.TelemetrySnapshot{
		{ID: 1, DeviceID: "dev-1", Data: models.FullTelemetryData{}},
	}

	rec := routeSummaryRequest(h, "GET", "/api/v1/devices/dev-1/summary")
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.True(t, strings.HasPrefix(body, "# Device Summary:"), "Markdown body must start with h1 heading")
	assert.Greater(t, len(body), 100, "Markdown body must be non-trivially long")
}
