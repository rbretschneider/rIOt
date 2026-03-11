package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

// --- MockDeviceRepo ---

type MockDeviceRepo struct {
	mu      sync.RWMutex
	Devices map[string]*models.Device
	APIKeys map[string]string // hash → deviceID
	Err     error
}

func NewMockDeviceRepo() *MockDeviceRepo {
	return &MockDeviceRepo{
		Devices: make(map[string]*models.Device),
		APIKeys: make(map[string]string),
	}
}

func (m *MockDeviceRepo) Create(_ context.Context, d *models.Device) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *d
	m.Devices[d.ID] = &copy
	return nil
}

func (m *MockDeviceRepo) Update(_ context.Context, d *models.Device) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *d
	m.Devices[d.ID] = &copy
	return nil
}

func (m *MockDeviceRepo) GetByID(_ context.Context, id string) (*models.Device, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.Devices[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	copy := *d
	return &copy, nil
}

func (m *MockDeviceRepo) List(_ context.Context) ([]models.Device, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []models.Device
	for _, d := range m.Devices {
		result = append(result, *d)
	}
	return result, nil
}

func (m *MockDeviceRepo) Delete(_ context.Context, id string) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Devices, id)
	return nil
}

func (m *MockDeviceRepo) SetStatus(_ context.Context, id string, status models.DeviceStatus) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.Devices[id]; ok {
		d.Status = status
	}
	return nil
}

func (m *MockDeviceRepo) UpdateHeartbeatTime(_ context.Context, id, agentVersion string) error {
	return m.Err
}

func (m *MockDeviceRepo) UpdateTelemetryTime(_ context.Context, id string) error {
	return m.Err
}

func (m *MockDeviceRepo) UpdatePrimaryIP(_ context.Context, id, ip string) error {
	return m.Err
}

func (m *MockDeviceRepo) UpdateDockerAvailable(_ context.Context, id string, available bool) error {
	return m.Err
}

func (m *MockDeviceRepo) Summary(_ context.Context) (*models.FleetSummary, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := &models.FleetSummary{TotalDevices: len(m.Devices)}
	for _, d := range m.Devices {
		switch d.Status {
		case models.DeviceStatusOnline:
			s.OnlineCount++
		case models.DeviceStatusOffline:
			s.OfflineCount++
		case models.DeviceStatusWarning:
			s.WarningCount++
		}
	}
	return s, nil
}

func (m *MockDeviceRepo) AgentVersionSummary(_ context.Context) ([]db.AgentVersionCount, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := make(map[string]int)
	for _, d := range m.Devices {
		v := d.AgentVersion
		if v == "" {
			v = "unknown"
		}
		counts[v]++
	}
	var result []db.AgentVersionCount
	for v, c := range counts {
		result = append(result, db.AgentVersionCount{Version: v, Count: c})
	}
	return result, nil
}

func (m *MockDeviceRepo) ListByVersion(_ context.Context, version string) ([]models.Device, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []models.Device
	for _, d := range m.Devices {
		v := d.AgentVersion
		if v == "" {
			v = "unknown"
		}
		if v == version {
			result = append(result, *d)
		}
	}
	return result, nil
}

func (m *MockDeviceRepo) StoreAPIKey(_ context.Context, plaintextKey, deviceID string) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.APIKeys[plaintextKey] = deviceID
	return nil
}

func (m *MockDeviceRepo) LookupAPIKey(_ context.Context, plaintextKey string) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.APIKeys[plaintextKey]
	if !ok {
		return "", fmt.Errorf("invalid api key")
	}
	return id, nil
}

func (m *MockDeviceRepo) DeleteAPIKeysByDevice(_ context.Context, deviceID string) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.APIKeys {
		if v == deviceID {
			delete(m.APIKeys, k)
		}
	}
	return nil
}

func (m *MockDeviceRepo) FindByDeviceUUID(ctx context.Context, id string) (*models.Device, error) {
	return m.GetByID(ctx, id)
}

