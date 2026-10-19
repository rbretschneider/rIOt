package handlers

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

const regTestClientAddr = "203.0.113.9:4444"

// Distinctive secret values so log-content assertions cannot match by accident.
const (
	regTestRegKey      = "REGKEY-SECRET-5f3a9c"
	regTestPresentedK  = "riot_PRESENTED_SECRET_91ce"
	regTestGarbageKey  = "garbage-presented-SECRET-77b2"
	regTestUnknownUUID = "00000000-0000-4000-8000-0000000000ff"
	regTestD1          = "11111111-1111-4111-8111-111111111111"
	regTestD2          = "22222222-2222-4222-8222-222222222222"
)

type regFixture struct {
	h      *Handlers
	dev    *testutil.MockDeviceRepo
	admin  *testutil.MockAdminRepo
	ca     *testutil.MockCARepo
	events *testutil.MockEventRepo
	tel    *testutil.MockTelemetryRepo
}

// newRegisterFixture builds a Handlers for RegisterDevice tests.
//
// With sideEffects=false the hub and event generator are nil, so any WebSocket
// broadcast or event emission panics and fails the test. Rejection tests use
// this to prove FR-002 ("no broadcast, no event") without WebSocket plumbing.
// With mtls=true a CA repository is wired (certificate proof available);
// otherwise caRepo is left as an untyped nil, as router.go does.
func newRegisterFixture(t *testing.T, sideEffects, mtls bool) *regFixture {
	t.Helper()
	f := &regFixture{
		dev:    testutil.NewMockDeviceRepo(),
		admin:  testutil.NewMockAdminRepo(""),
		ca:     testutil.NewMockCARepo(),
		events: testutil.NewMockEventRepo(),
		tel:    testutil.NewMockTelemetryRepo(),
	}
	h := &Handlers{devices: f.dev, adminRepo: f.admin, telemetry: f.tel}
	if mtls {
		h.caRepo = f.ca
	}
	if sideEffects {
		hub := websocket.NewHub()
		go hub.Run()
		h.hub = hub
		h.eventGen = events.NewGenerator(f.events, hub, testutil.NewMockAlertRuleRepo(),
			testutil.NewMockDispatcher(), testutil.NewMockCommandRepo())
	}
	f.h = h
	return f
}

func (f *regFixture) setRegKey(rk string) { f.admin.Config["registration_key"] = rk }

func (f *regFixture) seedDevice(id, hostname string, status models.DeviceStatus, tags []string) {
	f.dev.Devices[id] = &models.Device{
		ID: id, ShortID: id[:8], Hostname: hostname, Arch: "amd64", AgentVersion: "1.0.0",
		Status: status, Tags: tags,
		HardwareProfile: &models.HardwareProfile{CPUModel: "orig-cpu", CPUCores: 2},
	}
}

func (f *regFixture) seedKey(key, deviceID string) { f.dev.APIKeys[key] = deviceID }

// regState is a deep snapshot of the device and key stores.
type regState struct {
	devices string
	keys    map[string]string
	nEvents int
}

func (f *regFixture) state() regState {
	b, _ := json.Marshal(f.dev.Devices)
	keys := make(map[string]string, len(f.dev.APIKeys))
	for k, v := range f.dev.APIKeys {
		keys[k] = v
	}
	return regState{devices: string(b), keys: keys, nEvents: len(f.events.Events)}
}

func (f *regFixture) keysOf(deviceID string) []string {
	var out []string
	for k, v := range f.dev.APIKeys {
		if v == deviceID {
			out = append(out, k)
		}
	}
	return out
}

func regBody(deviceID, hostname string, tags []string) string {
	b, _ := json.Marshal(models.DeviceRegistration{
		Hostname:        hostname,
		Arch:            "arm64",
		AgentVersion:    "9.9.9",
		Tags:            tags,
		DeviceID:        deviceID,
		HardwareProfile: &models.HardwareProfile{CPUModel: "changed-cpu", CPUCores: 64},
	})
	return string(b)
}

// post sends a registration request through RegisterDevice. A panic (for
// example a broadcast on a nil hub) is turned into a test failure.
func (f *regFixture) post(t *testing.T, body, key string, cs *tls.ConnectionState) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-rIOt-Key", key)
	}
	req.RemoteAddr = regTestClientAddr
	req.TLS = cs
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			if rv := recover(); rv != nil {
				t.Fatalf("RegisterDevice panicked (side effect on a path that must have none?): %v", rv)
			}
		}()
		f.h.RegisterDevice(rec, req)
	}()
	return rec
}

func decodeRegResp(t *testing.T, rec *httptest.ResponseRecorder) models.DeviceRegistrationResponse {
	t.Helper()
	var resp models.DeviceRegistrationResponse
	require.NoError(t, json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&resp),
		"response body must be the registration JSON: %q", rec.Body.String())
	return resp
}

func assertUnchanged(t *testing.T, before, after regState, msg string) {
	t.Helper()
	assert.Equal(t, before.devices, after.devices, "%s: device records must be unchanged", msg)
	assert.Equal(t, before.keys, after.keys, "%s: API key set must be unchanged", msg)
	assert.Equal(t, before.nEvents, after.nEvents, "%s: no event may be emitted", msg)
}

func assertReject401(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, regBodyUnauthorized+"\n", rec.Body.String(), "401 must carry the fixed body")
	assert.NotContains(t, rec.Body.String(), "api_key")
}

func assertReject403(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, regBodyForbidden+"\n", rec.Body.String(), "403 must carry the fixed body")
}

