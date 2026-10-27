package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// EXTRA-UPD-001 group K (AC-046 to AC-055) and the extra-updater badge items
// (AC-038). The cases are small builders so AC-055 can run them all.

func dietPi(status models.ExtraUpdaterStatus, readiness string) models.ExtraUpdaterList {
	return models.ExtraUpdaterList{{
		ID: "dietpi", Status: status, Readiness: readiness,
		InstalledVersion: "9.20.1", AvailableVersion: "10.8.2", PendingCount: 1,
	}}
}

// vermont is the AC-046 host: apt, automatic patching off, nothing waiting.
func vermont() patchBadgeInput {
	in := healthyInput()
	in.AutoPatch = false
	in.Updates.UnattendedUpgrades = true
	return in
}

func hasCode(list []patchBadgeReason, code string) bool {
	for _, r := range list {
		if r.Code == code {
			return true
		}
	}
	return false
}

func TestPatchBadge_EU_AC046_VermontIsManualNotNeedsYou(t *testing.T) {
	b := computePatchBadge(vermont(), badgeNow)
	assert.Equal(t, badgeManual, b.Kind, "[AC-046] kind")
	assert.Equal(t, "Manual", b.Label, "[AC-046] label")
	assert.Empty(t, b.Explanation.NeedsYou, "[AC-046] no action items")
	assert.Equal(t, "Nothing.", b.Explanation.NeedsYouSummary, "[AC-046] summary counts action items only")
	info := findInfo(t, b, reasonAutoPatchOff)
	assert.Equal(t, "Automatic patching is off for this host.", info.Text, "[AC-046] the reason keeps its existing text")
	assert.Equal(t, "Turn on Auto-patch for this host.", info.Action)
	assert.Contains(t, strings.Join(b.Explanation.Notes, " "), "unattended-upgrades", "[AC-046] the note stays")
	assert.Equal(t, "No.", b.Explanation.Automatic)
}

func TestPatchBadge_EU_AC047_AutoPatchOffWithAptPendingNeedsYou(t *testing.T) {
	in := vermont()
	in.Updates.PendingUpdates = 7
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Contains(t, reasonCodes(b), reasonAutoPatchOff, "[AC-047] action item")
	assert.NotContains(t, infoCodes(b), reasonAutoPatchOff, "[AC-047] not also information")
	assert.Equal(t, "1 item needs you.", b.Explanation.NeedsYouSummary)
}

func TestPatchBadge_EU_AC048_OnlyDietPiPending(t *testing.T) {
	t.Run("[AC-048a] auto-patch off, DietPi enabled", func(t *testing.T) {
		in := vermont()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, reasonCodes(b), reasonAutoPatchOff)
	})
	t.Run("[AC-048b] set up, not opted in", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNotOptedIn)
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		r := findReason(t, b, reasonExtraNotApplied)
		assert.Equal(t, "dietpi", r.Updater)
		assert.Contains(t, r.Text, "DietPi 9.20.1 to 10.8.2", "[AC-048b] names the updater and both versions")
		assert.Contains(t, r.Text, "not opted in")
		assert.Contains(t, r.Action, "commands.allow_extra_updaters")
		assert.Contains(t, r.Action, "re-run install.sh as root", "[AC-048b] says what Ryan must do")
	})
	t.Run("[AC-048c] set up, enabled", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Empty(t, b.Explanation.NeedsYou, "[AC-048c] no action item")
	})
}

func TestPatchBadge_EU_AC049_RebootRequiredNothingReboots(t *testing.T) {
	in := vermont()
	in.OSPatch = windowPolicy("off")
	in.Updates.AllowReboot = boolPtr(false)
	in.Updates.RebootRequired = true
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Equal(t, []string{reasonManualReboot}, reasonCodes(b), "[AC-049] only the reboot item needs action")
	assert.Contains(t, infoCodes(b), reasonAutoPatchOff, "[AC-049] the setting is information")
	r := findReason(t, b, reasonManualReboot)
	assert.True(t, strings.HasPrefix(r.Text, "A reboot is required now and nothing will reboot this host automatically: "), "[AC-049] PATCH-FLOW-001 text unchanged")
	assert.Equal(t, manualRebootAction(boolPtr(false)), r.Action)
}

