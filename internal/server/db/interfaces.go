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
	UpdateDockerAvailable(ctx context.Context, id string, available bool, containerCount, autoUpdateCount int) error
	Summary(ctx context.Context) (*models.FleetSummary, error)
	AgentVersionSummary(ctx context.Context) ([]AgentVersionCount, error)
	ListByVersion(ctx context.Context, version string) ([]models.Device, error)
	StoreAPIKey(ctx context.Context, plaintextKey, deviceID string) error
	LookupAPIKey(ctx context.Context, plaintextKey string) (string, error)
	DeleteAPIKeysByDevice(ctx context.Context, deviceID string) error
	// HasAPIKey reports whether the device has at least one API key (REG-HARDEN-001).
	HasAPIKey(ctx context.Context, deviceID string) (bool, error)
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
	// LatestSnapshotTime is the timestamp of the device's current telemetry state
	// (snapshots more than models.TelemetryFutureTolerance ahead of now are
	// ignored); ok is false when the device has none.
	LatestSnapshotTime(ctx context.Context, deviceID string) (ts time.Time, ok bool, err error)
	GetAllLatestSnapshots(ctx context.Context) ([]models.TelemetrySnapshot, error)
	GetAllLatestSummaries(ctx context.Context) ([]SnapshotSummary, error)
	GetHistory(ctx context.Context, deviceID string, limit, offset int) ([]models.TelemetrySnapshot, error)
	GetHeartbeatHistory(ctx context.Context, deviceID string, since time.Time) ([]models.Heartbeat, error)
	// GetFleetHeartbeats returns heartbeats for all devices since the given time,
	// grouped by device ID. Used by the fleet dashboard endpoint.
	GetFleetHeartbeats(ctx context.Context, since time.Time) (map[string][]models.Heartbeat, error)
	// GetGPUDeviceIDs returns the IDs of devices whose latest telemetry snapshot
	// contains GPU data (gpu_telemetry.gpus length > 0).
	GetGPUDeviceIDs(ctx context.Context) ([]string, error)
	// GetFleetContainerLeaderboard returns a slim container projection per device
	// from the latest telemetry snapshots. Uses JSONB projection to avoid decoding
	// full blobs (AD-011).
	GetFleetContainerLeaderboard(ctx context.Context) ([]FleetContainerProjection, error)
}

// FleetContainerProjection holds the per-device container data returned by the
// projected leaderboard query. Only the fields the leaderboard renders are
// included; full telemetry blobs are never decoded server-side (AD-011).
type FleetContainerProjection struct {
	DeviceID   string
	Containers []FleetContainerProjectionRow
}