// ---------------------------------------------------------------------------
// AC-001 / AC-002: own key
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC001_OwnKeyOpenServer_NoNewKey(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.seedDevice(regTestD1, "old-name", models.DeviceStatusOffline, []string{"a"})
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "new-name", nil), "riot_k1", nil)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	resp := decodeRegResp(t, rec)
	assert.Equal(t, "", resp.APIKey, "own-key re-registration must not issue a key")
	assert.Equal(t, regTestD1, resp.DeviceID)
	d := f.dev.Devices[regTestD1]
	assert.Equal(t, "new-name", d.Hostname, "hostname must be updated")
	assert.Equal(t, models.DeviceStatusOnline, d.Status, "status must be online")
	assert.Equal(t, []string{"a"}, d.Tags, "tags omitted from the request must be kept")
	assert.Equal(t, map[string]string{"riot_k1": regTestD1}, f.dev.APIKeys, "K must still be D's only key")
}

func TestRegisterDevice_AC002_OwnKeyWithRegKeyConfigured_Accepted(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "old-name", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "renamed", []string{"x", "y"}), "riot_k1", nil)

	require.Equal(t, http.StatusOK, rec.Code,
		"Bug C regression: an agent presenting its own key must not be rejected for lacking the registration key; body: %s", rec.Body.String())
	assert.Equal(t, "", decodeRegResp(t, rec).APIKey)
	d := f.dev.Devices[regTestD1]
	assert.Equal(t, "renamed", d.Hostname)
	assert.Equal(t, []string{"x", "y"}, d.Tags, "supplied tags must be applied")
	assert.Equal(t, map[string]string{"riot_k1": regTestD1}, f.dev.APIKeys, "no new key may be created")
}

// ---------------------------------------------------------------------------
// AC-003 .. AC-005: rejections, no side effects
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC003_WrongDeviceKey_403NoSideEffects(t *testing.T) {
	f := newRegisterFixture(t, false, false) // nil hub/eventGen: any side effect panics
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, []string{"t1"})
	f.seedDevice(regTestD2, "d2-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)
	f.seedKey("riot_k2", regTestD2)
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "hijacked", []string{"evil"}), "riot_k2", nil)

	assertReject403(t, rec)
	for _, secret := range []string{regTestD1, regTestD2, "d1-host", "d2-host", "riot_k2"} {
		assert.NotContains(t, rec.Body.String(), secret, "403 body must not disclose %q", secret)
	}
	assertUnchanged(t, before, f.state(), "wrong-device key")
}

func TestRegisterDevice_AC004_NoKeyNoRegKey_Rejected(t *testing.T) {
	f := newRegisterFixture(t, false, false)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
	f.seedKey("riot_k1", regTestD1)
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "attacker-name", nil), "", nil)

	assertReject401(t, rec)
	assertUnchanged(t, before, f.state(), "unauthenticated re-registration (Flaw A)")
	assert.Equal(t, models.DeviceStatusOffline, f.dev.Devices[regTestD1].Status, "device must still be offline")
	assert.Equal(t, []string{"riot_k1"}, f.keysOf(regTestD1), "no key other than the pre-existing one may exist")
}

func TestRegisterDevice_AC005_GarbageKey_NoMetadataOverwrite(t *testing.T) {
	f := newRegisterFixture(t, false, false)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, []string{"keep"})
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "attacker-name", []string{"evil"}), regTestGarbageKey, nil)

	assertReject401(t, rec)
	assertUnchanged(t, before, f.state(), "garbage key (Flaw B)")
	d := f.dev.Devices[regTestD1]
	assert.Equal(t, "d1-host", d.Hostname)
	assert.Equal(t, []string{"keep"}, d.Tags)
	assert.Equal(t, "orig-cpu", d.HardwareProfile.CPUModel)
	assert.Equal(t, models.DeviceStatusOffline, d.Status)
}

// ---------------------------------------------------------------------------
// AC-006 / AC-007: registration key
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC006_RegKeyKeylessDevice_IssuesKey(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "enrolled-host", models.DeviceStatusOffline, nil) // zero keys

	rec := f.post(t, regBody(regTestD1, "enrolled-renamed", nil), regTestRegKey, nil)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	resp := decodeRegResp(t, rec)
	require.NotEmpty(t, resp.APIKey, "registration key must bootstrap a key for a keyless device")
	assert.Equal(t, []string{resp.APIKey}, f.keysOf(regTestD1), "exactly one new key bound to D")
	assert.Equal(t, "enrolled-renamed", f.dev.Devices[regTestD1].Hostname, "metadata must be updated")

	// The issued key must authenticate D's telemetry endpoint.
	r := chi.NewRouter()
	r.With(middleware.DeviceAuth(f.dev)).Post("/api/v1/devices/{id}/telemetry", f.h.Telemetry)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+regTestD1+"/telemetry",
		strings.NewReader(`{"data":{}}`))
	req.Header.Set("X-rIOt-Key", resp.APIKey)
	telRec := httptest.NewRecorder()
	r.ServeHTTP(telRec, req)
	assert.Equal(t, http.StatusOK, telRec.Code, "issued key must authenticate telemetry; body: %s", telRec.Body.String())
}

func TestRegisterDevice_AC007_RegKeyDeviceHasKeys_401(t *testing.T) {
	f := newRegisterFixture(t, false, false)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, []string{"t"})
	f.seedKey("riot_k1", regTestD1)
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "attacker-name", nil), regTestRegKey, nil)

	assertReject401(t, rec)
	assertUnchanged(t, before, f.state(), "registration key against a keyed device")
}

