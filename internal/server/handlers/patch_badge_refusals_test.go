package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DIETPI-PATH-001 FR-015 to FR-017: the badge reports a refused updater, with
// the Blocked and Action classes of DP-SEC-001.

const refusalSuccessLine2 = "Package state verified; maintenance completed"

// refusedRun is the latest finished os_update of a host whose apt part
// succeeded and whose DietPi step was refused, as a worker stores it.
func refusedRun(status, reason, detail string) *models.Command {
	msg := "apt: succeeded; dietpi: skipped (" + reason + ")\n" + refusalSuccessLine2 + "\ndietpi: skipped (" + reason + ")"
	if detail != "" {
		msg += "; " + detail
	}
	return &models.Command{ID: "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", Action: models.ActionOSUpdate, Status: status, ResultMsg: msg}
}

func pendingEnabledDietPi() models.ExtraUpdaterList {
	return dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessEnabled)
}

func blockedCodes(b patchBadge) []string {
	var out []string
	for _, r := range b.Explanation.Blocked {
		out = append(out, r.Code)
	}
	return out
}

func TestPatchBadge_DP_AC012_RefusalIsBlocked(t *testing.T) {
	in := healthyInput()
	in.Updates.ExtraUpdaters = pendingEnabledDietPi()
	in.LastRun = refusedRun("succeeded", "unsafe_dietpi_path", "/run/dietpi is writable by group or others (mode 0777)")
	b := computePatchBadge(in, badgeNow)

	t.Run("[AC-012] the badge kind is blocked, not needs_you", func(t *testing.T) {
		assert.Equal(t, badgeBlocked, b.Kind)
		assert.Equal(t, "Blocked", b.Label)
		assert.Empty(t, b.Explanation.NeedsYou, "nothing here needs the operator")
		assert.Equal(t, "Nothing.", b.Explanation.NeedsYouSummary)
	})
	t.Run("[AC-012] the explanation holds one Blocked item naming the path and the bounded detail", func(t *testing.T) {
		require.Len(t, b.Explanation.Blocked, 1)
		r := b.Explanation.Blocked[0]
		assert.Equal(t, reasonExtraBlocked, r.Code)
		assert.Equal(t, "dietpi", r.Updater)
		assert.True(t, strings.HasPrefix(r.Text, "DietPi update blocked: /run/dietpi permissions"), r.Text)
		assert.Contains(t, r.Text, "/run/dietpi is writable by group or others (mode 0777)")
	})
	t.Run("[AC-014a] the older worker's false positive says the fix arrives with an install.sh re-run as root", func(t *testing.T) {
		r := b.Explanation.Blocked[0]
		assert.Contains(t, r.Text, "(fixed by this rIOt release; re-run install.sh as root on this host to apply the fix)")
		assert.Equal(t, "Re-run install.sh as root on this host to apply the fix.", r.Action)
	})
	t.Run("[AC-012] no text says the update will be applied automatically", func(t *testing.T) {
		for _, text := range allBadgeText(b) {
			assert.NotContains(t, strings.ToLower(text), "will be applied automatically", text)
		}
		assert.NotContains(t, infoCodes(b), reasonExtraPending, "the pending line is suppressed for a refused updater")
	})
	t.Run("[AC-012] the Blocked list is always present in the JSON, even when empty", func(t *testing.T) {
		quiet := computePatchBadge(healthyInput(), badgeNow)
		data, err := json.Marshal(quiet)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"blocked":[]`)
		assert.NotContains(t, string(data), `"blocked":null`)
	})
}

func TestPatchBadge_DP_AC014_ReasonClasses(t *testing.T) {
	blocked := []string{
		maintenance.SkipTmpNotPrivate, maintenance.SkipMountPropagationUnver, maintenance.SkipRunDirNotPrivate,
		maintenance.SkipBusy, maintenance.SkipMinInterval, maintenance.SkipDpkgLocked,
	}
	for _, reason := range blocked {
		t.Run("[AC-014] "+reason+" is Blocked", func(t *testing.T) {
			in := healthyInput()
			in.Updates.ExtraUpdaters = pendingEnabledDietPi()
			in.LastRun = refusedRun("succeeded", reason, "")
			b := computePatchBadge(in, badgeNow)
			assert.Equal(t, badgeBlocked, b.Kind)
			assert.Equal(t, []string{reasonExtraBlocked}, blockedCodes(b))
			assert.Empty(t, b.Explanation.NeedsYou)
		})
	}
	t.Run("[AC-014] non_default_update_source and insufficient_time are action items with their own action", func(t *testing.T) {
		for reason, action := range map[string]string{
			maintenance.SkipNonDefaultSource: "Set DEV_GITOWNER back to MichaIng in /boot/dietpi.txt.",
			maintenance.SkipInsufficientTime: "Lengthen this host's patch window to at least 90 minutes.",
		} {
			in := healthyInput()
			in.Updates.ExtraUpdaters = pendingEnabledDietPi()
			in.LastRun = refusedRun("succeeded", reason, "")
			b := computePatchBadge(in, badgeNow)
			assert.Equal(t, badgeNeedsYou, b.Kind, reason)
			r := findReason(t, b, reasonExtraRefused)
			assert.Equal(t, action, r.Action, reason)
			assert.Empty(t, b.Explanation.Blocked, reason)
		}
	})
	t.Run("[AC-014] isolation failures never claim a retry fixes them and point at doctor", func(t *testing.T) {
		for _, reason := range []string{maintenance.SkipTmpNotPrivate, maintenance.SkipMountPropagationUnver, maintenance.SkipRunDirNotPrivate} {
			in := healthyInput()
			in.Updates.ExtraUpdaters = pendingEnabledDietPi()
			in.LastRun = refusedRun("succeeded", reason, "")
			r := computePatchBadge(in, badgeNow).Explanation.Blocked[0]
			assert.Contains(t, r.Text, "this host's mount layout prevented rIOt from isolating the step", reason)
			assert.Equal(t, "Run riot-agent doctor on this host.", r.Action, reason)
			assert.NotContains(t, strings.ToLower(r.Text+" "+r.Action), "retries", reason)
			assert.NotContains(t, strings.ToLower(r.Text+" "+r.Action), "next patch run", reason)
		}
	})
	t.Run("[AC-014] the temporary reasons keep the retry wording", func(t *testing.T) {
		for _, reason := range []string{maintenance.SkipBusy, maintenance.SkipMinInterval, maintenance.SkipDpkgLocked} {
			in := healthyInput()
			in.Updates.ExtraUpdaters = pendingEnabledDietPi()
			in.LastRun = refusedRun("succeeded", reason, "")
			r := computePatchBadge(in, badgeNow).Explanation.Blocked[0]
			assert.Equal(t, "Nothing to do; rIOt retries on the next patch run.", r.Action, reason)
		}
	})
	t.Run("[AC-014] with DietPi no longer pending the refusal is one information line and the badge is Auto", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpToDate, models.ExtraReadinessEnabled)
		in.LastRun = refusedRun("succeeded", maintenance.SkipDpkgLocked, "")
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeAuto, b.Kind)
		assert.Empty(t, b.Explanation.Blocked)
		assert.Empty(t, b.Explanation.NeedsYou)
		assert.Equal(t, "The last patch run skipped DietPi: the package lock was held.", findInfo(t, b, reasonExtraRefused).Text)
	})
	t.Run("[AC-014] a neutral skip raises nothing", func(t *testing.T) {
		for _, reason := range []string{"not_enabled", "status_unknown", "nothing_pending"} {
			in := healthyInput()
			in.Updates.ExtraUpdaters = pendingEnabledDietPi()
			in.LastRun = &models.Command{Status: "succeeded", ResultMsg: "apt: succeeded; dietpi: skipped (" + reason + ")\n" + refusalSuccessLine2}
			b := computePatchBadge(in, badgeNow)
			assert.Empty(t, b.Explanation.Blocked, reason)
			assert.NotContains(t, infoCodes(b), reasonExtraRefused, reason)
			assert.NotContains(t, reasonCodes(b), reasonExtraRefused, reason)
		}
	})
	t.Run("[AC-014] a refusal for an updater the host does not report raises nothing", func(t *testing.T) {
		in := healthyInput()
		in.LastRun = refusedRun("succeeded", maintenance.SkipDpkgLocked, "")
		b := computePatchBadge(in, badgeNow)
		assert.Empty(t, b.Explanation.Blocked)
		assert.Empty(t, infoCodes(b))
	})
	t.Run("[AC-014] a pending updater that is not enabled keeps its not-applied item and no refusal item", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpdateAvailable, models.ExtraReadinessNotOptedIn)
		in.LastRun = refusedRun("succeeded", maintenance.SkipDpkgLocked, "")
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, []string{reasonExtraNotApplied}, reasonCodes(b))
		assert.Empty(t, b.Explanation.Blocked)
	})
}

func TestPatchBadge_DP_AC014a_UnsafePathIsAnActionExceptTheStockFalsePositive(t *testing.T) {
	t.Run("[AC-014a] a /boot/dietpi path is needs_you with the investigate action", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = refusedRun("succeeded", maintenance.SkipUnsafePath, "/boot/dietpi/func/dietpi-globals is writable by group or others")
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeNeedsYou, b.Kind)
		r := findReason(t, b, reasonExtraRefused)
		assert.Equal(t, "Investigate the named path on this host (owner and permissions); rIOt will not run DietPi until it is root-only.", r.Action)
		assert.True(t, strings.HasPrefix(r.Text, "DietPi update blocked: /boot/dietpi/func/dietpi-globals permissions"), r.Text)
		assert.NotContains(t, strings.ToLower(r.Action), "nothing to do")
		assert.Empty(t, b.Explanation.Blocked)
	})
	t.Run("[AC-014a] it counts as an item that needs the operator", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = refusedRun("succeeded", maintenance.SkipUnsafePath, "/var/tmp/dietpi/logs is writable by group or others (mode 0777)")
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, "1 item needs you.", b.Explanation.NeedsYouSummary)
	})
	t.Run("[AC-014a] /run/dietpi is writable by group or others gives blocked with the re-run text", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = refusedRun("succeeded", maintenance.SkipUnsafePath, "/run/dietpi is writable by group or others (mode 0777)")
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeBlocked, b.Kind)
		assert.Contains(t, b.Explanation.Blocked[0].Text, "re-run install.sh as root")
	})
	t.Run("[AC-014a] a path that merely starts with /run/dietpi is not the false positive", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = refusedRun("succeeded", maintenance.SkipUnsafePath, "/run/dietpiX is writable by group or others")
		assert.Equal(t, badgeNeedsYou, computePatchBadge(in, badgeNow).Kind)
	})
	t.Run("[AC-014a] a detail without a usable path falls back to the generic words", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = refusedRun("succeeded", maintenance.SkipUnsafePath, "trust walk not configured")
		r := findReason(t, computePatchBadge(in, badgeNow), reasonExtraRefused)
		assert.Contains(t, r.Text, "a DietPi path failed rIOt's safety check")
	})
	t.Run("[AC-014a] a hostile path token is not echoed as a path", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = refusedRun("succeeded", maintenance.SkipUnsafePath, "/<script>alert(1)</script> is writable")
		r := findReason(t, computePatchBadge(in, badgeNow), reasonExtraRefused)
		assert.Contains(t, r.Text, "a DietPi path failed rIOt's safety check")
	})
}

func TestPatchBadge_DP_AC015_OldWorkerFailedRefusalIsBlocked(t *testing.T) {
	detail := "/run/dietpi is writable by group or others (mode 0777)"
	t.Run("[AC-015] PiCUPS today: a failed run whose only failure is the refusal is blocked with no last-run-failed item", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = refusedRun("failed", maintenance.SkipUnsafePath, detail)
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, badgeBlocked, b.Kind)
		assert.NotContains(t, reasonCodes(b), reasonLastRunFailed)
		assert.NotContains(t, infoCodes(b), reasonLastRunFailed)
	})
	t.Run("[AC-015] a failed run whose refusal is no longer pending is information, still without the failure item", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = dietPi(models.ExtraStatusUpToDate, models.ExtraReadinessEnabled)
		in.LastRun = refusedRun("failed", maintenance.SkipUnsafePath, detail)
		b := computePatchBadge(in, badgeNow)
		assert.NotContains(t, reasonCodes(b), reasonLastRunFailed)
		assert.NotContains(t, infoCodes(b), reasonLastRunFailed)
		assert.Contains(t, infoCodes(b), reasonExtraRefused)
	})
	t.Run("[AC-015] a failed run whose apt part failed still raises NY-9", func(t *testing.T) {
		in := healthyInput()
		in.Updates.PendingUpdates = 2
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = &models.Command{ID: "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", Status: "failed", ResultMsg: "apt: failed; dietpi: skipped (unsafe_dietpi_path)\nE: boom\ndietpi: skipped (unsafe_dietpi_path); " + detail}
		b := computePatchBadge(in, badgeNow)
		assert.Contains(t, reasonCodes(b), reasonLastRunFailed)
		assert.Empty(t, b.Explanation.Blocked, "no refusal is read from a run whose apt part failed")
	})

	// DP-SEC-004: the refusal must be the sole cause. Each of these keeps NY-9.
	cause := map[string]string{
		"a hold restore error on line 2":       "apt: succeeded; dietpi: skipped (unsafe_dietpi_path)\nhold restore failed: apt-mark refused\ndietpi: skipped (unsafe_dietpi_path); " + detail,
		"a verify error on line 2":             "apt: succeeded; dietpi: skipped (unsafe_dietpi_path)\npackage verification failed: dpkg --audit reports problems\ndietpi: skipped (unsafe_dietpi_path); " + detail,
		"an interrupted updater":               "apt: succeeded; dietpi: interrupted\nPackage state verified; maintenance completed\ndietpi: interrupted",
		"an interrupted apt part":              "apt: interrupted; dietpi: skipped (unsafe_dietpi_path)\nMaintenance interrupted; package state requires manual review: x\ndietpi: skipped (unsafe_dietpi_path); " + detail,
		"another updater that failed":          "apt: succeeded; dietpi: skipped (unsafe_dietpi_path); snap: failed (exit 1)\nPackage state verified; maintenance completed\ndietpi: skipped (unsafe_dietpi_path); " + detail + "\nsnap: failed (exit 1); log",
		"an unexpected line after the details": "apt: succeeded; dietpi: skipped (unsafe_dietpi_path)\nPackage state verified; maintenance completed\ndietpi: skipped (unsafe_dietpi_path); " + detail + "\nlast output lines of dietpi:\nboom",
		"no second line":                       "apt: succeeded; dietpi: skipped (unsafe_dietpi_path)",
		"a first line that is not apt":         "dietpi: skipped (unsafe_dietpi_path)\nPackage state verified; maintenance completed",
	}
	for name, msg := range cause {
		msg := msg
		t.Run("[DP-SEC-004] "+name+" keeps the failed-run item", func(t *testing.T) {
			in := healthyInput()
			in.Updates.PendingUpdates = 2
			in.Updates.ExtraUpdaters = pendingEnabledDietPi()
			in.LastRun = &models.Command{ID: "0b7a53a6-1f2e-4b6a-9d1d-2d3f5c6a7b8c", Status: "failed", ResultMsg: msg}
			b := computePatchBadge(in, badgeNow)
			assert.Contains(t, reasonCodes(b), reasonLastRunFailed)
			assert.Empty(t, b.Explanation.Blocked)
			assert.Equal(t, badgeNeedsYou, b.Kind)
		})
	}
	t.Run("[DP-SEC-004] a timed out run keeps the failed-run item whatever its text says", func(t *testing.T) {
		in := healthyInput()
		in.Updates.PendingUpdates = 2
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		run := refusedRun("timed_out", maintenance.SkipUnsafePath, detail)
		in.LastRun = run
		b := computePatchBadge(in, badgeNow)
		assert.Contains(t, reasonCodes(b), reasonLastRunFailed)
		assert.Empty(t, b.Explanation.Blocked)
	})
	t.Run("[DP-SEC-004] the apt note after the success summary is allowed", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = &models.Command{Status: "failed", ResultMsg: "apt: succeeded; dietpi: skipped (unsafe_dietpi_path)\nPackage state verified; maintenance completed; package lists refreshed\ndietpi: skipped (unsafe_dietpi_path); " + detail}
		assert.Equal(t, badgeBlocked, computePatchBadge(in, badgeNow).Kind)
	})
}

func TestPatchBadge_DP_AC016_ActionWinsOverBlocked(t *testing.T) {
	in := healthyInput()
	in.Updates.RebootRequired = true
	in.Updates.ExtraUpdaters = pendingEnabledDietPi()
	in.LastRun = refusedRun("succeeded", maintenance.SkipDpkgLocked, "")
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind, "[AC-016] a true action item wins")
	assert.Contains(t, reasonCodes(b), reasonManualReboot)
	assert.Equal(t, []string{reasonExtraBlocked}, blockedCodes(b), "[AC-016] the blocked reason still appears")
	t.Run("[AC-016] a Blocked refusal with an Action refusal of the same updater lists the first only", func(t *testing.T) {
		in := healthyInput()
		in.Updates.ExtraUpdaters = pendingEnabledDietPi()
		in.LastRun = &models.Command{Status: "succeeded", ResultMsg: "apt: succeeded; dietpi: skipped (dpkg_locked); dietpi: skipped (insufficient_time)\n" + refusalSuccessLine2}
		b := computePatchBadge(in, badgeNow)
		assert.Equal(t, 1, len(b.Explanation.Blocked)+len(b.Explanation.NeedsYou), "one item per updater")
	})
}

func TestParseRefusals_DP_Bounds(t *testing.T) {
	good := "apt: succeeded; dietpi: skipped (dpkg_locked)\n" + refusalSuccessLine2 + "\ndietpi: skipped (dpkg_locked); "
	run := func(status, msg string) refusalRun {
		return parseRefusals(&models.Command{Status: status, ResultMsg: msg})
	}
	t.Run("[DP-SEC-007] a message of exactly 2048 bytes is read whole, and byte 2049 is never read", func(t *testing.T) {
		head := "apt: succeeded; dietpi: skipped (dpkg_locked)\n" + refusalSuccessLine2 + "\n"
		tail := "dietpi: skipped (dpkg_locked); abcdefghij"
		filler := strings.Repeat("x", 2048-len(head)-len(tail)-1) + "\n"
		atBound := head + filler + tail
		require.Equal(t, 2048, len(atBound))
		at := run("succeeded", atBound)
		require.Len(t, at.Refusals, 1)
		assert.Equal(t, "abcdefghij", at.Refusals[0].Detail, "all 2048 bytes are read")
		over := run("succeeded", atBound+"k")
		require.Len(t, over.Refusals, 1)
		assert.Equal(t, "abcdefghij", over.Refusals[0].Detail, "the 2049th byte is not read")
	})
	t.Run("[DP-SEC-007] the detail is cut to 160 bytes", func(t *testing.T) {
		got := run("succeeded", good+strings.Repeat("d", 300))
		require.Len(t, got.Refusals, 1)
		assert.Equal(t, maxRefusalDetail, len(got.Refusals[0].Detail))
	})
	t.Run("[DP-SEC-007] a detail line that only starts after the bound is not found", func(t *testing.T) {
		msg := "apt: succeeded; dietpi: skipped (dpkg_locked)\n" + refusalSuccessLine2 + "\n" + strings.Repeat("x", 2048) + "\ndietpi: skipped (dpkg_locked); late"
		got := run("succeeded", msg)
		require.Len(t, got.Refusals, 1)
		assert.Empty(t, got.Refusals[0].Detail)
	})
	t.Run("[DP-Bounds] control characters, escapes and bidi marks are stripped from the detail", func(t *testing.T) {
		got := run("succeeded", good+"a\x00b\x1b[31mred\x07‮evil\x7f end")
		require.Len(t, got.Refusals, 1)
		d := got.Refusals[0].Detail
		for _, bad := range []string{"\x00", "\x1b", "\x07", "‮", "\x7f"} {
			assert.NotContains(t, d, bad)
		}
		assert.Contains(t, d, "ab")
	})
	t.Run("[DP-Bounds] a multi-byte character is never split by the 160-byte cut", func(t *testing.T) {
		got := run("succeeded", good+strings.Repeat("é", 200))
		require.Len(t, got.Refusals, 1)
		assert.LessOrEqual(t, len(got.Refusals[0].Detail), maxRefusalDetail)
		assert.True(t, utf8Valid(got.Refusals[0].Detail))
	})
	t.Run("[DP-Bounds] more than eight updater entries are ignored whole", func(t *testing.T) {
		parts := ""
		for i := 0; i < 9; i++ {
			parts += "; u" + string(rune('a'+i)) + ": skipped (dpkg_locked)"
		}
		assert.Empty(t, run("succeeded", "apt: succeeded"+parts+"\n"+refusalSuccessLine2).Refusals)
	})
	t.Run("[DP-Bounds] exactly eight entries are read", func(t *testing.T) {
		parts := ""
		for i := 0; i < 8; i++ {
			parts += "; u" + string(rune('a'+i)) + ": skipped (dpkg_locked)"
		}
		assert.Len(t, run("succeeded", "apt: succeeded"+parts+"\n"+refusalSuccessLine2).Refusals, 8)
	})
	garbage := map[string]string{
		"empty":                 "",
		"not a result":          "garbage",
		"upper case id":         "apt: succeeded; DietPi: skipped (dpkg_locked)",
		"id too long":           "apt: succeeded; " + strings.Repeat("a", 33) + ": skipped (dpkg_locked)",
		"unknown reason":        "apt: succeeded; dietpi: skipped (because)\n" + refusalSuccessLine2,
		"a neutral reason":      "apt: succeeded; dietpi: skipped (nothing_pending)\n" + refusalSuccessLine2,
		"apt_not_successful":    "apt: succeeded; dietpi: skipped (apt_not_successful)\n" + refusalSuccessLine2,
		"trailing garbage":      "apt: succeeded; dietpi: skipped (dpkg_locked) extra\n" + refusalSuccessLine2,
		"markup in the words":   "apt: succeeded; dietpi: skipped (<b>)\n" + refusalSuccessLine2,
		"apt failed":            "apt: failed; dietpi: skipped (dpkg_locked)\n" + refusalSuccessLine2,
		"no updater parts":      "apt: succeeded\n" + refusalSuccessLine2,
		"leading blank line":    "\napt: succeeded; dietpi: skipped (dpkg_locked)",
		"carriage return in id": "apt: succeeded; diet\rpi: skipped (dpkg_locked)",
	}
	for name, msg := range garbage {
		msg := msg
		t.Run("[DP-Bounds] garbage ("+name+") raises no refusal", func(t *testing.T) {
			assert.Empty(t, run("succeeded", msg).Refusals)
			assert.Empty(t, run("failed", msg).Refusals)
		})
	}
	t.Run("[DP-Bounds] a nil run, a running or timed-out status raise nothing", func(t *testing.T) {
		assert.Empty(t, parseRefusals(nil).Refusals)
		for _, status := range []string{"running", "timed_out", "queued", ""} {
			assert.Empty(t, run(status, good+"x").Refusals, status)
		}
	})
	t.Run("[DP-Bounds] a hostile oversized message is cheap and bounded", func(t *testing.T) {
		msg := strings.Repeat("apt: succeeded; ", 200000)
		assert.Empty(t, run("succeeded", msg).Refusals)
	})
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }

// DP-SEC-001 and the badge JSON contract: the kind and the list are served.
func TestPatchStatus_DP_AC017_ServesBlockedKind(t *testing.T) {
	in := healthyInput()
	in.Updates.ExtraUpdaters = pendingEnabledDietPi()
	in.LastRun = refusedRun("succeeded", maintenance.SkipRunDirNotPrivate, "")
	data, err := json.Marshal(computePatchBadge(in, badgeNow))
	require.NoError(t, err)
	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, "blocked", back["kind"])
	assert.Equal(t, "Blocked", back["label"])
	explanation := back["explanation"].(map[string]any)
	assert.Len(t, explanation["blocked"].([]any), 1)
}
