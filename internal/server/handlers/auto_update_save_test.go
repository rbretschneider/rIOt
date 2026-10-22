package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/middleware"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// auditRecord is one docker_audit row as the handler wrote it.
type auditRecord struct {
	Device, Kind, Action, Subject string
	Data                          map[string]interface{}
}

type saveEnv struct {
	h        *Handlers
	policies *testutil.MockAutoUpdateRepo
	audits   []auditRecord
}

func newSaveEnv(t *testing.T) *saveEnv {
	t.Helper()
	e := &saveEnv{policies: testutil.NewMockAutoUpdateRepo()}
	e.h = New(HandlerDeps{
		Devices: testutil.NewMockDeviceRepo(), Telemetry: testutil.NewMockTelemetryRepo(), Events: testutil.NewMockEventRepo(),
		AutoUpdateRepo: e.policies,
	})
	e.h.dockerAudit = func(_ context.Context, device, kind, action, subject string, data interface{}) error {
		m, _ := data.(map[string]interface{})
		e.audits = append(e.audits, auditRecord{Device: device, Kind: kind, Action: action, Subject: subject, Data: m})
		return nil
	}
	return e
}

func (e *saveEnv) seed(target string, isStack, enabled bool) {
	p := &models.AutoUpdatePolicy{DeviceID: "dev-1", Target: target, IsStack: isStack, Enabled: enabled}
	if isStack {
		p.ComposeWorkDir = "/srv/" + target
	}
	_ = e.policies.Upsert(context.Background(), p)
}

func (e *saveEnv) put(body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPut, "/api/v1/devices/dev-1/auto-updates", strings.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "dev-1")
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
	ctx = context.WithValue(ctx, middleware.ActorClaimsKey, jwt.MapClaims{"actor_sub": "admin-sub", "auth_method": "password"})
	w := httptest.NewRecorder()
	e.h.SetAutoUpdate(w, r.WithContext(ctx))
	return w
}

func (e *saveEnv) stored(target string) []models.AutoUpdatePolicy {
	var out []models.AutoUpdatePolicy
	for _, p := range e.policies.Policies["dev-1"] {
		if p.Target == target {
			out = append(out, p)
		}
	}
	return out
}

