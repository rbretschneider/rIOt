package collectors

import "context"

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

func (r *Registry) RegisterDefaults() {
	r.Register(&SystemCollector{})
	r.Register(&CPUCollector{})
	r.Register(&MemoryCollector{})
	r.Register(&DiskCollector{})
	r.Register(&NetworkCollector{})
	r.Register(&OSInfoCollector{})
	r.Register(&UpdatesCollector{})
	r.Register(&ServicesCollector{})
	r.Register(&ProcessesCollector{})
	r.Register(&DockerCollector{})
	r.Register(&SecurityCollector{})
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

func (r *Registry) Collectors() []Collector {
	return r.ordered
}
