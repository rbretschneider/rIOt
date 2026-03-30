//go:build !linux

package collectors

import (
	"context"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// GPUCollector gathers NVIDIA GPU runtime metrics. On non-Linux platforms it
// always returns an empty result because nvidia-smi is not available.
type GPUCollector struct{}

func (c *GPUCollector) Name() string { return "gpu" }

func (c *GPUCollector) Collect(_ context.Context) (interface{}, error) {
	return &models.GPUTelemetry{}, nil
}
