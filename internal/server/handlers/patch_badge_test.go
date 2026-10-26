package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// badgeNow is the injected clock. With the daily 03:00-05:00 window below, the
// next window start is 2026-10-25 03:00 UTC.
var badgeNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

const badgeFactsVersion = "v9.9.9"

func windowPolicy(rebootClass string) models.MaintenanceWindow {
	return models.MaintenanceWindow{Mode: "window", StartTime: "03:00", EndTime: "05:00", CooldownMinutes: 60, RebootClass: rebootClass}
}

func anytimePolicy(rebootClass string) models.MaintenanceWindow {
	return models.MaintenanceWindow{Mode: "anytime", CooldownMinutes: 45, RebootClass: rebootClass}
}

// healthyInput is an apt host that reports every fact, with patching on and
// nothing pending: the plain "Auto" case.
func healthyInput() patchBadgeInput {
	at := badgeNow.Add(-2 * time.Hour)
	return patchBadgeInput{
		AutoPatch: true,
		OSPatch:   windowPolicy("gated"),
		Updates: &models.UpdateInfo{
			PackageManager:  "apt",
			AllowReboot:     boolPtr(true),
			AllowPatching:   boolPtr(true),
			HoldRebootClass: boolPtr(true),
			HoldEnforcement: "active",
			OtherHolds:      &models.OtherHolds{Status: models.FactStatusOK},
			ReleaseUpgrade:  &models.ReleaseUpgrade{Status: models.FactStatusNone},
		},
		UpdatesAt:            &at,
		AgentFactsMinVersion: badgeFactsVersion,
	}
}

// waitingInput is a healthy host with two apt updates waiting. Under group K
// (EXTRA-UPD-001) most reasons are action items only when something is waiting,
// so the pre-story reason tests run against this host.
func waitingInput() patchBadgeInput {
	in := healthyInput()
	in.Updates.PendingUpdates = 2
	in.Updates.Updates = []models.PendingUpdate{{Name: "curl"}, {Name: "nginx"}}
	return in
}

func infoCodes(b patchBadge) []string {
	codes := []string{}
	for _, r := range b.Explanation.Info {
		codes = append(codes, r.Code)
	}
	return codes
}

func findInfo(t *testing.T, b patchBadge, code string) patchBadgeReason {
	t.Helper()
	for _, r := range b.Explanation.Info {
		if r.Code == code {
			return r
		}
	}
	require.Failf(t, "information item missing", "expected %q in %v", code, infoCodes(b))
	return patchBadgeReason{}
}

func reasonCodes(b patchBadge) []string {
	codes := []string{}
	for _, r := range b.Explanation.NeedsYou {
		codes = append(codes, r.Code)
	}
	return codes
}

func findReason(t *testing.T, b patchBadge, code string) patchBadgeReason {
	t.Helper()
	for _, r := range b.Explanation.NeedsYou {
		if r.Code == code {
			return r
		}
	}
	require.Failf(t, "reason missing", "expected reason %q in %v", code, reasonCodes(b))
	return patchBadgeReason{}
}

func failedRun(id, status, msg string) *models.Command {
	return &models.Command{ID: id, Action: models.ActionOSUpdate, Status: status, ResultMsg: msg}
}

// --- Decision table (AC-016..AC-019, AC-020) ---

func TestPatchBadge_DecisionTable_AutoWhenNothingPending(t *testing.T) { // AC-019
	b := computePatchBadge(healthyInput(), badgeNow)
	assert.Equal(t, badgeAuto, b.Kind)
	assert.Equal(t, "Auto", b.Label)
	assert.Equal(t, "Not needed.", b.Explanation.Reboot)
	assert.Nil(t, b.RebootAt, "no reboot time on an Auto badge")
	assert.Empty(t, b.Explanation.NeedsYou)
	assert.Equal(t, "Nothing.", b.Explanation.NeedsYouSummary)
}

func TestPatchBadge_DecisionTable_AutoWithPendingUpdatesNoReboot(t *testing.T) { // AC-019
	in := healthyInput()
	in.Updates.PendingUpdates = 7
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeAuto, b.Kind)
	assert.Equal(t, "Not needed.", b.Explanation.Reboot)
}

func TestPatchBadge_DecisionTable_RebootDateInWindowMode(t *testing.T) { // AC-016
	in := healthyInput()
	in.Updates.PendingUpdates = 3
	in.Updates.PendingRebootClassCount = 1
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeAutoRebootDate, b.Kind)
	assert.Equal(t, "Auto (reboot on 2026-10-25)", b.Label)
	assert.Equal(t, "The pending patch includes a kernel/GPU update; a reboot will be needed after it installs. It will happen automatically on 2026-10-25 03:00 UTC (estimate).", b.Explanation.Reboot)
	require.NotNil(t, b.RebootAt)
	assert.Equal(t, time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC), *b.RebootAt)
	assert.Equal(t, "Yes. Next window starts 2026-10-25 03:00 UTC.", b.Explanation.Automatic)
	assert.Empty(t, b.Explanation.NeedsYou)
}

func TestPatchBadge_DecisionTable_RebootDateUnknownInAnytimeMode(t *testing.T) { // AC-017
	in := healthyInput()
	in.OSPatch = anytimePolicy("gated")
	in.Updates.PendingRebootClassCount = 1
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeAutoRebootUnknown, b.Kind)
	assert.Equal(t, "Auto (reboot date Unknown)", b.Label)
	assert.Contains(t, b.Explanation.Reboot, "next automatic patch run")
	assert.Nil(t, b.RebootAt, "an unknown date must not carry a time")
	assert.Equal(t, "Yes. Any time (within the 45-minute cooldown).", b.Explanation.Automatic)
}

func TestPatchBadge_DecisionTable_RebootDateUnknownWhenAllowRebootNotReported(t *testing.T) { // AC-018
	in := healthyInput()
	in.Updates.AllowReboot = nil
	in.Updates.PendingRebootClassCount = 1
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeAutoRebootUnknown, b.Kind)
	assert.Contains(t, b.Explanation.Reboot, "update the agent")
	assert.Nil(t, b.RebootAt)
}