func TestRegisterDevice_AC007_HasAPIKeyError_FailsClosed(t *testing.T) {
	f := newRegisterFixture(t, false, false)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil) // keyless, but the check fails
	f.dev.HasAPIKeyErr = errors.New("db unavailable")
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "attacker-name", nil), regTestRegKey, nil)

	assertReject401(t, rec)
	assertUnchanged(t, before, f.state(), "HasAPIKey error")
	assert.Empty(t, f.keysOf(regTestD1), "no key may be issued when the keyless check failed")
}

// ---------------------------------------------------------------------------
// AC-008 .. AC-013: mTLS certificate path
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC008_MTLSEnrolledDevice_CertIssuesKey(t *testing.T) {
	for _, regKeyConfigured := range []bool{false, true} {
		for _, tc := range []struct{ name, key string }{
			{"no key", ""},
			{"regkey presented", regTestRegKey},
			{"unknown value", regTestGarbageKey},
		} {
			tc := tc
			regKeyConfigured := regKeyConfigured
			t.Run(fmt.Sprintf("[AC-008] %s, regkey configured=%v", tc.name, regKeyConfigured), func(t *testing.T) {
				f := newRegisterFixture(t, true, true)
				if regKeyConfigured {
					f.setRegKey(regTestRegKey)
				}
				f.seedDevice(regTestD1, "enrolled-host", models.DeviceStatusOffline, nil) // enrollment: zero keys
				cs := regTestVerifiedTLS(regTestLeaf(regTestD1, 100))

				rec := f.post(t, regBody(regTestD1, "enrolled-renamed", nil), tc.key, cs)

				require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
				resp := decodeRegResp(t, rec)
				require.NotEmpty(t, resp.APIKey, "certificate proof must issue a key")
				assert.Equal(t, []string{resp.APIKey}, f.keysOf(regTestD1), "the key must be bound to D")
				assert.Equal(t, "enrolled-renamed", f.dev.Devices[regTestD1].Hostname, "metadata must be updated")
				assert.Equal(t, models.DeviceStatusOnline, f.dev.Devices[regTestD1].Status)
			})
		}
	}
}

func TestRegisterDevice_AC009_CertPlusOwnKey_NoNewKey(t *testing.T) {
	f := newRegisterFixture(t, true, true)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)
	cs := regTestVerifiedTLS(regTestLeaf(regTestD1, 101))

	rec := f.post(t, regBody(regTestD1, "renamed", nil), "riot_k1", cs)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, "", decodeRegResp(t, rec).APIKey, "path (a) wins; no new key")
	assert.Equal(t, map[string]string{"riot_k1": regTestD1}, f.dev.APIKeys)
}

func TestRegisterDevice_AC010_CertPath_KeepsPriorKeys(t *testing.T) {
	f := newRegisterFixture(t, true, true)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)
	cs := regTestVerifiedTLS(regTestLeaf(regTestD1, 102))

	rec := f.post(t, regBody(regTestD1, "renamed", nil), regTestGarbageKey, cs)

	require.Equal(t, http.StatusOK, rec.Code, "an unresolved key must not block the certificate path; body: %s", rec.Body.String())
	newKey := decodeRegResp(t, rec).APIKey
	require.NotEmpty(t, newKey)
	require.NotEqual(t, "riot_k1", newKey)
	for _, k := range []string{"riot_k1", newKey} {
		id, err := f.dev.LookupAPIKey(t.Context(), k)
		require.NoError(t, err, "key %q must authenticate afterwards", k)
		assert.Equal(t, regTestD1, id)
	}
}

func TestRegisterDevice_AC011_RevokedCert_401(t *testing.T) {
	f := newRegisterFixture(t, false, true)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
	f.ca.Certs = []models.DeviceCert{{DeviceID: regTestD1, SerialNumber: fmt.Sprintf("%x", 255), Revoked: true}}
	cs := regTestVerifiedTLS(regTestLeaf(regTestD1, 255))
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "attacker-name", nil), "", cs)

	assertReject401(t, rec)
	assertUnchanged(t, before, f.state(), "revoked certificate")
}

func TestRegisterDevice_AC011_UnverifiedCertAndHeader_401(t *testing.T) {
	t.Run("[AC-011] PeerCertificates without VerifiedChains", func(t *testing.T) {
		f := newRegisterFixture(t, false, true)
		f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
		before := f.state()
		cs := &tls.ConnectionState{PeerCertificates: regTestVerifiedTLS(regTestLeaf(regTestD1, 7)).PeerCertificates}

		rec := f.post(t, regBody(regTestD1, "x", nil), "", cs)

		assertReject401(t, rec)
		assertUnchanged(t, before, f.state(), "unverified certificate")
	})
	t.Run("[AC-011] forwarded certificate header with no TLS identity", func(t *testing.T) {
		f := newRegisterFixture(t, false, true)
		f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
		before := f.state()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/devices/register",
			strings.NewReader(regBody(regTestD1, "x", nil)))
		req.Header.Set("X-SSL-Client-CN", regTestD1)
		req.Header.Set("X-Forwarded-Client-Cert", "CN="+regTestD1)
		rec := httptest.NewRecorder()
		f.h.RegisterDevice(rec, req)

		assertReject401(t, rec)
		assertUnchanged(t, before, f.state(), "header-conveyed certificate identity")
	})
	t.Run("[AC-011] verified chain on a router with mTLS inactive (nil caRepo)", func(t *testing.T) {
		f := newRegisterFixture(t, false, false)
		f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
		before := f.state()

		rec := f.post(t, regBody(regTestD1, "x", nil), "", regTestVerifiedTLS(regTestLeaf(regTestD1, 8)))

		assertReject401(t, rec)
		assertUnchanged(t, before, f.state(), "certificate on a non-mTLS router")
	})
	t.Run("[AC-011] empty CN", func(t *testing.T) {
		f := newRegisterFixture(t, false, true)
		f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
		before := f.state()

		rec := f.post(t, regBody(regTestD1, "x", nil), "", regTestVerifiedTLS(regTestLeaf("", 9)))

		assertReject401(t, rec)
		assertUnchanged(t, before, f.state(), "empty-CN certificate")
	})
}

