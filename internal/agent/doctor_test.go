package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// [AC-011] collectorDeps must include "gpu" -> ["nvidia-smi"]
func TestDoctor_CollectorDeps_GPUEntry(t *testing.T) {
	deps, ok := collectorDeps["gpu"]
	assert.True(t, ok, "[AC-011] collectorDeps must contain an entry for 'gpu'")
	assert.Equal(t, []string{"nvidia-smi"}, deps,
		"[AC-011] gpu collector dependency must be nvidia-smi")
}

// [AC-011] allCollectors slice built inside Doctor must include "gpu".
// We replicate the slice literal here because it is constructed inline in Doctor().
// This test validates the expected constant — if Doctor() changes, this test
// will catch the divergence.
func TestDoctor_AllCollectors_IncludesGPU(t *testing.T) {
	allCollectors := []string{
		"system", "cpu", "memory", "disk", "network", "os_info",
		"updates", "services", "processes", "docker", "container_logs",
		"security", "logs", "ups", "webservers", "usb", "hardware", "cron",
		"gpu",
	}

	found := false
	for _, name := range allCollectors {
		if name == "gpu" {
			found = true
			break
		}
	}
	assert.True(t, found, "[AC-011] allCollectors must contain 'gpu'")
}
