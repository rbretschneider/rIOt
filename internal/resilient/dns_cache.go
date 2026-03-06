package resilient

import (
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DNSCacheEntry holds a cached DNS resolution result.
type DNSCacheEntry struct {
	Hostname              string    `json:"hostname"`
	ResolvedIPs           []string  `json:"resolved_ips"`
	ResolvedAt            time.Time `json:"resolved_at"`
	LastSuccessfulConnect time.Time `json:"last_successful_connect"`
	TTLOverrideSeconds    int       `json:"ttl_override_seconds"`
	Source                string    `json:"source"` // "live" or "cache"
}

// ResilientResolver provides DNS resolution with disk-persisted caching.
// When live DNS fails, it falls back to cached results.
type ResilientResolver struct {
	cacheFile       string
	refreshInterval time.Duration
	stalenessWarn   time.Duration
	stalenessCrit   time.Duration

	mu      sync.RWMutex
	entries map[string]*DNSCacheEntry

	stopOnce sync.Once
	stopCh   chan struct{}
}

// ResolverOption configures the ResilientResolver.
type ResolverOption func(*ResilientResolver)

// WithRefreshInterval sets how often DNS entries are refreshed in the background.
func WithRefreshInterval(d time.Duration) ResolverOption {
	return func(r *ResilientResolver) { r.refreshInterval = d }
}

// WithStalenessWarn sets the duration after which a cache entry triggers a warning.
func WithStalenessWarn(d time.Duration) ResolverOption {
	return func(r *ResilientResolver) { r.stalenessWarn = d }
}

// WithStalenessCrit sets the duration after which a cache entry triggers a critical warning.
func WithStalenessCrit(d time.Duration) ResolverOption {
	return func(r *ResilientResolver) { r.stalenessCrit = d }
}

// NewResilientResolver creates a new resolver with disk-persisted cache.
func NewResilientResolver(cacheFile string, opts ...ResolverOption) *ResilientResolver {
	r := &ResilientResolver{
		cacheFile:       cacheFile,
		refreshInterval: 30 * time.Minute,
		stalenessWarn:   24 * time.Hour,
		stalenessCrit:   72 * time.Hour,
		entries:         make(map[string]*DNSCacheEntry),
		stopCh:          make(chan struct{}),
	}
	for _, opt := range opts {
		opt(r)
	}

	// Load existing cache from disk
	r.loadFromDisk()

	return r
}

// Resolve resolves a hostname to IP addresses. Tries live DNS first,
// falls back to cached results on failure.
func (r *ResilientResolver) Resolve(hostname string) ([]string, error) {
	// Try live DNS
	ips, err := net.LookupHost(hostname)
	if err == nil {
		r.mu.Lock()
		r.entries[hostname] = &DNSCacheEntry{
			Hostname:    hostname,
			ResolvedIPs: ips,
			ResolvedAt:  time.Now(),
			Source:      "live",
		}
		// Preserve last successful connect time from previous entry
		if old, ok := r.entries[hostname]; ok {
			r.entries[hostname].LastSuccessfulConnect = old.LastSuccessfulConnect
		}
		r.mu.Unlock()
		r.persist()
		return ips, nil
	}

	// Fall back to cache
	r.mu.RLock()
	entry, ok := r.entries[hostname]
	r.mu.RUnlock()

	if ok && len(entry.ResolvedIPs) > 0 {
		staleness := time.Since(entry.ResolvedAt)
		if staleness > r.stalenessCrit {
			slog.Warn("DNS cache: critically stale entry", "hostname", hostname, "age", staleness)
		} else if staleness > r.stalenessWarn {
			slog.Warn("DNS cache: stale entry", "hostname", hostname, "age", staleness)
		}
		slog.Info("DNS cache: using cached IPs", "hostname", hostname, "ips", entry.ResolvedIPs)
		return entry.ResolvedIPs, nil
	}

	return nil, err
}

// UpdateConnectTime records a successful connection to a hostname.
func (r *ResilientResolver) UpdateConnectTime(hostname string) {
	r.mu.Lock()
	if entry, ok := r.entries[hostname]; ok {
		entry.LastSuccessfulConnect = time.Now()
	}
	r.mu.Unlock()
}

// CachedIPs returns cached IPs for a hostname, or nil if not cached.
func (r *ResilientResolver) CachedIPs(hostname string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.entries[hostname]; ok {
		return entry.ResolvedIPs
	}
	return nil
}

// Start begins the background DNS refresh goroutine.
func (r *ResilientResolver) Start() {
	go r.refreshLoop()
}

// Stop halts the background refresh.
func (r *ResilientResolver) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}

func (r *ResilientResolver) refreshLoop() {
	ticker := time.NewTicker(r.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.refreshAll()
		}
	}
}

func (r *ResilientResolver) refreshAll() {
	r.mu.RLock()
	hostnames := make([]string, 0, len(r.entries))
	for h := range r.entries {
		hostnames = append(hostnames, h)
	}
	r.mu.RUnlock()

	for _, h := range hostnames {
		ips, err := net.LookupHost(h)
		if err != nil {
			slog.Debug("DNS refresh failed", "hostname", h, "error", err)
			continue
		}
		r.mu.Lock()
		if entry, ok := r.entries[h]; ok {
			entry.ResolvedIPs = ips
			entry.ResolvedAt = time.Now()
			entry.Source = "live"
		}
		r.mu.Unlock()
	}
	r.persist()
}

func (r *ResilientResolver) loadFromDisk() {
	if r.cacheFile == "" {
		return
	}
	data, err := os.ReadFile(r.cacheFile)
	if err != nil {
		return // file doesn't exist yet — that's fine
	}
	var entries map[string]*DNSCacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		slog.Warn("DNS cache: failed to parse cache file", "error", err)
		return
	}
	r.entries = entries
	slog.Info("DNS cache: loaded from disk", "entries", len(entries))
}

func (r *ResilientResolver) persist() {
	if r.cacheFile == "" {
		return
	}
	r.mu.RLock()
	data, err := json.MarshalIndent(r.entries, "", "  ")
	r.mu.RUnlock()
	if err != nil {
		return
	}

	// Atomic write: write to temp file, then rename
	dir := filepath.Dir(r.cacheFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	tmp := r.cacheFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	os.Rename(tmp, r.cacheFile)
}