func TestPatchBadge_EU_AC051_SettingsAloneAreInformation(t *testing.T) {
	t.Run("[AC-051] server-wide setting unusable", func(t *testing.T) {
		in := healthyInput()
		in.OSPatch = models.MaintenanceWindow{Mode: "disabled"}
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, infoCodes(b), reasonPatchingDisabled)
		assert.Empty(t, b.Explanation.NeedsYou)
	})
	t.Run("[AC-051] agent refuses patching", func(t *testing.T) {
		in := healthyInput()
		in.Updates.AllowPatching = boolPtr(false)
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, infoCodes(b), reasonAgentPatchingOff)
		assert.Empty(t, b.Explanation.NeedsYou)
	})
}

func TestPatchBadge_EU_AC052_HoldsAreActionOnlyWithWaitingWork(t *testing.T) {
	held := func() patchBadgeInput {
		in := healthyInput()
		in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1}
		return in
	}
	t.Run("[AC-052a] held, none with an update", func(t *testing.T) {
		b := computePatchBadge(held(), badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, infoCodes(b), reasonHeldPackages)
	})
	t.Run("[AC-052b] telemetry cannot tell", func(t *testing.T) {
		in := held()
		in.Updates.ListsFreshness = models.ListsStale
		in.Updates.PendingUpdates = 3
		in.Updates.Updates = []models.PendingUpdate{{Name: "nginx"}}
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind, "stale lists cannot show a newer version")
		assert.Contains(t, infoCodes(b), reasonHeldPackages)
	})
	t.Run("[AC-052c] held package with a newer version", func(t *testing.T) {
		in := held()
		in.Updates.PendingUpdates = 1
		in.Updates.Updates = []models.PendingUpdate{{Name: "nginx"}}
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, reasonCodes(b), reasonHeldPackages)
	})
	rcHeld := func() patchBadgeInput {
		in := healthyInput()
		in.OSPatch = windowPolicy("off")
		in.Updates.HeldPackages = []string{"linux-image"}
		return in
	}
	t.Run("[AC-052d] reboot-class holds, nothing pending", func(t *testing.T) {
		b := computePatchBadge(rcHeld(), badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, infoCodes(b), reasonRebootClassHeld)
	})
	t.Run("[AC-052e] reboot-class holds with one pending, fresh lists", func(t *testing.T) {
		in := rcHeld()
		in.Updates.PendingRebootClassCount = 1
		in.Updates.PendingUpdates = 1
		in.Updates.ListsFreshness = models.ListsFresh
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, reasonCodes(b), reasonRebootClassHeld)
	})
	t.Run("[AC-052f] hold enforcement broken is always information", func(t *testing.T) {
		in := healthyInput()
		in.Updates.HoldEnforcement = "no_privilege"
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, infoCodes(b), reasonHoldEnforcementBroke)
	})
}

func TestPatchBadge_EU_AC053_ReleaseFailedRunAndUnsupportedHost(t *testing.T) {
	runID := "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c"
	t.Run("[AC-053a] release upgrade available", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusAvailable}
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, reasonCodes(b), reasonManualApproval)
	})
	t.Run("[AC-053b] failed run, nothing waiting", func(t *testing.T) {
		in := healthyInput()
		in.LastRun = failedRun(runID, "failed", "disk full")
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		r := findInfo(t, b, reasonLastRunFailed)
		assert.Contains(t, r.Text, "Nothing is pending now.")
		assert.Equal(t, runID, r.CommandID, "[AC-053b] keeps its command link")
		assert.NotContains(t, reasonCodes(b), reasonLastRunFailed)
	})
	t.Run("[AC-053c] failed run, two updates pending", func(t *testing.T) {
		in := healthyInput()
		in.Updates.PendingUpdates = 2
		in.LastRun = failedRun(runID, "failed", "disk full")
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		r := findReason(t, b, reasonLastRunFailed)
		assert.NotContains(t, r.Text, "Nothing is pending now.")
		assert.Equal(t, runID, r.CommandID)
	})
	t.Run("[AC-053d] no patch data", func(t *testing.T) {
		in := healthyInput()
		in.Updates = nil
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, infoCodes(b), reasonCannotPatchHost)
	})
	t.Run("[AC-053e] unsupported package manager with 4 pending", func(t *testing.T) {
		in := healthyInput()
		in.Updates = &models.UpdateInfo{PackageManager: "pacman", PendingUpdates: 4, ListsFreshness: models.ListsFresh}
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		assert.Contains(t, reasonCodes(b), reasonCannotPatchHost)
	})
}

