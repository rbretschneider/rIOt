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

// --- MockExpectedActivityRepo ---

// MockExpectedActivityRepo is a faithful in-memory db.ExpectedActivityRepository
// (EXPECT-001). It is linked to a MockCommandRepo (windows are commands), a
// MockEventRepo (escalations and warning events land there) and a
// MockDeviceRepo (the powered-off purge predicate reads statuses and heartbeats
// from it). It enforces the escalation marker primary key, the no-re-stamp
// upsert rule and the purge predicate, records every call and can fail any
// method (FailOn).
type MockExpectedActivityRepo struct {
	mu       sync.Mutex
	Commands *MockCommandRepo
	Events   *MockEventRepo
	Devices  *MockDeviceRepo

	// Escalations holds the markers by "commandID|target".
	Escalations map[string]*models.Escalation
	// PoweredOff holds the rows by device ID.
	PoweredOff map[string]models.PoweredOffState
	// Outcomes holds the window outcome written by CloseWindow, by command ID.
	Outcomes map[string]models.WindowOutcome

	// Before, when set, runs at the start of every call with the method name
	// (outside the mock's lock). Tests use it to advance a fake clock.
	Before func(method string)

	failOn map[string]error
	calls  []string
}

func NewMockExpectedActivityRepo(cmds *MockCommandRepo, evts *MockEventRepo, devs *MockDeviceRepo) *MockExpectedActivityRepo {
	return &MockExpectedActivityRepo{
		Commands: cmds, Events: evts, Devices: devs,
		Escalations: map[string]*models.Escalation{},
		PoweredOff:  map[string]models.PoweredOffState{},
		Outcomes:    map[string]models.WindowOutcome{},
	}
}

