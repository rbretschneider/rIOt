package probes

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
	"github.com/DesyncTheThird/rIOt/internal/server/websocket"
)

// Runner loads enabled probes and runs them on their configured intervals.
type Runner struct {
	repo      db.ProbeRepository
	eventRepo db.EventRepository
	hub       *websocket.Hub

	mu      sync.Mutex
	cancels map[int64]context.CancelFunc // probeID → cancel
	// Track last state for recovery detection
	lastState map[int64]bool // probeID → last success state
}

func NewRunner(repo db.ProbeRepository, eventRepo db.EventRepository, hub *websocket.Hub) *Runner {
	return &Runner{
		repo:      repo,
		eventRepo: eventRepo,
		hub:       hub,
		cancels:   make(map[int64]context.CancelFunc),
		lastState: make(map[int64]bool),
	}
}

// Start begins the probe runner. It reloads probes periodically.
func (r *Runner) Start(ctx context.Context) {
	r.reload(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.stopAll()
			return
		case <-ticker.C:
			r.reload(ctx)
		}
	}
}

func (r *Runner) reload(ctx context.Context) {
	probes, err := r.repo.ListEnabled(ctx)
	if err != nil {
		slog.Error("probe runner: list enabled", "error", err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Build set of active probe IDs
	activeIDs := make(map[int64]bool)
	for _, p := range probes {
		activeIDs[p.ID] = true
		if _, exists := r.cancels[p.ID]; !exists {
			// Start new probe
			probeCtx, cancel := context.WithCancel(ctx)
			r.cancels[p.ID] = cancel
			go r.runProbe(probeCtx, p)
		}
	}

	// Stop probes no longer enabled
	for id, cancel := range r.cancels {
		if !activeIDs[id] {
			cancel()
			delete(r.cancels, id)
		}
	}
}

func (r *Runner) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, cancel := range r.cancels {
		cancel()
		delete(r.cancels, id)
	}
}

func (r *Runner) runProbe(ctx context.Context, probe models.Probe) {
	interval := time.Duration(probe.IntervalSeconds) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	slog.Info("probe started", "id", probe.ID, "name", probe.Name, "type", probe.Type, "interval", interval)

	// Run once immediately
	r.executeProbe(ctx, probe)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.executeProbe(ctx, probe)
		}
	}
}

func (r *Runner) executeProbe(ctx context.Context, probe models.Probe) {
	timeout := time.Duration(probe.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var result *models.ProbeResult
	switch probe.Type {
	case "ping":
		result = executePing(execCtx, probe)
	case "dns":
		result = executeDNS(execCtx, probe)
	case "http":
		result = executeHTTP(execCtx, probe)
	default:
		result = &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: fmt.Sprintf("unknown probe type: %s", probe.Type),
			Metadata: make(map[string]interface{}),
		}
	}

	if err := r.repo.StoreResult(ctx, result); err != nil {
		slog.Error("probe: store result", "probe", probe.Name, "error", err)
		return
	}

	// Broadcast result to dashboard
	r.hub.BroadcastProbeResult(probe.ID, result)

	// Check for state transitions (down → up, up → down)
	r.mu.Lock()
	prevSuccess, hasPrev := r.lastState[probe.ID]
	r.lastState[probe.ID] = result.Success
	r.mu.Unlock()

	if hasPrev && prevSuccess && !result.Success {
		// Probe went down
		r.createProbeEvent(ctx, probe, models.EventProbeDown, models.SeverityWarning,
			fmt.Sprintf("Probe '%s' is down: %s", probe.Name, result.ErrorMsg))
	} else if hasPrev && !prevSuccess && result.Success {
		// Probe recovered
		r.createProbeEvent(ctx, probe, models.EventProbeRecovered, models.SeverityInfo,
			fmt.Sprintf("Probe '%s' recovered (%.1fms)", probe.Name, result.LatencyMs))
	}
}

func (r *Runner) createProbeEvent(ctx context.Context, probe models.Probe, eventType models.EventType, severity models.EventSeverity, message string) {
	evt := &models.Event{
		DeviceID:  fmt.Sprintf("probe:%d", probe.ID),
		Type:      eventType,
		Severity:  severity,
		Message:   message,
		CreatedAt: time.Now().UTC(),
	}
	if err := r.eventRepo.Create(ctx, evt); err != nil {
		slog.Error("probe: create event", "error", err)
		return
	}
	r.hub.BroadcastEvent(evt)
}

// RunNow executes a single probe immediately and returns the result.
func (r *Runner) RunNow(ctx context.Context, probe models.Probe) *models.ProbeResult {
	timeout := time.Duration(probe.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var result *models.ProbeResult
	switch probe.Type {
	case "ping":
		result = executePing(execCtx, probe)
	case "dns":
		result = executeDNS(execCtx, probe)
	case "http":
		result = executeHTTP(execCtx, probe)
	default:
		result = &models.ProbeResult{
			ProbeID:  probe.ID,
			Success:  false,
			ErrorMsg: fmt.Sprintf("unknown probe type: %s", probe.Type),
			Metadata: make(map[string]interface{}),
		}
	}

	r.repo.StoreResult(ctx, result)
	return result
}