func TestPatchBadge_EU_AC054_StaleListsAreNotWaitingWork(t *testing.T) {
	in := vermont()
	in.Updates.ListsFreshness = models.ListsStale
	in.Updates.PendingUpdates = 38
	b := computePatchBadge(in, badgeNow)
	assert.NotEqual(t, badgeNeedsYou, b.Kind)
	assert.Contains(t, infoCodes(b), reasonAutoPatchOff)
	assert.Contains(t, b.Explanation.Notes, staleListsNote)
}

func TestPatchBadge_EU_AC038_ExtraItems(t *testing.T) {
	t.Run("[AC-038] enabled under a set-up window policy says it is applied automatically", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
		r := findInfo(t, computePatchBadge(in, badgeNow), reasonExtraPending)
		assert.Equal(t, "DietPi 9.20.1 to 10.8.2 is waiting; it will be applied automatically in the patch window.", r.Text)
		assert.Equal(t, "dietpi", r.Updater)
	})
	t.Run("[AC-038] enabled under an anytime policy", func(t *testing.T) {
		in := healthyInput()
		in.OSPatch = anytimePolicy("gated")
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
		r := findInfo(t, computePatchBadge(in, badgeNow), reasonExtraPending)
		assert.Contains(t, r.Text, "automatically at any time")
	})
	t.Run("[AC-038] enabled but automatic patching not set up says a manual Patch", func(t *testing.T) {
		in := vermont()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
		b := computePatchBadge(in, badgeNow)
		r := findInfo(t, b, reasonExtraPending)
		assert.Contains(t, r.Text, "only by a manual Patch")
		assert.Contains(t, reasonCodes(b), reasonAutoPatchOff)
	})
	t.Run("[AC-038] not opted in shows both versions and the reason in words", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNotOptedIn)
		r := findReason(t, computePatchBadge(in, badgeNow), reasonExtraNotApplied)
		assert.Equal(t, "DietPi 9.20.1 to 10.8.2 is waiting; not applied: not opted in.", r.Text)
	})
	for readiness, want := range map[string]string{
		models.ExtraReadinessRootSideMissing:        "the root-side opt-in is missing",
		models.ExtraReadinessPatchingDisabled:       "patching is turned off on the agent",
		models.ExtraReadinessUnsupportedHelper:      "the installed maintenance helper is too old",
		models.ExtraReadinessNonDefaultUpdateSource: "its update source is not the default repository",
		"something_new": "its apply readiness is unknown",
	} {
		readiness, want := readiness, want
		t.Run("[AC-038] readiness "+readiness, func(t *testing.T) {
			in := healthyInput()
			in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, readiness)
			r := findReason(t, computePatchBadge(in, badgeNow), reasonExtraNotApplied)
			assert.Contains(t, r.Text, "not applied: "+want+".")
			assert.NotEmpty(t, r.Action)
		})
	}
	t.Run("[AC-038] unknown with check_disabled is information, never up to date", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUnknown, UnknownReason: models.ExtraReasonCheckDisabled, Readiness: models.ExtraReadinessEnabled}}
		b := computePatchBadge(in, badgeNow)
		assert.NotEqual(t, badgeNeedsYou, b.Kind)
		assert.Empty(t, b.Explanation.NeedsYou, "no action item for DietPi")
		r := findInfo(t, b, reasonExtraUnknown)
		assert.Contains(t, r.Text, "its own update check is turned off")
		assert.NotContains(t, strings.ToLower(r.Text), "up to date")
		assert.NotContains(t, strings.ToLower(r.Text), "up-to-date")
	})
	t.Run("[AC-038] an up to date updater raises nothing", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpToDate, models.ExtraReadinessEnabled)
		b := computePatchBadge(in, badgeNow)
		assert.Empty(t, b.Explanation.Info)
		assert.Empty(t, b.Explanation.NeedsYou)
	})
	t.Run("[AC-038] hostile version text is stripped before it reaches the page", func(t *testing.T) {
		in := healthyInput()
		list := dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNotOptedIn)
		list[0].AvailableVersion = "10.8.2\x1b[31m<script>" + strings.Repeat("9", 100)
		in.Updates.ExtraUpdaters = list
		r := findReason(t, computePatchBadge(in, badgeNow), reasonExtraNotApplied)
		assert.NotContains(t, r.Text, "\x1b")
		assert.LessOrEqual(t, len(r.Text), 400, "bounded text")
	})
	t.Run("[AC-038] at most one item per updater and at most 8 entries", func(t *testing.T) {
		in := healthyInput()
		var list models.ExtraUpdaterList
		for i := 0; i < 12; i++ {
			list = append(list, models.ExtraUpdater{ID: "u" + string(rune('a'+i)), Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessNotOptedIn})
		}
		in.Updates.ExtraUpdaters = list
		b := computePatchBadge(in, badgeNow)
		assert.Len(t, b.Explanation.NeedsYou, models.MaxExtraUpdaters)
	})
}