func TestRegisterDevice_AC012_CertForOtherDevice_403(t *testing.T) {
	for _, tc := range []struct{ name, key string }{
		{"no key", ""},
		{"garbage key", regTestGarbageKey},
		{"D1's own key (PO-1: still 403)", "riot_k1"},
	} {
		tc := tc
		t.Run("[AC-012] "+tc.name, func(t *testing.T) {
			f := newRegisterFixture(t, false, true)
			f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
			f.seedDevice(regTestD2, "d2-host", models.DeviceStatusOnline, nil)
			f.seedKey("riot_k1", regTestD1)
			cs := regTestVerifiedTLS(regTestLeaf(regTestD2, 200))
			before := f.state()

			rec := f.post(t, regBody(regTestD1, "attacker-name", []string{"evil"}), tc.key, cs)

			assertReject403(t, rec)
			assertUnchanged(t, before, f.state(), "certificate for a different device")
		})
	}
}

func TestRegisterDevice_AC013_RevocationListError_FailsClosed(t *testing.T) {
	f := newRegisterFixture(t, false, true)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
	f.ca.Err = errors.New("revocation list unreadable")
	before := f.state()

	rec := f.post(t, regBody(regTestD1, "attacker-name", nil), "", regTestVerifiedTLS(regTestLeaf(regTestD1, 300)))

	assertReject401(t, rec)
	assertUnchanged(t, before, f.state(), "revocation list unavailable")
	assert.Empty(t, f.keysOf(regTestD1), "no key may be issued")
}

// ---------------------------------------------------------------------------
// AC-014: uniform 401, no UUID existence disclosure
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC014_Uniform401Body(t *testing.T) {
	f := newRegisterFixture(t, false, false)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "secret-hostname", models.DeviceStatusOffline, nil)
	f.seedKey("riot_k1", regTestD1)

	cases := []struct {
		name, body, key string
	}{
		{"(i) existing UUID, no credential", regBody(regTestD1, "x", nil), ""},
		{"(ii) existing UUID, garbage key", regBody(regTestD1, "x", nil), regTestGarbageKey},
		{"(iii) existing UUID, registration key against a keyed device", regBody(regTestD1, "x", nil), regTestRegKey},
		{"(iv) unknown UUID, wrong key", regBody(regTestUnknownUUID, "x", nil), regTestGarbageKey},
		{"(v) unknown UUID, no key", regBody(regTestUnknownUUID, "x", nil), ""},
		{"(vi) no device_id, no key", regBody("", "x", nil), ""},
	}

	var first *httptest.ResponseRecorder
	for _, tc := range cases {
		rec := f.post(t, tc.body, tc.key, nil)
		assertReject401(t, rec)
		if first == nil {
			first = rec
			continue
		}
		assert.Equal(t, first.Code, rec.Code, "%s: status must be identical", tc.name)
		assert.Equal(t, first.Body.String(), rec.Body.String(), "%s: body must be byte-identical", tc.name)
		assert.Equal(t, first.Header(), rec.Header(), "%s: headers must be identical", tc.name)
	}

	body := first.Body.String()
	for _, leak := range []string{regTestD1, "secret-hostname", "riot_k1", regTestRegKey,
		rejectNoCredential, rejectUnknownKey, rejectRegKeyDeviceHasKeys, rejectNewDeviceRegKeyMismatch} {
		assert.NotContains(t, body, leak, "401 body must not contain %q", leak)
	}
	assert.Len(t, f.dev.Devices, 1, "no device may be created by a rejected request")
}

// ---------------------------------------------------------------------------
// AC-015 .. AC-017: new-device registration
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC015_OpenNewDeviceRegistration_201(t *testing.T) {
	for _, tc := range []struct{ name, deviceID string }{
		{"no device_id", ""},
		{"unknown device_id", regTestUnknownUUID},
	} {
		tc := tc
		t.Run("[AC-015] "+tc.name, func(t *testing.T) {
			f := newRegisterFixture(t, true, false)

			rec := f.post(t, regBody(tc.deviceID, "brand-new", []string{"t"}), "", nil)

			require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
			resp := decodeRegResp(t, rec)
			assert.NotEmpty(t, resp.DeviceID)
			assert.NotEqual(t, regTestUnknownUUID, resp.DeviceID, "the server must generate the ID; the supplied one is ignored")
			assert.NotEmpty(t, resp.APIKey)
			require.Contains(t, f.dev.Devices, resp.DeviceID)
			assert.Equal(t, []string{resp.APIKey}, f.keysOf(resp.DeviceID))
		})
	}
}

