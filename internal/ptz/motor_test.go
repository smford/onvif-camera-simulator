package ptz

import (
	"testing"
	"time"
)

func TestVirtualPTZKinematics(t *testing.T) {
	updated := false
	p := NewVirtualPTZ("cam0", 0, 0, 0, 1.0, 1.0, 1.0, func(pan, tilt, zoom float64, status, lastCmd string) {
		updated = true
	})
	defer p.Close()

	pan, tilt, zoom, status, _ := p.GetStatus()
	if pan != 0 || tilt != 0 || zoom != 0 || status != "IDLE" {
		t.Fatalf("expected initial 0,0,0 IDLE, got %.2f,%.2f,%.2f %s", pan, tilt, zoom, status)
	}

	// Continuous Move
	p.ContinuousMove(0.5, 0.5, 0.0, 100*time.Millisecond)
	_, _, _, status, _ = p.GetStatus()
	if status != "MOVING" {
		t.Errorf("expected MOVING status, got %s", status)
	}

	time.Sleep(150 * time.Millisecond)
	// Timeout should have halted motion
	_, _, _, status, _ = p.GetStatus()
	if status != "IDLE" {
		t.Errorf("expected IDLE after timeout, got %s", status)
	}

	// Stop
	p.ContinuousMove(1.0, 1.0, 1.0, 0)
	p.Stop()
	_, _, _, status, _ = p.GetStatus()
	if status != "IDLE" {
		t.Errorf("expected IDLE after Stop, got %s", status)
	}

	if !updated {
		t.Errorf("expected onUpdate callback to have been invoked")
	}
}

func TestVirtualPTZPresets(t *testing.T) {
	p := NewVirtualPTZ("cam0", 0.5, -0.5, 0.2, 1.0, 1.0, 1.0, nil)
	defer p.Close()

	tok := p.SetPreset("Entrance")
	presets := p.GetPresets()
	if len(presets) < 2 { // Home + Entrance
		t.Fatalf("expected at least 2 presets, got %d", len(presets))
	}

	p.AbsoluteMove(0, 0, 0)
	time.Sleep(20 * time.Millisecond)

	if err := p.GotoPreset(tok); err != nil {
		t.Fatalf("GotoPreset failed: %v", err)
	}

	p.RemovePreset(tok)
	if err := p.GotoPreset(tok); err == nil {
		t.Fatalf("expected error after removing preset, got nil")
	}
}