func TestPatchBadge_EU_SEC003_SourceActionItem(t *testing.T) {
	in := healthyInput()
	in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNonDefaultUpdateSource)
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind, "[SEC-003] a pending update on a non-default source is waiting work nobody will apply")
	r := findReason(t, b, reasonExtraNotApplied)
	assert.Contains(t, r.Text, "its update source is not the default repository")
	assert.Contains(t, r.Action, "DEV_GITOWNER")
}

// groupKCases are the inputs of AC-046 to AC-054, plus the variations that
// raise the most items.
func groupKCases() map[string]patchBadgeInput {
	runID := "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c"
	cases := map[string]patchBadgeInput{}
	add := func(name string, mutate func(*patchBadgeInput)) {
		in := healthyInput()
		mutate(&in)
		cases[name] = in
	}
	add("healthy", func(in *patchBadgeInput) {})
	add("vermont", func(in *patchBadgeInput) { *in = vermont() })
	add("vermont-7-pending", func(in *patchBadgeInput) { *in = vermont(); in.Updates.PendingUpdates = 7 })
	add("vermont-stale", func(in *patchBadgeInput) {
		*in = vermont()
		in.Updates.ListsFreshness = models.ListsStale
		in.Updates.PendingUpdates = 38
	})
	add("vermont-reboot", func(in *patchBadgeInput) {
		*in = vermont()
		in.OSPatch = windowPolicy("off")
		in.Updates.AllowReboot = boolPtr(false)
		in.Updates.RebootRequired = true
	})
	add("dietpi-a", func(in *patchBadgeInput) {
		*in = vermont()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
	})
	add("dietpi-b", func(in *patchBadgeInput) {
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNotOptedIn)
	})
	add("dietpi-c", func(in *patchBadgeInput) {
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
	})
	add("dietpi-unknown", func(in *patchBadgeInput) {
		in.Updates.ExtraUpdaters = models.ExtraUpdaterList{{ID: "dietpi", Status: models.ExtraStatusUnknown, UnknownReason: models.ExtraReasonCheckDisabled}}
	})
	add("disabled-server", func(in *patchBadgeInput) { in.OSPatch = models.MaintenanceWindow{Mode: "disabled"} })
	add("agent-refuses", func(in *patchBadgeInput) { in.Updates.AllowPatching = boolPtr(false) })
	add("held-none", func(in *patchBadgeInput) {
		in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1}
	})
	add("held-update", func(in *patchBadgeInput) {
		in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1}
		in.Updates.PendingUpdates = 1
		in.Updates.Updates = []models.PendingUpdate{{Name: "nginx"}}
	})
	add("rc-held-none", func(in *patchBadgeInput) {
		in.OSPatch = windowPolicy("off")
		in.Updates.HeldPackages = []string{"linux-image"}
	})
	add("rc-held-one", func(in *patchBadgeInput) {
		in.OSPatch = windowPolicy("off")
		in.Updates.HeldPackages = []string{"linux-image"}
		in.Updates.PendingRebootClassCount, in.Updates.PendingUpdates = 1, 1
		in.Updates.ListsFreshness = models.ListsFresh
	})
	add("enforcement-broken", func(in *patchBadgeInput) { in.Updates.HoldEnforcement = "no_privilege" })
	add("release", func(in *patchBadgeInput) {
		in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusAvailable}
	})
	add("failed-nothing", func(in *patchBadgeInput) { in.LastRun = failedRun(runID, "failed", "x") })
	add("failed-pending", func(in *patchBadgeInput) { in.Updates.PendingUpdates = 2; in.LastRun = failedRun(runID, "failed", "x") })
	add("no-data", func(in *patchBadgeInput) { in.Updates = nil })
	add("unsupported", func(in *patchBadgeInput) {
		in.Updates = &models.UpdateInfo{PackageManager: "pacman", PendingUpdates: 4, ListsFreshness: models.ListsFresh}
	})
	add("everything", func(in *patchBadgeInput) {
		*in = vermont()
		in.OSPatch = windowPolicy("off")
		in.Updates.AllowPatching = boolPtr(false)
		in.Updates.PendingUpdates = 3
		in.Updates.Updates = []models.PendingUpdate{{Name: "nginx"}}
		in.Updates.OtherHolds = &models.OtherHolds{Status: models.FactStatusOK, Packages: []string{"nginx"}, Total: 1}
		in.Updates.HeldPackages = []string{"linux-image"}
		in.Updates.PendingRebootClassCount = 1
		in.Updates.HoldEnforcement = "active"
		in.Updates.RebootRequired = true
		in.Updates.ReleaseUpgrade = &models.ReleaseUpgrade{Status: models.FactStatusAvailable}
		in.LastRun = failedRun(runID, "timed_out", "x")
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNotOptedIn)
	})
	return cases
}

