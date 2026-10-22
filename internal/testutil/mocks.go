package testutil

import (
	"context"
	"fmt"
	"sort"
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
	// HasAPIKeyErr, when set, makes only HasAPIKey fail (REG-HARDEN-001).
	HasAPIKeyErr error
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

func (m *MockDeviceRepo) UpdateDockerAvailable(_ context.Context, id string, available bool, containerCount, autoUpdateCount int) error {
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

// HasAPIKey reports whether any stored key is bound to deviceID. It returns
// m.Err if set, then HasAPIKeyErr, so tests can fail only this call.
func (m *MockDeviceRepo) HasAPIKey(_ context.Context, deviceID string) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	if m.HasAPIKeyErr != nil {
		return false, m.HasAPIKeyErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, id := range m.APIKeys {
		if id == deviceID {
			return true, nil
		}
	}
	return false, nil
}

func (m *MockDeviceRepo) FindByDeviceUUID(ctx context.Context, id string) (*models.Device, error) {
	return m.GetByID(ctx, id)
}

func (m *MockDeviceRepo) UpdateLocation(_ context.Context, id, location string) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.Devices[id]; ok {
		d.Location = location
	}
	return nil
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

func (m *MockDeviceRepo) UpdateAutoPatch(_ context.Context, id string, enabled bool) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.Devices[id]; ok {
		d.AutoPatch = enabled
	}
	return nil
}

func (m *MockDeviceRepo) GetAutoPatch(_ context.Context, id string) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.Devices[id]; ok {
		return d.AutoPatch, nil
	}
	return false, nil
}

// --- MockTelemetryRepo ---