func TestAC003_TurningOffStoresDisabledPolicyAndKeepsIt(t *testing.T) {
	e := newSaveEnv(t)
	e.seed("web", false, true)

	w := e.put(`{"target":"web","is_stack":false,"compose_work_dir":"","enabled":false}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	rows := e.stored("web")
	require.Len(t, rows, 1, "[AC-003] turning off keeps the policy row instead of deleting it")
	assert.False(t, rows[0].Enabled, "[AC-003] the stored policy is disabled")
	assert.False(t, rows[0].IsStack)
}

func TestAC004_SuccessfulSaveWritesOneAuditWithKindAndValue(t *testing.T) {
	cases := []struct {
		name, body, kind string
		enabled          bool
	}{
		{"container on", `{"target":"web","is_stack":false,"compose_work_dir":"","enabled":true}`, "container", true},
		{"container off", `{"target":"web","is_stack":false,"compose_work_dir":"","enabled":false}`, "container", false},
		{"stack on", `{"target":"media","is_stack":true,"compose_work_dir":"/srv/media","enabled":true}`, "stack", true},
		{"stack off", `{"target":"media","is_stack":true,"compose_work_dir":"/srv/media","enabled":false}`, "stack", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSaveEnv(t)

			w := e.put(tc.body)

			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, e.audits, 1, "[AC-004] exactly one audit entry per successful save")
			a := e.audits[0]
			assert.Equal(t, "dev-1", a.Device)
			assert.Equal(t, "policy", a.Kind, "[AC-004] the existing Docker policy audit kind")
			assert.Equal(t, "auto_update", a.Action)
			assert.Equal(t, "admin-sub", a.Subject, "[AC-004] the acting admin is recorded")
			assert.Equal(t, tc.kind, a.Data["policy_kind"], "[AC-004] stack or container policy")
			assert.Equal(t, tc.enabled, a.Data["enabled"], "[AC-004] the new enabled value")
			assert.Len(t, a.Data, 3, "[AC-004] only target, policy_kind and enabled: no working dir or labels")
		})
	}
}

func TestAC004_AuditTargetIsSanitisedAndBounded(t *testing.T) {
	e := newSaveEnv(t)
	long := strings.Repeat("a", 200)

	w := e.put(`{"target":"` + long + `","is_stack":false,"enabled":true}`)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, e.audits, 1)
	assert.Len(t, e.audits[0].Data["target"], 128, "the audited target is truncated to 128 bytes")
}

func TestAC004_FailedSaveWritesNoAudit(t *testing.T) {
	t.Run("repository error", func(t *testing.T) {
		e := newSaveEnv(t)
		e.policies.Err = errors.New("database down")

		w := e.put(`{"target":"web","is_stack":false,"enabled":true}`)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Empty(t, e.audits, "[AC-004] a failed save writes no audit entry")
	})
	t.Run("invalid body", func(t *testing.T) {
		e := newSaveEnv(t)

		w := e.put(`{"target":`)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Empty(t, e.audits)
	})
	t.Run("missing target", func(t *testing.T) {
		e := newSaveEnv(t)

		w := e.put(`{"target":"","enabled":true}`)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Empty(t, e.audits)
	})
}

func TestAC004_AuditWriteFailureIsLoggedWithoutTarget(t *testing.T) {
	buf := captureServerLog(t)
	e := newSaveEnv(t)
	e.h.dockerAudit = func(context.Context, string, string, string, string, interface{}) error {
		return errors.New("insert failed")
	}

	w := e.put(`{"target":"secret-name","is_stack":false,"enabled":true}`)

	assert.Equal(t, http.StatusOK, w.Code, "the policy was stored; the audit failure does not change the response")
	assert.Contains(t, buf.String(), "docker policy audit write failed")
	assert.Contains(t, buf.String(), "policy_kind=container")
	assert.NotContains(t, buf.String(), "secret-name", "the warning carries no target text")
}

func TestAC011_NameClashMatrix(t *testing.T) {
	cases := []struct {
		name             string
		existingStack    bool
		existingEnabled  bool
		body             string
		wantStatus       int
		wantExistingKind string
	}{
		{"container on over enabled stack", true, true, `{"target":"app","is_stack":false,"enabled":true}`, 409, "stack"},
		{"container off over enabled stack", true, true, `{"target":"app","is_stack":false,"enabled":false}`, 409, "stack"},
		{"stack on over enabled container", false, true, `{"target":"app","is_stack":true,"compose_work_dir":"/srv/app","enabled":true}`, 409, "container"},
		{"stack off over enabled container", false, true, `{"target":"app","is_stack":true,"compose_work_dir":"/srv/app","enabled":false}`, 409, "container"},
		{"container on over disabled stack", true, false, `{"target":"app","is_stack":false,"enabled":true}`, 200, ""},
		{"stack on over disabled container", false, false, `{"target":"app","is_stack":true,"compose_work_dir":"/srv/app","enabled":true}`, 200, ""},
		{"same kind container", false, true, `{"target":"app","is_stack":false,"enabled":false}`, 200, ""},
		{"same kind stack", true, true, `{"target":"app","is_stack":true,"compose_work_dir":"/srv/app","enabled":false}`, 200, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newSaveEnv(t)
			e.seed("app", tc.existingStack, tc.existingEnabled)
			before, err := json.Marshal(e.stored("app"))
			require.NoError(t, err)

			w := e.put(tc.body)

			require.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			if tc.wantStatus == http.StatusConflict {
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, "auto_update_name_clash", body["code"], "[AC-011] the clash has its own code")
				assert.Equal(t, tc.wantExistingKind, body["existing_kind"], "[AC-011] the other policy's kind is named")
				assert.NotEmpty(t, body["error"])
				after, err := json.Marshal(e.stored("app"))
				require.NoError(t, err)
				assert.Equal(t, string(before), string(after), "[AC-011] a refused save leaves the row byte-identical")
				assert.Empty(t, e.audits, "[AC-011] a refused save is not audited")
				return
			}
			rows := e.stored("app")
			require.Len(t, rows, 1, "one row per target")
			var req struct {
				IsStack bool `json:"is_stack"`
				Enabled bool `json:"enabled"`
			}
			require.NoError(t, json.Unmarshal([]byte(tc.body), &req))
			assert.Equal(t, req.IsStack, rows[0].IsStack, "[AC-011] replacing a disabled policy of the other kind is allowed")
			assert.Equal(t, req.Enabled, rows[0].Enabled)
			assert.Len(t, e.audits, 1)
		})
	}
}

func TestAC011_ClashLogCarriesNoTarget(t *testing.T) {
	buf := captureServerLog(t)
	e := newSaveEnv(t)
	e.seed("private-app", true, true)

	w := e.put(`{"target":"private-app","is_stack":false,"enabled":true}`)

	require.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, buf.String(), "code=auto_update_name_clash")
	assert.NotContains(t, buf.String(), "private-app", "the 409 log has only the device and code")
}