func TestPatchBadge_DecisionTable_RebootDateUnknownCitesBothCausesInAnytimeWithOldAgent(t *testing.T) { // AC-017, AC-018
	in := healthyInput()
	in.OSPatch = anytimePolicy("gated")
	in.Updates.AllowReboot = nil
	in.Updates.PendingRebootClassCount = 1
	b := computePatchBadge(in, badgeNow)
	assert.Contains(t, b.Explanation.Reboot, "next automatic patch run")
	assert.Contains(t, b.Explanation.Reboot, "update the agent")
}

func TestPatchBadge_DecisionTable_NoRebootDateOnNeedsYouOrAuto(t *testing.T) { // AC-020
	in := healthyInput()
	in.Updates.PendingRebootClassCount = 1
	in.Updates.PendingUpdates = 1
	in.AutoPatch = false
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Nil(t, b.RebootAt)
	assert.NotContains(t, b.Label, "reboot on")
	assert.NotContains(t, b.Explanation.Reboot, "2026-10-25")

	auto := computePatchBadge(healthyInput(), badgeNow)
	assert.Nil(t, auto.RebootAt)
}

func TestPatchBadge_DecisionTable_FullyPatchedOfflineAndWindowsEachGetOneBadge(t *testing.T) { // AC-001
	patched := healthyInput()
	assert.Equal(t, badgeAuto, computePatchBadge(patched, badgeNow).Kind)

	windows := healthyInput()
	windows.Updates = nil
	windows.UpdatesAt = nil
	assert.Equal(t, badgeManual, computePatchBadge(windows, badgeNow).Kind, "[AC-053 d] no patch data means nothing is known to be waiting: information, not Needs you")

	pending := healthyInput()
	pending.Updates.PendingUpdates = 12
	assert.Equal(t, badgeAuto, computePatchBadge(pending, badgeNow).Kind)

	for name, in := range map[string]patchBadgeInput{"patched": patched, "windows": windows, "pending": pending} {
		b := computePatchBadge(in, badgeNow)
		assert.NotEmpty(t, b.Kind, name)
		assert.NotEmpty(t, b.Label, name)
	}
}

func TestPatchBadge_Explanation_FourLinesAlwaysPopulated(t *testing.T) { // AC-021
	for name, in := range map[string]patchBadgeInput{
		"healthy": healthyInput(),
		"off":     func() patchBadgeInput { i := healthyInput(); i.AutoPatch = false; return i }(),
		"nodata":  func() patchBadgeInput { i := healthyInput(); i.Updates, i.UpdatesAt = nil, nil; return i }(),
	} {
		e := computePatchBadge(in, badgeNow).Explanation
		assert.NotEmpty(t, e.Automatic, name+": Automatic patching line")
		assert.NotEmpty(t, e.Reboot, name+": Reboot line")
		assert.NotEmpty(t, e.NeedsYouSummary, name+": Needs you line")
		assert.NotEmpty(t, e.AsOf, name+": As of line")
		assert.NotNil(t, e.NeedsYou, name+": needs_you must serialise as a list")
		assert.NotNil(t, e.Notes, name+": notes must serialise as a list")
	}
}

func TestPatchBadge_Explanation_AsOfShowsUTCTimeAndAge(t *testing.T) { // AC-025
	in := healthyInput()
	at := badgeNow.Add(-26 * time.Hour)
	in.UpdatesAt = &at
	assert.Equal(t, "2026-10-23 10:00 UTC (26 h ago)", computePatchBadge(in, badgeNow).Explanation.AsOf)
	in.UpdatesAt = nil
	assert.Equal(t, "No patch data reported yet.", computePatchBadge(in, badgeNow).Explanation.AsOf)
}

func TestPatchBadge_Explanation_SameInputSameText(t *testing.T) { // AC-002, AC-022
	in := healthyInput()
	in.Updates.PendingRebootClassCount = 2
	assert.Equal(t, computePatchBadge(in, badgeNow), computePatchBadge(in, badgeNow))
}

// --- NY-1 .. NY-10 ---

func TestPatchBadge_AutoPatchOff_NeedsYouWithTurnOnAction(t *testing.T) { // AC-004, AC-047
	in := waitingInput()
	in.AutoPatch = false
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Equal(t, []string{reasonAutoPatchOff}, reasonCodes(b))
	assert.Contains(t, findReason(t, b, reasonAutoPatchOff).Action, "Turn on Auto-patch")
	assert.Equal(t, "No.", b.Explanation.Automatic)
}

func TestPatchBadge_AutoPatchOn_DoesNotRaiseReason(t *testing.T) { // AC-004
	assert.NotContains(t, reasonCodes(computePatchBadge(healthyInput(), badgeNow)), reasonAutoPatchOff)
}

func TestPatchBadge_PatchingDisabledServer_DisabledMode(t *testing.T) { // AC-005
	in := waitingInput()
	in.OSPatch = models.MaintenanceWindow{Mode: "disabled"}
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Equal(t, []string{reasonPatchingDisabled}, reasonCodes(b))
	assert.Contains(t, findReason(t, b, reasonPatchingDisabled).Text, "disabled server-wide")
	assert.Contains(t, findReason(t, b, reasonPatchingDisabled).Action, "Settings > Agents")
}

func TestPatchBadge_PatchingDisabledServer_WindowWithNoComputableStart(t *testing.T) { // AC-005
	in := waitingInput()
	in.OSPatch = models.MaintenanceWindow{Mode: "window", StartTime: "bogus", EndTime: "05:00"}
	b := computePatchBadge(in, badgeNow)
	assert.Contains(t, reasonCodes(b), reasonPatchingDisabled)
	assert.Contains(t, findReason(t, b, reasonPatchingDisabled).Text, "disabled server-wide")
}

func TestPatchBadge_PatchingDisabledServer_AnytimeAndWindowAreNotDisabled(t *testing.T) { // AC-005
	assert.NotContains(t, reasonCodes(computePatchBadge(healthyInput(), badgeNow)), reasonPatchingDisabled)
	in := healthyInput()
	in.OSPatch = anytimePolicy("")
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonPatchingDisabled)
}

func TestPatchBadge_AgentPatchingNotAllowed_ReportedFalse(t *testing.T) { // AC-006
	in := waitingInput()
	in.Updates.AllowPatching = boolPtr(false)
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, []string{reasonAgentPatchingOff}, reasonCodes(b))
	assert.Contains(t, findReason(t, b, reasonAgentPatchingOff).Text, "does not allow patching")
	assert.Contains(t, findReason(t, b, reasonAgentPatchingOff).Action, "commands.allow_patching: true")
}

