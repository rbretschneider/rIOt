package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/testutil"
)

// Patch-status rows carry the per-host badge, and ?all=true returns every
// registered device.

type badgeRow struct {
	DeviceID       string      `json:"device_id"`
	Hostname       string      `json:"hostname"`
	PendingUpdates int         `json:"pending_updates"`
	UpdatesAt      *time.Time  `json:"updates_at"`
	PatchBadge     *patchBadge `json:"patch_badge"`
}

type badgeFixture struct {
	h       *Handlers
	tel     *testutil.MockTelemetryRepo
	devices *testutil.MockDeviceRepo
	cmds    *testutil.MockCommandRepo
}

const (
	badgeCmdID1 = "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c"
	badgeCmdID2 = "1c8b64b7-2a3f-4c7b-8e2e-3e4a6d7b8c9d"
)

// newBadgeFixture builds a handler with a daily 03:00 window, gated reboot
// class, a fixed clock and four devices: patched, pending, reboot-only and one
// without telemetry. The command repository is included only when withCmds.
func newBadgeFixture(t *testing.T, withCmds bool) *badgeFixture {
	t.Helper()
	adminRepo := testutil.NewMockAdminRepo("hash")
	raw, err := json.Marshal(models.AutomationConfig{OSPatch: windowPolicy("gated")})
	require.NoError(t, err)
	require.NoError(t, adminRepo.SetConfig(context.Background(), "automation_config", string(raw)))

	f := &badgeFixture{
		tel:     testutil.NewMockTelemetryRepo(),
		devices: testutil.NewMockDeviceRepo(),
	}
	for _, d := range []models.Device{
		{ID: "dev-patched", Hostname: "alpha", AutoPatch: true},
		{ID: "dev-pending", Hostname: "bravo", AutoPatch: true},
		{ID: "dev-reboot", Hostname: "charlie", AutoPatch: false},
		{ID: "dev-nodata", Hostname: "delta", AutoPatch: true},
	} {
		d := d
		f.devices.Devices[d.ID] = &d
	}
	facts := func() *models.UpdateInfo {
		return &models.UpdateInfo{
			PackageManager: "apt", AllowReboot: boolPtr(true), AllowPatching: boolPtr(true),
			HoldRebootClass: boolPtr(true), OtherHolds: &models.OtherHolds{Status: "ok"},
			ReleaseUpgrade: &models.ReleaseUpgrade{Status: "none"},
		}
	}
	snap := func(id string, u *models.UpdateInfo) {
		f.tel.Snapshots[id] = []models.TelemetrySnapshot{{
			DeviceID: id, Timestamp: badgeNow.Add(-time.Hour), Data: models.FullTelemetryData{Updates: u},
		}}
	}
	snap("dev-patched", facts())
	pending := facts()
	pending.PendingUpdates = 4
	snap("dev-pending", pending)
	reboot := facts()
	reboot.RebootRequired = true
	snap("dev-reboot", reboot)

	f.h = &Handlers{telemetry: f.tel, devices: f.devices, adminRepo: adminRepo, now: func() time.Time { return badgeNow }}
	if withCmds {
		f.cmds = testutil.NewMockCommandRepo()
		f.h.commandRepo = f.cmds
	}
	return f
}

func (f *badgeFixture) get(t *testing.T, query string) []badgeRow {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/fleet/patch-status"+query, nil)
	rec := httptest.NewRecorder()
	f.h.PatchStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []badgeRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows), "response must stay a top-level JSON array")
	return rows
}

func rowIDs(rows []badgeRow) []string {
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, r.DeviceID)
	}
	return ids
}

func rowByID(t *testing.T, rows []badgeRow, id string) badgeRow {
	t.Helper()
	for _, r := range rows {
		if r.DeviceID == id {
			return r
		}
	}
	require.Failf(t, "row missing", "no row for %s in %v", id, rowIDs(rows))
	return badgeRow{}
}

func TestPatchStatusBadge_AllTrue_OneRowPerRegisteredDeviceEachWithABadge(t *testing.T) { // AC-001
	f := newBadgeFixture(t, true)
	rows := f.get(t, "?all=true")
	assert.Equal(t, []string{"dev-patched", "dev-pending", "dev-reboot", "dev-nodata"}, rowIDs(rows), "one row per device, ordered by hostname")
	for _, r := range rows {
		require.NotNil(t, r.PatchBadge, "device %s must have a badge", r.DeviceID)
		assert.NotEmpty(t, r.PatchBadge.Label, r.DeviceID)
	}
	assert.Equal(t, "Auto", rowByID(t, rows, "dev-patched").PatchBadge.Label, "a fully patched host still gets a badge")
	assert.Equal(t, "Needs you", rowByID(t, rows, "dev-nodata").PatchBadge.Label, "a host with no patch data cannot be patched by rIOt")
}

func TestPatchStatusBadge_DefaultFilter_UnchangedAndBadgeOnEveryRow(t *testing.T) { // AC-001, AC-027
	f := newBadgeFixture(t, true)
	rows := f.get(t, "")
	assert.ElementsMatch(t, []string{"dev-pending", "dev-reboot"}, rowIDs(rows), "without all=true only hosts with pending updates or a reboot are listed")
	for _, r := range rows {
		require.NotNil(t, r.PatchBadge, r.DeviceID)
	}
}

