package resilient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRetryQueueEnqueueAndDrain(t *testing.T) {
	q := NewRetryQueue("", 10)

	q.Enqueue(QueuedNotification{
		Payload: json.RawMessage(`{"msg":"test1"}`),
		URL:     "http://example.com/1",
	})
	q.Enqueue(QueuedNotification{
		Payload: json.RawMessage(`{"msg":"test2"}`),
		URL:     "http://example.com/2",
	})

	if q.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", q.Len())
	}

	// Drain all successfully
	sent := q.Drain(func(n QueuedNotification) error {
		return nil
	})

	if sent != 2 {
		t.Fatalf("expected 2 sent, got %d", sent)
	}
	if q.Len() != 0 {
		t.Fatalf("expected empty queue after drain, got %d", q.Len())
	}
}

func TestRetryQueueCapacity(t *testing.T) {
	q := NewRetryQueue("", 3)

	for i := 0; i < 5; i++ {
		q.Enqueue(QueuedNotification{
			Payload: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)),
			URL:     fmt.Sprintf("http://example.com/%d", i),
		})
	}

	if q.Len() != 3 {
		t.Fatalf("expected 3 items (capped), got %d", q.Len())
	}

	// Verify oldest items were dropped (items 0 and 1 should be gone)
	q.mu.Lock()
	firstURL := q.items[0].URL
	q.mu.Unlock()

	if firstURL != "http://example.com/2" {
		t.Fatalf("expected oldest remaining to be /2, got %s", firstURL)
	}
}

func TestRetryQueuePartialDrain(t *testing.T) {
	q := NewRetryQueue("", 10)

	q.Enqueue(QueuedNotification{URL: "http://example.com/1"})
	q.Enqueue(QueuedNotification{URL: "http://example.com/2"})
	q.Enqueue(QueuedNotification{URL: "http://example.com/3"})

	// Only succeed for URLs ending in /2
	sent := q.Drain(func(n QueuedNotification) error {
		if n.URL == "http://example.com/2" {
			return nil
		}
		return fmt.Errorf("fail")
	})

	if sent != 1 {
		t.Fatalf("expected 1 sent, got %d", sent)
	}
	if q.Len() != 2 {
		t.Fatalf("expected 2 remaining, got %d", q.Len())
	}
}

func TestRetryQueueDiskPersistence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "queue.json")

	q1 := NewRetryQueue(file, 10)
	q1.Enqueue(QueuedNotification{
		Payload: json.RawMessage(`{"test":true}`),
		URL:     "http://example.com/persist",
	})

	// Verify file was written
	if _, err := os.Stat(file); os.IsNotExist(err) {
		t.Fatal("queue file was not created")
	}

	// Load into new queue
	q2 := NewRetryQueue(file, 10)
	if q2.Len() != 1 {
		t.Fatalf("expected 1 item loaded from disk, got %d", q2.Len())
	}
}

func TestRetryQueueDrainOrder(t *testing.T) {
	q := NewRetryQueue("", 10)

	q.Enqueue(QueuedNotification{URL: "first"})
	q.Enqueue(QueuedNotification{URL: "second"})
	q.Enqueue(QueuedNotification{URL: "third"})

	var order []string
	q.Drain(func(n QueuedNotification) error {
		order = append(order, n.URL)
		return nil
	})

	if len(order) != 3 || order[0] != "first" || order[1] != "second" || order[2] != "third" {
		t.Fatalf("expected [first second third], got %v", order)
	}
}
