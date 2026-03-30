package collectors

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// parseNvidiaSMIOutput parses the CSV output of nvidia-smi --query-gpu into a
// slice of GPUDeviceMetrics. Each non-empty line is expected to have exactly 12
// comma-separated fields in the query order:
//
//	index, name, uuid, temperature.gpu, fan.speed, utilization.gpu,
//	utilization.memory, memory.used, memory.total, power.draw, power.limit,
//	pci.bus_id
//
// Lines with an unexpected field count are skipped with a warning.
// Numeric fields that report [Not Supported] or N/A are left as nil.
func parseNvidiaSMIOutput(output string) []models.GPUDeviceMetrics {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	gpus := make([]models.GPUDeviceMetrics, 0, len(lines))

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) != 12 {
			slog.Warn("gpu collector: unexpected field count", "line", lineNum+1, "got", len(fields))
			continue
		}

		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}

		// Field 0: index (required, always an integer)
		idx, err := strconv.Atoi(fields[0])
		if err != nil {
			slog.Warn("gpu collector: unexpected field count", "line", lineNum+1, "got", len(fields))
			continue
		}

		gpu := models.GPUDeviceMetrics{
			Index:    idx,
			Name:     fields[1],
			UUID:     fields[2],
			PCIBusID: fields[11],
		}

		// Field 3: temperature.gpu
		gpu.TemperatureC = parseIntField(fields[3])
		// Field 4: fan.speed
		gpu.FanSpeedPercent = parseIntField(fields[4])
		// Field 5: utilization.gpu
		gpu.UtilizationPct = parseIntField(fields[5])
		// Field 6: utilization.memory
		gpu.MemUtilPct = parseIntField(fields[6])
		// Field 7: memory.used
		gpu.MemUsedMiB = parseIntField(fields[7])
		// Field 8: memory.total
		gpu.MemTotalMiB = parseIntField(fields[8])
		// Field 9: power.draw
		gpu.PowerDrawW = parseFloat64Field(fields[9])
		// Field 10: power.limit
		gpu.PowerLimitW = parseFloat64Field(fields[10])

		gpus = append(gpus, gpu)
	}

	return gpus
}

// parseIntField converts a trimmed nvidia-smi field to *int.
// Returns nil for [Not Supported], N/A, empty strings, or non-integer values.
func parseIntField(s string) *int {
	if isAbsent(s) {
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &v
}

// parseFloat64Field converts a trimmed nvidia-smi field to *float64.
// Returns nil for [Not Supported], N/A, empty strings, or non-numeric values.
func parseFloat64Field(s string) *float64 {
	if isAbsent(s) {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// isAbsent returns true for the sentinel values nvidia-smi uses when a field
// is not supported or not applicable on the queried GPU.
func isAbsent(s string) bool {
	return s == "" || s == "[Not Supported]" || s == "N/A"
}
