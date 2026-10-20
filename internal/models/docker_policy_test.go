package models

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"
)

func TestDUX_AC137_EveryBoundHasATest(t *testing.T) {
	// Pin externally observable policy values to the approved contract. The
	// behavioral tests exercise these caps with oversized, hostile payloads.
	limits := map[string]int64{
		"StoredLogMaxLines": StoredLogMaxLines, "StoredLogMaxBytes": StoredLogMaxBytes, "StoredLogMaxLineBytes": StoredLogMaxLineBytes,
		"StoredLogDeviceMaxLines": StoredLogDeviceMaxLines, "StoredLogDeviceMaxBytes": StoredLogDeviceMaxBytes,
		"StoredLogUnknownMaxLines": StoredLogUnknownMaxLines, "StoredLogUnknownMaxBytes": StoredLogUnknownMaxBytes,
		"StoredLogGapRowsPerDevice": StoredLogGapRowsPerDevice, "StoredLogContainerLimiterMax": StoredLogContainerLimiterMax,
		"StoredLogLimiterIdle": int64(StoredLogLimiterIdle), "StoredLogWindow": int64(StoredLogWindow),
		"DeviceDailyLogBytes": DeviceDailyLogBytes, "DeviceDailyTelemetryBytes": DeviceDailyTelemetryBytes,
		"DeviceDailyDeviceLogBytes": DeviceDailyDeviceLogBytes, "DeviceDailyCommandBytes": DeviceDailyCommandBytes,
		"StdoutSampleLinesPerContainerPerMinute": StdoutSampleLinesPerContainerPerMinute,
		"TelemetryQuotaDownsampleInterval":       int64(TelemetryQuotaDownsampleInterval), "TelemetryDownsampleMaxBytes": TelemetryDownsampleMaxBytes,
		"AgentWSReadLimit": AgentWSReadLimit, "DBIdleInTxTimeout": int64(DBIdleInTxTimeout), "DBReadStatementTimeout": int64(DBReadStatementTimeout),
	}
	want := map[string]int64{
		"StoredLogMaxLines": 1000, "StoredLogMaxBytes": 256 << 10, "StoredLogMaxLineBytes": 16 << 10,
		"StoredLogDeviceMaxLines": 5000, "StoredLogDeviceMaxBytes": 2 << 20, "StoredLogUnknownMaxLines": 200, "StoredLogUnknownMaxBytes": 64 << 10,
		"StoredLogGapRowsPerDevice": 200, "StoredLogContainerLimiterMax": 512, "StoredLogLimiterIdle": int64(10 * time.Minute), "StoredLogWindow": int64(time.Minute),
		"DeviceDailyLogBytes": 512 << 20, "DeviceDailyTelemetryBytes": 1 << 30, "DeviceDailyDeviceLogBytes": 256 << 20, "DeviceDailyCommandBytes": 64 << 20,
		"StdoutSampleLinesPerContainerPerMinute": 5, "TelemetryQuotaDownsampleInterval": int64(15 * time.Minute), "TelemetryDownsampleMaxBytes": 2 << 20,
		"AgentWSReadLimit": 4 << 20, "DBIdleInTxTimeout": int64(time.Minute), "DBReadStatementTimeout": int64(30 * time.Second),
	}
	file, err := parser.ParseFile(token.NewFileSet(), "docker_policy.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		decl, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for _, name := range decl.Names {
			if !name.IsExported() {
				continue
			}
			value, exists := limits[name.Name]
			if !exists {
				t.Errorf("untested exported Docker bound %s", name.Name)
			} else if value != want[name.Name] {
				t.Errorf("Docker policy %s drifted: %d", name.Name, value)
			}
		}
		return true
	})
}
