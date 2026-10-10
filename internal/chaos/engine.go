package chaos

import (
	"log/slog"
	"sync"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/metrics"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// Engine manages fault-injection states and test scenarios.
type Engine struct {
	mu          sync.Mutex
	hub         *telemetry.Hub
	freezeTimer *time.Timer
	lossTimer   *time.Timer
	onReboot    func(duration time.Duration)
}

// NewEngine creates a chaos fault injector.
func NewEngine(hub *telemetry.Hub, onReboot func(duration time.Duration)) *Engine {
	return &Engine{
		hub:      hub,
		onReboot: onReboot,
	}
}

// TriggerFreeze pauses RTP emissions while keeping sockets open.
func (e *Engine) TriggerFreeze(duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.freezeTimer != nil {
		e.freezeTimer.Stop()
	}

	e.hub.SetChaosFrozen(true)
	metrics.ChaosFaultsInjected.WithLabelValues("freeze").Inc()
	slog.Warn("SRE Chaos: Stream freeze injected", "duration", duration)

	if duration > 0 {
		e.freezeTimer = time.AfterFunc(duration, func() {
			e.Unfreeze()
		})
	}
}

// Unfreeze resumes normal RTP packet emissions.
func (e *Engine) Unfreeze() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.freezeTimer != nil {
		e.freezeTimer.Stop()
		e.freezeTimer = nil
	}

	e.hub.SetChaosFrozen(false)
	slog.Info("SRE Chaos: Stream freeze cleared")
}

// ToggleFreeze toggles the stream freeze state.
func (e *Engine) ToggleFreeze() bool {
	if e.hub.IsChaosFrozen() {
		e.Unfreeze()
		return false
	}
	e.TriggerFreeze(0)
	return true
}

// TriggerReboot simulates a camera reboot by terminating active sessions.
func (e *Engine) TriggerReboot(duration time.Duration) {
	if duration <= 0 {
		duration = 5 * time.Second
	}

	metrics.ChaosFaultsInjected.WithLabelValues("reboot").Inc()
	slog.Warn("SRE Chaos: Simulated camera reboot started", "duration", duration)

	e.hub.SetChaosReboot(true)
	if e.onReboot != nil {
		e.onReboot(duration)
	}

	go func() {
		time.Sleep(duration)
		e.hub.SetChaosReboot(false)
		slog.Info("SRE Chaos: Camera reboot simulation complete")
	}()
}

// SetPacketLoss introduces artificial frame/packet dropping.
func (e *Engine) SetPacketLoss(percentage int, duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}

	if e.lossTimer != nil {
		e.lossTimer.Stop()
	}

	e.hub.SetChaosLossPct(percentage)
	metrics.ChaosFaultsInjected.WithLabelValues("loss").Inc()
	slog.Warn("SRE Chaos: Artificial packet loss configured", "percentage", percentage, "duration", duration)

	if duration > 0 {
		e.lossTimer = time.AfterFunc(duration, func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.hub.SetChaosLossPct(0)
			slog.Info("SRE Chaos: Artificial packet loss cleared")
		})
	}
}