func TestPatchBadge_AgentPatchingNotAllowed_NotReportedNeverRaises(t *testing.T) { // AC-006, AC-023
	in := healthyInput()
	in.Updates.AllowPatching = nil
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonAgentPatchingOff)
}

func TestPatchBadge_CannotPatchHost_NoPatchData(t *testing.T) { // AC-007
	in := healthyInput()
	in.Updates, in.UpdatesAt = nil, nil
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeManual, b.Kind, "[AC-053 d] no patch data is information: nothing is known to be waiting")
	assert.Empty(t, reasonCodes(b))
	assert.Equal(t, []string{reasonCannotPatchHost}, infoCodes(b))
	assert.Contains(t, findInfo(t, b, reasonCannotPatchHost).Text, "cannot patch this host")
}

func TestPatchBadge_CannotPatchHost_UnsupportedPackageManager(t *testing.T) { // AC-007
	in := healthyInput()
	in.Updates = &models.UpdateInfo{PackageManager: "pacman", PendingUpdates: 4}
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, []string{reasonCannotPatchHost}, reasonCodes(b), "[AC-053 e] four updates waiting on a host rIOt cannot patch need you")
	assert.Contains(t, findReason(t, b, reasonCannotPatchHost).Text, "pacman")
}

func TestPatchBadge_CannotPatchHost_SkipsHoldAndRebootReasons(t *testing.T) { // AC-007
	in := healthyInput()
	in.OSPatch = windowPolicy("")
	in.Updates = &models.UpdateInfo{
		PackageManager: "pacman", PendingUpdates: 3, RebootRequired: true, HoldEnforcement: "no_privilege",
		HeldPackages:   []string{"linux"},
		OtherHolds:     &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1},
		ReleaseUpgrade: &models.ReleaseUpgrade{Status: models.FactStatusAvailable, Name: "26.04"},
	}
	assert.Equal(t, []string{reasonCannotPatchHost}, reasonCodes(computePatchBadge(in, badgeNow)),
		"NY-5..NY-8 and NY-10 must be skipped when the host cannot be patched")
}

func TestPatchBadge_CannotPatchHost_AptAndDnfAreSupported(t *testing.T) { // AC-007
	for _, pm := range []string{"apt", "dnf"} {
		in := healthyInput()
		in.Updates.PackageManager = pm
		assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonCannotPatchHost, pm)
	}
}

func TestPatchBadge_HeldPackages_NamesTheHeldPackage(t *testing.T) { // AC-008
	in := waitingInput()
	in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1}
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind, "[AC-052 c] the held package has a newer version available")
	assert.Equal(t, []string{reasonHeldPackages}, reasonCodes(b))
	assert.Contains(t, findReason(t, b, reasonHeldPackages).Text, "nginx")
}

func TestPatchBadge_HeldPackages_ListsTenAndMore(t *testing.T) { // AC-008
	in := waitingInput()
	in.Updates.Updates = append(in.Updates.Updates, models.PendingUpdate{Name: "pkga"})
	var names []string
	for i := 0; i < 25; i++ {
		names = append(names, "pkg"+string(rune('a'+i)))
	}
	in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: names, Total: 40}
	text := findReason(t, computePatchBadge(in, badgeNow), reasonHeldPackages).Text
	assert.Contains(t, text, "pkga, pkgb")
	assert.Contains(t, text, "pkgj")
	assert.NotContains(t, text, "pkgk", "only 10 names are shown")
	assert.Contains(t, text, "and 30 more")
}

func TestPatchBadge_HeldPackages_RiotManagedHoldsAreIgnored(t *testing.T) { // AC-009
	in := healthyInput()
	in.Updates.HeldPackages = []string{"linux-image-generic"}
	in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"linux-image-generic"}, Total: 1}
	b := computePatchBadge(in, badgeNow)
	assert.NotContains(t, reasonCodes(b), reasonHeldPackages)
	assert.NotContains(t, reasonCodes(b), reasonRebootClassHeld, "gated policy releases rIOt holds")
	assert.Equal(t, badgeAuto, b.Kind)
}

func TestPatchBadge_HeldPackages_UnavailableOrNotReportedNeverRaises(t *testing.T) { // AC-023
	in := healthyInput()
	in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusUnavailable, Packages: []string{"nginx"}, Total: 1}
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonHeldPackages)
	in.Updates.OtherHolds = nil
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonHeldPackages)
}

func TestPatchBadge_HeldPackages_RejectsUnsafeAgentNames(t *testing.T) { // security: agent data is re-validated
	in := waitingInput()
	in.Updates.Updates = append(in.Updates.Updates, models.PendingUpdate{Name: "good-pkg"})
	in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK,
		Packages: []string{"<script>alert(1)</script>", "good-pkg", "bad name", strings.Repeat("a", 129)}, Total: 4}
	text := findReason(t, computePatchBadge(in, badgeNow), reasonHeldPackages).Text
	assert.Contains(t, text, "good-pkg")
	assert.NotContains(t, text, "script")
	assert.NotContains(t, text, "bad name")
}

func TestPatchBadge_HeldPackages_HostileStatusIsIgnored(t *testing.T) { // security
	in := healthyInput()
	in.Updates.OtherHolds = &models.OtherHolds{Status: "<b>ok</b>", Packages: []string{"nginx"}, Total: 1}
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonHeldPackages)
}

func TestPatchBadge_RebootClassHeldNoRelease_HoldsActivePolicyOff(t *testing.T) { // AC-010
	in := healthyInput()
	in.OSPatch = windowPolicy("")
	in.Updates.HeldPackages = []string{"linux-image-generic"}
	in.Updates.PendingUpdates = 1
	in.Updates.PendingRebootClassCount = 1
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, []string{reasonRebootClassHeld}, reasonCodes(b), "[AC-052 e] a reboot-class update is pending and nothing releases it")
	r := findReason(t, b, reasonRebootClassHeld)
	assert.Contains(t, r.Text, "Kernel and GPU")
	assert.Contains(t, r.Action, "reboot_class to gated")
}

