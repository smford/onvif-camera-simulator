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

func TestHubFleetNaming(t *testing.T) {
	hub := NewHub(16)
	if hub.fleetSize != 16 {
		t.Fatalf("expected fleetSize 16, got %d", hub.fleetSize)
	}

	for i := 0; i < 16; i++ {
		expectedName := getCamName(i)
		ptz := hub.GetPTZ(expectedName)
		if ptz.MoveStatus != "IDLE" {
			t.Errorf("expected IDLE status for camera %s, got %s", expectedName, ptz.MoveStatus)
		}
	}

	if getCamName(10) != "cam10" {
		t.Fatalf("expected cam10, got %s", getCamName(10))
	}
	if getCamName(25) != "cam25" {
		t.Fatalf("expected cam25, got %s", getCamName(25))
	}
}