func (m *MockDeviceRepo) UpdateTags(_ context.Context, id string, tags []string) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.Devices[id]; ok {
		d.Tags = tags
	}
	return nil
}

// --- MockTelemetryRepo ---

type MockTelemetryRepo struct {
	Snapshots  map[string][]models.TelemetrySnapshot // deviceID → snapshots
	Heartbeats map[string][]models.Heartbeat
	Err        error
}

func NewMockTelemetryRepo() *MockTelemetryRepo {
	return &MockTelemetryRepo{
		Snapshots:  make(map[string][]models.TelemetrySnapshot),
		Heartbeats: make(map[string][]models.Heartbeat),
	}
}

func (m *MockTelemetryRepo) StoreHeartbeat(_ context.Context, hb *models.Heartbeat) error {
	if m.Err != nil {
		return m.Err
	}
	m.Heartbeats[hb.DeviceID] = append(m.Heartbeats[hb.DeviceID], *hb)
	return nil
}

func (m *MockTelemetryRepo) StoreSnapshot(_ context.Context, snap *models.TelemetrySnapshot) error {
	if m.Err != nil {
		return m.Err
	}
	m.Snapshots[snap.DeviceID] = append(m.Snapshots[snap.DeviceID], *snap)
	return nil
}

func (m *MockTelemetryRepo) GetLatestSnapshot(_ context.Context, deviceID string) (*models.TelemetrySnapshot, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	snaps := m.Snapshots[deviceID]
	if len(snaps) == 0 {
		return nil, fmt.Errorf("not found")
	}
	s := snaps[len(snaps)-1]
	return &s, nil
}

func (m *MockTelemetryRepo) GetAllLatestSnapshots(_ context.Context) ([]models.TelemetrySnapshot, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.TelemetrySnapshot
	for _, snaps := range m.Snapshots {
		if len(snaps) > 0 {
			result = append(result, snaps[len(snaps)-1])
		}
	}
	return result, nil
}

func (m *MockTelemetryRepo) GetHistory(_ context.Context, deviceID string, limit, offset int) ([]models.TelemetrySnapshot, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	snaps := m.Snapshots[deviceID]
	if offset >= len(snaps) {
		return nil, nil
	}
	end := offset + limit
	if end > len(snaps) {
		end = len(snaps)
	}
	return snaps[offset:end], nil
}

func (m *MockTelemetryRepo) GetHeartbeatHistory(_ context.Context, deviceID string, since time.Time) ([]models.Heartbeat, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Heartbeats[deviceID], nil
}

func (m *MockTelemetryRepo) PurgeHeartbeats(_ context.Context, _ time.Time) (int64, error) {
	return 0, m.Err
}

func (m *MockTelemetryRepo) PurgeSnapshots(_ context.Context, _ time.Time) (int64, error) {
	return 0, m.Err
}

// --- MockEventRepo ---

type MockEventRepo struct {
	mu     sync.Mutex
	Events []models.Event
	NextID int64
	Err    error
}

func NewMockEventRepo() *MockEventRepo {
	return &MockEventRepo{NextID: 1}
}

func (m *MockEventRepo) Create(_ context.Context, e *models.Event) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e.ID = m.NextID
	m.NextID++
	m.Events = append(m.Events, *e)
	return nil
}

func (m *MockEventRepo) ListByDevice(_ context.Context, deviceID string, limit int) ([]models.Event, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []models.Event
	for _, e := range m.Events {
		if e.DeviceID == deviceID {
			result = append(result, e)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockEventRepo) ListAll(_ context.Context, limit, offset int) ([]models.Event, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if offset >= len(m.Events) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.Events) {
		end = len(m.Events)
	}
	return m.Events[offset:end], nil
}

func (m *MockEventRepo) Purge(_ context.Context, _ time.Time) (int64, error) {
	return 0, m.Err
}

func (m *MockEventRepo) CountUnacknowledged(_ context.Context) (int, error) {
	if m.Err != nil {
		return 0, m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, e := range m.Events {
		if e.AcknowledgedAt == nil && (e.Severity == "warning" || e.Severity == "critical") {
			count++
		}
	}
	return count, nil
}

func (m *MockEventRepo) Acknowledge(_ context.Context, id int64) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for i := range m.Events {
		if m.Events[i].ID == id {
			m.Events[i].AcknowledgedAt = &now
			return nil
		}
	}
	return nil
}

func (m *MockEventRepo) AcknowledgeAll(_ context.Context) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for i := range m.Events {
		if m.Events[i].AcknowledgedAt == nil {
			m.Events[i].AcknowledgedAt = &now
		}
	}
	return nil
}