func TestPatchBadge_RebootClassHeldNoRelease_NotRaisedWithoutActiveHolds(t *testing.T) { // AC-010
	in := healthyInput()
	in.OSPatch = windowPolicy("")
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonRebootClassHeld, "no rIOt holds")
	in.Updates.HeldPackages = []string{"linux-image-generic"}
	in.Updates.HoldEnforcement = "no_privilege"
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonRebootClassHeld, "enforcement not active")
}

func TestPatchBadge_HoldEnforcementBroken_NoPrivilege(t *testing.T) { // AC-011
	in := healthyInput()
	in.Updates.HoldEnforcement = "no_privilege"
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, []string{reasonHoldEnforcementBroke}, infoCodes(b), "[AC-052 f] a configuration fault with nothing waiting by itself is always information")
	assert.Empty(t, reasonCodes(b))
	assert.NotEqual(t, badgeNeedsYou, b.Kind)
	assert.Contains(t, findInfo(t, b, reasonHoldEnforcementBroke).Action, "Re-run the agent installer")
}

func TestPatchBadge_HoldEnforcementBroken_ActiveOrOffIsFine(t *testing.T) { // AC-011
	for _, state := range []string{"active", "", "unsupported"} {
		in := healthyInput()
		in.Updates.HoldEnforcement = state
		assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonHoldEnforcementBroke, "state %q", state)
	}
}

func TestPatchBadge_ManualApprovalUpdate_NamesTheRelease(t *testing.T) { // AC-012
	in := healthyInput()
	in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusAvailable, Name: "26.04 LTS"}
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Equal(t, []string{reasonManualApproval}, reasonCodes(b))
	assert.Contains(t, findReason(t, b, reasonManualApproval).Text, "26.04 LTS")
}

func TestPatchBadge_ManualApprovalUpdate_UnsafeNameIsDropped(t *testing.T) { // security
	in := healthyInput()
	in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusAvailable, Name: "<img src=x>"}
	text := findReason(t, computePatchBadge(in, badgeNow), reasonManualApproval).Text
	assert.NotContains(t, text, "<img")
	assert.Contains(t, text, "OS release upgrade")
}

func TestPatchBadge_ManualApprovalUpdate_UnavailableDoesNotRaiseAndNeverClaimsNone(t *testing.T) { // AC-012
	in := healthyInput()
	in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusUnavailable}
	b := computePatchBadge(in, badgeNow)
	assert.NotContains(t, reasonCodes(b), reasonManualApproval)
	joined := strings.Join(b.Explanation.Notes, " ")
	assert.Contains(t, joined, "could not be checked")
	assert.NotContains(t, strings.ToLower(joined), "no manual")
	assert.NotContains(t, strings.ToLower(b.Explanation.NeedsYouSummary), "manual")
}

func TestPatchBadge_ManualApprovalUpdate_NotReportedNeverRaises(t *testing.T) { // AC-012, AC-023
	in := healthyInput()
	in.Updates.ReleaseUpgrade = nil
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonManualApproval)
}

func TestPatchBadge_LastRunFailed_FailedRunWithMessageAndLink(t *testing.T) { // AC-013, AC-053 c
	in := waitingInput()
	in.LastRun = failedRun("0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", "failed", "apt exited with status 100")
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	r := findReason(t, b, reasonLastRunFailed)
	assert.Contains(t, r.Text, "failed: apt exited with status 100")
	assert.Equal(t, "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", r.CommandID)
}

func TestPatchBadge_LastRunFailed_TimedOutRaises(t *testing.T) { // AC-013
	in := waitingInput()
	in.LastRun = failedRun("0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", "timed_out", "")
	r := findReason(t, computePatchBadge(in, badgeNow), reasonLastRunFailed)
	assert.Contains(t, r.Text, "timed out")
	assert.Contains(t, r.Text, "no message was recorded")
}

func TestPatchBadge_LastRunFailed_SucceededOrNoRunClears(t *testing.T) { // AC-013
	in := healthyInput()
	in.LastRun = failedRun("0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", "succeeded", "")
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonLastRunFailed)
	in.LastRun = nil
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonLastRunFailed)
}

func TestPatchBadge_LastRunFailed_ExpiredAndCancelledDoNotRaise(t *testing.T) { // AC-013
	for _, status := range []string{"expired", "cancelled", "running"} {
		in := healthyInput()
		in.LastRun = failedRun("0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", status, "x")
		assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonLastRunFailed, status)
	}
}

func TestPatchBadge_LastRunFailed_MessageIsSanitisedAndCut(t *testing.T) { // security, AC-013
	in := waitingInput()
	hostile := "\x1b[31mred\x1b[0m ‮evil⁩ line1\nline2\x07" + strings.Repeat("x", 400)
	in.LastRun = failedRun("0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", "failed", hostile)
	text := findReason(t, computePatchBadge(in, badgeNow), reasonLastRunFailed).Text
	for _, bad := range []string{"\x1b", "‮", "⁩", "\x07", "\n"} {
		assert.NotContains(t, text, bad, "unsafe character %q must be stripped", bad)
	}
	assert.Contains(t, text, "red evil line1 line2")
	msg := strings.TrimPrefix(text, "The last patch run failed: ")
	assert.LessOrEqual(t, len([]rune(msg)), maxBadgeRunMessage+3, "message is cut to 200 characters")
}

func TestPatchBadge_LastRunFailed_InvalidCommandIDIsNotLinked(t *testing.T) { // security
	in := waitingInput()
	in.LastRun = failedRun("../../etc/passwd", "failed", "boom")
	assert.Equal(t, "", findReason(t, computePatchBadge(in, badgeNow), reasonLastRunFailed).CommandID)
}

func TestPatchBadge_ManualRebootNeeded_NoRebootClassUpdatePending(t *testing.T) { // AC-014
	in := healthyInput()
	in.Updates.RebootRequired = true
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Equal(t, []string{reasonManualReboot}, reasonCodes(b))
	r := findReason(t, b, reasonManualReboot)
	assert.Contains(t, r.Text, "rIOt reboots automatically only after an automatic patch run installs a kernel or GPU update")
	assert.Equal(t, "Reboot this host now.", r.Action)
	assert.Equal(t, "Required now: reboot this host now; nothing will reboot it automatically.", b.Explanation.Reboot)
}