// FailOn makes the named method return err (nil clears it).
func (m *MockExpectedActivityRepo) FailOn(method string, err error) {
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

// CallCount returns how many times the named method was called.
func (m *MockExpectedActivityRepo) CallCount(method string) int {
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

// begin records the call and returns the injected failure. It runs the Before
// hook first, outside the lock.
func (m *MockExpectedActivityRepo) begin(method string) error {
	m.mu.Lock()
	hook := m.Before
	m.mu.Unlock()
	if hook != nil {
		hook(method)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, method)
	return m.failOn[method]
}

// EscalationMarkers returns the stored markers of a command, sorted by target.
func (m *MockExpectedActivityRepo) EscalationMarkers(commandID string) []models.Escalation {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.Escalation
	for _, e := range m.Escalations {
		if e.CommandID == commandID {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

// PoweredOffRow returns the device's powered-off row, if any (test helper; not recorded).
func (m *MockExpectedActivityRepo) PoweredOffRow(deviceID string) (models.PoweredOffState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.PoweredOff[deviceID]
	return p, ok
}

// SeedPoweredOff stores a row directly (test helper; not recorded).
func (m *MockExpectedActivityRepo) SeedPoweredOff(p models.PoweredOffState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PoweredOff[p.DeviceID] = p
}

// WindowOutcome returns the outcome written by CloseWindow for a command.
func (m *MockExpectedActivityRepo) WindowOutcome(commandID string) (models.WindowOutcome, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.Outcomes[commandID]
	return o, ok
}

func inExpectScope(action string) bool {
	_, ok := models.ExpectPolicy(action)
	return ok
}

// pendingCommands returns copies of the in-scope commands with an unprocessed
// window that satisfy keep, ordered by less.
func (m *MockExpectedActivityRepo) pendingCommands(keep func(c models.Command) bool, less func(a, b models.Command) bool, limit int) []models.Command {
	m.Commands.mu.Lock()
	var out []models.Command
	for _, c := range m.Commands.Commands {
		if c.WindowClosedAt != nil || !inExpectScope(c.Action) {
			continue
		}
		cp := copyCommand(c)
		if keep(cp) {
			out = append(out, cp)
		}
	}
	m.Commands.mu.Unlock()
	sortCommands(out, less)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func finishedOrUpdated(c models.Command) time.Time {
	if c.FinishedAt != nil {
		return *c.FinishedAt
	}
	return c.UpdatedAt
}

func (m *MockExpectedActivityRepo) ListOpenWindows(_ context.Context, now time.Time, limit int) ([]models.Command, error) {
	if err := m.begin("ListOpenWindows"); err != nil {
		return nil, err
	}
	cutoff := now.Add(-models.ExpectDeferredRecheckBound)
	return m.pendingCommands(func(c models.Command) bool {
		return c.State().IsActive() || !finishedOrUpdated(c).Before(cutoff)
	}, func(a, b models.Command) bool { return a.CreatedAt.After(b.CreatedAt) }, limit), nil
}

func (m *MockExpectedActivityRepo) ListDueBacklog(_ context.Context, _ time.Time, limit int) ([]models.Command, error) {
	if err := m.begin("ListDueBacklog"); err != nil {
		return nil, err
	}
	return m.pendingCommands(func(c models.Command) bool { return !c.State().IsActive() },
		func(a, b models.Command) bool { return finishedOrUpdated(a).Before(finishedOrUpdated(b)) }, limit), nil
}

func (m *MockExpectedActivityRepo) ListLapseCandidates(_ context.Context, cutoff time.Time, limit int) ([]models.Command, error) {
	if err := m.begin("ListLapseCandidates"); err != nil {
		return nil, err
	}
	return m.pendingCommands(func(c models.Command) bool {
		return !c.State().IsActive() && (finishedOrUpdated(c).Before(cutoff) || c.DispatchedAt == nil)
	}, func(a, b models.Command) bool { return finishedOrUpdated(a).Before(finishedOrUpdated(b)) }, limit), nil
}

func (m *MockExpectedActivityRepo) CloseWindow(_ context.Context, commandID string, at time.Time, outcome models.WindowOutcome) (bool, error) {
	if err := m.begin("CloseWindow"); err != nil {
		return false, err
	}
	m.Commands.mu.Lock()
	c, ok := m.Commands.Commands[commandID]
	applied := ok && c.WindowClosedAt == nil
	if applied {
		t := at
		c.WindowClosedAt = &t
	}
	m.Commands.mu.Unlock()
	if applied {
		m.mu.Lock()
		m.Outcomes[commandID] = outcome
		m.mu.Unlock()
	}
	return applied, nil
}

func (m *MockExpectedActivityRepo) RecordEscalation(ctx context.Context, esc *models.Escalation, evt *models.Event) (bool, error) {
	if err := m.begin("RecordEscalation"); err != nil {
		return false, err
	}
	m.Commands.mu.Lock()
	_, cmdExists := m.Commands.Commands[esc.CommandID]
	m.Commands.mu.Unlock()
	if !cmdExists {
		return false, fmt.Errorf("escalation marker: command %s does not exist (foreign key)", esc.CommandID)
	}
	key := esc.CommandID + "|" + esc.Target
	m.mu.Lock()
	if _, dup := m.Escalations[key]; dup {
		m.mu.Unlock()
		return false, nil
	}
	m.mu.Unlock()
	if err := m.Events.Create(ctx, evt); err != nil {
		return false, err
	}
	id := evt.ID
	stored := *esc
	stored.EventID = &id
	m.mu.Lock()
	m.Escalations[key] = &stored
	m.mu.Unlock()
	esc.EventID = &id
	return true, nil
}

func (m *MockExpectedActivityRepo) ListEscalationTargets(_ context.Context, commandID string) ([]string, error) {
	if err := m.begin("ListEscalationTargets"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []string{}
	for _, e := range m.Escalations {
		if e.CommandID == commandID {
			out = append(out, e.Target)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *MockExpectedActivityRepo) ListOpenDeviceEscalations(_ context.Context, since time.Time) ([]models.Escalation, error) {
	if err := m.begin("ListOpenDeviceEscalations"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []models.Escalation{}
	for _, e := range m.Escalations {
		if e.Target == models.EscalationTargetDevice && e.ClearedAt == nil && !e.CreatedAt.Before(since) {
			out = append(out, *e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *MockExpectedActivityRepo) ClearDeviceEscalations(_ context.Context, deviceID string, at time.Time) (int64, error) {
	if err := m.begin("ClearDeviceEscalations"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, e := range m.Escalations {
		if e.DeviceID == deviceID && e.Target == models.EscalationTargetDevice && e.ClearedAt == nil {
			t := at
			e.ClearedAt = &t
			n++
		}
	}
	return n, nil
}

func (m *MockExpectedActivityRepo) SuppressedDeaths(_ context.Context, deviceID, commandID string, limit int) ([]models.ContainerDeath, error) {
	if err := m.begin("SuppressedDeaths"); err != nil {
		return nil, err
	}
	m.Events.mu.Lock()
	last := map[string]time.Time{}
	for _, e := range m.Events.Events {
		if e.DeviceID == deviceID && e.CommandID == commandID && e.Type == models.EventContainerDied &&
			e.CommandAttr == models.CommandAttrExpected {
			if t, ok := last[e.ContainerName]; !ok || e.CreatedAt.After(t) {
				last[e.ContainerName] = e.CreatedAt
			}
		}
	}
	m.Events.mu.Unlock()
	out := make([]models.ContainerDeath, 0, len(last))
	for n, t := range last {
		out = append(out, models.ContainerDeath{ContainerName: n, At: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ContainerName < out[j].ContainerName })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MockExpectedActivityRepo) ContainerStartsSince(_ context.Context, deviceID string, since time.Time, limit int) (map[string]time.Time, error) {
	if err := m.begin("ContainerStartsSince"); err != nil {
		return nil, err
	}
	m.Events.mu.Lock()
	defer m.Events.mu.Unlock()
	out := map[string]time.Time{}
	for _, e := range m.Events.Events {
		if e.DeviceID == deviceID && e.Type == models.EventContainerStart && !e.CreatedAt.Before(since) {
			if t, ok := out[e.ContainerName]; !ok || e.CreatedAt.After(t) {
				out[e.ContainerName] = e.CreatedAt
			}
		}
	}
	return out, nil
}

func (m *MockExpectedActivityRepo) UpsertPoweredOff(_ context.Context, p *models.PoweredOffState) (bool, error) {
	if err := m.begin("UpsertPoweredOff"); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = p.OfflineAt
	}
	if old, ok := m.PoweredOff[p.DeviceID]; ok {
		// Same rule as the SQL: replace only by a different, newer shutdown.
		if old.CommandID == p.CommandID || !p.ShutdownAt.After(old.ShutdownAt) {
			return false, nil
		}
	}
	m.PoweredOff[p.DeviceID] = *p
	return true, nil
}

func (m *MockExpectedActivityRepo) DeletePoweredOff(_ context.Context, deviceID string) (*models.PoweredOffState, error) {
	if err := m.begin("DeletePoweredOff"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.PoweredOff[deviceID]
	if !ok {
		return nil, nil
	}
	delete(m.PoweredOff, deviceID)
	return &p, nil
}

// poweredOffInvalid is the purge predicate: the device is live, or it
// heartbeated after the row's offline_at. A row of an unknown device is kept
// (the SQL join would not see it either).
func (m *MockExpectedActivityRepo) poweredOffInvalid(p models.PoweredOffState) bool {
	m.Devices.mu.RLock()
	defer m.Devices.mu.RUnlock()
	d, ok := m.Devices.Devices[p.DeviceID]
	if !ok {
		return false
	}
	return d.Status != models.DeviceStatusOffline || (d.LastHeartbeat != nil && d.LastHeartbeat.After(p.OfflineAt))
}

func (m *MockExpectedActivityRepo) PurgeInvalidPoweredOff(_ context.Context) (int64, error) {
	if err := m.begin("PurgeInvalidPoweredOff"); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, p := range m.PoweredOff {
		if m.poweredOffInvalid(p) {
			delete(m.PoweredOff, id)
			n++
		}
	}
	return n, nil
}

func (m *MockExpectedActivityRepo) ListPoweredOff(_ context.Context) ([]models.PoweredOffState, error) {
	if err := m.begin("ListPoweredOff"); err != nil {
		return nil, err
	}
	m.mu.Lock()
	rows := make([]models.PoweredOffState, 0, len(m.PoweredOff))
	for _, p := range m.PoweredOff {
		rows = append(rows, p)
	}
	m.mu.Unlock()
	out := []models.PoweredOffState{}
	for _, p := range rows {
		m.Devices.mu.RLock()
		_, known := m.Devices.Devices[p.DeviceID]
		m.Devices.mu.RUnlock()
		if known && !m.poweredOffInvalid(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out, nil
}

func (m *MockExpectedActivityRepo) CreateWarningEvent(ctx context.Context, e *models.Event) error {
	if err := m.begin("CreateWarningEvent"); err != nil {
		return err
	}
	return m.Events.Create(ctx, e)
}

var _ db.ExpectedActivityRepository = (*MockExpectedActivityRepo)(nil)
