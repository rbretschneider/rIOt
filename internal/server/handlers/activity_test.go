package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/events"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/v1/activity, the registration hook and the Docker event keys of
// EXPECT-001 (ADD 6.1, AD-006, AD-005).

type fakeActivity struct {
	mu      sync.Mutex
	resp    models.ActivityResponse
	noted   []string
	journal *[]string
}

func (f *fakeActivity) Activities() models.ActivityResponse { return f.resp }

func (f *fakeActivity) NoteRegistration(deviceID string, _ time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.noted = append(f.noted, deviceID)
	if f.journal != nil {
		*f.journal = append(*f.journal, "NoteRegistration")
	}
}

func getActivity(t *testing.T, h *Handlers) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.GetActivity(rec, httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil))
	return rec
}

func TestEX_Activity_ReturnsTheTrackersCurrentState(t *testing.T) {
	back := time.Date(2026, 10, 26, 10, 15, 0, 0, time.UTC)
	started := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	src := &fakeActivity{resp: models.ActivityResponse{
		GeneratedAt: started.Add(time.Minute), NewestAgentVersion: "2.78.0",
		Activities: []models.DeviceActivity{{
			DeviceID: "hawaii", Kind: models.ActivityHost, Phase: models.PhaseReturn, Action: "reboot", CommandID: "c-1",
			ActorPhrase: "by ryan", StartedAt: started, BackBy: &back, CoversOffline: true, DeviceOnline: false,
		}, {
			DeviceID: "vermont", Kind: models.ActivityEscalated, Phase: models.PhaseEscalated, Action: "reboot", CommandID: "c-2",
			Escalation: &models.ActivityEscalation{Reason: "not_back", Bound: "15m", At: started},
		}},
	}}
	h := &Handlers{activity: src}

	rec := getActivity(t, h)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var raw struct {
		GeneratedAt        time.Time                    `json:"generated_at"`
		NewestAgentVersion string                       `json:"newest_agent_version"`
		Activities         []map[string]json.RawMessage `json:"activities"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	assert.Equal(t, "2.78.0", raw.NewestAgentVersion)
	require.Len(t, raw.Activities, 2, "one entry per device")
	for _, field := range []string{"device_id", "kind", "phase", "action", "command_id", "actor_phrase", "started_at", "back_by",
		"waiting_dpkg_lock", "progress", "agent_from_version", "target", "docker_actions", "covers_offline", "device_online",
		"escalation", "powered_off_at"} {
		assert.Contains(t, raw.Activities[0], field, "the ADD 6.1 field %q is always present", field)
	}
	assert.JSONEq(t, `true`, string(raw.Activities[0]["covers_offline"]))
	assert.JSONEq(t, `"2026-10-26T10:15:00Z"`, string(raw.Activities[0]["back_by"]))
	assert.JSONEq(t, `{"reason":"not_back","bound":"15m","at":"2026-10-26T10:00:00Z"}`, string(raw.Activities[1]["escalation"]),
		"escalation.bound is a string such as 15m")
	assert.JSONEq(t, `null`, string(raw.Activities[1]["back_by"]), "an absent time is null, not omitted")
}

func TestEX_Activity_EmptyListIsAnArrayNotNull(t *testing.T) {
	// The tracker builds its list with make; a nil slice from another source
	// would reach the web as null, which the web tolerates, but the contract says list.
	src := &fakeActivity{resp: models.ActivityResponse{Activities: []models.DeviceActivity{}}}
	rec := getActivity(t, &Handlers{activity: src})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"activities":[]`)
}