// FleetContainerProjectionRow is the per-container slice of a
// FleetContainerProjection, mirroring only the fields the dashboard leaderboard
// requires.
type FleetContainerProjectionRow struct {
	ShortID            string               `json:"short_id"`
	ImageID            string               `json:"image_id,omitempty"`
	Status             string               `json:"status"`
	HealthStatus       string               `json:"health_status,omitempty"`
	StartedAt          string               `json:"started_at,omitempty"`
	FinishedAt         string               `json:"finished_at,omitempty"`
	ExitCode           *int                 `json:"exit_code,omitempty"`
	Ports              []models.PortMapping `json:"ports"`
	UpdateStatus       string               `json:"update_status,omitempty"`
	UpdateReason       string               `json:"update_reason,omitempty"`
	NetworkMode        string               `json:"network_mode,omitempty"`
	TargetKey          string               `json:"target_key"`
	StackKey           string               `json:"stack_key,omitempty"`
	RiotName           string               `json:"riot_name,omitempty"`
	RiotIcon           string               `json:"riot_icon,omitempty"`
	RiotGroup          string               `json:"riot_group,omitempty"`
	RiotTags           []string             `json:"riot_tags,omitempty"`
	MemLimitConfigured int64                `json:"mem_limit_configured,omitempty"`
	NetworkParent      string               `json:"network_parent,omitempty"`
	SelfRole           string               `json:"self_role,omitempty"`

	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Image           string  `json:"image"`
	State           string  `json:"state"`
	CPUPercent      float64 `json:"cpu_percent"`
	MemUsage        int64   `json:"mem_usage"`
	MemLimit        int64   `json:"mem_limit"`
	RestartCount    int     `json:"restart_count"`
	UpdateAvailable *bool   `json:"update_available,omitempty"`
	// Labels is retained solely to extract com.docker.compose.project for the
	// stack column. The handler MUST NOT serialize any other label key (AD-011/AD-012).
	Labels map[string]string `json:"labels,omitempty"`
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
	// HasCommandEvent reports whether an event of the given type already exists
	// for the command on the device (EXTRA-UPD-001 SEC-005 dedupe).
	HasCommandEvent(ctx context.Context, deviceID, commandID string, eventType models.EventType) (bool, error)
	// ListByCommandSplit returns the events linked to one command of one device in
	// two groups, oldest first: every server-authored event (at most serverLimit)
	// and the agent-sourced ones (at most agentLimit), with the number of matching
	// rows that were not returned (EXPECT-002 Section 6.3, C-3).
	ListByCommandSplit(ctx context.Context, deviceID, commandID string, serverLimit, agentLimit int) (models.CommandEvents, error)
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
//
// Every status change is a guarded transition (models.CommandTransition): the
// database enforces the lifecycle state machine with
// UPDATE ... WHERE status IN (allowed sources). Methods that report "not
// applied" return a nil command and applied=false rather than an error.
type CommandRepository interface {
	// CreateIfNoConflict inserts cmd unless a non-terminal command on the same
	// device already holds one of its target keys; a non-nil result is the
	// existing holder and no row is created.
	CreateIfNoConflict(ctx context.Context, cmd *models.Command) (conflict *models.Command, err error)
	// GetByID returns ErrCommandNotFound when there is no such command.
	GetByID(ctx context.Context, id string) (*models.Command, error)
	// GetForDevice is GetByID scoped to a device in SQL: a foreign command reads
	// like a missing one (ErrCommandNotFound).
	GetForDevice(ctx context.Context, id, deviceID string) (*models.Command, error)
	ListByDevice(ctx context.Context, deviceID string, limit int) ([]models.Command, error)
	// ListByDeviceFiltered filters by status (already alias-expanded, bound as an
	// array), action and target key (bound).
	ListByDeviceFiltered(ctx context.Context, deviceID string, limit, offset int, statuses []string, action, targetKey string) ([]models.Command, error)
	// ListActive returns the device's non-terminal commands, optionally only
	// those holding targetKey.
	ListActive(ctx context.Context, deviceID, targetKey string) ([]models.Command, error)
	// LatestFinishedOSUpdates returns, per device, the newest os_update that
	// finished as succeeded, failed or timed_out. Expired, cancelled and
	// unfinished commands are ignored; devices with none are absent.
	LatestFinishedOSUpdates(ctx context.Context) (map[string]models.Command, error)
	// ClaimQueued atomically moves up to limit queued, unexpired commands of the
	// device to dispatched (heartbeat delivery), ordered by created_at.
	ClaimQueued(ctx context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error)
	// ClaimByID atomically moves one queued, unexpired command to dispatched.
	ClaimByID(ctx context.Context, id string, now time.Time) (cmd *models.Command, claimed bool, err error)
	// Transition applies one guarded state change.
	Transition(ctx context.Context, t models.CommandTransition) (cmd *models.Command, applied bool, err error)
	// ListExpiredQueued returns queued commands past expires_at; deviceID "" means all.
	ListExpiredQueued(ctx context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error)
	// ListOverdue returns in-flight commands past deadline_at; deviceID "" means all.
	ListOverdue(ctx context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error)
	SaveCommandOutput(ctx context.Context, output *models.CommandOutput) error
	GetCommandOutput(ctx context.Context, commandID string) ([]models.CommandOutput, error)
}

// SelfUpdateRepository is the persistence of agent self-update records
// (EXPECT-002): commands rows with origin agent_self_update. CommandRepo
// implements it.
type SelfUpdateRepository interface {
	// CreateSelfUpdate recognises one validated start in one transaction under the
	// per-device command lock: attribution to an agent_update command (FR-004),
	// retirement of a stale open record (C-4), the one-open-record rule (FR-006),
	// the rate cap with its once-per-24h event (FR-007) and the insert (AD-002).
	CreateSelfUpdate(ctx context.Context, s models.SelfUpdateStart) (models.SelfUpdateCreateResult, error)
	// OpenSelfUpdate returns the device's running or verifying record, or nil.
	OpenSelfUpdate(ctx context.Context, deviceID string) (*models.Command, error)
	// LatestSelfUpdateSince returns the device's newest record created at or after
	// since, or nil.
	LatestSelfUpdateSince(ctx context.Context, deviceID string, since time.Time) (*models.Command, error)
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
}

// LogRepository defines the interface for server log database operations.
type LogRepository interface {
	Insert(ctx context.Context, entries []models.ServerLog) error
	List(ctx context.Context, level string, limit int, before *time.Time) ([]models.ServerLog, error)
	CountSince(ctx context.Context, level string, since time.Time) (int, error)
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
	PurgeAll(ctx context.Context) (int64, error)
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
}

// ContainerLogRepository defines the interface for container log operations.
type ContainerLogRepository interface {
	InsertBatch(ctx context.Context, deviceID string, entries []models.ContainerLogEntry) error
	List(ctx context.Context, deviceID, containerID string, limit int, stream string, since *time.Time) ([]models.ContainerLogEntry, error)
}

// ContainerMetricRepository defines the interface for container metric operations.
type ContainerMetricRepository interface {
	StoreBatch(ctx context.Context, deviceID string, metrics []models.ContainerMetric) error
	GetHistory(ctx context.Context, deviceID, containerName string, since time.Time) ([]models.ContainerMetric, error)
}

// AutoUpdateRepository defines the interface for auto-update policy operations.
type AutoUpdateRepository interface {
	ListByDevice(ctx context.Context, deviceID string) ([]models.AutoUpdatePolicy, error)
	// ListAll returns the policies of every device in one query.
	ListAll(ctx context.Context) ([]models.AutoUpdatePolicy, error)
	Upsert(ctx context.Context, p *models.AutoUpdatePolicy) error
	// UpsertUnlessKindClash stores p unless an enabled policy of the other kind
	// already uses the same target on the device; stored reports which happened.
	UpsertUnlessKindClash(ctx context.Context, p *models.AutoUpdatePolicy) (stored bool, err error)
	Delete(ctx context.Context, deviceID, target string) error
	// RecordSuccess starts the cooldown at `at` and resets the failure backoff
	// (AD-007). RecordFailure increments the consecutive-failure count and
	// stamps last_failure_at.
	RecordSuccess(ctx context.Context, id int, at time.Time) error
	RecordFailure(ctx context.Context, id int, at time.Time) error
}

// DeviceProbeRepository defines the interface for device probe operations.
type DeviceProbeRepository interface {
	List(ctx context.Context, deviceID string) ([]models.DeviceProbe, error)
	ListAll(ctx context.Context) ([]models.DeviceProbe, error)
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

// ExternalIdentityRepository defines the interface for OIDC identity audit
// operations. RecordLogin's firstSeen return value drives the OIDC-001 AD-021
// admission warning and is only meaningful when err == nil.
type ExternalIdentityRepository interface {
	// RecordLogin upserts the identity and reports whether this (issuer, subject)
	// had never been seen before. firstSeen drives the AD-021 admission warning and
	// is only meaningful when err == nil.
	RecordLogin(ctx context.Context, ident models.ExternalIdentity) (firstSeen bool, err error)
}

// ExpectedActivityRepository is the persistence of expected-activity windows
// (EXPECT-001): the processed marker, exactly-once escalation markers and the
// powered-off state. See ExpectRepo for the bounds of every table.
type ExpectedActivityRepository interface {
	// ListOpenWindows returns unprocessed in-scope commands that are active or
	// finished within 24 h, newest first.
	ListOpenWindows(ctx context.Context, now time.Time, limit int) ([]models.Command, error)
	// ListDueBacklog returns unprocessed terminal in-scope commands, oldest finish first.
	ListDueBacklog(ctx context.Context, now time.Time, limit int) ([]models.Command, error)
	// ListLapseCandidates returns unprocessed terminal commands older than cutoff,
	// or terminal without a dispatch time.
	ListLapseCandidates(ctx context.Context, cutoff time.Time, limit int) ([]models.Command, error)
	// CloseWindow marks a window processed; applied is false when it already was.
	CloseWindow(ctx context.Context, commandID string, at time.Time, outcome models.WindowOutcome) (applied bool, err error)
	// RecordEscalation inserts the marker and the event in one transaction;
	// inserted is false when the marker already exists.
	RecordEscalation(ctx context.Context, esc *models.Escalation, evt *models.Event) (inserted bool, err error)
	ListEscalationTargets(ctx context.Context, commandID string) ([]string, error)
	ListOpenDeviceEscalations(ctx context.Context, since time.Time) ([]models.Escalation, error)
	ClearDeviceEscalations(ctx context.Context, deviceID string, at time.Time) (int64, error)
	SuppressedDeaths(ctx context.Context, deviceID, commandID string, limit int) ([]models.ContainerDeath, error)
	ContainerStartsSince(ctx context.Context, deviceID string, since time.Time, limit int) (map[string]time.Time, error)
	// UpsertPoweredOff never re-stamps a row of the same command (AD-007).
	UpsertPoweredOff(ctx context.Context, p *models.PoweredOffState) (stored bool, err error)
	DeletePoweredOff(ctx context.Context, deviceID string) (*models.PoweredOffState, error)
	// PurgeInvalidPoweredOff deletes rows whose device is live or heartbeated after offline_at.
	PurgeInvalidPoweredOff(ctx context.Context) (int64, error)
	// ListPoweredOff returns valid rows only.
	ListPoweredOff(ctx context.Context) ([]models.PoweredOffState, error)
	// CreateWarningEvent is a plain event insert (expect_backstop_skipped, held summary).
	CreateWarningEvent(ctx context.Context, e *models.Event) error
}

// Compile-time interface conformance checks.
var (
	_ DeviceRepository           = (*DeviceRepo)(nil)
	_ TelemetryRepository        = (*TelemetryRepo)(nil)
	_ EventRepository            = (*EventRepo)(nil)
	_ AlertRuleRepository        = (*AlertRuleRepo)(nil)
	_ NotifyRepository           = (*NotifyRepo)(nil)
	_ CommandRepository          = (*CommandRepo)(nil)
	_ ProbeRepository            = (*ProbeRepo)(nil)
	_ LogRepository              = (*LogRepo)(nil)
	_ AdminRepository            = (*AdminRepo)(nil)
	_ TerminalRepository         = (*TerminalRepo)(nil)
	_ CARepository               = (*CARepo)(nil)
	_ DeviceLogRepository        = (*DeviceLogRepo)(nil)
	_ AutoUpdateRepository       = (*AutoUpdateRepo)(nil)
	_ ContainerLogRepository     = (*ContainerLogRepo)(nil)
	_ ContainerMetricRepository  = (*ContainerMetricRepo)(nil)
	_ DeviceProbeRepository      = (*DeviceProbeRepo)(nil)
	_ ExternalIdentityRepository = (*ExternalIdentityRepo)(nil)
	_ ExpectedActivityRepository = (*ExpectRepo)(nil)
	_ SelfUpdateRepository       = (*CommandRepo)(nil)
)
