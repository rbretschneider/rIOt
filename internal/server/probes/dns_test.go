package probes

import (
	"context"
	"testing"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestExecuteDNS_MissingHostname(t *testing.T) {
	probe := models.Probe{
		ID:     1,
		Type:   "dns",
		Config: map[string]interface{}{},
	}

	result := executeDNS(context.Background(), probe)
	assert.False(t, result.Success)
	assert.Equal(t, "hostname not configured", result.ErrorMsg)
}

func TestExecuteDNS_ResolveLocalhost(t *testing.T) {
	probe := models.Probe{
		ID:             2,
		Type:           "dns",
		Config:         map[string]interface{}{"hostname": "localhost"},
		TimeoutSeconds: 5,
	}

	result := executeDNS(context.Background(), probe)
	assert.True(t, result.Success)
	assert.Greater(t, result.LatencyMs, 0.0)
	ips, ok := result.Metadata["resolved_ips"].([]string)
	assert.True(t, ok)
	assert.NotEmpty(t, ips)
}
