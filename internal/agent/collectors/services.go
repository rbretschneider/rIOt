package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

type ServicesCollector struct{}

func (c *ServicesCollector) Name() string { return "services" }

func (c *ServicesCollector) Collect(ctx context.Context) (interface{}, error) {
	if runtime.GOOS != "linux" {
		return []models.ServiceInfo{}, nil
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		return []models.ServiceInfo{}, nil
	}

	// List enabled and failed services
	out, err := exec.CommandContext(ctx, "systemctl", "list-units", "--type=service",
		"--no-pager", "--no-legend", "--plain").Output()
	if err != nil {
		return []models.ServiceInfo{}, err
	}

	var services []models.ServiceInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		name := fields[0]
		// loadState := fields[1]
		activeState := fields[2]
		subState := fields[3]

		svc := models.ServiceInfo{
			Name:  name,
			State: activeState + " (" + subState + ")",
		}

		// Check if enabled
		enableOut, err := exec.CommandContext(ctx, "systemctl", "is-enabled", name).Output()
		if err == nil {
			svc.Enabled = strings.TrimSpace(string(enableOut)) == "enabled"
		}

		services = append(services, svc)
	}

	return services, nil
}
