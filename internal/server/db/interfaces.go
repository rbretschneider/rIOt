package db

import (
	"context"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
)

// DeviceRepository defines the interface for device database operations.
type DeviceRepository interface {
	Create(ctx context.Context, d *models.Device) error
	Update(ctx context.Context, d *models.Device) error
	GetByID(ctx context.Context, id string) (*models.Device, error)
	List(ctx context.Context) ([]models.Device, error)
	Delete(ctx context.Context, id string) error
	SetStatus(ctx context.Context, id string, status models.DeviceStatus) error
	UpdateHeartbeatTime(ctx context.Context, id string, agentVersion string) error
	UpdateTelemetryTime(ctx context.Context, id string) error
	UpdatePrimaryIP(ctx context.Context, id, ip string) error
	UpdateDockerAvailable(ctx context.Context, id string, available bool, containerCount ...int) error
	Summary(ctx context.Context) (*models.FleetSummary, error)
	AgentVersionSummary(ctx context.Context) ([]AgentVersionCount, error)
	ListByVersion(ctx context.Context, version string) ([]models.Device, error)
	StoreAPIKey(ctx context.Context, plaintextKey, deviceID string) error
	LookupAPIKey(ctx context.Context, plaintextKey string) (string, error)
	DeleteAPIKeysByDevice(ctx context.Context, deviceID string) error
	FindByDeviceUUID(ctx context.Context, id string) (*models.Device, error)
	UpdateLocation(ctx context.Context, id, location string) error
	UpdateTags(ctx context.Context, id string, tags []string) error
	UpdateAutoPatch(ctx context.Context, id string, enabled bool) error
	GetAutoPatch(ctx context.Context, id string) (bool, error)
}

// TelemetryRepository defines the interface for telemetry database operations.
type TelemetryRepository interface {
	StoreHeartbeat(ctx context.Context, hb *models.Heartbeat) error
	StoreSnapshot(ctx context.Context, snap *models.TelemetrySnapshot) error
	GetLatestSnapshot(ctx context.Context, deviceID string) (*models.TelemetrySnapshot, error)
	GetAllLatestSnapshots(ctx context.Context) ([]models.TelemetrySnapshot, error)
	GetHistory(ctx context.Context, deviceID string, limit, offset int) ([]models.TelemetrySnapshot, error)
	GetHeartbeatHistory(ctx context.Context, deviceID string, since time.Time) ([]models.Heartbeat, error)
	PurgeHeartbeats(ctx context.Context, olderThan time.Time) (int64, error)
	PurgeSnapshots(ctx context.Context, olderThan time.Time) (int64, error)
}

// EventRepository defines the interface for event database operations.
type EventRepository interface {
	Create(ctx context.Context, e *models.Event) error
	ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Event, error)
	ListAll(ctx context.Context, limit, offset int) ([]models.Event, error)
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
	CountUnacknowledged(ctx context.Context) (int, error)
	Acknowledge(ctx context.Context, id int64) error
	AcknowledgeAll(ctx context.Context) error
}

// AlertRuleRepository defines the interface for alert rule database operations.
type AlertRuleRepository interface {
	List(ctx context.Context) ([]models.AlertRule, error)
	ListEnabled(ctx context.Context) ([]models.AlertRule, error)
	GetByID(ctx context.Context, id int64) (*models.AlertRule, error)
	Create(ctx context.Context, rule *models.AlertRule) error
	Update(ctx context.Context, rule *models.AlertRule) error
	Delete(ctx context.Context, id int64) error
}

// NotifyRepository defines the interface for notification database operations.
type NotifyRepository interface {
	ListChannels(ctx context.Context) ([]models.NotificationChannel, error)
	ListEnabledChannels(ctx context.Context) ([]models.NotificationChannel, error)
	GetChannel(ctx context.Context, id int64) (*models.NotificationChannel, error)
	CreateChannel(ctx context.Context, ch *models.NotificationChannel) error
	UpdateChannel(ctx context.Context, ch *models.NotificationChannel) error
	DeleteChannel(ctx context.Context, id int64) error
	LogNotification(ctx context.Context, log *models.NotificationLog) error
	ListNotificationLog(ctx context.Context, limit, offset int) ([]models.NotificationLog, error)
	PurgeNotificationLog(ctx context.Context, olderThan time.Time) (int64, error)
}

// CommandRepository defines the interface for command database operations.
type CommandRepository interface {
	Create(ctx context.Context, cmd *models.Command) error
	UpdateStatus(ctx context.Context, id, status, resultMsg string) error
	ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Command, error)
	ListPending(ctx context.Context, deviceID string) ([]models.Command, error)
	GetByID(ctx context.Context, id string) (*models.Command, error)
}

// ProbeRepository defines the interface for probe database operations.
type ProbeRepository interface {
	List(ctx context.Context) ([]models.Probe, error)
	ListEnabled(ctx context.Context) ([]models.Probe, error)
	GetByID(ctx context.Context, id int64) (*models.Probe, error)
	Create(ctx context.Context, p *models.Probe) error
	Update(ctx context.Context, p *models.Probe) error
	Delete(ctx context.Context, id int64) error
	StoreResult(ctx context.Context, result *models.ProbeResult) error
	ListResults(ctx context.Context, probeID int64, limit int) ([]models.ProbeResult, error)
	LatestResult(ctx context.Context, probeID int64) (*models.ProbeResult, error)
	SuccessRate(ctx context.Context, probeID int64) (float64, int, error)
	PurgeResults(ctx context.Context, olderThan time.Time) (int64, error)
}

// LogRepository defines the interface for server log database operations.
type LogRepository interface {
	Insert(ctx context.Context, entries []models.ServerLog) error
	List(ctx context.Context, level string, limit int, before *time.Time) ([]models.ServerLog, error)
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
}

