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
	})
	r.Register(&ContainerLogCollector{
		SocketPath: opts.SocketPath,
		TailLines:  50,
	})
	r.Register(&SecurityCollector{})
	r.Register(&LogsCollector{})
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

func (r *Registry) Collectors() []Collector {
	return r.ordered
}