func TestRegisterDevice_AC016_NewDeviceRegKey(t *testing.T) {
	t.Run("[AC-016] registration key presented => 201", func(t *testing.T) {
		f := newRegisterFixture(t, true, false)
		f.setRegKey(regTestRegKey)

		rec := f.post(t, regBody("", "brand-new", nil), regTestRegKey, nil)

		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		resp := decodeRegResp(t, rec)
		assert.NotEmpty(t, resp.APIKey)
		assert.Contains(t, f.dev.Devices, resp.DeviceID)
	})
	for _, tc := range []struct{ name, key string }{
		{"missing key", ""},
		{"different key", regTestGarbageKey},
		{"key that is a prefix of the registration key", regTestRegKey[:len(regTestRegKey)-1]},
	} {
		tc := tc
		t.Run("[AC-016] "+tc.name+" => 401, nothing created", func(t *testing.T) {
			f := newRegisterFixture(t, false, false)
			f.setRegKey(regTestRegKey)
			before := f.state()

			rec := f.post(t, regBody("", "brand-new", nil), tc.key, nil)

			assertReject401(t, rec)
			assertUnchanged(t, before, f.state(), "new-device registration without the registration key")
			assert.Empty(t, f.dev.Devices, "no device may be created")
		})
	}
}

func TestRegisterDevice_AC017_DeviceKeyNotRegKeyForUnknownUUID(t *testing.T) {
	f := newRegisterFixture(t, false, false)
	f.setRegKey(regTestRegKey)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)
	before := f.state()

	rec := f.post(t, regBody(regTestUnknownUUID, "stowaway", nil), "riot_k1", nil)

	assertReject401(t, rec)
	assertUnchanged(t, before, f.state(), "device key presented as a registration key")
	assert.Len(t, f.dev.Devices, 1, "no device may be created")
}

// ---------------------------------------------------------------------------
// AC-019: malformed body
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC019_MalformedBody_400NoSideEffects(t *testing.T) {
	oversized := `{"hostname":"` + strings.Repeat("a", maxRegistrationBodyBytes+1024) + `","arch":"x"}`
	bodies := map[string]string{
		"malformed JSON": `{"hostname": "x", `,
		"not JSON":       `hello`,
		"empty body":     ``,
		"over 1 MiB":     oversized,
	}
	for _, regKey := range []string{"", regTestRegKey} {
		for name, body := range bodies {
			name, body, regKey := name, body, regKey
			t.Run(fmt.Sprintf("[AC-019] %s, regkey configured=%v", name, regKey != ""), func(t *testing.T) {
				f := newRegisterFixture(t, false, false)
				if regKey != "" {
					f.setRegKey(regKey)
				}
				f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
				f.seedKey("riot_k1", regTestD1)
				before := f.state()

				rec := f.post(t, body, "", nil)

				assert.Equal(t, http.StatusBadRequest, rec.Code)
				assert.JSONEq(t, `{"error":"invalid request body"}`, rec.Body.String())
				assertUnchanged(t, before, f.state(), "malformed body")
				assert.Len(t, f.dev.Devices, 1)
			})
		}
	}
}

// ---------------------------------------------------------------------------
// C3: every 401/403 class rejects with no side effects (nil hub/eventGen)
// ---------------------------------------------------------------------------

func TestRegisterDevice_C3_EveryRejectClass_NoSideEffects(t *testing.T) {
	type tc struct {
		name   string
		mtls   bool
		setup  func(f *regFixture)
		key    string
		cs     *tls.ConnectionState
		body   func() string
		status int
		class  string
	}
	existingBody := func() string { return regBody(regTestD1, "attacker-name", []string{"evil"}) }
	cases := []tc{
		{name: rejectNoCredential, status: 401, body: existingBody,
			setup: func(f *regFixture) {}},
		{name: rejectUnknownKey, status: 401, key: regTestGarbageKey, body: existingBody,
			setup: func(f *regFixture) {}},
		{name: rejectWrongDeviceKey, status: 403, key: "riot_k2", body: existingBody,
			setup: func(f *regFixture) {}},
		{name: rejectWrongDeviceCert, status: 403, mtls: true, body: existingBody,
			cs:    regTestVerifiedTLS(regTestLeaf(regTestD2, 11)),
			setup: func(f *regFixture) {}},
		{name: rejectRegKeyDeviceHasKeys, status: 401, key: regTestRegKey, body: existingBody,
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
		{name: rejectRegKeyKeycheckError, status: 401, key: regTestRegKey, body: existingBody,
			setup: func(f *regFixture) {
				f.setRegKey(regTestRegKey)
				f.dev.HasAPIKeyErr = errors.New("boom")
				delete(f.dev.APIKeys, "riot_k1")
			}},
		{name: rejectCertRevoked, status: 401, mtls: true, body: existingBody,
			cs: regTestVerifiedTLS(regTestLeaf(regTestD1, 12)),
			setup: func(f *regFixture) {
				f.ca.Certs = []models.DeviceCert{{SerialNumber: "c", Revoked: true}}
			}},
		{name: rejectCertRevocationUnavailable, status: 401, mtls: true, body: existingBody,
			cs:    regTestVerifiedTLS(regTestLeaf(regTestD1, 13)),
			setup: func(f *regFixture) { f.ca.Err = errors.New("down") }},
		{name: rejectCertEmptyCN, status: 401, mtls: true, body: existingBody,
			cs:    regTestVerifiedTLS(regTestLeaf("", 14)),
			setup: func(f *regFixture) {}},
		{name: rejectCertUnverified, status: 401, mtls: true, body: existingBody,
			cs:    &tls.ConnectionState{PeerCertificates: regTestVerifiedTLS(regTestLeaf(regTestD1, 15)).PeerCertificates},
			setup: func(f *regFixture) {}},
		{name: rejectNewDeviceRegKeyMismatch, status: 401, key: regTestGarbageKey,
			body:  func() string { return regBody(regTestUnknownUUID, "stowaway", nil) },
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
	}

	for _, c := range cases {
		c := c
		t.Run("[AC-004] class "+c.name, func(t *testing.T) {
			f := newRegisterFixture(t, false, c.mtls) // nil hub + nil eventGen
			f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, []string{"keep"})
			f.seedDevice(regTestD2, "d2-host", models.DeviceStatusOnline, nil)
			f.seedKey("riot_k1", regTestD1)
			f.seedKey("riot_k2", regTestD2)
			c.setup(f)
			before := f.state()
			nDevices := len(f.dev.Devices)

			rec := f.post(t, c.body(), c.key, c.cs)

			if c.status == 403 {
				assertReject403(t, rec)
			} else {
				assertReject401(t, rec)
			}
			assertUnchanged(t, before, f.state(), c.name)
			assert.Len(t, f.dev.Devices, nDevices, "nothing may be created")
		})
	}
}

// ---------------------------------------------------------------------------
// AC-020 (+ C1, C2): rejection logs
// ---------------------------------------------------------------------------

type logLine map[string]any

func parseLogLines(t *testing.T, buf *bytes.Buffer) []logLine {
	t.Helper()
	var out []logLine
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var m logLine
		require.NoError(t, json.Unmarshal([]byte(ln), &m), "every log line must be one JSON object: %q", ln)
		out = append(out, m)
	}
	return out
}