type MockTelemetryRepo struct {
	Snapshots  map[string][]models.TelemetrySnapshot // deviceID → snapshots
	Heartbeats map[string][]models.Heartbeat
	Err        error
	// GetAllLatestSnapshotsCalled is set to true whenever GetAllLatestSnapshots
	// is invoked. Tests for AD-011 assert this is NOT set after calling the
	// FleetContainers handler.
	GetAllLatestSnapshotsCalled bool
	// Now, when set, makes the "latest" reads ignore snapshots stamped more than
	// models.TelemetryFutureTolerance ahead of it (SEC C-3).
	Now func() time.Time
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

// latest mirrors the SQL "latest" queries: the snapshot with the greatest
// timestamp (the later-stored one on a tie), ignoring snapshots more than
// models.TelemetryFutureTolerance ahead of Now when Now is set.
func (m *MockTelemetryRepo) latest(deviceID string) (models.TelemetrySnapshot, bool) {
	var best models.TelemetrySnapshot
	found := false
	for _, s := range m.Snapshots[deviceID] {
		if m.Now != nil && s.Timestamp.After(m.Now().Add(models.TelemetryFutureTolerance)) {
			continue
		}
		if !found || !s.Timestamp.Before(best.Timestamp) {
			best, found = s, true
		}
	}
	return best, found
}

func (m *MockTelemetryRepo) GetLatestSnapshot(_ context.Context, deviceID string) (*models.TelemetrySnapshot, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	s, ok := m.latest(deviceID)
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &s, nil
}

func (m *MockTelemetryRepo) LatestSnapshotTime(_ context.Context, deviceID string) (time.Time, bool, error) {
	if m.Err != nil {
		return time.Time{}, false, m.Err
	}
	s, ok := m.latest(deviceID)
	return s.Timestamp, ok, nil
}

func (m *MockTelemetryRepo) GetAllLatestSnapshots(_ context.Context) ([]models.TelemetrySnapshot, error) {
	// Record the call so AD-011 spy tests can assert this was NOT invoked
	// during FleetContainers handler execution.
	m.GetAllLatestSnapshotsCalled = true
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

func (m *MockTelemetryRepo) GetAllLatestSummaries(_ context.Context) ([]db.SnapshotSummary, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []db.SnapshotSummary
	for deviceID, snaps := range m.Snapshots {
		if len(snaps) == 0 {
			continue
		}
		s := snaps[len(snaps)-1]
		summary := db.SnapshotSummary{
			DeviceID: deviceID,
			Updates:  s.Data.Updates,
			Security: s.Data.Security,
		}
		if s.Data.Updates != nil {
			ts := s.Timestamp
			summary.UpdatesAt = &ts
		}
		if s.Data.WebServers != nil {
			for _, srv := range s.Data.WebServers.Servers {
				summary.WebCerts = append(summary.WebCerts, srv.Certs...)
			}
		}
		result = append(result, summary)
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

// FleetHeartbeats is the fixture map set by tests to control GetFleetHeartbeats output.
func (m *MockTelemetryRepo) GetFleetHeartbeats(_ context.Context, since time.Time) (map[string][]models.Heartbeat, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	result := make(map[string][]models.Heartbeat)
	for deviceID, hbs := range m.Heartbeats {
		for _, hb := range hbs {
			if !hb.Timestamp.Before(since) {
				result[deviceID] = append(result[deviceID], hb)
			}
		}
	}
	return result, nil
}

// GetGPUDeviceIDs returns device IDs from snapshots that have GPU telemetry.
// The mock checks the in-memory snapshot store for the presence of gpu_telemetry.gpus.
func (m *MockTelemetryRepo) GetGPUDeviceIDs(_ context.Context) ([]string, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var ids []string
	for deviceID, snaps := range m.Snapshots {
		if len(snaps) == 0 {
			continue
		}
		latest := snaps[len(snaps)-1]
		if latest.Data.GPUTelemetry != nil && len(latest.Data.GPUTelemetry.GPUs) > 0 {
			ids = append(ids, deviceID)
		}
	}
	return ids, nil
}

// FleetContainerLeaderboardFixture allows tests to set up the fixture data for
// GetFleetContainerLeaderboard without going through the snapshot store.
type FleetContainerLeaderboardFixture struct {
	Rows                        []db.FleetContainerProjection
	GetAllLatestSnapshotsCalled bool
}

// GetFleetContainerLeaderboard returns the fixture data. It is instrumented to
// record calls for the AD-011 spy-test that verifies GetAllLatestSnapshots is
// NOT called during fleet-container requests.
func (m *MockTelemetryRepo) GetFleetContainerLeaderboard(_ context.Context) ([]db.FleetContainerProjection, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	// Build a slim projection from snapshot store if no explicit fixture is set.
	var result []db.FleetContainerProjection
	for deviceID, snaps := range m.Snapshots {
		if len(snaps) == 0 {
			continue
		}
		latest := snaps[len(snaps)-1]
		if latest.Data.Docker == nil {
			continue
		}
		proj := db.FleetContainerProjection{DeviceID: deviceID}
		for _, c := range latest.Data.Docker.Containers {
			var labels map[string]string
			if c.Labels != nil {
				labels = c.Labels
			}
			updateAvail := c.UpdateAvailable
			proj.Containers = append(proj.Containers, db.FleetContainerProjectionRow{
				ID:              c.ID,
				Name:            c.Name,
				Image:           c.Image,
				State:           c.State,
				CPUPercent:      c.CPUPercent,
				MemUsage:        c.MemUsage,
				MemLimit:        c.MemLimit,
				RestartCount:    c.RestartCount,
				UpdateAvailable: updateAvail,
				Labels:          labels,
			})
		}
		result = append(result, proj)
	}
	return result, nil
}

// --- MockEventRepo ---

type MockEventRepo struct {
	mu     sync.Mutex
	Events []models.Event
	NextID int64
	Err    error
	// PurgeCalled records whether Purge was invoked (PART-001 AC-015: this
	// out-of-scope table's DELETE retention must still run).
	PurgeCalled bool
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
	m.mu.Lock()
	m.PurgeCalled = true
	m.mu.Unlock()
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
	Channels   []models.NotificationChannel
	Logs       []models.NotificationLog
	NextChanID int64
	NextLogID  int64
	Err        error

	// UpdateChannelCalls counts UpdateChannel invocations, so tests can assert
	// that a rejected request wrote nothing.
	UpdateChannelCalls int
	// PurgeNotificationLogCalled records whether PurgeNotificationLog was
	// invoked (PART-001 AC-015: this out-of-scope table's DELETE retention
	// must still run).
	PurgeNotificationLogCalled bool
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
	m.UpdateChannelCalls++
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
	m.PurgeNotificationLogCalled = true
	return 0, m.Err
}

// --- MockCommandRepo ---

// MockCommandRepo is a faithful in-memory db.CommandRepository: it enforces the
// same guards as the SQL (alias-aware status lists, device scope, claim and
// conflict semantics) under one mutex, so concurrency tests are meaningful, and
// it records every call so tests can assert "repository never called".
type MockCommandRepo struct {
	mu       sync.Mutex
	Commands map[string]*models.Command
	Outputs  map[string][]models.CommandOutput
	// Err, when set, is returned by every method.
	Err    error
	failOn map[string]error
	calls  []string
}

func NewMockCommandRepo() *MockCommandRepo {
	return &MockCommandRepo{Commands: make(map[string]*models.Command)}
}

// Seed stores c under the mock's lock. Use it instead of writing Commands
// directly whenever a goroutine (e.g. a running sweeper) may read the repo.
func (m *MockCommandRepo) Seed(c *models.Command) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Commands[c.ID] = c
}

// FailOn makes the named repository method return err (nil clears it).
func (m *MockCommandRepo) FailOn(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn == nil {
		m.failOn = map[string]error{}
	}
	if err == nil {
		delete(m.failOn, method)
		return
	}
	m.failOn[method] = err
}

// Calls returns the names of the repository methods called so far, in order.
func (m *MockCommandRepo) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// CallCount returns how many times the named method was called.
func (m *MockCommandRepo) CallCount(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if c == method {
			n++
		}
	}
	return n
}

// All returns normalized copies of every stored command (test helper; not recorded).
func (m *MockCommandRepo) All() []models.Command {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.Command, 0, len(m.Commands))
	for _, c := range m.Commands {
		out = append(out, copyCommand(c))
	}
	return out
}

// Get returns a normalized copy of a stored command (test helper; not recorded).
func (m *MockCommandRepo) Get(id string) (models.Command, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.Commands[id]
	if !ok {
		return models.Command{}, false
	}
	return copyCommand(c), true
}

// begin records the call and returns the injected failure, if any. The caller
// holds m.mu.
func (m *MockCommandRepo) begin(method string) error {
	m.calls = append(m.calls, method)
	if m.Err != nil {
		return m.Err
	}
	return m.failOn[method]
}

func copyInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyTime(p *time.Time) *time.Time {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func copyInt64(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// copyCommand returns a normalized deep-enough copy (pointers and slices are
// duplicated so callers cannot mutate the stored row).
func copyCommand(c *models.Command) models.Command {
	cp := *c
	cp.Progress = copyInt(c.Progress)
	cp.PolicyID = copyInt(c.PolicyID)
	cp.ExitCode = copyInt(c.ExitCode)
	cp.DurationMs = copyInt64(c.DurationMs)
	cp.DeadlineAt = copyTime(c.DeadlineAt)
	cp.DispatchedAt = copyTime(c.DispatchedAt)
	cp.AcknowledgedAt = copyTime(c.AcknowledgedAt)
	cp.StartedAt = copyTime(c.StartedAt)
	cp.VerifyingAt = copyTime(c.VerifyingAt)
	cp.FinishedAt = copyTime(c.FinishedAt)
	if c.TargetKeys != nil {
		cp.TargetKeys = append([]string(nil), c.TargetKeys...)
	}
	cp.Normalize()
	return cp
}

func rawStatusIn(raw string, states []models.CommandState) bool {
	for _, s := range states {
		if string(s) == raw {
			return true
		}
	}
	return false
}

var (
	mockActiveStates   = models.ExpandAliases(models.ActiveStates()...)
	mockQueuedStates   = []models.CommandState{models.CommandQueued, models.LegacyPending}
	mockInflightStates = []models.CommandState{
		models.CommandDispatched, models.LegacySent, models.CommandAcknowledged,
		models.CommandRunning, models.CommandVerifying,
	}
)

func keysOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func sortCommands(cmds []models.Command, less func(a, b models.Command) bool) {
	sort.SliceStable(cmds, func(i, j int) bool {
		if less(cmds[i], cmds[j]) {
			return true
		}
		if less(cmds[j], cmds[i]) {
			return false
		}
		return cmds[i].ID < cmds[j].ID
	})
}

func (m *MockCommandRepo) CreateIfNoConflict(_ context.Context, cmd *models.Command) (*models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("CreateIfNoConflict"); err != nil {
		return nil, err
	}
	keys := models.NormalizeTargetKeys(cmd.TargetKeys...)
	if len(keys) > 0 {
		var holder *models.Command
		for _, c := range m.Commands {
			if c.DeviceID != cmd.DeviceID || !rawStatusIn(c.Status, mockActiveStates) || !keysOverlap(c.TargetKeys, keys) {
				continue
			}
			if holder == nil || c.CreatedAt.Before(holder.CreatedAt) || (c.CreatedAt.Equal(holder.CreatedAt) && c.ID < holder.ID) {
				holder = c
			}
		}
		if holder != nil {
			existing := copyCommand(holder)
			return &existing, nil
		}
	}
	stored := *cmd
	if stored.Status == "" {
		stored.Status = string(models.CommandQueued)
	}
	if stored.Origin == "" {
		stored.Origin = models.OriginUser
	}
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}
	if stored.UpdatedAt.IsZero() {
		stored.UpdatedAt = stored.CreatedAt
	}
	stored.TargetKeys = keys
	m.Commands[stored.ID] = &stored
	cmd.Status = stored.Status
	cmd.Origin = stored.Origin
	cmd.CreatedAt = stored.CreatedAt
	cmd.UpdatedAt = stored.UpdatedAt
	cmd.TargetKeys = keys
	return nil, nil
}

func (m *MockCommandRepo) GetByID(_ context.Context, id string) (*models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("GetByID"); err != nil {
		return nil, err
	}
	c, ok := m.Commands[id]
	if !ok {
		return nil, db.ErrCommandNotFound
	}
	cp := copyCommand(c)
	return &cp, nil
}

func (m *MockCommandRepo) GetForDevice(_ context.Context, id, deviceID string) (*models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("GetForDevice"); err != nil {
		return nil, err
	}
	c, ok := m.Commands[id]
	if !ok || c.DeviceID != deviceID {
		return nil, db.ErrCommandNotFound
	}
	cp := copyCommand(c)
	return &cp, nil
}

func (m *MockCommandRepo) ListByDevice(_ context.Context, deviceID string, limit int) ([]models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("ListByDevice"); err != nil {
		return nil, err
	}
	var result []models.Command
	for _, c := range m.Commands {
		if c.DeviceID == deviceID {
			result = append(result, copyCommand(c))
		}
	}
	sortCommands(result, func(a, b models.Command) bool { return a.CreatedAt.After(b.CreatedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockCommandRepo) ListByDeviceFiltered(_ context.Context, deviceID string, limit, offset int, statuses []string, action, targetKey string) ([]models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("ListByDeviceFiltered"); err != nil {
		return nil, err
	}
	statusSet := make(map[string]bool)
	for _, s := range statuses {
		statusSet[s] = true
	}
	var result []models.Command
	for _, c := range m.Commands {
		if c.DeviceID != deviceID {
			continue
		}
		if len(statusSet) > 0 && !statusSet[c.Status] {
			continue
		}
		if action != "" && c.Action != action {
			continue
		}
		if targetKey != "" && !keysOverlap(c.TargetKeys, []string{targetKey}) {
			continue
		}
		result = append(result, copyCommand(c))
	}
	sortCommands(result, func(a, b models.Command) bool { return a.CreatedAt.After(b.CreatedAt) })
	if offset > 0 {
		if offset >= len(result) {
			return []models.Command{}, nil
		}
		result = result[offset:]
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// LatestFinishedOSUpdates mirrors the SQL: the newest os_update per device that
// finished as succeeded, failed or timed_out. Expired, cancelled and unfinished
// commands are ignored.
func (m *MockCommandRepo) LatestFinishedOSUpdates(_ context.Context) (map[string]models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("LatestFinishedOSUpdates"); err != nil {
		return nil, err
	}
	out := map[string]models.Command{}
	for _, c := range m.Commands {
		if c.Action != models.ActionOSUpdate {
			continue
		}
		switch models.CommandState(c.Status) {
		case models.CommandSucceeded, models.CommandFailed, models.CommandTimedOut:
		default:
			continue
		}
		if prev, ok := out[c.DeviceID]; !ok || c.CreatedAt.After(prev.CreatedAt) {
			out[c.DeviceID] = copyCommand(c)
		}
	}
	return out, nil
}

func (m *MockCommandRepo) ListActive(_ context.Context, deviceID, targetKey string) ([]models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("ListActive"); err != nil {
		return nil, err
	}
	result := []models.Command{}
	for _, c := range m.Commands {
		if c.DeviceID != deviceID || !rawStatusIn(c.Status, mockActiveStates) {
			continue
		}
		if targetKey != "" && !keysOverlap(c.TargetKeys, []string{targetKey}) {
			continue
		}
		result = append(result, copyCommand(c))
	}
	sortCommands(result, func(a, b models.Command) bool { return a.CreatedAt.Before(b.CreatedAt) })
	return result, nil
}

// claimLocked moves a queued, unexpired row to dispatched. The caller holds m.mu.
func (m *MockCommandRepo) claimLocked(c *models.Command, now time.Time) bool {
	if !rawStatusIn(c.Status, mockQueuedStates) || !c.ExpiresAt.After(now) {
		return false
	}
	deadline := now.Add(time.Duration(c.ExecDeadlineS) * time.Second)
	c.Status = string(models.CommandDispatched)
	c.DispatchedAt = &now
	c.DeadlineAt = &deadline
	c.UpdatedAt = now
	return true
}

func (m *MockCommandRepo) ClaimQueued(_ context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("ClaimQueued"); err != nil {
		return nil, err
	}
	var candidates []*models.Command
	for _, c := range m.Commands {
		if c.DeviceID == deviceID && rawStatusIn(c.Status, mockQueuedStates) && c.ExpiresAt.After(now) {
			candidates = append(candidates, c)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	claimed := []models.Command{}
	for _, c := range candidates {
		if len(claimed) >= limit {
			break
		}
		if m.claimLocked(c, now) {
			claimed = append(claimed, copyCommand(c))
		}
	}
	return claimed, nil
}

func (m *MockCommandRepo) ClaimByID(_ context.Context, id string, now time.Time) (*models.Command, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("ClaimByID"); err != nil {
		return nil, false, err
	}
	c, ok := m.Commands[id]
	if !ok || !m.claimLocked(c, now) {
		return nil, false, nil
	}
	cp := copyCommand(c)
	return &cp, true, nil
}

func (m *MockCommandRepo) Transition(_ context.Context, t models.CommandTransition) (*models.Command, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("Transition"); err != nil {
		return nil, false, err
	}
	guard := t.GuardStates()
	if len(guard) == 0 || t.Now.IsZero() {
		return nil, false, nil
	}
	c, ok := m.Commands[t.ID]
	if !ok || (t.DeviceID != "" && c.DeviceID != t.DeviceID) || !rawStatusIn(c.Status, guard) {
		return nil, false, nil
	}
	to := t.To.Canonical()
	now := t.Now
	c.Status = string(to)
	c.UpdatedAt = now
	if t.Step != nil {
		c.Step = *t.Step
	}
	if t.ProgressSet {
		c.Progress = copyInt(t.Progress)
	}
	if t.ResultMsg != nil {
		c.ResultMsg = *t.ResultMsg
	}
	if t.FailureStep != nil {
		c.FailureStep = *t.FailureStep
	}
	if t.StderrTail != nil {
		c.StderrTail = *t.StderrTail
	}
	if t.ExitCode != nil {
		c.ExitCode = copyInt(t.ExitCode)
	}
	if t.DurationMs != nil {
		c.DurationMs = copyInt64(t.DurationMs)
	}
	switch to {
	case models.CommandAcknowledged:
		c.AcknowledgedAt = &now
	case models.CommandRunning:
		if c.StartedAt == nil {
			c.StartedAt = &now
		}
	case models.CommandVerifying:
		if c.StartedAt == nil {
			c.StartedAt = &now
		}
		c.VerifyingAt = &now
	case models.CommandSucceeded, models.CommandFailed, models.CommandTimedOut,
		models.CommandCancelled, models.CommandExpired:
		c.FinishedAt = &now
	case models.CommandQueued:
		c.DispatchedAt = nil
		c.DeadlineAt = nil
		c.SendAmbiguous = true
	case models.CommandDispatched:
		deadline := now.Add(time.Duration(c.ExecDeadlineS) * time.Second)
		c.DispatchedAt = &now
		c.DeadlineAt = &deadline
	}
	if t.Actor == models.ActorAgent {
		if c.DispatchedAt == nil {
			c.DispatchedAt = &now
		}
		if c.DeadlineAt == nil {
			deadline := now.Add(time.Duration(c.ExecDeadlineS) * time.Second)
			c.DeadlineAt = &deadline
		}
	}
	cp := copyCommand(c)
	return &cp, true, nil
}

func (m *MockCommandRepo) ListExpiredQueued(_ context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("ListExpiredQueued"); err != nil {
		return nil, err
	}
	result := []models.Command{}
	for _, c := range m.Commands {
		if (deviceID == "" || c.DeviceID == deviceID) && rawStatusIn(c.Status, mockQueuedStates) && !c.ExpiresAt.After(now) {
			result = append(result, copyCommand(c))
		}
	}
	sortCommands(result, func(a, b models.Command) bool { return a.ExpiresAt.Before(b.ExpiresAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockCommandRepo) ListOverdue(_ context.Context, deviceID string, now time.Time, limit int) ([]models.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("ListOverdue"); err != nil {
		return nil, err
	}
	result := []models.Command{}
	for _, c := range m.Commands {
		if (deviceID == "" || c.DeviceID == deviceID) && rawStatusIn(c.Status, mockInflightStates) &&
			c.DeadlineAt != nil && !c.DeadlineAt.After(now) {
			result = append(result, copyCommand(c))
		}
	}
	sortCommands(result, func(a, b models.Command) bool { return a.DeadlineAt.Before(*b.DeadlineAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockCommandRepo) SaveCommandOutput(_ context.Context, output *models.CommandOutput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("SaveCommandOutput"); err != nil {
		return err
	}
	if m.Outputs == nil {
		m.Outputs = make(map[string][]models.CommandOutput)
	}
	output.ID = int64(len(m.Outputs[output.CommandID]) + 1)
	output.CreatedAt = time.Now()
	m.Outputs[output.CommandID] = append(m.Outputs[output.CommandID], *output)
	return nil
}

func (m *MockCommandRepo) GetCommandOutput(_ context.Context, commandID string) ([]models.CommandOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.begin("GetCommandOutput"); err != nil {
		return nil, err
	}
	if m.Outputs == nil {
		return nil, nil
	}
	return m.Outputs[commandID], nil
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

func (m *MockProbeRepo) SuccessRate(_ context.Context, probeID int64) (float64, int, error) {
	if m.Err != nil {
		return 0, 0, m.Err
	}
	var total, successes int
	for _, r := range m.Results {
		if r.ProbeID == probeID {
			total++
			if r.Success {
				successes++
			}
		}
	}
	if total == 0 {
		return 0, 0, nil
	}
	return float64(successes) / float64(total), total, nil
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
	// PurgeStaleBootstrapKeysCalled records whether the purge was invoked
	// (PART-001 AC-015: this out-of-scope table's retention must still run).
	PurgeStaleBootstrapKeysCalled bool
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

func (m *MockCARepo) PurgeStaleBootstrapKeys(_ context.Context) (int64, error) {
	m.PurgeStaleBootstrapKeysCalled = true
	if m.Err != nil {
		return 0, m.Err
	}
	now := time.Now()
	var kept []models.BootstrapKey
	var purged int64
	for _, k := range m.BootstrapKeys {
		if k.Used || now.After(k.ExpiresAt) {
			purged++
		} else {
			kept = append(kept, k)
		}
	}
	m.BootstrapKeys = kept
	return purged, nil
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

// policyByID returns a pointer into the stored policy slice, or nil.
func (m *MockAutoUpdateRepo) policyByID(id int) *models.AutoUpdatePolicy {
	for deviceID := range m.Policies {
		for i := range m.Policies[deviceID] {
			if m.Policies[deviceID][i].ID == id {
				return &m.Policies[deviceID][i]
			}
		}
	}
	return nil
}

// RecordSuccess mirrors AutoUpdateRepo.RecordSuccess: the cooldown starts at
// `at` and the failure backoff resets.
func (m *MockAutoUpdateRepo) RecordSuccess(_ context.Context, id int, at time.Time) error {
	if m.Err != nil {
		return m.Err
	}
	if p := m.policyByID(id); p != nil {
		t := at
		p.LastTriggeredAt = &t
		p.ConsecutiveFailures = 0
		p.LastFailureAt = nil
	}
	return nil
}

// RecordFailure mirrors AutoUpdateRepo.RecordFailure: the failure count grows,
// last_triggered_at is untouched.
func (m *MockAutoUpdateRepo) RecordFailure(_ context.Context, id int, at time.Time) error {
	if m.Err != nil {
		return m.Err
	}
	if p := m.policyByID(id); p != nil {
		t := at
		p.ConsecutiveFailures++
		p.LastFailureAt = &t
	}
	return nil
}

// --- MockContainerLogRepo ---

type MockContainerLogRepo struct {
	Logs map[string][]models.ContainerLogEntry // "deviceID:containerID" → logs
	Err  error
}

func NewMockContainerLogRepo() *MockContainerLogRepo {
	return &MockContainerLogRepo{Logs: make(map[string][]models.ContainerLogEntry)}
}

func (m *MockContainerLogRepo) InsertBatch(_ context.Context, deviceID string, entries []models.ContainerLogEntry) error {
	if m.Err != nil {
		return m.Err
	}
	for _, e := range entries {
		key := deviceID + ":" + e.ContainerID
		m.Logs[key] = append(m.Logs[key], e)
	}
	return nil
}

func (m *MockContainerLogRepo) List(_ context.Context, deviceID, containerID string, limit int, stream string, since *time.Time) ([]models.ContainerLogEntry, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	key := deviceID + ":" + containerID
	var result []models.ContainerLogEntry
	for _, e := range m.Logs[key] {
		if stream != "" && e.Stream != stream {
			continue
		}
		if since != nil && e.Timestamp.Before(*since) {
			continue
		}
		result = append(result, e)
	}
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result, nil
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

// --- MockDeviceProbeRepo ---

type MockDeviceProbeRepo struct {
	Probes  []models.DeviceProbe
	Results []models.DeviceProbeResult
	NextID  int64
	Err     error
	// PurgeResultsCalled records whether PurgeResults was invoked (PART-001
	// AC-015: device_probe_results is a DIFFERENT table from probe_results
	// and stays out of scope — its DELETE retention must still run).
	PurgeResultsCalled bool
}

func NewMockDeviceProbeRepo() *MockDeviceProbeRepo {
	return &MockDeviceProbeRepo{NextID: 1}
}

func (m *MockDeviceProbeRepo) List(_ context.Context, deviceID string) ([]models.DeviceProbe, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.DeviceProbe
	for _, p := range m.Probes {
		if p.DeviceID == deviceID {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *MockDeviceProbeRepo) ListAll(_ context.Context) ([]models.DeviceProbe, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	result := make([]models.DeviceProbe, len(m.Probes))
	copy(result, m.Probes)
	return result, nil
}

func (m *MockDeviceProbeRepo) ListEnabled(_ context.Context, deviceID string) ([]models.DeviceProbe, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.DeviceProbe
	for _, p := range m.Probes {
		if p.DeviceID == deviceID && p.Enabled {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *MockDeviceProbeRepo) GetByID(_ context.Context, id int64) (*models.DeviceProbe, error) {
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

func (m *MockDeviceProbeRepo) Create(_ context.Context, p *models.DeviceProbe) error {
	if m.Err != nil {
		return m.Err
	}
	p.ID = m.NextID
	m.NextID++
	m.Probes = append(m.Probes, *p)
	return nil
}

func (m *MockDeviceProbeRepo) Update(_ context.Context, p *models.DeviceProbe) error {
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

func (m *MockDeviceProbeRepo) Delete(_ context.Context, id int64) error {
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

func (m *MockDeviceProbeRepo) StoreResult(_ context.Context, result *models.DeviceProbeResult) error {
	if m.Err != nil {
		return m.Err
	}
	m.Results = append(m.Results, *result)
	return nil
}

func (m *MockDeviceProbeRepo) ListResults(_ context.Context, probeID int64, limit int) ([]models.DeviceProbeResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var result []models.DeviceProbeResult
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

func (m *MockDeviceProbeRepo) LatestResult(_ context.Context, probeID int64) (*models.DeviceProbeResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	var latest *models.DeviceProbeResult
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

func (m *MockDeviceProbeRepo) SuccessRate(_ context.Context, probeID int64) (float64, int, error) {
	if m.Err != nil {
		return 0, 0, m.Err
	}
	var total, successes int
	for _, r := range m.Results {
		if r.ProbeID == probeID {
			total++
			if r.Success {
				successes++
			}
		}
	}
	if total == 0 {
		return 0, 0, nil
	}
	return float64(successes) / float64(total), total, nil
}

func (m *MockDeviceProbeRepo) PurgeResults(_ context.Context, _ time.Time) (int64, error) {
	m.PurgeResultsCalled = true
	return 0, m.Err
}

// --- MockExternalIdentityRepo ---

// MockExternalIdentityRecord mirrors the external_identities table row shape
// so tests can assert on both first_login_at and last_login_at independently
// (AC-010, AC-011).
type MockExternalIdentityRecord struct {
	Issuer        string
	Subject       string
	Email         *string
	EmailVerified *bool
	FirstLoginAt  time.Time
	LastLoginAt   time.Time
}

// MockExternalIdentityRepo reproduces the upsert semantics of AD-016 in memory:
// first_login_at is preserved across repeat logins, last_login_at/email/
// email_verified are overwritten, and RecordLogin reports firstSeen correctly.
type MockExternalIdentityRepo struct {
	mu      sync.Mutex
	Records map[string]*MockExternalIdentityRecord // key: issuer + "|" + subject
	Err     error
}

func NewMockExternalIdentityRepo() *MockExternalIdentityRepo {
	return &MockExternalIdentityRepo{Records: make(map[string]*MockExternalIdentityRecord)}
}

func (m *MockExternalIdentityRepo) RecordLogin(_ context.Context, ident models.ExternalIdentity) (bool, error) {
	if m.Err != nil {
		return false, m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := ident.Issuer + "|" + ident.Subject
	existing, ok := m.Records[key]
	if !ok {
		m.Records[key] = &MockExternalIdentityRecord{
			Issuer:        ident.Issuer,
			Subject:       ident.Subject,
			Email:         ident.Email,
			EmailVerified: ident.EmailVerified,
			FirstLoginAt:  ident.LoginAt,
			LastLoginAt:   ident.LoginAt,
		}
		return true, nil
	}
	existing.Email = ident.Email
	existing.EmailVerified = ident.EmailVerified
	existing.LastLoginAt = ident.LoginAt
	return false, nil
}

// Count returns the number of distinct (issuer, subject) records — a test
// convenience for asserting "exactly one record" (AC-010, AC-011).
func (m *MockExternalIdentityRepo) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Records)
}

// Get returns the record for (issuer, subject), or nil if none exists.
func (m *MockExternalIdentityRepo) Get(issuer, subject string) *MockExternalIdentityRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Records[issuer+"|"+subject]
	if !ok {
		return nil
	}
	copy := *r
	return &copy
}

// --- Fakes for the commands.Service collaborators ---

// FakeSentCommand is one payload FakeAgents delivered.
type FakeSentCommand struct {
	DeviceID string
	Payload  models.CommandPayload
}

// FakeAgents is a commands.AgentSender. Devices are disconnected unless listed
// in Connected; SendErr makes every delivery to a connected device fail.
type FakeAgents struct {
	mu        sync.Mutex
	Connected map[string]bool
	SendErr   error
	// OnConnectCheck, when set, runs inside IsConnected (before it answers), so a
	// test can interleave an operator action between the check and the claim.
	OnConnectCheck func()
	// OnSend, when set, runs inside SendCommand before it answers, so a test can
	// interleave an agent report with the send.
	OnSend func(deviceID string, payload models.CommandPayload)
	// DisconnectAfterCheck makes SendCommand report "not connected" even though
	// IsConnected said yes (the connection vanished in between).
	DisconnectAfterCheck bool
	sent                 []FakeSentCommand
}

func NewFakeAgents(connected ...string) *FakeAgents {
	f := &FakeAgents{Connected: map[string]bool{}}
	for _, id := range connected {
		f.Connected[id] = true
	}
	return f
}

func (f *FakeAgents) IsConnected(deviceID string) bool {
	f.mu.Lock()
	hook := f.OnConnectCheck
	connected := f.Connected[deviceID]
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return connected
}

func (f *FakeAgents) SendCommand(deviceID string, payload models.CommandPayload) (bool, error) {
	f.mu.Lock()
	connected := f.Connected[deviceID] && !f.DisconnectAfterCheck
	sendErr := f.SendErr
	hook := f.OnSend
	f.mu.Unlock()
	if !connected {
		return false, nil
	}
	if hook != nil {
		hook(deviceID, payload)
	}
	if sendErr != nil {
		return false, sendErr
	}
	f.mu.Lock()
	f.sent = append(f.sent, FakeSentCommand{DeviceID: deviceID, Payload: payload})
	f.mu.Unlock()
	return true, nil
}

// Sent returns the payloads delivered so far.
func (f *FakeAgents) Sent() []FakeSentCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeSentCommand, len(f.sent))
	copy(out, f.sent)
	return out
}

// FakeBroadcaster is a commands.Broadcaster that records every update.
type FakeBroadcaster struct {
	mu      sync.Mutex
	updates []models.CommandUpdate
}

func (f *FakeBroadcaster) BroadcastCommandUpdate(u models.CommandUpdate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, u)
}

// Updates returns the recorded broadcasts in order.
func (f *FakeBroadcaster) Updates() []models.CommandUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]models.CommandUpdate, len(f.updates))
	copy(out, f.updates)
	return out
}

// FakeCompletion is one recorded CommandCompleted call.
type FakeCompletion struct {
	DeviceID, Action, Status, Message string
}

// FakeCompletionEvents is a commands.CompletionEvents that records its calls.
type FakeCompletionEvents struct {
	mu    sync.Mutex
	calls []FakeCompletion
}

func (f *FakeCompletionEvents) CommandCompleted(_ context.Context, deviceID, action, status, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCompletion{DeviceID: deviceID, Action: action, Status: status, Message: message})
}

// Calls returns the recorded completions in order.
func (f *FakeCompletionEvents) Calls() []FakeCompletion {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeCompletion, len(f.calls))
	copy(out, f.calls)
	return out
}

// --- MockLogRepo ---

// MockLogRepo is a minimal in-memory stand-in for db.LogRepository, used by
// the PART-001 retention wiring test (AC-015: server logs are an
// out-of-scope table and must keep their existing DELETE retention).
type MockLogRepo struct {
	mu   sync.Mutex
	Logs []models.ServerLog
	Err  error
	// PurgeCalled records whether Purge was invoked.
	PurgeCalled bool
}

func NewMockLogRepo() *MockLogRepo {
	return &MockLogRepo{}
}

func (m *MockLogRepo) Insert(_ context.Context, entries []models.ServerLog) error {
	if m.Err != nil {
		return m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Logs = append(m.Logs, entries...)
	return nil
}

func (m *MockLogRepo) List(_ context.Context, level string, limit int, before *time.Time) ([]models.ServerLog, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Logs, nil
}

func (m *MockLogRepo) CountSince(_ context.Context, level string, since time.Time) (int, error) {
	if m.Err != nil {
		return 0, m.Err
	}
	return len(m.Logs), nil
}

func (m *MockLogRepo) Purge(_ context.Context, _ time.Time) (int64, error) {
	m.mu.Lock()
	m.PurgeCalled = true
	m.mu.Unlock()
	return 0, m.Err
}

func (m *MockLogRepo) PurgeAll(_ context.Context) (int64, error) {
	if m.Err != nil {
		return 0, m.Err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := int64(len(m.Logs))
	m.Logs = nil
	return n, nil
}

// Compile-time interface conformance checks.
var (
	_ db.DeviceRepository           = (*MockDeviceRepo)(nil)
	_ db.TelemetryRepository        = (*MockTelemetryRepo)(nil)
	_ db.EventRepository            = (*MockEventRepo)(nil)
	_ db.AlertRuleRepository        = (*MockAlertRuleRepo)(nil)
	_ db.NotifyRepository           = (*MockNotifyRepo)(nil)
	_ db.CommandRepository          = (*MockCommandRepo)(nil)
	_ db.ProbeRepository            = (*MockProbeRepo)(nil)
	_ db.AdminRepository            = (*MockAdminRepo)(nil)
	_ db.TerminalRepository         = (*MockTerminalRepo)(nil)
	_ db.CARepository               = (*MockCARepo)(nil)
	_ db.LogRepository              = (*MockLogRepo)(nil)
	_ db.DeviceLogRepository        = (*MockDeviceLogRepo)(nil)
	_ db.AutoUpdateRepository       = (*MockAutoUpdateRepo)(nil)
	_ db.ContainerLogRepository     = (*MockContainerLogRepo)(nil)
	_ db.ContainerMetricRepository  = (*MockContainerMetricRepo)(nil)
	_ db.DeviceProbeRepository      = (*MockDeviceProbeRepo)(nil)
	_ db.ExternalIdentityRepository = (*MockExternalIdentityRepo)(nil)
)
