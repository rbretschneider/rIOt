package commands

import (
	"context"
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestDUX_CheckUpdatesScopeValidation(t *testing.T) {
	ids := make([]string, 501)
	for i := range ids {
		ids[i] = strings.Repeat("a", 64)
	}
	cases := []struct {
		name   string
		params map[string]interface{}
		valid  bool
	}{
		{"host-wide", nil, true}, {"stack", map[string]interface{}{"compose_project": "example-stack"}, true}, {"selected", map[string]interface{}{"container_ids": []string{strings.Repeat("a", 64)}}, true}, {"intersection", map[string]interface{}{"compose_project": "example-stack", "container_ids": []interface{}{strings.Repeat("b", 12)}}, true}, {"empty selection", map[string]interface{}{"container_ids": []string{}}, false}, {"too many", map[string]interface{}{"container_ids": ids}, false}, {"wrong shape", map[string]interface{}{"container_ids": "web"}, false}, {"invalid ID", map[string]interface{}{"container_ids": []string{"../invalid-id"}}, false}, {"invalid project", map[string]interface{}{"compose_project": "../stack"}, false}, {"null project", map[string]interface{}{"compose_project": nil}, false}, {"unknown", map[string]interface{}{"all": true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { err := validateDockerCheckParams(tc.params); assert.Equal(t, tc.valid, err == nil) })
	}
}
func TestDUX_CheckUpdatesDispatchRetainsScope(t *testing.T) {
	h := newHarness(t)
	id := strings.Repeat("a", 64)
	params := map[string]interface{}{"container_ids": []string{id}, "compose_project": "example-stack"}
	result, err := h.svc.Dispatch(context.Background(), DispatchRequest{DeviceID: devA, Action: "docker_check_updates", Params: params})
	require.NoError(t, err)
	require.NotNil(t, result.Command)
	assert.Equal(t, params, result.Command.Params)
	assert.Empty(t, result.Command.TargetKeys)
	assert.Equal(t, models.OriginUser, result.Command.Origin)
}
