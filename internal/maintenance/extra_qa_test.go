package maintenance

import (
	"testing"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/extraupd"
	"github.com/DesyncTheThird/rIOt/internal/models"
)

// Added by QA Engineer. Covers AC-025 and FR-032.
//
// The two existing AC-025 tests each hold half of the chain: the models test
// restates the 40, 80 and 85 minute values as literals, and the worker test
// restates 88 as a literal. Neither can notice that the worker constants and the
// server/agent constants have drifted apart. This test builds the whole chain from
// the shipped constants of both packages, so changing any one of them breaks it.
func TestBounds_EU_AC025_QA_WholeChainFromShippedConstants(t *testing.T) {
	spec := extraupd.NewDietPi().Apply()
	chain := []struct {
		name string
		d    time.Duration
	}{
		{"DietPi step limit", spec.StepLimit},
		{"worker budget", ExtendedWorkerBudget},
		{"service backstop (drop-in RuntimeMaxSec)", ExtraServiceBackstop},
		{"agent hard max", models.OSUpdateExtraAgentHardMax},
		{"command deadline", models.OSUpdateExtraExecDeadline},
	}
	t.Run("[AC-025] step limit < worker < backstop < agent hard max < command deadline, strictly", func(t *testing.T) {
		for i := 1; i < len(chain); i++ {
			if chain[i-1].d >= chain[i].d {
				t.Errorf("%s (%s) must be below %s (%s)", chain[i-1].name, chain[i-1].d, chain[i].name, chain[i].d)
			}
		}
	})
	t.Run("[AC-025] the stated values are the shipped values", func(t *testing.T) {
		want := []time.Duration{40 * time.Minute, 80 * time.Minute, 85 * time.Minute, 88 * time.Minute, 95 * time.Minute}
		for i, w := range want {
			if chain[i].d != w {
				t.Errorf("%s = %s, ADD AD-006 states %s", chain[i].name, chain[i].d, w)
			}
		}
	})
	t.Run("[AC-025] a service killed by its backstop (plus the stop grace) is still seen by the agent before its own limit", func(t *testing.T) {
		if ExtraServiceBackstop+TerminationGrace >= models.OSUpdateExtraAgentHardMax {
			t.Errorf("backstop %s + grace %s must stay below the agent hard max %s", ExtraServiceBackstop, TerminationGrace, models.OSUpdateExtraAgentHardMax)
		}
	})
	t.Run("[AC-025] the server deadline leaves the agent its slack", func(t *testing.T) {
		if models.OSUpdateExtraAgentHardMax+models.DeadlineSlack > models.OSUpdateExtraExecDeadline {
			t.Errorf("agent hard max %s + slack %s exceeds the deadline %s", models.OSUpdateExtraAgentHardMax, models.DeadlineSlack, models.OSUpdateExtraExecDeadline)
		}
	})
	t.Run("[AC-025][AC-044] an unstamped os_update keeps the pre-story agent and server bounds", func(t *testing.T) {
		if got := models.AgentHardMax(models.ActionOSUpdate, 0); got != 31*time.Minute+30*time.Second {
			t.Errorf("default agent hard max = %s, want 31m30s", got)
		}
		if got := models.ExecDeadline(models.ActionOSUpdate, 0); got != 40*time.Minute {
			t.Errorf("default command deadline = %s, want 40m", got)
		}
		if models.ExecDeadlineSeconds(models.ActionOSUpdate, 0) != 2400 {
			t.Error("default stored deadline must stay 2400 s")
		}
	})
}
