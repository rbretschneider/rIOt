package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/maintenance"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer. The agent may send a version 2 request only when the
// installed helper's --capabilities answer advertises request version 2 AND the
// updater. The worker-side parser tests prove the request bytes; the readiness
// tests prove the five reasons. These prove the two ends meet: a helper that
// answers but cannot take a v2 request leaves the run at exactly four fields.

// Covers AC-013, AC-037, AC-044 (NFR-005: new agent with an old helper).
func TestPlanExtras_EU_AC037_QA_HelperAnswerDecidesTheRequestVersion(t *testing.T) {
	budget := map[string]interface{}{models.ParamExtraUpdaterBudget: true}
	for name, answer := range map[string]string{
		"a helper that speaks only request version 1":        "request-versions=1\nextra-updaters=dietpi\n",
		"a v2 helper that lists no updater":                  "request-versions=1,2\nextra-updaters=\n",
		"a v2 helper that lists only an unknown updater":     "request-versions=1,2\nextra-updaters=snap\n",
		"an answer with the keys in the wrong order":         "extra-updaters=dietpi\nrequest-versions=1,2\n",
		"an answer carrying an extra line":                   "request-versions=1,2\nextra-updaters=dietpi\nx=1\n",
		"an old helper's error text (it takes no arguments)": "maintenance: maintenance helper accepts no arguments\n",
	} {
		answer := answer
		t.Run("[AC-037] "+name+" keeps the request at four fields and reports unsupported_helper", func(t *testing.T) {
			g := newExtraRig(t, dietpiRoot(t), enabledCommands())
			g.client.capabilities = func(context.Context) ([]byte, error) { return []byte(answer), nil }
			plan, res := runPlanned(t, g, "full", budget, nil)
			require.NotNil(t, plan)
			assert.Equal(t, models.ExtraReadinessUnsupportedHelper, plan.present[0].readiness)
			assert.Equal(t, maintenance.Request{Version: 1, CommandID: idA, Mode: "full"}, g.request, "a v1 request has no extra_updaters field")
			assert.Empty(t, g.request.ExtraUpdaters)
			assert.Contains(t, res.Message, "dietpi: skipped (not_enabled)", "the run says why DietPi was not applied")
		})
	}
	t.Run("[AC-037] the same host with a v2 helper does send version 2 (the probe is what decides)", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		runPlanned(t, g, "full", budget, nil)
		assert.Equal(t, maintenance.Request{Version: 2, CommandID: idA, Mode: "full", ExtraUpdaters: []string{"dietpi"}}, g.request)
	})
	t.Run("[AC-037] a helper whose probe fails outright never gets a v2 request", func(t *testing.T) {
		g := newExtraRig(t, dietpiRoot(t), enabledCommands())
		g.client.capabilities = nil
		runPlanned(t, g, "full", budget, nil)
		assert.Equal(t, 1, g.request.Version)
	})
}
