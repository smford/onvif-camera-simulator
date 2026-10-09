package events

import (
	"testing"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

func TestEventManagerSubscriptions(t *testing.T) {
	hub := telemetry.NewHub(1)
	m := NewManager(hub)
	defer m.Close()

	sub := m.CreateSubscription("cam0", 30*time.Second)
	if sub == nil || sub.ID == "" {
		t.Fatalf("failed to create subscription")
	}

	// Trigger motion
	m.TriggerMotion("cam0", true, 0)

	// Pull immediately
	pulled := sub.Pull(5, 100*time.Millisecond)
	if len(pulled) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pulled))
	}
	if pulled[0].Topic != "tns1:RuleEngine/CellMotionDetector/Motion" {
		t.Errorf("unexpected topic %q", pulled[0].Topic)
	}
	if pulled[0].SimpleItems["IsInside"] != "true" {
		t.Errorf("expected IsInside=true, got %s", pulled[0].SimpleItems["IsInside"])
	}

	// Test long-polling timeout with empty queue
	start := time.Now()
	empty := sub.Pull(5, 50*time.Millisecond)
	elapsed := time.Since(start)
	if len(empty) != 0 {
		t.Errorf("expected 0 events, got %d", len(empty))
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("expected timeout delay of ~50ms, elapsed %v", elapsed)
	}

	// Renew
	newExp, err := m.Renew(sub.ID, 45*time.Second)
	if err != nil || newExp.Before(time.Now()) {
		t.Fatalf("Renew failed: %v", err)
	}

	// Unsubscribe
	m.Unsubscribe(sub.ID)
	if _, ok := m.GetSubscription(sub.ID); ok {
		t.Errorf("subscription should have been removed")
	}
}