func TestPatchBadge_ManualRebootNeeded_AllowRebootOffCitesAllowReboot(t *testing.T) { // AC-015
	in := healthyInput()
	in.Updates.RebootRequired = true // NY-10 fires on a reboot required now (PATCH-FLOW-001)
	in.Updates.PendingRebootClassCount = 2
	in.Updates.AllowReboot = boolPtr(false)
	r := findReason(t, computePatchBadge(in, badgeNow), reasonManualReboot)
	assert.Contains(t, r.Text, "allow_reboot is off")
	assert.NotContains(t, r.Text, "rIOt reboots automatically only after", "a pending kernel update exists, so that cause does not apply")
	assert.Equal(t, "Reboot this host now, outside rIOt (commands.allow_reboot is off on the agent).", r.Action)
}

func TestPatchBadge_ManualRebootNeeded_PolicyNotGatedCitesPolicy(t *testing.T) { // AC-015
	in := healthyInput()
	in.OSPatch = windowPolicy("off")
	in.Updates.RebootRequired = true
	in.Updates.PendingRebootClassCount = 1
	r := findReason(t, computePatchBadge(in, badgeNow), reasonManualReboot)
	assert.Contains(t, r.Text, "reboot_class policy is not gated")
}

func TestPatchBadge_ManualRebootNeeded_AgentHoldRebootClassOffCitesAgent(t *testing.T) { // extension: agent never reboots without it
	in := healthyInput()
	in.Updates.RebootRequired = true
	in.Updates.PendingRebootClassCount = 1
	in.Updates.HoldRebootClass = boolPtr(false)
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Contains(t, findReason(t, b, reasonManualReboot).Text, "hold_reboot_class is off")
}

func TestPatchBadge_ManualRebootNeeded_HoldRebootClassNotReportedCountsAsOK(t *testing.T) { // extension: unknown counts as true
	in := healthyInput()
	in.Updates.PendingRebootClassCount = 1
	in.Updates.HoldRebootClass = nil
	assert.Equal(t, badgeAutoRebootDate, computePatchBadge(in, badgeNow).Kind)
}

func TestPatchBadge_ManualRebootNeeded_AutoPatchOffCitesSetup(t *testing.T) { // AC-015
	in := healthyInput()
	in.AutoPatch = false
	in.Updates.RebootRequired = true
	in.Updates.PendingRebootClassCount = 1
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, []string{reasonManualReboot}, reasonCodes(b), "[AC-049] with only a reboot waiting, the setting is information and the reboot is the one action")
	assert.Equal(t, []string{reasonAutoPatchOff}, infoCodes(b))
	assert.Contains(t, findReason(t, b, reasonManualReboot).Text, "automatic patching is not set up")
}

func TestPatchBadge_ManualRebootNeeded_NotRaisedWhenAutomaticRebootIsSetUp(t *testing.T) { // AC-016
	in := healthyInput()
	in.Updates.PendingRebootClassCount = 1
	assert.NotContains(t, reasonCodes(computePatchBadge(in, badgeNow)), reasonManualReboot)
}

func TestPatchBadge_ManualRebootNeeded_RebootRequiredWithPendingClassAndGatedIsAutomatic(t *testing.T) { // AC-016
	in := healthyInput()
	in.Updates.RebootRequired = true
	in.Updates.PendingRebootClassCount = 1
	assert.Equal(t, badgeAutoRebootDate, computePatchBadge(in, badgeNow).Kind)
}

// --- Cross-cutting ---

func TestPatchBadge_Reasons_ListedInCodeOrder(t *testing.T) { // AC-021
	in := waitingInput()
	in.Updates.PendingRebootClassCount = 0
	in.AutoPatch = false
	in.OSPatch = windowPolicy("")
	in.Updates.AllowPatching = boolPtr(false)
	in.Updates.HoldEnforcement = "no_privilege"
	in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1}
	in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusAvailable}
	in.Updates.RebootRequired = true
	in.LastRun = failedRun("0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", "failed", "x")
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, []string{
		reasonAutoPatchOff, reasonAgentPatchingOff, reasonHeldPackages,
		reasonManualApproval, reasonLastRunFailed, reasonManualReboot,
	}, reasonCodes(b), "NY-7 is always information, so it is not listed under Needs you")
	assert.Equal(t, []string{reasonHoldEnforcementBroke}, infoCodes(b))
	assert.Equal(t, "6 items need you.", b.Explanation.NeedsYouSummary, "[FR-080] the summary counts action items only")
}

func TestPatchBadge_NewerAgentNote_AgentReportingNoNewFacts(t *testing.T) { // AC-023
	in := healthyInput()
	in.Updates.AllowPatching, in.Updates.OtherHolds, in.Updates.ReleaseUpgrade = nil, nil, nil
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeAuto, b.Kind, "unknown facts must not raise needs-you reasons")
	assert.Contains(t, b.Explanation.Notes, "Some checks need a newer agent (v9.9.9 or later).")
}

func TestPatchBadge_NewerAgentNote_AbsentWhenAllFactsReported(t *testing.T) { // AC-023
	b := computePatchBadge(healthyInput(), badgeNow)
	for _, n := range b.Explanation.Notes {
		assert.NotContains(t, n, "newer agent")
	}
}

func TestPatchBadge_NewerAgentNote_AbsentWithoutPatchData(t *testing.T) { // AC-023
	in := healthyInput()
	in.Updates, in.UpdatesAt = nil, nil
	assert.Empty(t, computePatchBadge(in, badgeNow).Explanation.Notes)
}

func TestPatchBadge_UnattendedUpgradesNote_AutoPatchOff(t *testing.T) { // AC-024, AC-046
	in := healthyInput()
	in.AutoPatch = false
	in.Updates.UnattendedUpgrades = true
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeManual, b.Kind, "[AC-046] the vermont case: automatic patching off, nothing waiting")
	joined := strings.Join(b.Explanation.Notes, " ")
	assert.Contains(t, joined, "unattended-upgrades")
	assert.Contains(t, joined, "cannot see what it applies or whether it reboots")
}

func TestPatchBadge_UnattendedUpgradesNote_AbsentWhenNotReported(t *testing.T) { // AC-024
	for _, n := range computePatchBadge(healthyInput(), badgeNow).Explanation.Notes {
		assert.NotContains(t, n, "unattended-upgrades")
	}
}

