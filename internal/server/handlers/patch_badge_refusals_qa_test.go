package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer.

// Covers FR-015 (the detail is bounded to 160 bytes). The existing test compares
// the length with the production constant, so it cannot notice the bound moving.
func TestParseRefusals_DP_QA_DetailBoundIsExactly160Bytes(t *testing.T) {
	prefix := "apt: succeeded; dietpi: skipped (dpkg_locked)\n" + refusalSuccessLine2 + "\ndietpi: skipped (dpkg_locked); "
	for _, tc := range []struct {
		name   string
		detail int
		want   int
	}{
		{"159 bytes are kept whole", 159, 159},
		{"160 bytes are kept whole", 160, 160},
		{"161 bytes are cut to 160", 161, 160},
		{"1000 bytes are cut to 160", 1000, 160},
	} {
		t.Run("[AC-012] "+tc.name, func(t *testing.T) {
			got := parseRefusals(&models.Command{Status: "succeeded", ResultMsg: prefix + strings.Repeat("d", tc.detail)})
			require.Len(t, got.Refusals, 1)
			assert.Equal(t, tc.want, len(got.Refusals[0].Detail))
		})
	}
}

// Covers AC-014a and AC-015 together: an old worker's failed run whose refusal is
// a /boot/dietpi path (a possible tampering signal) must not be cleared to
// "Blocked" by the FR-016 reinterpretation. The generic last-run-failed item is
// replaced by the specific investigate action, and the host counts as needing
// the operator.
func TestPatchBadge_DP_QA_AC015_OldWorkerFailedBootPathRefusalNeedsYou(t *testing.T) {
	in := healthyInput()
	in.Updates.ExtraUpdaters = pendingEnabledDietPi()
	in.LastRun = refusedRun("failed", maintenance.SkipUnsafePath, "/boot/dietpi/func/dietpi-globals is writable by group or others")
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Empty(t, b.Explanation.Blocked)
	assert.NotContains(t, reasonCodes(b), reasonLastRunFailed, "the refusal is the sole cause, so the generic failure item is replaced by the specific one")
	r := findReason(t, b, reasonExtraRefused)
	assert.Equal(t, "Investigate the named path on this host (owner and permissions); rIOt will not run DietPi until it is root-only.", r.Action)
	assert.Equal(t, "1 item needs you.", b.Explanation.NeedsYouSummary)
}

// Covers AC-016: the stock false positive, an action refusal and a reboot all
// at once still give needs_you, with every item present exactly once.
func TestPatchBadge_DP_QA_AC016_TwoUpdatersOneBlockedOneActionGiveNeedsYou(t *testing.T) {
	in := healthyInput()
	in.Updates.ExtraUpdaters = append(pendingEnabledDietPi(), models.ExtraUpdater{
		ID: "snap", Status: models.ExtraStatusUpdateAvailable, Readiness: models.ExtraReadinessEnabled, PendingCount: 1,
	})
	in.LastRun = &models.Command{Status: "succeeded", ResultMsg: "apt: succeeded; dietpi: skipped (dpkg_locked); snap: skipped (insufficient_time)\n" + refusalSuccessLine2}
	b := computePatchBadge(in, badgeNow)
	assert.Equal(t, badgeNeedsYou, b.Kind)
	assert.Equal(t, []string{reasonExtraBlocked}, blockedCodes(b))
	assert.Contains(t, reasonCodes(b), reasonExtraRefused)
}