func rejectionLines(lines []logLine) []logLine {
	var out []logLine
	for _, l := range lines {
		if l["msg"] == "device registration rejected" {
			out = append(out, l)
		}
	}
	return out
}

func TestRegisterDevice_AC020_RejectionLogsWithoutSecrets(t *testing.T) {
	type tc struct {
		name     string
		mtls     bool
		setup    func(f *regFixture)
		key      string
		cs       *tls.ConnectionState
		deviceID string
		class    string
		wantID   string
	}
	cases := []tc{
		{name: "no-credential", class: rejectNoCredential, deviceID: regTestD1, wantID: regTestD1,
			setup: func(f *regFixture) {}},
		{name: "unknown-key", class: rejectUnknownKey, deviceID: regTestD1, wantID: regTestD1, key: regTestPresentedK,
			setup: func(f *regFixture) {}},
		{name: "wrong-device-key", class: rejectWrongDeviceKey, deviceID: regTestD1, wantID: regTestD1, key: regTestPresentedK,
			setup: func(f *regFixture) { f.seedKey(regTestPresentedK, regTestD2) }},
		{name: "wrong-device-cert", class: rejectWrongDeviceCert, deviceID: regTestD1, wantID: regTestD1, mtls: true,
			cs: regTestVerifiedTLS(regTestLeaf(regTestD2, 21)), key: regTestPresentedK,
			setup: func(f *regFixture) {}},
		{name: "regkey-device-has-keys", class: rejectRegKeyDeviceHasKeys, deviceID: regTestD1, wantID: regTestD1, key: regTestRegKey,
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey); f.seedKey("riot_existing_key_aa11", regTestD1) }},
		{name: "regkey-keycheck-error", class: rejectRegKeyKeycheckError, deviceID: regTestD1, wantID: regTestD1, key: regTestRegKey,
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey); f.dev.HasAPIKeyErr = errors.New("boom") }},
		{name: "cert-revoked", class: rejectCertRevoked, deviceID: regTestD1, wantID: regTestD1, mtls: true, key: regTestPresentedK,
			cs: regTestVerifiedTLS(regTestLeaf(regTestD1, 22)),
			setup: func(f *regFixture) {
				f.ca.Certs = []models.DeviceCert{{SerialNumber: "16", Revoked: true}}
			}},
		{name: "cert-revocation-unavailable", class: rejectCertRevocationUnavailable, deviceID: regTestD1, wantID: regTestD1, mtls: true,
			cs:    regTestVerifiedTLS(regTestLeaf(regTestD1, 23)),
			setup: func(f *regFixture) { f.ca.Err = errors.New("down") }},
		{name: "cert-empty-cn", class: rejectCertEmptyCN, deviceID: regTestD1, wantID: regTestD1, mtls: true,
			cs:    regTestVerifiedTLS(regTestLeaf("", 24)),
			setup: func(f *regFixture) {}},
		{name: "cert-unverified", class: rejectCertUnverified, deviceID: regTestD1, wantID: regTestD1, mtls: true,
			cs:    &tls.ConnectionState{PeerCertificates: regTestVerifiedTLS(regTestLeaf(regTestD1, 25)).PeerCertificates},
			setup: func(f *regFixture) {}},
		{name: "new-device-regkey-mismatch", class: rejectNewDeviceRegKeyMismatch, deviceID: regTestUnknownUUID, wantID: regTestUnknownUUID,
			key:   regTestPresentedK,
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
		// C1: attacker-controlled device_id on the new-device path.
		{name: "C1 device_id with NUL and newline", class: rejectNewDeviceRegKeyMismatch,
			deviceID: "\u0000x\nforged-log-line\u001b[31m", wantID: "[invalid]", key: regTestPresentedK,
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
		{name: "C1 overlong device_id", class: rejectNewDeviceRegKeyMismatch,
			deviceID: strings.Repeat("a", 5000), wantID: "[invalid]", key: regTestPresentedK,
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
	}

	for _, c := range cases {
		c := c
		t.Run("[AC-020] "+c.name, func(t *testing.T) {
			buf := captureLogs(t)
			f := newRegisterFixture(t, false, c.mtls)
			f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
			f.seedDevice(regTestD2, "d2-host", models.DeviceStatusOnline, nil)
			c.setup(f)

			rec := f.post(t, regBody(c.deviceID, "attacker-name", nil), c.key, c.cs)
			require.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden,
				"expected a rejection, got %d: %s", rec.Code, rec.Body.String())

			raw := buf.String()
			lines := parseLogLines(t, buf)
			rej := rejectionLines(lines)
			require.Len(t, rej, 1, "exactly one rejection entry per rejected request; log: %s", raw)
			e := rej[0]
			assert.Equal(t, "WARN", e["level"], "rejections are logged at warn level")
			assert.Equal(t, c.class, e["class"])
			assert.Equal(t, c.wantID, e["device_id"], "device_id as supplied when safe, placeholder otherwise")
			assert.Equal(t, "203.0.113.9", e["client"], "client address must be logged")
			assert.EqualValues(t, rec.Code, e["status"])

			// C2: no secret value reaches the log.
			secrets := []string{regTestRegKey, regTestPresentedK}
			for k := range f.dev.APIKeys {
				secrets = append(secrets, k)
			}
			for _, s := range secrets {
				assert.NotContains(t, raw, s, "log must not contain %q", s)
			}
			// C1: no NUL / control / injected content, one physical line.
			assert.NotContains(t, raw, `\u0000`, "log must not contain a NUL escape")
			assert.NotContains(t, raw, "forged-log-line", "attacker-controlled device_id content must not be echoed")
			assert.NotContains(t, raw, `\u001b`, "log must not contain an escape character")
			assert.Equal(t, 1, strings.Count(raw, "\n"), "the entry must be a single physical line: %q", raw)
		})
	}
}