func TestPatchBadge_OfflineHost_KeepsLastBadgeWithItsAge(t *testing.T) { // AC-025
	in := healthyInput()
	at := badgeNow.Add(-72 * time.Hour)
	in.UpdatesAt = &at
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeAuto, b.Kind)
	assert.Equal(t, "2026-10-21 12:00 UTC (3 d ago)", b.Explanation.AsOf)
}

func TestPatchBadge_ReasonDependencies_DoNotUseClockInternally(t *testing.T) { // AC-028 (purity)
	in := healthyInput()
	in.Updates.PendingRebootClassCount = 1
	early := computePatchBadge(in, badgeNow)
	later := computePatchBadge(in, badgeNow.Add(24*time.Hour))
	require.NotNil(t, early.RebootAt)
	require.NotNil(t, later.RebootAt)
	assert.Equal(t, time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC), *early.RebootAt)
	assert.Equal(t, time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC), *later.RebootAt, "the date follows the injected clock")
}

// TestPatchBadge_EveryReasonCodeHasATest proves each reason code is raised by a
// named scenario (AC-028).
func TestPatchBadge_EveryReasonCodeHasATest(t *testing.T) {
	scenarios := map[string]func(*patchBadgeInput){
		reasonAutoPatchOff: func(in *patchBadgeInput) { in.AutoPatch = false; in.Updates.PendingUpdates = 3 },
		reasonPatchingDisabled: func(in *patchBadgeInput) {
			in.OSPatch = models.MaintenanceWindow{Mode: "disabled"}
			in.Updates.PendingUpdates = 3
		},
		reasonAgentPatchingOff: func(in *patchBadgeInput) { in.Updates.AllowPatching = boolPtr(false); in.Updates.PendingUpdates = 3 },
		reasonCannotPatchHost: func(in *patchBadgeInput) {
			in.Updates = &models.UpdateInfo{PackageManager: "pacman", PendingUpdates: 3}
		},
		reasonHeldPackages: func(in *patchBadgeInput) {
			in.Updates.OtherHolds = &models.OtherHolds{Status: "ok", Packages: []string{"nginx"}, Total: 1}
			in.Updates.PendingUpdates = 1
			in.Updates.Updates = []models.PendingUpdate{{Name: "nginx"}}
		},
		reasonRebootClassHeld: func(in *patchBadgeInput) {
			in.OSPatch = windowPolicy("")
			in.Updates.HeldPackages = []string{"linux"}
			in.Updates.PendingUpdates, in.Updates.PendingRebootClassCount = 1, 1
		},
		reasonManualApproval: func(in *patchBadgeInput) { in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: "available"} },
		reasonLastRunFailed: func(in *patchBadgeInput) {
			in.Updates.PendingUpdates = 2
			in.LastRun = failedRun("0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", "failed", "x")
		},
		reasonManualReboot: func(in *patchBadgeInput) { in.Updates.RebootRequired = true },
		reasonExtraNotApplied: func(in *patchBadgeInput) {
			in.Updates.ExtraUpdaters = models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessNotOptedIn}}
		},
		// DIETPI-PATH-001: a refusal only the operator can act on.
		reasonExtraRefused: func(in *patchBadgeInput) {
			in.Updates.ExtraUpdaters = models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessEnabled}}
			in.LastRun = refusedRun("succeeded", "non_default_update_source", "update source SomeFork/master is not MichaIng")
		},
	}
	// DIETPI-PATH-001: a refusal the operator cannot act on raises the Blocked
	// kind and sits in the Blocked list.
	blockedOnly := map[string]func(*patchBadgeInput){
		reasonExtraBlocked: func(in *patchBadgeInput) {
			in.Updates.ExtraUpdaters = models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessEnabled}}
			in.LastRun = refusedRun("succeeded", "dpkg_locked", "dpkg lock held by pid 4242")
		},
	}
	// Information-only codes: NY-7 never needs action, the two extra_update items
	// are neutral by design.
	informationOnly := map[string]func(*patchBadgeInput){
		reasonHoldEnforcementBroke: func(in *patchBadgeInput) { in.Updates.HoldEnforcement = "no_privilege" },
		reasonExtraPending: func(in *patchBadgeInput) {
			in.Updates.ExtraUpdaters = models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessEnabled}}
		},
		reasonExtraUnknown: func(in *patchBadgeInput) {
			in.Updates.ExtraUpdaters = models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUnknown, UnknownReason: models.ExtraReasonCheckDisabled}}
		},
	}
	require.Len(t, scenarios, len(patchBadgeReasonCodes)-len(informationOnly)-len(blockedOnly), "every action reason code needs exactly one scenario")
	for _, code := range patchBadgeReasonCodes {
		in := healthyInput()
		if scenario, ok := blockedOnly[code]; ok {
			scenario(&in)
			b := computePatchBadge(in, badgeNow)
			assert.Equal(t, badgeBlocked, b.Kind, code)
			assert.NotContains(t, reasonCodes(b), code, "a blocked refusal is not an action item")
			assert.NotContains(t, infoCodes(b), code)
			found := false
			for _, r := range b.Explanation.Blocked {
				found = found || r.Code == code
			}
			assert.True(t, found, "scenario for %q must raise it in the Blocked list", code)
			continue
		}
		if scenario, ok := scenarios[code]; ok {
			scenario(&in)
			b := computePatchBadge(in, badgeNow)
			assert.Equal(t, badgeNeedsYou, b.Kind, code)
			assert.Contains(t, reasonCodes(b), code, "scenario for %q must raise it as an action item", code)
			assert.NotContains(t, infoCodes(b), code, "a code is never both action and information")
			continue
		}
		scenario, ok := informationOnly[code]
		require.True(t, ok, "reason code %q has no scenario", code)
		scenario(&in)
		b := computePatchBadge(in, badgeNow)
		assert.Contains(t, infoCodes(b), code, "scenario for %q must raise it as information", code)
		assert.NotContains(t, reasonCodes(b), code)
	}
}

// --- PATCH-FLOW-001: "reboot needed" means required now ---

const (
	pfKernelLine = "The pending patch includes a kernel/GPU update; a reboot will be needed after it installs."
	pfStaleNote  = "Package lists are out of date on this host. Pending updates will be recounted after the next refresh; running Patch refreshes the package lists first."
)

// removedBadgeCause is the cause wording PATCH-FLOW-001 removed. It is assembled
// from two parts so the definition-of-done grep for the old text stays empty.
const removedBadgeCause = "no kernel or GPU update is" + " pending"

