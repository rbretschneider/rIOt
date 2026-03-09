package collectors

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// UPSCollector gathers NUT UPS status via the upsc command.
// Linux-only; returns empty UPSInfo on other platforms or when upsc is not found.
type UPSCollector struct{}

func (c *UPSCollector) Name() string { return "ups" }

func (c *UPSCollector) Collect(ctx context.Context) (interface{}, error) {
	if runtime.GOOS != "linux" {
		return &models.UPSInfo{}, nil
	}

	// Discover UPS names
	out, err := exec.CommandContext(ctx, "upsc", "-l").Output()
	if err != nil {
		return &models.UPSInfo{}, nil
	}

	var upsName string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			upsName = line
			break
		}
	}
	if upsName == "" {
		return &models.UPSInfo{}, nil
	}

	// Get all variables for this UPS
	out, err = exec.CommandContext(ctx, "upsc", upsName).Output()
	if err != nil {
		return &models.UPSInfo{}, nil
	}

	vars := parseUPSCOutput(string(out))
	info := &models.UPSInfo{
		Name:         upsName,
		Status:       vars["ups.status"],
		Model:        vars["ups.model"],
		Manufacturer: vars["ups.mfr"],
	}

	info.OnBattery = strings.Contains(info.Status, "OB")
	info.LowBattery = strings.Contains(info.Status, "LB")

	if v, err := strconv.ParseFloat(vars["battery.charge"], 64); err == nil {
		info.BatteryCharge = &v
	}
	if v, err := strconv.Atoi(vars["battery.runtime"]); err == nil {
		info.BatteryRuntime = &v
	}
	if v, err := strconv.ParseFloat(vars["input.voltage"], 64); err == nil {
		info.InputVoltage = &v
	}
	if v, err := strconv.ParseFloat(vars["output.voltage"], 64); err == nil {
		info.OutputVoltage = &v
	}
	if v, err := strconv.ParseFloat(vars["ups.load"], 64); err == nil {
		info.Load = &v
	}

	return info, nil
}

// parseUPSCOutput parses "key: value" lines from upsc output.
func parseUPSCOutput(output string) map[string]string {
	vars := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ": ", 2)
		if len(parts) == 2 {
			vars[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return vars
}
