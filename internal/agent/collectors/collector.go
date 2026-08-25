package collectors

import (
	"context"
	"time"
)

// Collector is the interface all system collectors implement.
type Collector interface {
	Name() string
	Collect(ctx context.Context) (interface{}, error)
}

// Registry holds all registered collectors.
type Registry struct {
	collectors map[string]Collector
	ordered    []Collector
}

func NewRegistry() *Registry {
	return &Registry{
		collectors: make(map[string]Collector),
	}
}

func (r *Registry) Register(c Collector) {
	r.collectors[c.Name()] = c
	r.ordered = append(r.ordered, c)
}

// DockerOptions configures the Docker collector.
type DockerOptions struct {
	CollectStats bool
	SocketPath   string
	CheckUpdates bool
	CachePath    string
}

func (r *Registry) RegisterDefaults() {
	r.RegisterDefaultsWithDocker(DockerOptions{CollectStats: true})
}

func (r *Registry) RegisterDefaultsWithDocker(opts DockerOptions) {
	r.Register(&SystemCollector{})
	r.Register(&CPUCollector{})
	r.Register(&MemoryCollector{})
	r.Register(&DiskCollector{})
	r.Register(&NetworkCollector{})
	r.Register(&OSInfoCollector{})
	r.Register(&UpdatesCollector{})
	r.Register(&ServicesCollector{})
	r.Register(&ProcessesCollector{})
	r.Register(&DockerCollector{
		CollectStats: opts.CollectStats,
		SocketPath:   opts.SocketPath,
		CheckUpdates: opts.CheckUpdates,
		CachePath:    opts.CachePath,
	})
	r.Register(&ContainerLogCollector{
		SocketPath: opts.SocketPath,
		TailLines:  50,
	})

	// authCounter is a shared per-interval counter that LogsCollector
	// increments (inside parseAndCount) and SecurityCollector drains.
	//
	// ORDERING CONSTRAINT (AD-002, SEC-005 serialization invariant):
	// LogsCollector MUST be registered BEFORE SecurityCollector so that
	// collectAll iterates them in the correct order. LogsCollector populates
	// the counter and calls MarkReady; SecurityCollector then drains it.
	// Reversing the order or parallelizing collectAll would silently lose
	// counts — no error, no race detector warning, just false-negative alerts.
	// Any future parallelization of collectAll must either preserve this
	// ordering with an explicit barrier OR replace authFailureCounter with a
	// channel-based primitive. See docs/security/LOG-001-security-review.md.
	counter := &authFailureCounter{}

	r.Register(&LogsCollector{authCounter: counter})
	r.Register(&SecurityCollector{authCounter: counter})

	r.Register(&UPSCollector{})
	r.Register(&WebServersCollector{})
	r.Register(&USBCollector{})
	r.Register(&HardwareCollector{})
	r.Register(&CronCollector{})
	r.Register(&GPUCollector{})
}

func (r *Registry) FilterEnabled(enabled []string) {
	if len(enabled) == 0 {
		return
	}
	enabledSet := make(map[string]bool)
	for _, name := range enabled {
		enabledSet[name] = true
	}
	var filtered []Collector
	for _, c := range r.ordered {
		if enabledSet[c.Name()] {
			filtered = append(filtered, c)
		}
	}
	r.ordered = filtered
}

// SetSMARTInterval configures how often the hardware collector re-runs
// smartctl. Zero uses the default (4 hours).
func (r *Registry) SetSMARTInterval(d time.Duration) {
	for _, c := range r.ordered {
		if hw, ok := c.(*HardwareCollector); ok {
			hw.smartInterval = d
			return
		}
	}
}

// SetNginxAccessLog configures the nginx access log path on the WebServersCollector.
// An empty path disables access log parsing.
func (r *Registry) SetNginxAccessLog(path string) {
	for _, c := range r.ordered {
		if ws, ok := c.(*WebServersCollector); ok {
			ws.AccessLogPath = path
			return
		}
	}
}

// SetHoldManager wires the shared reboot-class HoldManager into the
// UpdatesCollector so hold reconciliation rides the telemetry cycle and
// held-package state is reported in telemetry (PATCH-GATE AD-003, AD-007).
func (r *Registry) SetHoldManager(hm *HoldManager) {
	for _, c := range r.ordered {
		if uc, ok := c.(*UpdatesCollector); ok {
			uc.holdMgr = hm
			return
		}
	}
}

// SetDockerCachePath configures the on-disk freshness cache path on the
// DockerCollector. An empty path disables cache persistence.
func (r *Registry) SetDockerCachePath(path string) {
	for _, c := range r.ordered {
		if dc, ok := c.(*DockerCollector); ok {
			dc.CachePath = path
			return
		}
	}
}

func (r *Registry) Collectors() []Collector {
	return r.ordered
}