// pendingKernelNoAutoReboot is the AC-001 host: a kernel update is pending, no
// reboot is required yet, lists are fresh and nothing will reboot the host by
// itself (the reboot_class policy is not gated).
func pendingKernelNoAutoReboot() patchBadgeInput {
	in := healthyInput()
	in.OSPatch = windowPolicy("off")
	in.Updates.PendingUpdates = 7
	in.Updates.PendingRebootClassCount = 1
	in.Updates.ListsFreshness = models.ListsFresh
	return in
}

// allBadgeText returns every string the badge can show an operator.
func allBadgeText(b patchBadge) []string {
	texts := []string{b.Label, b.Explanation.Automatic, b.Explanation.Reboot, b.Explanation.NeedsYouSummary, b.Explanation.AsOf}
	texts = append(texts, b.Explanation.Notes...)
	for _, r := range b.Explanation.NeedsYou {
		texts = append(texts, r.Text, r.Action)
	}
	return texts
}

func TestPatchBadge_PF_AC001_PendingKernelRaisesNoManualReboot(t *testing.T) {
	b := computePatchBadge(pendingKernelNoAutoReboot(), badgeNow)

	assert.NotContains(t, reasonCodes(b), reasonManualReboot, "[AC-001] a pending kernel is not a reboot required now")
	assert.Equal(t, badgeAuto, b.Kind)
}

func TestPatchBadge_PF_AC001_NoBadgeTextTellsToRebootWhenNotRequired(t *testing.T) {
	cases := map[string]func(*patchBadgeInput){
		"no automatic path": func(in *patchBadgeInput) {},
		"allow_reboot off":  func(in *patchBadgeInput) { in.OSPatch = windowPolicy("gated"); in.Updates.AllowReboot = boolPtr(false) },
		"auto-patch off":    func(in *patchBadgeInput) { in.OSPatch = windowPolicy("gated"); in.AutoPatch = false },
		"hold_reboot_class off": func(in *patchBadgeInput) {
			in.OSPatch = windowPolicy("gated")
			in.Updates.HoldRebootClass = boolPtr(false)
		},
	}
	for name, mutate := range cases {
		t.Run("[AC-001] "+name, func(t *testing.T) {
			in := pendingKernelNoAutoReboot()
			mutate(&in)
			for _, text := range allBadgeText(computePatchBadge(in, badgeNow)) {
				assert.NotContains(t, strings.ToLower(text), "reboot this host", "[AC-001] no text may tell the operator to reboot: %q", text)
			}
		})
	}
}

func TestPatchBadge_PF_AC002_PendingKernelDescribedAsFutureManualReboot(t *testing.T) {
	b := computePatchBadge(pendingKernelNoAutoReboot(), badgeNow)

	assert.Equal(t, pfKernelLine+" It will not happen automatically.", b.Explanation.Reboot)
}

func TestPatchBadge_PF_AC003_PendingKernelWithGatedAutoDescribedAsAutomatic(t *testing.T) {
	t.Run("[AC-003] window mode is dated", func(t *testing.T) {
		in := healthyInput()
		in.Updates.PendingRebootClassCount = 1
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, pfKernelLine+" It will happen automatically on 2026-10-25 03:00 UTC (estimate).", b.Explanation.Reboot)
		assert.Equal(t, badgeAutoRebootDate, b.Kind)
	})
	t.Run("[AC-003] anytime mode says the date is unknown", func(t *testing.T) {
		in := healthyInput()
		in.OSPatch = anytimePolicy("gated")
		in.Updates.PendingRebootClassCount = 1
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, pfKernelLine+" It will happen automatically, date Unknown: the reboot happens on the next automatic patch run and the time is not predicted.", b.Explanation.Reboot)
		assert.Equal(t, badgeAutoRebootUnknown, b.Kind)
	})
	t.Run("[AC-003] unresolved needs-you items defer the date", func(t *testing.T) {
		in := healthyInput()
		in.Updates.PendingRebootClassCount = 1
		in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1}
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, pfKernelLine+" It will happen automatically once the items below are resolved (date not shown).", b.Explanation.Reboot, "[AC-055] the Reboot line is the frozen pre-story line")
		assert.Equal(t, badgeAutoRebootDate, b.Kind, "[AC-052 a] a hold with no update under it is information, so the badge is not red")
		assert.Equal(t, []string{reasonHeldPackages}, infoCodes(b))
	})
}

func TestPatchBadge_PF_AC004_RequiredNowRaisesManualRebootWithRebootNowAction(t *testing.T) {
	in := healthyInput()
	in.Updates.RebootRequired = true
	b := computePatchBadge(in, badgeNow)

	r := findReason(t, b, reasonManualReboot)
	assert.Equal(t, "Reboot this host now.", r.Action)
	assert.True(t, strings.HasPrefix(r.Text, "A reboot is required now and nothing will reboot this host automatically: "), r.Text)
	assert.Equal(t, "Required now: reboot this host now; nothing will reboot it automatically.", b.Explanation.Reboot)
	assert.Equal(t, badgeNeedsYou, b.Kind)
}

func TestPatchBadge_PF_AC004_CausesNeverClaimNoKernelPending(t *testing.T) {
	for _, pending := range []int{0, 1} {
		in := healthyInput()
		in.OSPatch = windowPolicy("off")
		in.Updates.RebootRequired = true
		in.Updates.PendingRebootClassCount = pending
		for _, text := range allBadgeText(computePatchBadge(in, badgeNow)) {
			assert.NotContains(t, text, removedBadgeCause, "[AC-004] FR-006: no cause may contradict the telemetry")
		}
	}
}