// frozenPreStoryLines is the Automatic, Reboot and As-of lines as they were
// before this story, with the same reason count the old function used: the ten
// NY-1 to NY-10 conditions, whatever their class (FR-084).
func frozenPreStoryLines(in patchBadgeInput, now time.Time) (automatic, reboot, asOf string) {
	now = now.UTC()
	u := in.Updates
	mode := in.OSPatch.Mode
	nextStart, haveStart := nextMaintenanceWindowStart(in.OSPatch, now)
	var allowReboot, allowPatching *bool
	if u != nil {
		allowReboot, allowPatching = u.AllowReboot, u.AllowPatching
	}
	gated := in.OSPatch.RebootClass == "gated"
	modeUsable := mode == "anytime" || (mode == "window" && haveStart)
	agentRefuses := allowPatching != nil && !*allowPatching
	canPatch := u != nil && (u.PackageManager == "apt" || u.PackageManager == "dnf")
	autoPatchSetUp := in.AutoPatch && modeUsable && !agentRefuses && canPatch
	requiredNow := u != nil && u.RebootRequired
	stale := u != nil && u.Freshness() == string(models.ListsStale)
	pendingRC := u != nil && !stale && u.PendingRebootClassCount > 0
	autoRebootSetUp := autoPatchSetUp && gated && pendingRC && (allowReboot == nil || *allowReboot) && agentCanReleaseRebootClass(u)
	autoCovers := (requiredNow || pendingRC) && autoRebootSetUp

	n := 0
	for _, raised := range []bool{!in.AutoPatch, !modeUsable, agentRefuses, !canPatch} {
		if raised {
			n++
		}
	}
	if canPatch {
		if _, total := otherHolds(u); total > 0 {
			n++
		}
		if u.HoldEnforcement == "active" && len(u.HeldPackages) > 0 && !gated {
			n++
		}
		if u.HoldEnforcement == "no_privilege" {
			n++
		}
		if ru := u.ReleaseUpgrade; ru != nil && ru.Status == models.FactStatusAvailable {
			n++
		}
	}
	if r := in.LastRun; r != nil && (r.Status == string(models.CommandFailed) || r.Status == string(models.CommandTimedOut)) {
		n++
	}
	if canPatch && requiredNow && !autoCovers {
		n++
	}
	dated := mode == "window" && allowReboot != nil && *allowReboot
	reboot = rebootLine(rebootFacts{requiredNow: requiredNow, pendingRC: pendingRC, autoCovers: autoCovers, reasons: n, dated: dated, at: nextStart, why: unknownDateReasons(mode, allowReboot)})
	switch {
	case !autoPatchSetUp:
		automatic = "No."
	case mode == "window":
		automatic = "Yes. Next window starts " + models.FormatDisplayMinute(nextStart, nil) + "."
	default:
		automatic = "Yes. Any time (within the " + itoa(in.OSPatch.CooldownMinutes) + "-minute cooldown)."
	}
	return automatic, reboot, asOfText(in.UpdatesAt, now, nil)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestPatchBadge_EU_AC055_EachCodeOnceAndOtherLinesUnchanged(t *testing.T) {
	for name, in := range groupKCases() {
		name, in := name, in
		t.Run("[AC-055] "+name, func(t *testing.T) {
			b := computePatchBadge(in, badgeNow)

			seen := map[string]int{}
			for _, r := range b.Explanation.NeedsYou {
				seen[r.Code+"/"+r.Updater]++
			}
			for _, r := range b.Explanation.Info {
				seen[r.Code+"/"+r.Updater]++
			}
			for key, count := range seen {
				assert.Equal(t, 1, count, "reason %q appears %d times across action and information items", key, count)
			}
			assert.Equal(t, len(b.Explanation.NeedsYou) > 0, b.Kind == badgeNeedsYou, "kind needs_you if, and only if, an action item exists (FR-069)")
			if b.Kind == badgeManual {
				assert.False(t, strings.HasPrefix(b.Explanation.Automatic, "Yes"), "Manual is never shown beside Automatic: Yes")
			}

			auto, reboot, asOf := frozenPreStoryLines(in, badgeNow)
			assert.Equal(t, auto, b.Explanation.Automatic, "Automatic line must equal the pre-story value")
			assert.Equal(t, reboot, b.Explanation.Reboot, "Reboot line must equal the pre-story value")
			assert.Equal(t, asOf, b.Explanation.AsOf, "As-of line must equal the pre-story value")
		})
	}
}

func TestPatchBadge_EU_AC055_DisplayOnlyNeverTouchesDispatch(t *testing.T) {
	// The badge function is pure: same input, same output, and it takes no
	// repository, so it cannot dispatch (FR-084).
	for name, in := range groupKCases() {
		a := computePatchBadge(in, badgeNow)
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, a, b, "[AC-055] %s: same input, same badge", name)
	}
}

