package telemetry

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestRingBufferLogHandler(t *testing.T) {
	capacity := 5
	h := NewRingBufferLogHandler(capacity, slog.LevelDebug)

	for i := 0; i < 10; i++ {
		record := slog.Record{
			Time:    time.Now(),
			Level:   slog.LevelInfo,
			Message: "Test Message",
		}
		record.AddAttrs(slog.Int("idx", i))
		if err := h.Handle(context.Background(), record); err != nil {
			t.Fatalf("Handle failed: %v", err)
		}
	}

	entries := h.GetEntries()
	if len(entries) != capacity {
		t.Fatalf("expected retained count %d, got %d", capacity, len(entries))
	}

	// Verify order: oldest retained should be idx 5, newest idx 9
	firstIdx := entries[0].Attrs["idx"]
	if firstIdx != int64(5) {
		t.Errorf("expected first retained entry idx=5, got %v (%T)", firstIdx, firstIdx)
	}

	lastIdx := entries[len(entries)-1].Attrs["idx"]
	if lastIdx != int64(9) {
		t.Errorf("expected last retained entry idx=9, got %v (%T)", lastIdx, lastIdx)
	}

	h.Clear()
	if len(h.GetEntries()) != 0 {
		t.Errorf("expected 0 entries after Clear")
	}
}
