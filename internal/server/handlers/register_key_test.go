package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const invalidRegKeyBody = `{"error":"invalid registration key"}` + "\n"

// newRegisterTestHandlers builds Handlers able to run RegisterDevice end to end
// against mocks. regKey == "" leaves registration open.
func newRegisterTestHandlers(t *testing.T, regKey string) (*Handlers, *testutil.MockDeviceRepo) {
	t.Helper()
	adminRepo := testutil.NewMockAdminRepo("")
	if regKey != "" {
		adminRepo.Config["registration_key"] = regKey
	}
	devRepo := testutil.NewMockDeviceRepo()
	hub := websocket.NewHub()
	go hub.Run()
	gen := events.NewGenerator(testutil.NewMockEventRepo(), hub, testutil.NewMockAlertRuleRepo(),
		testutil.NewMockDispatcher(), testutil.NewMockCommandRepo())
	return &Handlers{adminRepo: adminRepo, devices: devRepo, hub: hub, eventGen: gen}, devRepo
}

func postRegister(h *Handlers, key *string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(models.DeviceRegistration{Hostname: "host-a", Arch: "amd64"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/register", bytes.NewReader(body))
	if key != nil {
		req.Header.Set("X-rIOt-Key", *key)
	}
	rec := httptest.NewRecorder()
	h.RegisterDevice(rec, req)
	return rec
}

func strPtr(s string) *string { return &s }

func TestHARDEN002_AC041_CorrectKeyRegisters(t *testing.T) {
	h, devRepo := newRegisterTestHandlers(t, "K-abc123")

	rec := postRegister(h, strPtr("K-abc123"))

	require.Equal(t, http.StatusCreated, rec.Code, "a matching key must register; body %q", rec.Body.String())
	var resp models.DeviceRegistrationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.DeviceID, "response must carry the device id")
	assert.NotEmpty(t, resp.APIKey, "response must carry a freshly issued API key")
	assert.Len(t, devRepo.Devices, 1, "exactly one device must be created")
}

func TestHARDEN002_AC042_WrongKeysRejectedIdentically(t *testing.T) {
	cases := []struct {
		name string
		key  *string
	}{
		{"same length, last byte differs", strPtr("K-abc124")},
		{"prefix", strPtr("K-abc")},
		{"longer", strPtr("K-abc1234")},
		{"empty header value", strPtr("")},
		{"header absent", nil},
		{"different case", strPtr("k-ABC123")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			h, devRepo := newRegisterTestHandlers(t, "K-abc123")
			rec := postRegister(h, tc.key)

			assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s must be rejected", tc.name)
			assert.Equal(t, invalidRegKeyBody, rec.Body.String(), "%s: the 401 body must be byte-identical to the pre-story body", tc.name)
			assert.Empty(t, devRepo.Devices, "%s: nothing may be registered", tc.name)
		})
	}
}

func TestHARDEN002_AC043_OpenRegistrationUnchanged(t *testing.T) {
	h, devRepo := newRegisterTestHandlers(t, "")

	rec := postRegister(h, nil)

	require.Equal(t, http.StatusCreated, rec.Code, "with no key configured registration is open; body %q", rec.Body.String())
	assert.Len(t, devRepo.Devices, 1)

	// An arbitrary header must not matter when no key is configured either.
	h2, _ := newRegisterTestHandlers(t, "")
	rec = postRegister(h2, strPtr("whatever"))
	assert.Equal(t, http.StatusCreated, rec.Code, "an unneeded key header must be ignored")
}

func TestHARDEN002_AC044_RegistrationKeyMatchesTruthTable(t *testing.T) {
	const configured = "K-abc123"
	cases := []struct {
		provided string
		want     bool
	}{
		{"K-abc123", true},
		{"K-abc124", false},
		{"K-abc", false},
		{"K-abc1234", false},
		{"", false},
		{"k-abc123", false},
		{"K-abc123 ", false},
		{"K-abc123\x00", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, registrationKeyMatches(tc.provided, configured), "registrationKeyMatches(%q, %q)", tc.provided, configured)
	}
	assert.True(t, registrationKeyMatches("", ""), "two empty strings are equal (the empty-key bypass is decided by the caller)")
}

// TestHARDEN002_AC044_UsesConstantTimePrimitive is the automated half of the
// AC-044 review check: the comparison goes through crypto/subtle and the
// registration key strings are never compared with == or !=.
func TestHARDEN002_AC044_UsesConstantTimePrimitive(t *testing.T) {
	raw, err := os.ReadFile("handlers.go")
	require.NoError(t, err)
	src := string(raw)

	assert.Contains(t, src, `"crypto/subtle"`, "handlers.go must import crypto/subtle")
	assert.Contains(t, src, "subtle.ConstantTimeCompare(", "the comparison must use ConstantTimeCompare")
	assert.Contains(t, src, "sha256.Sum256([]byte(provided))", "keys must be digested to fixed length first")
	for _, banned := range []string{"apiKey != regKey", "apiKey == regKey", "regKey != apiKey", "regKey == apiKey"} {
		assert.False(t, strings.Contains(src, banned), "raw comparison %q must not be present", banned)
	}
}
