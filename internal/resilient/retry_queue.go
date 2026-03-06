package resilient

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// QueuedNotification is a notification that failed to send and is queued for retry.
type QueuedNotification struct {
	Payload   json.RawMessage `json:"payload"`
	URL       string          `json:"url"`
	QueuedAt  time.Time       `json:"queued_at"`
	Attempts  int             `json:"attempts"`
	LastError string          `json:"last_error,omitempty"`
}

// RetryQueue persists failed notifications to disk for later retry.
type RetryQueue struct {
	mu      sync.Mutex
	file    string
	maxSize int
	items   []QueuedNotification
}

// NewRetryQueue creates a retry queue backed by a JSON file.
func NewRetryQueue(file string, maxSize int) *RetryQueue {
	if maxSize <= 0 {
		maxSize = 100
	}
	q := &RetryQueue{
		file:    file,
		maxSize: maxSize,
	}
	q.loadFromDisk()
	return q
}

// Enqueue adds a notification to the retry queue.
// If the queue is at capacity, the oldest item is dropped.
func (q *RetryQueue) Enqueue(n QueuedNotification) {
	q.mu.Lock()
	defer q.mu.Unlock()

	n.QueuedAt = time.Now()
	q.items = append(q.items, n)

	// Drop oldest if over capacity
	if len(q.items) > q.maxSize {
		q.items = q.items[len(q.items)-q.maxSize:]
	}

	q.persistLocked()
}

// Drain attempts to send all queued items using the provided send function.
// Items are attempted oldest-first. Successfully sent items are removed.
func (q *RetryQueue) Drain(sendFn func(QueuedNotification) error) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return 0
	}

	sent := 0
	var remaining []QueuedNotification

	for _, item := range q.items {
		item.Attempts++
		if err := sendFn(item); err != nil {
			item.LastError = err.Error()
			remaining = append(remaining, item)
			slog.Debug("retry queue: send failed", "url", item.URL, "attempts", item.Attempts, "error", err)
		} else {
			sent++
		}
	}

	q.items = remaining
	q.persistLocked()

	if sent > 0 {
		slog.Info("retry queue: drained", "sent", sent, "remaining", len(remaining))
	}
	return sent
}

// Len returns the number of queued items.
func (q *RetryQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *RetryQueue) loadFromDisk() {
	if q.file == "" {
		return
	}
	data, err := os.ReadFile(q.file)
	if err != nil {
		return
	}
	var items []QueuedNotification
	if err := json.Unmarshal(data, &items); err != nil {
		slog.Warn("retry queue: failed to parse file", "error", err)
		return
	}
	q.items = items
}

func (q *RetryQueue) persistLocked() {
	if q.file == "" {
		return
	}
	data, err := json.MarshalIndent(q.items, "", "  ")
	if err != nil {
		return
	}

	dir := filepath.Dir(q.file)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	tmp := q.file + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	os.Rename(tmp, q.file)
}