func TestPatchStatusBadge_AllFalseValuesKeepDefaultFilter(t *testing.T) { // AC-027
	f := newBadgeFixture(t, true)
	assert.Len(t, f.get(t, "?all=false"), 2)
	assert.Len(t, f.get(t, "?all=1"), 2, "only the exact value true opts in")
}

func TestPatchStatusBadge_RowBadgeEqualsComputePatchBadge(t *testing.T) { // AC-002
	f := newBadgeFixture(t, true)
	row := rowByID(t, f.get(t, "?all=true"), "dev-reboot")
	cfg := f.h.loadAutomationConfig(context.Background())
	u := f.tel.Snapshots["dev-reboot"][0].Data.Updates
	at := f.tel.Snapshots["dev-reboot"][0].Timestamp
	want := computePatchBadge(patchBadgeInput{
		AutoPatch: false, OSPatch: cfg.OSPatch, Updates: u, UpdatesAt: &at, AgentFactsMinVersion: patchFactsMinAgentVersion,
	}, badgeNow)
	require.NotNil(t, row.PatchBadge)
	assert.Equal(t, want, *row.PatchBadge, "the served badge must be the function output")
	require.NotNil(t, row.UpdatesAt)
	assert.True(t, row.UpdatesAt.Equal(at), "updates_at is the snapshot time")
}

func TestPatchStatusBadge_DefaultAndAllRowsCarryIdenticalBadge(t *testing.T) { // AC-002
	f := newBadgeFixture(t, true)
	a := rowByID(t, f.get(t, ""), "dev-pending")
	b := rowByID(t, f.get(t, "?all=true"), "dev-pending")
	assert.Equal(t, a.PatchBadge, b.PatchBadge)
}

func TestPatchStatusBadge_NilCommandRepoStillServesBadges(t *testing.T) { // AC-026, AC-027
	f := newBadgeFixture(t, false)
	rows := f.get(t, "?all=true")
	require.Len(t, rows, 4)
	assert.Equal(t, "Auto", rowByID(t, rows, "dev-patched").PatchBadge.Label)
}

func TestPatchStatusBadge_FailingOutcomeQueryStillReturns200(t *testing.T) { // AD-5
	f := newBadgeFixture(t, true)
	f.cmds.FailOn("LatestFinishedOSUpdates", errors.New("db down"))
	rows := f.get(t, "?all=true")
	require.Len(t, rows, 4)
	assert.Equal(t, "Auto", rowByID(t, rows, "dev-patched").PatchBadge.Label, "no outcomes means no failed-run reason")
}

func TestPatchStatusBadge_BuildsNoCommandsAndOnlyReadsOutcomes(t *testing.T) { // AC-026
	f := newBadgeFixture(t, true)
	f.get(t, "?all=true")
	f.get(t, "")
	assert.Empty(t, f.cmds.All(), "serving patch-status must never create a command")
	for _, call := range f.cmds.Calls() {
		assert.Equal(t, "LatestFinishedOSUpdates", call, "patch-status may only read finished outcomes")
	}
}

func TestPatchStatusBadge_LastRunFailed_RaisedClearedAndNotChangedByExpired(t *testing.T) { // AC-013
	f := newBadgeFixture(t, true)
	seed := func(id, status string, ago time.Duration) {
		f.cmds.Seed(&models.Command{ID: id, DeviceID: "dev-patched", Action: models.ActionOSUpdate, Status: status,
			ResultMsg: "dpkg failed", CreatedAt: badgeNow.Add(-ago)})
	}
	label := func() patchBadge {
		return *rowByID(t, f.get(t, "?all=true"), "dev-patched").PatchBadge
	}

	seed(badgeCmdID1, "failed", 3*time.Hour)
	b := label()
	assert.Equal(t, "Needs you", b.Label)
	assert.Equal(t, []string{reasonLastRunFailed}, reasonCodes(b))
	assert.Equal(t, badgeCmdID1, b.Explanation.NeedsYou[0].CommandID)

	seed(badgeCmdID2, "expired", 2*time.Hour)
	assert.Equal(t, "Needs you", label().Label, "a later expired run neither clears nor changes the failure")

	f.cmds.Seed(&models.Command{ID: "2d9c75c8-3b4a-4d8c-9f3f-4f5b7e8c9dae", DeviceID: "dev-patched", Action: models.ActionOSUpdate,
		Status: "succeeded", CreatedAt: badgeNow.Add(-time.Hour)})
	assert.Equal(t, "Auto", label().Label, "a later succeeded run clears it")
}