// --- MockAlertRuleRepo ---

type MockAlertRuleRepo struct {
	Rules  []models.AlertRule
	NextID int64
	Err    error
}

func NewMockAlertRuleRepo() *MockAlertRuleRepo {
	return &MockAlertRuleRepo{NextID: 1}
}

func (m *MockAlertRuleRepo) List(_ context.Context) ([]models.AlertRule, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Rules, nil
}

func (m *MockAlertRuleRepo) ListEnabled(_ context.Context) ([]models.AlertRule, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.AlertRule
	for _, r := range m.Rules {
		if r.Enabled {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *MockAlertRuleRepo) GetByID(_ context.Context, id int64) (*models.AlertRule, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for i := range m.Rules {
		if m.Rules[i].ID == id {
			return &m.Rules[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *MockAlertRuleRepo) Create(_ context.Context, rule *models.AlertRule) error {
	if m.Err != nil {
		return m.Err
	}
	rule.ID = m.NextID
	m.NextID++
	m.Rules = append(m.Rules, *rule)
	return nil
}

func (m *MockAlertRuleRepo) Update(_ context.Context, rule *models.AlertRule) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Rules {
		if m.Rules[i].ID == rule.ID {
			m.Rules[i] = *rule
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *MockAlertRuleRepo) Delete(_ context.Context, id int64) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Rules {
		if m.Rules[i].ID == id {
			m.Rules = append(m.Rules[:i], m.Rules[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

// --- MockNotifyRepo ---

type MockNotifyRepo struct {
	Channels     []models.NotificationChannel
	Logs         []models.NotificationLog
	NextChanID   int64
	NextLogID    int64
	Err          error
}

func NewMockNotifyRepo() *MockNotifyRepo {
	return &MockNotifyRepo{NextChanID: 1, NextLogID: 1}
}

func (m *MockNotifyRepo) ListChannels(_ context.Context) ([]models.NotificationChannel, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Channels, nil
}

func (m *MockNotifyRepo) ListEnabledChannels(_ context.Context) ([]models.NotificationChannel, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.NotificationChannel
	for _, ch := range m.Channels {
		if ch.Enabled {
			result = append(result, ch)
		}
	}
	return result, nil
}

func (m *MockNotifyRepo) GetChannel(_ context.Context, id int64) (*models.NotificationChannel, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for i := range m.Channels {
		if m.Channels[i].ID == id {
			return &m.Channels[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *MockNotifyRepo) CreateChannel(_ context.Context, ch *models.NotificationChannel) error {
	if m.Err != nil {
		return m.Err
	}
	ch.ID = m.NextChanID
	m.NextChanID++
	m.Channels = append(m.Channels, *ch)
	return nil
}

func (m *MockNotifyRepo) UpdateChannel(_ context.Context, ch *models.NotificationChannel) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Channels {
		if m.Channels[i].ID == ch.ID {
			m.Channels[i] = *ch
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *MockNotifyRepo) DeleteChannel(_ context.Context, id int64) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Channels {
		if m.Channels[i].ID == id {
			m.Channels = append(m.Channels[:i], m.Channels[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *MockNotifyRepo) LogNotification(_ context.Context, log *models.NotificationLog) error {
	if m.Err != nil {
		return m.Err
	}
	log.ID = m.NextLogID
	m.NextLogID++
	m.Logs = append(m.Logs, *log)
	return nil
}

func (m *MockNotifyRepo) ListNotificationLog(_ context.Context, limit, offset int) ([]models.NotificationLog, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if offset >= len(m.Logs) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.Logs) {
		end = len(m.Logs)
	}
	return m.Logs[offset:end], nil
}

func (m *MockNotifyRepo) PurgeNotificationLog(_ context.Context, _ time.Time) (int64, error) {
	return 0, m.Err
}

// --- MockCommandRepo ---

type MockCommandRepo struct {
	Commands map[string]*models.Command
	Err      error
}

func NewMockCommandRepo() *MockCommandRepo {
	return &MockCommandRepo{Commands: make(map[string]*models.Command)}
}

func (m *MockCommandRepo) Create(_ context.Context, cmd *models.Command) error {
	if m.Err != nil {
		return m.Err
	}
	copy := *cmd
	m.Commands[cmd.ID] = &copy
	return nil
}

func (m *MockCommandRepo) UpdateStatus(_ context.Context, id, status, resultMsg string) error {
	if m.Err != nil {
		return m.Err
	}
	if cmd, ok := m.Commands[id]; ok {
		cmd.Status = status
		cmd.ResultMsg = resultMsg
	}
	return nil
}

func (m *MockCommandRepo) ListPending(_ context.Context, deviceID string) ([]models.Command, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.Command
	for _, cmd := range m.Commands {
		if cmd.DeviceID == deviceID && (cmd.Status == "pending" || cmd.Status == "queued") {
			result = append(result, *cmd)
		}
	}
	return result, nil
}

func (m *MockCommandRepo) ListByDevice(_ context.Context, deviceID string, limit int) ([]models.Command, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.Command
	for _, cmd := range m.Commands {
		if cmd.DeviceID == deviceID {
			result = append(result, *cmd)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockCommandRepo) GetByID(_ context.Context, id string) (*models.Command, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	cmd, ok := m.Commands[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	copy := *cmd
	return &copy, nil
}

// --- MockProbeRepo ---

type MockProbeRepo struct {
	Probes  []models.Probe
	Results []models.ProbeResult
	NextID  int64
	Err     error
}

func NewMockProbeRepo() *MockProbeRepo {
	return &MockProbeRepo{NextID: 1}
}

func (m *MockProbeRepo) List(_ context.Context) ([]models.Probe, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Probes, nil
}

func (m *MockProbeRepo) ListEnabled(_ context.Context) ([]models.Probe, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.Probe
	for _, p := range m.Probes {
		if p.Enabled {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *MockProbeRepo) GetByID(_ context.Context, id int64) (*models.Probe, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for i := range m.Probes {
		if m.Probes[i].ID == id {
			return &m.Probes[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *MockProbeRepo) Create(_ context.Context, p *models.Probe) error {
	if m.Err != nil {
		return m.Err
	}
	p.ID = m.NextID
	m.NextID++
	m.Probes = append(m.Probes, *p)
	return nil
}

func (m *MockProbeRepo) Update(_ context.Context, p *models.Probe) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Probes {
		if m.Probes[i].ID == p.ID {
			m.Probes[i] = *p
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *MockProbeRepo) Delete(_ context.Context, id int64) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Probes {
		if m.Probes[i].ID == id {
			m.Probes = append(m.Probes[:i], m.Probes[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *MockProbeRepo) StoreResult(_ context.Context, result *models.ProbeResult) error {
	if m.Err != nil {
		return m.Err
	}
	m.Results = append(m.Results, *result)
	return nil
}

func (m *MockProbeRepo) ListResults(_ context.Context, probeID int64, limit int) ([]models.ProbeResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.ProbeResult
	for _, r := range m.Results {
		if r.ProbeID == probeID {
			result = append(result, r)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockProbeRepo) LatestResult(_ context.Context, probeID int64) (*models.ProbeResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var latest *models.ProbeResult
	for i := range m.Results {
		if m.Results[i].ProbeID == probeID {
			latest = &m.Results[i]
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("not found")
	}
	return latest, nil
}

func (m *MockProbeRepo) PurgeResults(_ context.Context, _ time.Time) (int64, error) {
	return 0, m.Err
}

// --- MockAdminRepo ---

type MockAdminRepo struct {
	PasswordHash string
	Config       map[string]string
	Err          error
}

func NewMockAdminRepo(hash string) *MockAdminRepo {
	return &MockAdminRepo{PasswordHash: hash, Config: make(map[string]string)}
}

func (m *MockAdminRepo) GetPasswordHash(_ context.Context) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	if m.PasswordHash == "" {
		return "", fmt.Errorf("not found")
	}
	return m.PasswordHash, nil
}

func (m *MockAdminRepo) SetPasswordHash(_ context.Context, hash string) error {
	if m.Err != nil {
		return m.Err
	}
	m.PasswordHash = hash
	return nil
}

func (m *MockAdminRepo) GetConfig(_ context.Context, key string) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	v, ok := m.Config[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}

func (m *MockAdminRepo) SetConfig(_ context.Context, key, value string) error {
	if m.Err != nil {
		return m.Err
	}
	m.Config[key] = value
	return nil
}

func (m *MockAdminRepo) GetConfigMap(_ context.Context, keys []string) (map[string]string, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := m.Config[k]; ok {
			result[k] = v
		}
	}
	return result, nil
}

func (m *MockAdminRepo) IsSetupComplete(_ context.Context) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	return m.Config["setup_complete"] == "true", nil
}

func (m *MockAdminRepo) GetServerTLSCert(_ context.Context) (string, string, error) {
	if m.Err != nil {
		return "", "", m.Err
	}
	return m.Config["tls_cert_pem"], m.Config["tls_key_pem"], nil
}

func (m *MockAdminRepo) StoreServerTLSCert(_ context.Context, certPEM, keyPEM string) error {
	if m.Err != nil {
		return m.Err
	}
	m.Config["tls_cert_pem"] = certPEM
	m.Config["tls_key_pem"] = keyPEM
	return nil
}

// --- MockTerminalRepo ---

type MockTerminalRepo struct {
	Sessions []struct {
		DeviceID    string
		ContainerID string
		SessionID   string
		RemoteAddr  string
		EndedAt     *time.Time
	}
	Err error
}

func NewMockTerminalRepo() *MockTerminalRepo {
	return &MockTerminalRepo{}
}

func (m *MockTerminalRepo) LogSessionStart(_ context.Context, deviceID, containerID, sessionID, remoteAddr string) error {
	if m.Err != nil {
		return m.Err
	}
	m.Sessions = append(m.Sessions, struct {
		DeviceID    string
		ContainerID string
		SessionID   string
		RemoteAddr  string
		EndedAt     *time.Time
	}{deviceID, containerID, sessionID, remoteAddr, nil})
	return nil
}

func (m *MockTerminalRepo) LogSessionEnd(_ context.Context, sessionID string) error {
	if m.Err != nil {
		return m.Err
	}
	now := time.Now()
	for i := range m.Sessions {
		if m.Sessions[i].SessionID == sessionID {
			m.Sessions[i].EndedAt = &now
		}
	}
	return nil
}

// --- MockDispatcher ---

type MockDispatcher struct {
	mu     sync.Mutex
	Alerts []models.Alert
}

func NewMockDispatcher() *MockDispatcher {
	return &MockDispatcher{}
}

func (m *MockDispatcher) Dispatch(_ context.Context, alert models.Alert) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Alerts = append(m.Alerts, alert)
}

// --- MockCARepo ---

type MockCARepo struct {
	CACertPEM     string
	CAKeyPEM      string
	Certs         []models.DeviceCert
	BootstrapKeys []models.BootstrapKey
	Err           error
}

func NewMockCARepo() *MockCARepo {
	return &MockCARepo{}
}

func (m *MockCARepo) GetCA(_ context.Context) (string, string, error) {
	if m.Err != nil {
		return "", "", m.Err
	}
	if m.CACertPEM == "" {
		return "", "", fmt.Errorf("no CA")
	}
	return m.CACertPEM, m.CAKeyPEM, nil
}

func (m *MockCARepo) StoreCA(_ context.Context, certPEM, keyPEM string) error {
	if m.Err != nil {
		return m.Err
	}
	m.CACertPEM = certPEM
	m.CAKeyPEM = keyPEM
	return nil
}

func (m *MockCARepo) StoreCert(_ context.Context, cert *models.DeviceCert) error {
	if m.Err != nil {
		return m.Err
	}
	m.Certs = append(m.Certs, *cert)
	return nil
}

func (m *MockCARepo) GetCertByDevice(_ context.Context, deviceID string) (*models.DeviceCert, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for i := range m.Certs {
		if m.Certs[i].DeviceID == deviceID {
			return &m.Certs[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *MockCARepo) GetCertBySerial(_ context.Context, serial string) (*models.DeviceCert, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for i := range m.Certs {
		if m.Certs[i].SerialNumber == serial {
			return &m.Certs[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *MockCARepo) ListCerts(_ context.Context) ([]models.DeviceCert, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Certs, nil
}

func (m *MockCARepo) RevokeCert(_ context.Context, serial string) error {
	if m.Err != nil {
		return m.Err
	}
	now := time.Now()
	for i := range m.Certs {
		if m.Certs[i].SerialNumber == serial {
			m.Certs[i].Revoked = true
			m.Certs[i].RevokedAt = &now
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *MockCARepo) ListRevokedSerials(_ context.Context) ([]string, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var serials []string
	for _, c := range m.Certs {
		if c.Revoked {
			serials = append(serials, c.SerialNumber)
		}
	}
	return serials, nil
}

func (m *MockCARepo) CreateBootstrapKey(_ context.Context, keyHash, label string, expiresAt time.Time) error {
	if m.Err != nil {
		return m.Err
	}
	m.BootstrapKeys = append(m.BootstrapKeys, models.BootstrapKey{
		KeyHash:   keyHash,
		Label:     label,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	})
	return nil
}

func (m *MockCARepo) LookupBootstrapKey(_ context.Context, keyHash string) (*models.BootstrapKey, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for i := range m.BootstrapKeys {
		if m.BootstrapKeys[i].KeyHash == keyHash {
			return &m.BootstrapKeys[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *MockCARepo) MarkBootstrapKeyUsed(_ context.Context, keyHash, deviceID string) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.BootstrapKeys {
		if m.BootstrapKeys[i].KeyHash == keyHash {
			m.BootstrapKeys[i].Used = true
			m.BootstrapKeys[i].UsedByDevice = deviceID
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *MockCARepo) ListBootstrapKeys(_ context.Context) ([]models.BootstrapKey, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.BootstrapKeys, nil
}

func (m *MockCARepo) DeleteBootstrapKey(_ context.Context, keyHash string) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.BootstrapKeys {
		if m.BootstrapKeys[i].KeyHash == keyHash {
			m.BootstrapKeys = append(m.BootstrapKeys[:i], m.BootstrapKeys[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

// --- MockDeviceLogRepo ---

type MockDeviceLogRepo struct {
	Logs map[string][]models.LogEntry
	Err  error
}

func NewMockDeviceLogRepo() *MockDeviceLogRepo {
	return &MockDeviceLogRepo{Logs: make(map[string][]models.LogEntry)}
}

func (m *MockDeviceLogRepo) InsertBatch(_ context.Context, deviceID string, entries []models.LogEntry) error {
	if m.Err != nil {
		return m.Err
	}
	m.Logs[deviceID] = append(m.Logs[deviceID], entries...)
	return nil
}

func (m *MockDeviceLogRepo) List(_ context.Context, deviceID string, priority, limit int, exact bool) ([]models.LogEntry, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.LogEntry
	for _, e := range m.Logs[deviceID] {
		if exact && e.Priority == priority || !exact && e.Priority <= priority {
			result = append(result, e)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockDeviceLogRepo) Purge(_ context.Context, _ time.Time) (int64, error) {
	return 0, m.Err
}

// --- MockAutoUpdateRepo ---

type MockAutoUpdateRepo struct {
	Policies map[string][]models.AutoUpdatePolicy // deviceID → policies
	NextID   int
	Err      error
}

func NewMockAutoUpdateRepo() *MockAutoUpdateRepo {
	return &MockAutoUpdateRepo{Policies: make(map[string][]models.AutoUpdatePolicy), NextID: 1}
}

func (m *MockAutoUpdateRepo) ListByDevice(_ context.Context, deviceID string) ([]models.AutoUpdatePolicy, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Policies[deviceID], nil
}

func (m *MockAutoUpdateRepo) Upsert(_ context.Context, p *models.AutoUpdatePolicy) error {
	if m.Err != nil {
		return m.Err
	}
	policies := m.Policies[p.DeviceID]
	for i, existing := range policies {
		if existing.Target == p.Target {
			policies[i] = *p
			m.Policies[p.DeviceID] = policies
			return nil
		}
	}
	p.ID = m.NextID
	m.NextID++
	m.Policies[p.DeviceID] = append(m.Policies[p.DeviceID], *p)
	return nil
}

func (m *MockAutoUpdateRepo) Delete(_ context.Context, deviceID, target string) error {
	if m.Err != nil {
		return m.Err
	}
	policies := m.Policies[deviceID]
	for i, p := range policies {
		if p.Target == target {
			m.Policies[deviceID] = append(policies[:i], policies[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MockAutoUpdateRepo) SetLastTriggered(_ context.Context, id int) error {
	return m.Err
}

// --- MockContainerMetricRepo ---

type MockContainerMetricRepo struct {
	Metrics map[string][]models.ContainerMetric // "deviceID:containerName" → metrics
	Err     error
}

func NewMockContainerMetricRepo() *MockContainerMetricRepo {
	return &MockContainerMetricRepo{Metrics: make(map[string][]models.ContainerMetric)}
}

func (m *MockContainerMetricRepo) StoreBatch(_ context.Context, deviceID string, metrics []models.ContainerMetric) error {
	if m.Err != nil {
		return m.Err
	}
	for _, met := range metrics {
		key := deviceID + ":" + met.ContainerName
		m.Metrics[key] = append(m.Metrics[key], met)
	}
	return nil
}

func (m *MockContainerMetricRepo) GetHistory(_ context.Context, deviceID, containerName string, since time.Time) ([]models.ContainerMetric, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	key := deviceID + ":" + containerName
	var result []models.ContainerMetric
	for _, met := range m.Metrics[key] {
		if !met.Timestamp.Before(since) {
			result = append(result, met)
		}
	}
	return result, nil
}

func (m *MockContainerMetricRepo) Purge(_ context.Context, _ time.Time) (int64, error) {
	return 0, m.Err
}

// Compile-time interface conformance checks.
var (
	_ db.DeviceRepository    = (*MockDeviceRepo)(nil)
	_ db.TelemetryRepository = (*MockTelemetryRepo)(nil)
	_ db.EventRepository     = (*MockEventRepo)(nil)
	_ db.AlertRuleRepository = (*MockAlertRuleRepo)(nil)
	_ db.NotifyRepository    = (*MockNotifyRepo)(nil)
	_ db.CommandRepository   = (*MockCommandRepo)(nil)
	_ db.ProbeRepository     = (*MockProbeRepo)(nil)
	_ db.AdminRepository     = (*MockAdminRepo)(nil)
	_ db.TerminalRepository  = (*MockTerminalRepo)(nil)
	_ db.CARepository        = (*MockCARepo)(nil)
	_ db.DeviceLogRepository = (*MockDeviceLogRepo)(nil)
	_ db.AutoUpdateRepository      = (*MockAutoUpdateRepo)(nil)
	_ db.ContainerMetricRepository = (*MockContainerMetricRepo)(nil)
)