// --- Fleet patch-status rows (AC-038 panel) ---

type extraRow struct {
	DeviceID       string                `json:"device_id"`
	PendingUpdates int                   `json:"pending_updates"`
	ExtraUpdaters  []models.ExtraUpdater `json:"extra_updaters"`
	PatchBadge     *patchBadge           `json:"patch_badge"`
}

func (f *badgeFixture) extraRows(t *testing.T, query string) []extraRow {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/fleet/patch-status"+query, nil)
	rec := httptest.NewRecorder()
	f.h.PatchStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []extraRow
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	return rows
}

func TestPatchStatus_EU_AC038_PanelListsDietPi(t *testing.T) {
	f := newBadgeFixture(t, true)
	setExtra := func(id string, list models.ExtraUpdaterList) {
		u := f.tel.Snapshots[id][0].Data.Updates
		u.ExtraUpdaters = list
	}
	// dev-patched: only DietPi pending and enabled under the set-up policy.
	setExtra("dev-patched", dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled))
	// dev-nodata gets telemetry with only DietPi pending and not opted in.
	not := &models.UpdateInfo{PackageManager: "apt", AllowReboot: boolPtr(true), AllowPatching: boolPtr(true), HoldRebootClass: boolPtr(true),
		OtherHolds: &models.OtherHolds{Status: "ok"}, ReleaseUpgrade: &models.ReleaseUpgrade{Status: "none"},
		ExtraUpdaters: dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNotOptedIn)}
	f.tel.Snapshots["dev-nodata"] = []models.TelemetrySnapshot{{DeviceID: "dev-nodata", Timestamp: badgeNow.Add(-time.Hour), Data: models.FullTelemetryData{Updates: not}}}

	rows := f.extraRows(t, "")
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, r.DeviceID)
	}
	assert.Contains(t, ids, "dev-patched", "[AC-038] a host with only DietPi pending and enabled is listed")
	assert.Contains(t, ids, "dev-nodata", "[AC-038] a host with only DietPi pending and not opted in is listed")
	for _, r := range rows {
		if r.DeviceID != "dev-patched" && r.DeviceID != "dev-nodata" {
			continue
		}
		require.Len(t, r.ExtraUpdaters, 1, r.DeviceID)
		assert.Equal(t, "10.8.2", r.ExtraUpdaters[0].AvailableVersion, "the panel names DietPi 10.8.2")
		assert.Equal(t, 0, r.PendingUpdates)
	}
	for _, r := range rows {
		if r.DeviceID == "dev-nodata" {
			assert.Equal(t, badgeNeedsYou, r.PatchBadge.Kind)
		}
		if r.DeviceID == "dev-patched" {
			assert.NotEqual(t, badgeNeedsYou, r.PatchBadge.Kind)
		}
	}
}

func TestPatchStatus_EU_AC038_UnknownAndUpToDateAreNotListedOrCarried(t *testing.T) {
	f := newBadgeFixture(t, true)
	f.tel.Snapshots["dev-patched"][0].Data.Updates.ExtraUpdaters = models.ExtraUpdaterList{
		{ID: "dietpi", Status: models.ExtraStatusUnknown, UnknownReason: models.ExtraReasonCheckDisabled},
	}
	rows := f.extraRows(t, "")
	for _, r := range rows {
		assert.NotEqual(t, "dev-patched", r.DeviceID, "[AC-038] an unknown status is not waiting work, so no panel row")
	}
	all := f.extraRows(t, "?all=true")
	for _, r := range all {
		assert.Empty(t, r.ExtraUpdaters, "only pending entries are carried on a row: %s", r.DeviceID)
	}
}