func TestPatchStatusBadge_MockOutcomeQueryIgnoresExpiredCancelledAndRunning(t *testing.T) { // AC-013
	repo := testutil.NewMockCommandRepo()
	add := func(id, device, action, status string, ago time.Duration) {
		repo.Seed(&models.Command{ID: id, DeviceID: device, Action: action, Status: status, CreatedAt: badgeNow.Add(-ago)})
	}
	add("a", "d1", models.ActionOSUpdate, "failed", 5*time.Hour)
	add("b", "d1", models.ActionOSUpdate, "expired", 4*time.Hour)
	add("c", "d1", models.ActionOSUpdate, "cancelled", 3*time.Hour)
	add("d", "d1", models.ActionOSUpdate, "running", 2*time.Hour)
	add("e", "d2", models.ActionOSUpdate, "expired", time.Hour)
	add("f", "d3", models.ActionReboot, "succeeded", time.Hour)

	got, err := repo.LatestFinishedOSUpdates(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1, "only d1 has a finished os_update")
	assert.Equal(t, "a", got["d1"].ID)
}

func TestPatchStatusBadge_OldAgentPayloadServesBadgeWithNewerAgentNote(t *testing.T) { // AC-023, AC-027
	f := newBadgeFixture(t, true)
	f.tel.Snapshots["dev-patched"][0].Data.Updates = &models.UpdateInfo{PackageManager: "apt", AllowReboot: boolPtr(true)}
	b := rowByID(t, f.get(t, "?all=true"), "dev-patched").PatchBadge
	require.NotNil(t, b)
	assert.Equal(t, "Auto", b.Label, "missing facts never raise a reason")
	assert.Contains(t, b.Explanation.Notes, "Some checks need a newer agent ("+patchFactsMinAgentVersion+" or later).")
}

func TestPatchStatusBadge_AllTrueFailsWhenDeviceListFails(t *testing.T) {
	f := newBadgeFixture(t, true)
	f.devices.Err = errors.New("db down")
	req := httptest.NewRequest("GET", "/api/v1/fleet/patch-status?all=true", nil)
	rec := httptest.NewRecorder()
	f.h.PatchStatus(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// --- PATCH-FLOW-001: freshness on patch-status rows ---

type freshnessRow struct {
	DeviceID       string `json:"device_id"`
	PendingUpdates int    `json:"pending_updates"`
	SecurityCount  int    `json:"security_count"`
	RebootRequired bool   `json:"reboot_required"`
	ListsFreshness string `json:"lists_freshness"`
}

func (f *badgeFixture) freshnessRows(t *testing.T, query string) map[string]freshnessRow {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/fleet/patch-status"+query, nil)
	rec := httptest.NewRecorder()
	f.h.PatchStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []freshnessRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	byID := map[string]freshnessRow{}
	for _, r := range rows {
		byID[r.DeviceID] = r
	}
	return byID
}

func TestPatchStatus_PF_AC027_RowCarriesStaleFreshness(t *testing.T) {
	f := newBadgeFixture(t, true)
	stale := f.tel.Snapshots["dev-pending"][0].Data.Updates
	stale.PendingUpdates, stale.PendingSecurityCount = 38, 3
	stale.ListsFreshness = models.ListsStale
	f.tel.Snapshots["dev-patched"][0].Data.Updates.ListsFreshness = models.ListsFresh

	rows := f.freshnessRows(t, "")

	pending, ok := rows["dev-pending"]
	require.True(t, ok, "[AC-027] a stale host with a raw count stays listed and patchable")
	assert.Equal(t, "stale", pending.ListsFreshness)
	assert.Equal(t, 38, pending.PendingUpdates, "[AC-027] the raw count is still served for diagnostics")
	assert.Equal(t, 3, pending.SecurityCount, "[SEC-007] the raw security count is still served")
	all := f.freshnessRows(t, "?all=true")
	assert.Equal(t, "fresh", all["dev-patched"].ListsFreshness)
}

func TestPatchStatus_PF_AC029_AbsentAndBogusServeUnknown(t *testing.T) {
	f := newBadgeFixture(t, true)
	var bogus models.UpdateInfo
	require.NoError(t, json.Unmarshal([]byte(`{"package_manager":"apt","pending_updates":4,"lists_freshness":"bogus"}`), &bogus))
	f.tel.Snapshots["dev-pending"][0].Data.Updates = &bogus

	rows := f.freshnessRows(t, "?all=true")

	assert.Equal(t, "unknown", rows["dev-pending"].ListsFreshness, "[AC-029] an unrecognized value is served as unknown")
	assert.Equal(t, "unknown", rows["dev-patched"].ListsFreshness, "[AC-029] an older agent (no field) is served as unknown")
	assert.Equal(t, "unknown", rows["dev-nodata"].ListsFreshness, "[AC-029] a host with no telemetry is always present as unknown")
}

func TestPatchStatus_PF_AC006_PendingOnlyRowIsNotRebootRequired(t *testing.T) {
	f := newBadgeFixture(t, true)
	pending := f.tel.Snapshots["dev-pending"][0].Data.Updates
	pending.PendingRebootClassCount = 1
	pending.RebootRequired = false

	rows := f.freshnessRows(t, "")

	assert.False(t, rows["dev-pending"].RebootRequired, "[AC-006] a pending kernel update never marks a host reboot_required")
	assert.True(t, rows["dev-reboot"].RebootRequired)
}