// AdminRepository defines the interface for admin configuration operations.
type AdminRepository interface {
	GetPasswordHash(ctx context.Context) (string, error)
	SetPasswordHash(ctx context.Context, hash string) error
	GetConfig(ctx context.Context, key string) (string, error)
	SetConfig(ctx context.Context, key, value string) error
	GetConfigMap(ctx context.Context, keys []string) (map[string]string, error)
	IsSetupComplete(ctx context.Context) (bool, error)
	GetServerTLSCert(ctx context.Context) (certPEM, keyPEM string, err error)
	StoreServerTLSCert(ctx context.Context, certPEM, keyPEM string) error
}

// TerminalRepository defines the interface for terminal session audit logging.
type TerminalRepository interface {
	LogSessionStart(ctx context.Context, deviceID, containerID, sessionID, remoteAddr string) error
	LogSessionEnd(ctx context.Context, sessionID string) error
}

// CARepository defines the interface for CA, certificate, and bootstrap key operations.
type CARepository interface {
	GetCA(ctx context.Context) (certPEM, keyPEM string, err error)
	StoreCA(ctx context.Context, certPEM, keyPEM string) error
	StoreCert(ctx context.Context, cert *models.DeviceCert) error
	GetCertByDevice(ctx context.Context, deviceID string) (*models.DeviceCert, error)
	GetCertBySerial(ctx context.Context, serial string) (*models.DeviceCert, error)
	ListCerts(ctx context.Context) ([]models.DeviceCert, error)
	RevokeCert(ctx context.Context, serial string) error
	ListRevokedSerials(ctx context.Context) ([]string, error)
	CreateBootstrapKey(ctx context.Context, keyHash, label string, expiresAt time.Time) error
	LookupBootstrapKey(ctx context.Context, keyHash string) (*models.BootstrapKey, error)
	MarkBootstrapKeyUsed(ctx context.Context, keyHash, deviceID string) error
	ListBootstrapKeys(ctx context.Context) ([]models.BootstrapKey, error)
	DeleteBootstrapKey(ctx context.Context, keyHash string) error
	PurgeStaleBootstrapKeys(ctx context.Context) (int64, error)
}

// DeviceLogRepository defines the interface for device log operations.
type DeviceLogRepository interface {
	InsertBatch(ctx context.Context, deviceID string, entries []models.LogEntry) error
	List(ctx context.Context, deviceID string, priority, limit int, exact bool) ([]models.LogEntry, error)
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
}

// ContainerLogRepository defines the interface for container log operations.
type ContainerLogRepository interface {
	InsertBatch(ctx context.Context, deviceID string, entries []models.ContainerLogEntry) error
	List(ctx context.Context, deviceID, containerID string, limit int, stream string, since *time.Time) ([]models.ContainerLogEntry, error)
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
}

// ContainerMetricRepository defines the interface for container metric operations.
type ContainerMetricRepository interface {
	StoreBatch(ctx context.Context, deviceID string, metrics []models.ContainerMetric) error
	GetHistory(ctx context.Context, deviceID, containerName string, since time.Time) ([]models.ContainerMetric, error)
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
}

// AutoUpdateRepository defines the interface for auto-update policy operations.
type AutoUpdateRepository interface {
	ListByDevice(ctx context.Context, deviceID string) ([]models.AutoUpdatePolicy, error)
	Upsert(ctx context.Context, p *models.AutoUpdatePolicy) error
	Delete(ctx context.Context, deviceID, target string) error
	SetLastTriggered(ctx context.Context, id int) error
}

// DeviceProbeRepository defines the interface for device probe operations.
type DeviceProbeRepository interface {
	List(ctx context.Context, deviceID string) ([]models.DeviceProbe, error)
	ListEnabled(ctx context.Context, deviceID string) ([]models.DeviceProbe, error)
	GetByID(ctx context.Context, id int64) (*models.DeviceProbe, error)
	Create(ctx context.Context, p *models.DeviceProbe) error
	Update(ctx context.Context, p *models.DeviceProbe) error
	Delete(ctx context.Context, id int64) error
	StoreResult(ctx context.Context, result *models.DeviceProbeResult) error
	ListResults(ctx context.Context, probeID int64, limit int) ([]models.DeviceProbeResult, error)
	LatestResult(ctx context.Context, probeID int64) (*models.DeviceProbeResult, error)
	SuccessRate(ctx context.Context, probeID int64) (float64, int, error)
	PurgeResults(ctx context.Context, olderThan time.Time) (int64, error)
}

// Compile-time interface conformance checks.
var (
	_ DeviceRepository    = (*DeviceRepo)(nil)
	_ TelemetryRepository = (*TelemetryRepo)(nil)
	_ EventRepository     = (*EventRepo)(nil)
	_ AlertRuleRepository = (*AlertRuleRepo)(nil)
	_ NotifyRepository    = (*NotifyRepo)(nil)
	_ CommandRepository   = (*CommandRepo)(nil)
	_ ProbeRepository     = (*ProbeRepo)(nil)
	_ LogRepository       = (*LogRepo)(nil)
	_ AdminRepository     = (*AdminRepo)(nil)
	_ TerminalRepository  = (*TerminalRepo)(nil)
	_ CARepository        = (*CARepo)(nil)
	_ DeviceLogRepository = (*DeviceLogRepo)(nil)
	_ AutoUpdateRepository      = (*AutoUpdateRepo)(nil)
	_ ContainerLogRepository    = (*ContainerLogRepo)(nil)
	_ ContainerMetricRepository = (*ContainerMetricRepo)(nil)
	_ DeviceProbeRepository     = (*DeviceProbeRepo)(nil)
)