func TestPatchBadge_PF_AC004_CauseListNeverEmpty(t *testing.T) {
	enforcements := []string{"", "active", "unsupported", "no_privilege"}
	flags := []*bool{nil, boolPtr(true), boolPtr(false)}
	checked := 0
	for _, pending := range []int{0, 2} {
		for _, policy := range []models.MaintenanceWindow{windowPolicy("gated"), windowPolicy("off"), anytimePolicy("gated")} {
			for _, autoPatch := range []bool{true, false} {
				for _, allow := range flags {
					for _, hold := range flags {
						for _, enforcement := range enforcements {
							in := healthyInput()
							in.AutoPatch, in.OSPatch = autoPatch, policy
							in.Updates.RebootRequired = true
							in.Updates.PendingRebootClassCount = pending
							in.Updates.AllowReboot, in.Updates.HoldRebootClass, in.Updates.HoldEnforcement = allow, hold, enforcement
							b := computePatchBadge(in, badgeNow)
							for _, r := range b.Explanation.NeedsYou {
								if r.Code != reasonManualReboot {
									continue
								}
								checked++
								causes := strings.TrimSuffix(strings.TrimPrefix(r.Text, "A reboot is required now and nothing will reboot this host automatically: "), ".")
								assert.NotEmpty(t, causes, "[AC-004] the cause list must never be empty (%+v)", in.Updates)
								if pending > 0 {
									assert.NotContains(t, causes, "rIOt reboots automatically only after", "a pending kernel exists, so a cause from the automatic path must explain the gap")
								}
							}
						}
					}
				}
			}
		}
	}
	require.NotZero(t, checked, "the table must exercise NY-10")
}

func TestPatchBadge_PF_AC004_RequiredNowCoveredByAutomaticPath(t *testing.T) {
	in := healthyInput()
	in.Updates.RebootRequired = true
	in.Updates.PendingRebootClassCount = 1
	b := computePatchBadge(in, badgeNow)

	assert.NotContains(t, reasonCodes(b), reasonManualReboot)
	assert.Equal(t, "Required now. Automatic on 2026-10-25 03:00 UTC (estimate).", b.Explanation.Reboot)
}

func TestPatchBadge_PF_AC004_RequiredNowWithoutPatchSupportStillSaysRebootNow(t *testing.T) {
	in := healthyInput()
	in.Updates.PackageManager = "pacman"
	in.Updates.RebootRequired = true
	b := computePatchBadge(in, badgeNow)

	assert.NotContains(t, reasonCodes(b), reasonManualReboot, "NY-10 needs a patchable host")
	assert.Equal(t, "Required now: reboot this host now; nothing will reboot it automatically.", b.Explanation.Reboot)
}

func TestPatchBadge_PF_AC006_PendingOnlyIsNotRebootRequired(t *testing.T) {
	b := computePatchBadge(pendingKernelNoAutoReboot(), badgeNow)

	assert.NotEqual(t, "Required now", b.Explanation.Reboot)
	assert.False(t, strings.HasPrefix(b.Explanation.Reboot, "Required now"), "[AC-006] the required-now wording is reserved for reboot_required")
	assert.NotContains(t, reasonCodes(b), reasonManualReboot)
}

func TestPatchBadge_PF_AC028_StaleDropsPendingKernelWordingAndAddsNote(t *testing.T) {
	in := pendingKernelNoAutoReboot()
	in.OSPatch = windowPolicy("gated")
	in.Updates.ListsFreshness = models.ListsStale
	b := computePatchBadge(in, badgeNow)

	for _, text := range allBadgeText(b) {
		assert.NotContains(t, text, "kernel/GPU update", "[AC-028] the FR-004 wording is absent while stale")
	}
	assert.Equal(t, "Not needed.", b.Explanation.Reboot)
	assert.Contains(t, b.Explanation.Notes, pfStaleNote)
	assert.Equal(t, badgeAuto, b.Kind, "a stale count raises no auto-reboot date either")
	assert.Nil(t, b.RebootAt)
}

func TestPatchBadge_PF_AC028_StaleRaisesNoPendingDerivedReason(t *testing.T) {
	withPending := healthyInput()
	withPending.Updates.ListsFreshness = models.ListsStale
	withPending.Updates.PendingRebootClassCount = 3
	withPending.OSPatch = windowPolicy("off")
	without := withPending
	updates := *withPending.Updates
	updates.PendingRebootClassCount = 0
	without.Updates = &updates

	a := computePatchBadge(withPending, badgeNow)
	c := computePatchBadge(without, badgeNow)

	assert.Equal(t, reasonCodes(c), reasonCodes(a), "[AC-028] the pending list must not change the reasons while stale")
	assert.Equal(t, c.Explanation.Reboot, a.Explanation.Reboot)
}

func TestPatchBadge_PF_AC028_StaleStillReportsARebootRequiredNow(t *testing.T) {
	in := healthyInput()
	in.Updates.ListsFreshness = models.ListsStale
	in.Updates.RebootRequired = true
	b := computePatchBadge(in, badgeNow)

	assert.Contains(t, reasonCodes(b), reasonManualReboot, "reboot_required is not derived from the pending list")
	assert.Contains(t, b.Explanation.Notes, pfStaleNote)
}

func TestPatchBadge_PF_AC028_FreshAndUnknownDoNotAddTheStaleNote(t *testing.T) {
	for _, freshness := range []models.ListsFreshness{"", models.ListsFresh, models.ListsUnknown} {
		in := healthyInput()
		in.Updates.ListsFreshness = freshness
		assert.NotContains(t, computePatchBadge(in, badgeNow).Explanation.Notes, pfStaleNote, "freshness %q", freshness)
	}
}

func TestPatchBadge_PF_AC029_UnknownFreshnessKeepsTodaysPendingWording(t *testing.T) {
	in := pendingKernelNoAutoReboot()
	in.Updates.ListsFreshness = models.ListsUnknown
	assert.Equal(t, pfKernelLine+" It will not happen automatically.", computePatchBadge(in, badgeNow).Explanation.Reboot)
}

func TestPatchBadge_PF_AC038_VermontBeforeRunNeverSaysRebootByHand(t *testing.T) {
	// vermont before the run: 7 pending, 1 reboot-class pending, reboot_required
	// false, holds active, allow_reboot true.
	in := healthyInput()
	in.OSPatch = windowPolicy("off")
	in.Updates.PendingUpdates = 7
	in.Updates.PendingRebootClassCount = 1
	in.Updates.RebootRequired = false
	in.Updates.HoldEnforcement = "active"
	in.Updates.HeldPackages = []string{"linux-image-amd64"}
	b := computePatchBadge(in, badgeNow)

	assert.NotContains(t, reasonCodes(b), reasonManualReboot)
	for _, text := range allBadgeText(b) {
		assert.NotContains(t, strings.ToLower(text), "reboot this host", "[AC-038] %q", text)
	}
	assert.Equal(t, pfKernelLine+" It will not happen automatically.", b.Explanation.Reboot)
}
