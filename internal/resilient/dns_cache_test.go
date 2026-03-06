package resilient

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolverLiveDNS(t *testing.T) {
	r := NewResilientResolver("")
	ips, err := r.Resolve("localhost")
	if err != nil {
		t.Fatalf("expected localhost to resolve, got: %v", err)
	}
	if len(ips) == 0 {
		t.Fatal("expected at least one IP for localhost")
	}
}

func TestResolverCacheFallback(t *testing.T) {
	r := NewResilientResolver("")

	// Pre-populate cache
	r.mu.Lock()
	r.entries["nonexistent.invalid"] = &DNSCacheEntry{
		Hostname:    "nonexistent.invalid",
		ResolvedIPs: []string{"1.2.3.4"},
		ResolvedAt:  time.Now(),
		Source:      "live",
	}
	r.mu.Unlock()

	ips, err := r.Resolve("nonexistent.invalid")
	if err != nil {
		t.Fatalf("expected cache fallback, got error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "1.2.3.4" {
		t.Fatalf("expected [1.2.3.4], got %v", ips)
	}
}

func TestResolverDiskPersistence(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "dns-cache.json")

	// Create resolver and add an entry
	r1 := NewResilientResolver(cacheFile)
	r1.mu.Lock()
	r1.entries["test.example"] = &DNSCacheEntry{
		Hostname:    "test.example",
		ResolvedIPs: []string{"10.0.0.1"},
		ResolvedAt:  time.Now(),
		Source:      "live",
	}
	r1.mu.Unlock()
	r1.persist()

	// Verify file exists
	if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
		t.Fatal("cache file was not created")
	}

	// Create new resolver that should load from disk
	r2 := NewResilientResolver(cacheFile)
	r2.mu.RLock()
	entry, ok := r2.entries["test.example"]
	r2.mu.RUnlock()

	if !ok {
		t.Fatal("entry not loaded from disk")
	}
	if len(entry.ResolvedIPs) != 1 || entry.ResolvedIPs[0] != "10.0.0.1" {
		t.Fatalf("expected [10.0.0.1], got %v", entry.ResolvedIPs)
	}
}

func TestResolverUpdateConnectTime(t *testing.T) {
	r := NewResilientResolver("")
	r.mu.Lock()
	r.entries["test.local"] = &DNSCacheEntry{
		Hostname:    "test.local",
		ResolvedIPs: []string{"127.0.0.1"},
		ResolvedAt:  time.Now(),
	}
	r.mu.Unlock()

	r.UpdateConnectTime("test.local")

	r.mu.RLock()
	entry := r.entries["test.local"]
	r.mu.RUnlock()

	if entry.LastSuccessfulConnect.IsZero() {
		t.Fatal("expected LastSuccessfulConnect to be set")
	}
}

func TestResolverCachedIPs(t *testing.T) {
	r := NewResilientResolver("")

	// No entry
	if ips := r.CachedIPs("missing.host"); ips != nil {
		t.Fatalf("expected nil for missing host, got %v", ips)
	}

	// With entry
	r.mu.Lock()
	r.entries["cached.host"] = &DNSCacheEntry{
		ResolvedIPs: []string{"5.6.7.8"},
	}
	r.mu.Unlock()

	ips := r.CachedIPs("cached.host")
	if len(ips) != 1 || ips[0] != "5.6.7.8" {
		t.Fatalf("expected [5.6.7.8], got %v", ips)
	}
}
