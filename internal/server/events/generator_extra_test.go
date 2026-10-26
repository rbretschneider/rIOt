package events

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// [EXTRA-UPD-001 NFR-008] The command-sent event shows when the operator
// excluded extra updaters from a manual os_update.
func TestCommandSent_EU_AC037_EventMentionsExclusion(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		params  map[string]interface{}
		wantMsg string
	}{
		{"excluded os_update", "os_update", map[string]interface{}{"mode": "full", "exclude_extra_updaters": true},
			"OS update initiated on gpu-host (extra updaters excluded)"},
		{"included os_update", "os_update", map[string]interface{}{"mode": "full"},
			"OS update initiated on gpu-host"},
		{"explicit false", "os_update", map[string]interface{}{"exclude_extra_updaters": false},
			"OS update initiated on gpu-host"},
		{"non-boolean value", "os_update", map[string]interface{}{"exclude_extra_updaters": "true"},
			"OS update initiated on gpu-host"},
		{"other action never mentions it", "fetch_logs", map[string]interface{}{"exclude_extra_updaters": true},
			"Log fetch initiated on gpu-host"},
	}
	for _, tc := range cases {
		t.Run("[AC-037] "+tc.name, func(t *testing.T) {
			gen, eventRepo, _, _ := setupGenerator(t)
			gen.CommandSent(context.Background(), "dev-1", "gpu-host", tc.action, tc.params)
			require.Len(t, eventRepo.Events, 1)
			assert.Equal(t, models.EventCommandSent, eventRepo.Events[0].Type)
			assert.Equal(t, tc.wantMsg, eventRepo.Events[0].Message)
		})
	}
	t.Run("[AC-037] both suffixes together", func(t *testing.T) {
		gen, eventRepo, _, _ := setupGenerator(t)
		gen.CommandSent(context.Background(), "dev-1", "gpu-host", "os_update", map[string]interface{}{"reboot_if_required": true, "exclude_extra_updaters": true})
		require.Len(t, eventRepo.Events, 1)
		assert.Equal(t, "OS update initiated on gpu-host (reboot authorized if required) (extra updaters excluded)", eventRepo.Events[0].Message)
	})
}