func TestRegisterDevice_AC020_SuccessPathsLogNoSecrets(t *testing.T) {
	type tc struct {
		name    string
		mtls    bool
		setup   func(f *regFixture)
		key     string
		cs      *tls.ConnectionState
		body    string
		want    int
		wantKey bool
	}
	cases := []tc{
		{name: "path (a) own key", want: http.StatusOK, key: regTestPresentedK,
			body:  regBody(regTestD1, "renamed", nil),
			setup: func(f *regFixture) { f.seedKey(regTestPresentedK, regTestD1) }},
		{name: "path (b) certificate", want: http.StatusOK, wantKey: true, mtls: true, key: regTestPresentedK,
			cs:    regTestVerifiedTLS(regTestLeaf(regTestD1, 31)),
			body:  regBody(regTestD1, "renamed", nil),
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
		{name: "path (c) registration key", want: http.StatusOK, wantKey: true, key: regTestRegKey,
			body:  regBody(regTestD1, "renamed", nil),
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
		{name: "new device 201 with registration key", want: http.StatusCreated, wantKey: true, key: regTestRegKey,
			body:  regBody("", "brand-new", nil),
			setup: func(f *regFixture) { f.setRegKey(regTestRegKey) }},
	}
	for _, c := range cases {
		c := c
		t.Run("[AC-020] "+c.name, func(t *testing.T) {
			buf := captureLogs(t)
			f := newRegisterFixture(t, true, c.mtls)
			f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOffline, nil)
			c.setup(f)

			rec := f.post(t, c.body, c.key, c.cs)
			require.Equal(t, c.want, rec.Code, "body: %s", rec.Body.String())
			resp := decodeRegResp(t, rec)
			assert.Equal(t, c.wantKey, resp.APIKey != "")

			raw := buf.String()
			secrets := []string{regTestRegKey, regTestPresentedK}
			for k := range f.dev.APIKeys {
				secrets = append(secrets, k)
			}
			if resp.APIKey != "" {
				secrets = append(secrets, resp.APIKey)
			}
			for _, s := range secrets {
				assert.NotContains(t, raw, s, "log must not contain %q", s)
			}
			assert.Empty(t, rejectionLines(parseLogLines(t, buf)), "a successful registration must not log a rejection")
		})
	}
}

// ---------------------------------------------------------------------------
// AC-021 / AC-022: recovery paths
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC021_RotateKeyThenOwnKeyReRegisters(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOnline, nil)
	f.seedKey("riot_lost_old", regTestD1)

	// An agent whose key is lost/wrong is rejected.
	rec := f.post(t, regBody(regTestD1, "d1-host", nil), "riot_wrong_key", nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Admin rotates the key.
	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/rotate-key", f.h.RotateKey)
	rotReq := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+regTestD1+"/rotate-key", nil)
	rotRec := httptest.NewRecorder()
	r.ServeHTTP(rotRec, rotReq)
	require.Equal(t, http.StatusOK, rotRec.Code, "body: %s", rotRec.Body.String())
	var rot map[string]string
	require.NoError(t, json.NewDecoder(rotRec.Body).Decode(&rot))
	newKey := rot["api_key"]
	require.NotEmpty(t, newKey)

	// The agent sets server.api_key to the new key; its next re-registration is path (a).
	rec = f.post(t, regBody(regTestD1, "d1-host", nil), newKey, nil)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, "", decodeRegResp(t, rec).APIKey, "re-registration with the rotated key issues no key")

	_, err := f.dev.LookupAPIKey(t.Context(), "riot_lost_old")
	assert.Error(t, err, "no previously issued key may still authenticate")
	assert.Equal(t, []string{newKey}, f.keysOf(regTestD1), "device must hold exactly the rotated key")
}

