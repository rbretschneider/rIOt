//go:build linux

package collectors

import (
	"context"
	"log/slog"
	"os/exec"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// GPUCollector gathers NVIDIA GPU runtime metrics via the nvidia-smi command.
// Linux-only; returns empty GPUTelemetry on non-Linux platforms or when
// nvidia-smi is not found in PATH.
type GPUCollector struct{}

func (c *GPUCollector) Name() string { return "gpu" }

func (c *GPUCollector) Collect(ctx context.Context) (interface{}, error) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return &models.GPUTelemetry{}, nil
	}

	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,uuid,temperature.gpu,fan.speed,utilization.gpu,utilization.memory,memory.used,memory.total,power.draw,power.limit,pci.bus_id",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		slog.Warn("gpu collector: nvidia-smi failed", "error", err)
		return &models.GPUTelemetry{}, nil
	}

	gpus := parseNvidiaSMIOutput(string(out))
	return &models.GPUTelemetry{GPUs: gpus}, nil
}
