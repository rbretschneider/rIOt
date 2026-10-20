package models

import "time"

const (
	StoredLogMaxLines                      = 1000
	StoredLogMaxBytes                      = 256 << 10
	StoredLogMaxLineBytes                  = 16 << 10
	StoredLogDeviceMaxLines                = 5000
	StoredLogDeviceMaxBytes                = 2 << 20
	StoredLogUnknownMaxLines               = 200
	StoredLogUnknownMaxBytes               = 64 << 10
	StoredLogGapRowsPerDevice              = 200
	StoredLogContainerLimiterMax           = 512
	StoredLogLimiterIdle                   = 10 * time.Minute
	StoredLogWindow                        = time.Minute
	DeviceDailyLogBytes                    = 512 << 20
	DeviceDailyTelemetryBytes              = 1 << 30
	DeviceDailyDeviceLogBytes              = 256 << 20
	DeviceDailyCommandBytes                = 64 << 20
	StdoutSampleLinesPerContainerPerMinute = 5
	TelemetryQuotaDownsampleInterval       = 15 * time.Minute
	TelemetryDownsampleMaxBytes            = 2 << 20
	AgentWSReadLimit                       = 4 << 20
	DBIdleInTxTimeout                      = time.Minute
	DBReadStatementTimeout                 = 30 * time.Second
)
