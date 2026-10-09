package chaos

import (
	"testing"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

func TestChaosEngine(t *testing.T) {
	hub := telemetry.NewHub(1)
	rebootTriggered := false
	eng := NewEngine(hub, func(d time.Duration) {
		rebootTriggered = true
	})

	// Freeze
	eng.TriggerFreeze(50 * time.Millisecond)
	if !hub.IsChaosFrozen() {
		t.Errorf("expected stream to be frozen")
	}
	time.Sleep(70 * time.Millisecond)
	if hub.IsChaosFrozen() {
		t.Errorf("expected stream to be unfrozen after timeout")
	}

	// Reboot
	eng.TriggerReboot(30 * time.Millisecond)
	if !rebootTriggered {
		t.Errorf("expected onReboot callback to be called")
	}
	if !hub.IsChaosReboot() {
		t.Errorf("expected reboot flag active")
	}
	time.Sleep(50 * time.Millisecond)
	if hub.IsChaosReboot() {
		t.Errorf("expected reboot flag cleared")
	}

	// Packet loss
	eng.SetPacketLoss(30, 20*time.Millisecond)
	if hub.GetChaosLossPct() != 30 {
		t.Errorf("expected 30%% loss, got %d", hub.GetChaosLossPct())
	}
	time.Sleep(30 * time.Millisecond)
	if hub.GetChaosLossPct() != 0 {
		t.Errorf("expected 0%% loss after expiration, got %d", hub.GetChaosLossPct())
	}
}