func TestRegisterDevice_AC022_NoDeviceID_RegistersAsNewDevice(t *testing.T) {
	for _, withRegKey := range []bool{false, true} {
		withRegKey := withRegKey
		t.Run(fmt.Sprintf("[AC-022] regkey configured=%v", withRegKey), func(t *testing.T) {
			f := newRegisterFixture(t, true, false)
			if withRegKey {
				f.setRegKey(regTestRegKey)
			}
			f.seedDevice(regTestD1, "d1-host", models.DeviceStatusOnline, []string{"t"})
			f.seedKey("riot_k1", regTestD1)

			// The agent with a lost key is rejected...
			rec := f.post(t, regBody(regTestD1, "d1-host", nil), "riot_lost", nil)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			originalJSON, _ := json.Marshal(f.dev.Devices[regTestD1])

			// ...deletes its ID file and registers again (RK in server.api_key when the server has one).
			key := ""
			if withRegKey {
				key = regTestRegKey
			}
			rec = f.post(t, regBody("", "d1-host", nil), key, nil)

			require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
			resp := decodeRegResp(t, rec)
			assert.NotEqual(t, regTestD1, resp.DeviceID, "must be a new device")
			assert.NotEmpty(t, resp.APIKey)

			// The original record and its keys are untouched.
			nowJSON, _ := json.Marshal(f.dev.Devices[regTestD1])
			assert.JSONEq(t, string(originalJSON), string(nowJSON), "original device record must be unchanged")
			assert.Equal(t, []string{"riot_k1"}, f.keysOf(regTestD1))
		})
	}
}

// ---------------------------------------------------------------------------
// AC-024: request shapes sent by the released agent (internal/agent/register.go
// and http_client.go): JSON body with device_id from config or ID file, header
// X-rIOt-Key = server.api_key (possibly empty), client certificate presented
// when configured, returned api_key persisted only when non-empty.
// ---------------------------------------------------------------------------

func TestRegisterDevice_AC024_ReleasedAgentRequestShapes(t *testing.T) {
	type flow struct {
		name          string
		regKey        string // configured on the server
		mtls          bool
		seed          func(f *regFixture)
		deviceID      string // agent's device_id ("" on first install)
		agentAPIKey   string // agent's server.api_key
		cs            *tls.ConnectionState
		wantStatus    int
		wantKey       bool
		wantSameDevID bool
	}
	flows := []flow{
		{name: "first install with registration key", regKey: regTestRegKey, agentAPIKey: regTestRegKey,
			seed: func(f *regFixture) {}, wantStatus: http.StatusCreated, wantKey: true},
		{name: "first install against an open server", agentAPIKey: "",
			seed: func(f *regFixture) {}, wantStatus: http.StatusCreated, wantKey: true},
		{name: "startup re-registration with own key, no registration key configured",
			deviceID: regTestD1, agentAPIKey: "riot_own", wantSameDevID: true,
			seed: func(f *regFixture) {
				f.seedDevice(regTestD1, "h", models.DeviceStatusOnline, nil)
				f.seedKey("riot_own", regTestD1)
			},
			wantStatus: http.StatusOK},
		{name: "periodic re-registration with own key, registration key configured", regKey: regTestRegKey,
			deviceID: regTestD1, agentAPIKey: "riot_own", wantSameDevID: true,
			seed: func(f *regFixture) {
				f.seedDevice(regTestD1, "h", models.DeviceStatusOnline, nil)
				f.seedKey("riot_own", regTestD1)
			},
			wantStatus: http.StatusOK},
		{name: "first key acquisition after mTLS enrollment (no registration key)", mtls: true,
			deviceID: regTestD1, agentAPIKey: "", wantSameDevID: true,
			cs:         regTestVerifiedTLS(regTestLeaf(regTestD1, 41)),
			seed:       func(f *regFixture) { f.seedDevice(regTestD1, "h", models.DeviceStatusOffline, nil) },
			wantStatus: http.StatusOK, wantKey: true},
		{name: "first key acquisition after mTLS enrollment (registration key as api_key)", mtls: true, regKey: regTestRegKey,
			deviceID: regTestD1, agentAPIKey: regTestRegKey, wantSameDevID: true,
			cs:         regTestVerifiedTLS(regTestLeaf(regTestD1, 42)),
			seed:       func(f *regFixture) { f.seedDevice(regTestD1, "h", models.DeviceStatusOffline, nil) },
			wantStatus: http.StatusOK, wantKey: true},
		{name: "enrolled device on a registration-key server, no verified cert (Bug D via path c)", regKey: regTestRegKey,
			deviceID: regTestD1, agentAPIKey: regTestRegKey, wantSameDevID: true,
			seed:       func(f *regFixture) { f.seedDevice(regTestD1, "h", models.DeviceStatusOffline, nil) },
			wantStatus: http.StatusOK, wantKey: true},
	}
	for _, fl := range flows {
		fl := fl
		t.Run("[AC-024] "+fl.name, func(t *testing.T) {
			f := newRegisterFixture(t, true, fl.mtls)
			if fl.regKey != "" {
				f.setRegKey(fl.regKey)
			}
			fl.seed(f)

			rec := f.post(t, regBody(fl.deviceID, "agent-host", []string{"agent-tag"}), fl.agentAPIKey, fl.cs)

			require.Equal(t, fl.wantStatus, rec.Code, "body: %s", rec.Body.String())
			resp := decodeRegResp(t, rec)
			assert.Equal(t, fl.wantKey, resp.APIKey != "", "api_key presence")
			assert.NotEmpty(t, resp.DeviceID)
			assert.NotEmpty(t, resp.ShortID)
			if fl.wantSameDevID {
				assert.Equal(t, fl.deviceID, resp.DeviceID)
			}
		})
	}
}
