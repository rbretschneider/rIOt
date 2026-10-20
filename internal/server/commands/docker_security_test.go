package commands

import (
	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestDUX_SEC004_ProgressMaskedAfterSanitization(t *testing.T) {
	r := sanitizeReport(models.CommandReport{Kind: models.ReportKindProgress, Message: "password=secret-value"})
	assert.NotContains(t, r.Message, "secret-value")
	assert.Contains(t, r.Message, "masked")
}
func TestDUX_SEC004_CommandResultStepsAndTargetsMaskedBounded(t *testing.T) {
	steps := make([]models.DockerCommandStep, 100)
	targets := make([]models.DockerTargetResult, 200)
	for i := range steps {
		steps[i] = models.DockerCommandStep{Name: "pull", Output: strings.Repeat("password=secret-value\n", 4000)}
	}
	for i := range targets {
		targets[i] = models.DockerTargetResult{Container: "web", Outcome: "succeeded", Message: "token=secret-value"}
	}
	r := sanitizeReport(models.CommandReport{Kind: models.ReportKindResult, Result: &models.CommandResult{Message: "password=secret-value", Output: "token=secret-value", Steps: steps, TargetResults: targets}})
	require.NotNil(t, r.Result)
	assert.Len(t, r.Result.Steps, 32)
	assert.Len(t, r.Result.TargetResults, 100)
	total := 0
	for _, s := range r.Result.Steps {
		total += len(s.Output)
		assert.NotContains(t, s.Output, "secret-value")
	}
	assert.LessOrEqual(t, total, 64<<10)
	for _, target := range r.Result.TargetResults {
		assert.NotContains(t, target.Message, "secret-value")
	}
	assert.Contains(t, steps[0].Output, "secret-value", "sanitizing a copy must not mutate caller")
}
func TestDUX_OperatorAndSystemCreationBounds(t *testing.T) {
	g := &creationGate{}
	now := time.Now()
	for i := 0; i < 120; i++ {
		require.True(t, g.allow("host-a", models.OriginUser, "", now))
	}
	require.False(t, g.allow("host-a", models.OriginUser, "", now))
	require.True(t, g.allow("host-a", models.OriginUser, "", now.Add(time.Minute)))
	require.True(t, g.allow("host-b", models.OriginAutoUpdate, "policy/web", now))
	require.False(t, g.allow("host-b", models.OriginAutoUpdate, "policy/web", now.Add(11*time.Minute)), "idle cleanup must preserve 15 minute policy cooldown")
	require.True(t, g.allow("host-b", models.OriginAutoUpdate, "policy/web", now.Add(15*time.Minute)))
}
