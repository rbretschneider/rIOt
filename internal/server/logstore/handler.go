package logstore

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/DesyncTheThird/rIOt/internal/server/db"
)

// DBHandler is a slog.Handler that buffers log entries and flushes them to the database.
type DBHandler struct {
	repo      *db.LogRepo
	minLevel  slog.Level
	mu        sync.Mutex
	buffer    []models.ServerLog
	attrs     []slog.Attr
	flushSize int
	stopOnce  sync.Once
	done      chan struct{}
}

// NewDBHandler creates a new database log handler.
func NewDBHandler(repo *db.LogRepo, minLevel slog.Level) *DBHandler {
	h := &DBHandler{
		repo:      repo,
		minLevel:  minLevel,
		buffer:    make([]models.ServerLog, 0, 100),
		flushSize: 100,
		done:      make(chan struct{}),
	}
	go h.flushLoop()
	return h
}

// Enabled reports whether the handler handles records at the given level.
func (h *DBHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// Handle stores a log record in the buffer.
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
	// Skip noisy heartbeat request logs
	if strings.Contains(r.Message, "/heartbeat") {
		return nil
	}

	entry := models.ServerLog{
		Timestamp: r.Time.UTC(),
		Level:     r.Level.String(),
		Message:   r.Message,
	}

	// Get source location
	if r.PC != 0 {
		fs := runtime.FuncForPC(r.PC)
		if fs != nil {
			file, line := fs.FileLine(r.PC)
			// Trim to just filename for brevity
			if idx := strings.LastIndex(file, "/"); idx >= 0 {
				file = file[idx+1:]
			}
			entry.Source = fmt.Sprintf("%s:%d", file, line)
		}
	}

	// Collect attributes
	attrs := make(map[string]any)
	for _, a := range h.attrs {
		attrs[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	if len(attrs) > 0 {
		entry.Attrs = attrs
	}

	h.mu.Lock()
	h.buffer = append(h.buffer, entry)
	shouldFlush := len(h.buffer) >= h.flushSize
	h.mu.Unlock()

	if shouldFlush {
		h.flush()
	}

	return nil
}

// WithAttrs returns a new handler with the given attributes.
func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &DBHandler{
		repo:      h.repo,
		minLevel:  h.minLevel,
		buffer:    h.buffer,
		attrs:     append(append([]slog.Attr{}, h.attrs...), attrs...),
		flushSize: h.flushSize,
		done:      h.done,
	}
}

// WithGroup returns a new handler with the given group name.
func (h *DBHandler) WithGroup(_ string) slog.Handler {
	// Groups are not used in this handler; return a copy.
	return &DBHandler{
		repo:      h.repo,
		minLevel:  h.minLevel,
		buffer:    h.buffer,
		attrs:     h.attrs,
		flushSize: h.flushSize,
		done:      h.done,
	}
}

// Stop flushes remaining entries and stops the flush loop.
func (h *DBHandler) Stop() {
	h.stopOnce.Do(func() {
		close(h.done)
		h.flush()
	})
}

func (h *DBHandler) flushLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.flush()
		}
	}
}

func (h *DBHandler) flush() {
	h.mu.Lock()
	if len(h.buffer) == 0 {
		h.mu.Unlock()
		return
	}
	entries := h.buffer
	h.buffer = make([]models.ServerLog, 0, 100)
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = h.repo.Insert(ctx, entries)
}

// MultiHandler fans out log records to multiple handlers.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler creates a handler that writes to all provided handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r)
		}
	}
	return nil
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: handlers}
}
