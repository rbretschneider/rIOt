// Package collectors provides system telemetry collectors for the rIOt agent.
//
// Serialization invariant (SEC-005):
// This counter assumes sequential execution of LogsCollector.Collect and
// SecurityCollector.Collect within a single collectAll pass. LogsCollector
// MUST run first, increment the counter, and call MarkReady before
// SecurityCollector calls Drain. Any future parallelization of collectAll
// must preserve this ordering (explicit barrier) OR replace this type with
// a channel-based primitive. Parallel execution would lose counts silently
// (no error, no test failure under -race, just a false-negative alert
// signal). See docs/security/LOG-001-security-review.md SEC-005.
package collectors

import "sync"

// authFailureCounter is a thread-safe counter with a one-way "ready" latch.
//
// The latch (ready) is set by LogsCollector after its first real cursor-based
// scan completes. SecurityCollector checks IsReady before deciding whether to
// report the drained value or emit zero (FR-005: first-interval zero).
//
// Serialization invariant: see package doc comment above.
type authFailureCounter struct {
	mu    sync.Mutex
	n     int
	ready bool
}

// Add increments the counter by delta. Safe for concurrent use, though under
// the current sequential collectAll design it is called from a single goroutine.
func (c *authFailureCounter) Add(delta int) {
	c.mu.Lock()
	c.n += delta
	c.mu.Unlock()
}

// Drain returns the current count and atomically resets it to zero.
func (c *authFailureCounter) Drain() int {
	c.mu.Lock()
	v := c.n
	c.n = 0
	c.mu.Unlock()
	return v
}

// MarkReady sets the ready latch. Idempotent; once set it stays set.
func (c *authFailureCounter) MarkReady() {
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
}

// IsReady returns true once MarkReady has been called at least once.
func (c *authFailureCounter) IsReady() bool {
	c.mu.Lock()
	v := c.ready
	c.mu.Unlock()
	return v
}
