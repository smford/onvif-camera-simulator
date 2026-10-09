package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// LogEntry represents an individual structured log message in memory.
type LogEntry struct {
	Timestamp time.Time
	Level     slog.Level
	Message   string
	Attrs     map[string]any
}

// Format returns a colorized string for terminal display.
func (e LogEntry) Format() string {
	ts := e.Timestamp.Format("15:04:05.000")
	var lvlStr string
	switch e.Level {
	case slog.LevelDebug:
		lvlStr = "[DEBUG]"
	case slog.LevelInfo:
		lvlStr = "[INFO ]"
	case slog.LevelWarn:
		lvlStr = "[WARN ]"
	case slog.LevelError:
		lvlStr = "[ERROR]"
	default:
		lvlStr = fmt.Sprintf("[%s]", e.Level.String())
	}

	attrStr := ""
	for k, v := range e.Attrs {
		attrStr += fmt.Sprintf(" %s=%v", k, v)
	}

	return fmt.Sprintf("%s %s %s%s", ts, lvlStr, e.Message, attrStr)
}

// RingBufferLogHandler is a thread-safe circular buffer for slog records.
type RingBufferLogHandler struct {
	mu       sync.RWMutex
	capacity int
	entries  []LogEntry
	head     int
	count    int
	level    slog.Level
}

// NewRingBufferLogHandler creates a new circular buffer slog handler.
func NewRingBufferLogHandler(capacity int, level slog.Level) *RingBufferLogHandler {
	if capacity <= 0 {
		capacity = 250
	}
	return &RingBufferLogHandler{
		capacity: capacity,
		entries:  make([]LogEntry, capacity),
		level:    level,
	}
}

func (h *RingBufferLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *RingBufferLogHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})

	entry := LogEntry{
		Timestamp: r.Time,
		Level:     r.Level,
		Message:   r.Message,
		Attrs:     attrs,
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries[h.head] = entry
	h.head = (h.head + 1) % h.capacity
	if h.count < h.capacity {
		h.count++
	}

	return nil
}

func (h *RingBufferLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *RingBufferLogHandler) WithGroup(name string) slog.Handler {
	return h
}

// GetEntries returns an ordered snapshot of all retained log records (oldest to newest).
func (h *RingBufferLogHandler) GetEntries() []LogEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]LogEntry, h.count)
	if h.count < h.capacity {
		copy(result, h.entries[:h.count])
		return result
	}

	// Buffer is full: oldest entry is at h.head
	tail := h.head
	n := copy(result, h.entries[tail:])
	copy(result[n:], h.entries[:tail])
	return result
}

// Clear wipes all log entries from the ring buffer.
func (h *RingBufferLogHandler) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.head = 0
	h.count = 0
}