func TestEX_Activity_WithoutATrackerIs503(t *testing.T) {
	rec := getActivity(t, &Handlers{})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"error":"activity unavailable","code":"activity_unavailable"}`, rec.Body.String())
}

func TestEX_Activity_NewKeepsANilSourceNil(t *testing.T) {
	h := New(HandlerDeps{})
	assert.Nil(t, h.activity, "an unwired router must not look wired")
	assert.Equal(t, http.StatusServiceUnavailable, getActivity(t, h).Code)
}

// ---------------------------------------------------------------------------
// Registration hook (AD-006)
// ---------------------------------------------------------------------------

// orderedOracle journals the generator's NoteOnline next to the handler's
// NoteRegistration, to prove the order.
type orderedOracle struct{ journal *[]string }

func (o orderedOracle) CoverContainer(string, []string, time.Time) (models.Coverage, bool, error) {
	return models.Coverage{}, false, nil
}

func (o orderedOracle) CoverOffline(context.Context, string, time.Time) (models.OfflineCoverage, bool, error) {
	return models.OfflineCoverage{}, false, nil
}

func (o orderedOracle) NoteOnline(context.Context, string, time.Time) *models.OnlineNote {
	*o.journal = append(*o.journal, "NoteOnline")
	return nil
}

func registrationFixture(t *testing.T) (*regFixture, *fakeActivity, *[]string) {
	t.Helper()
	f := newRegisterFixture(t, true, false)
	journal := &[]string{}
	act := &fakeActivity{journal: journal}
	f.h.activity = act
	f.h.eventGen.SetExpectedActivity(orderedOracle{journal: journal})
	return f, act, journal
}

func TestEX_SEC001_RegistrationNotesBeforeDeviceOnline(t *testing.T) {
	f, act, journal := registrationFixture(t)
	f.seedDevice(regTestD1, "hawaii", models.DeviceStatusOffline, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "hawaii", nil), "riot_k1", nil)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Equal(t, []string{f.dev.Devices[regTestD1].ID}, act.noted, "the tracker is told of the registration")
	assert.Equal(t, []string{"NoteRegistration", "NoteOnline"}, *journal,
		"[SEC-001] NoteRegistration runs before DeviceOnline's NoteOnline: the online note reads what the registration recorded")
}

func TestEX_SEC001_RegistrationWhileOnlineStillNotesTheTracker(t *testing.T) {
	f, act, journal := registrationFixture(t)
	f.seedDevice(regTestD1, "hawaii", models.DeviceStatusOnline, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "hawaii", nil), "riot_k1", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, act.noted, 1, "[SEC-001] a registration is evidence of life even when the device was already online")
	assert.Equal(t, []string{"NoteRegistration"}, *journal, "and no online event is raised for a device that was live")
}

func TestEX_SEC004_RegistrationOfAWarningDeviceIsNotAReturn(t *testing.T) {
	f, act, journal := registrationFixture(t)
	f.seedDevice(regTestD1, "hawaii", models.DeviceStatusWarning, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "hawaii", nil), "riot_k1", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, act.noted, 1)
	assert.Equal(t, []string{"NoteRegistration"}, *journal, "[SEC-004] a warning device is live: no came-online transition")
}

func TestEX_SEC001_RejectedRegistrationNotesNothing(t *testing.T) {
	f, act, journal := registrationFixture(t)
	f.seedDevice(regTestD1, "hawaii", models.DeviceStatusOffline, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "hawaii", nil), "wrong-key", nil)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, act.noted, "a rejected caller is no evidence of life")
	assert.Empty(t, *journal)
	assert.Equal(t, models.DeviceStatusOffline, f.dev.Devices[regTestD1].Status)
}

func TestEX_SEC001_NewDeviceRegistrationHasNoWindowsToNote(t *testing.T) {
	f, act, _ := registrationFixture(t)

	rec := f.post(t, regBody("", "brand-new", nil), "", nil)

	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
	assert.Empty(t, act.noted, "a new device has no commands, so no tracker hook")
}

func TestEX_SEC001_RegistrationWithoutATrackerStillWorks(t *testing.T) {
	f := newRegisterFixture(t, true, false)
	f.seedDevice(regTestD1, "hawaii", models.DeviceStatusOffline, nil)
	f.seedKey("riot_k1", regTestD1)

	rec := f.post(t, regBody(regTestD1, "hawaii", nil), "riot_k1", nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, models.DeviceStatusOnline, f.dev.Devices[regTestD1].Status)
}

// ---------------------------------------------------------------------------
// ReceiveDockerEvent hands the target keys to the generator (AD-005)
// ---------------------------------------------------------------------------

type keyOracle struct {
	mu   sync.Mutex
	keys [][]string
	cov  models.Coverage
	ok   bool
}

func (k *keyOracle) CoverContainer(_ string, keys []string, _ time.Time) (models.Coverage, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = append(k.keys, keys)
	return k.cov, k.ok, nil
}

func (k *keyOracle) CoverOffline(context.Context, string, time.Time) (models.OfflineCoverage, bool, error) {
	return models.OfflineCoverage{}, false, nil
}

func (k *keyOracle) NoteOnline(context.Context, string, time.Time) *models.OnlineNote { return nil }

func TestEX_AC044_ReceiveDockerEventAsksTheTrackerWithTheServersKeys(t *testing.T) {
	eventRepo := testutil.NewMockEventRepo()
	hub := websocket.NewHub()
	go hub.Run()
	devs := testutil.NewMockDeviceRepo()
	devs.Devices["dev-1"] = &models.Device{ID: "dev-1", Hostname: "hawaii", Status: models.DeviceStatusOnline}
	gen := events.NewGenerator(eventRepo, hub, testutil.NewMockAlertRuleRepo(), testutil.NewMockDispatcher(), nil)
	oracle := &keyOracle{ok: true, cov: models.Coverage{CommandID: "cmd-1", ShortID: "cmd-1", ActionLabel: "Docker update", ActorPhrase: "by ryan"}}
	gen.SetExpectedActivity(oracle)
	h := &Handlers{
		devices: devs, hub: hub, eventGen: gen, commandRepo: testutil.NewMockCommandRepo(),
		telemetry: composeSnapshot("dev-1", "web", "/srv/stack"),
	}

	r := chi.NewRouter()
	r.Post("/api/v1/devices/{id}/docker/events", h.ReceiveDockerEvent)
	body, err := json.Marshal(map[string]string{"container_name": "web", "container_id": "0123456789abcdef", "action": "die"})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/devices/dev-1/docker/events", bytes.NewReader(body)))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	require.Len(t, oracle.keys, 1, "the generator asked the tracker once")
	assert.Equal(t, []string{models.TargetKeyCompose("/srv/stack"), models.TargetKeyContainer("web")}, oracle.keys[0],
		"[AC-044] with the keys the handler computed from the server's own data")
	require.Len(t, eventRepo.Events, 1)
	assert.Equal(t, models.SeverityInfo, eventRepo.Events[0].Severity, "the covered death is recorded, not alerted")
	assert.Equal(t, "cmd-1", eventRepo.Events[0].CommandID)
	assert.Equal(t, models.CommandAttrExpected, eventRepo.Events[0].CommandAttr)
}
