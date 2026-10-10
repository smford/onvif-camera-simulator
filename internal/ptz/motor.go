package ptz

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/smford/onvif-camera-simulator/internal/metrics"
)

// Preset represents a saved PTZ coordinate location.
type Preset struct {
	Token string
	Name  string
	Pan   float64
	Tilt  float64
	Zoom  float64
}

// VirtualPTZ simulates pan-tilt-zoom motor kinematics at 20Hz.
type VirtualPTZ struct {
	mu           sync.RWMutex
	cameraID     string
	pan          float64
	tilt         float64
	zoom         float64
	vx           float64
	vy           float64
	vz           float64
	maxPanSpeed  float64
	maxTiltSpeed float64
	maxZoomSpeed float64
	targetPan    *float64
	targetTilt   *float64
	targetZoom   *float64
	moveTimeout  time.Time
	status       string // "IDLE" or "MOVING"
	lastCmd      string
	presets      map[string]Preset
	ticker       *time.Ticker
	stopCh       chan struct{}
	onUpdate     func(pan, tilt, zoom float64, status, lastCmd string)
}

// NewVirtualPTZ initializes the motor simulation.
func NewVirtualPTZ(
	cameraID string,
	homePan, homeTilt, homeZoom float64,
	maxPanSpeed, maxTiltSpeed, maxZoomSpeed float64,
	onUpdate func(pan, tilt, zoom float64, status, lastCmd string),
) *VirtualPTZ {
	if maxPanSpeed <= 0 {
		maxPanSpeed = 1.0
	}
	if maxTiltSpeed <= 0 {
		maxTiltSpeed = 1.0
	}
	if maxZoomSpeed <= 0 {
		maxZoomSpeed = 1.0
	}

	p := &VirtualPTZ{
		cameraID:     cameraID,
		pan:          clamp(homePan, -1.0, 1.0),
		tilt:         clamp(homeTilt, -1.0, 1.0),
		zoom:         clamp(homeZoom, 0.0, 1.0),
		maxPanSpeed:  maxPanSpeed,
		maxTiltSpeed: maxTiltSpeed,
		maxZoomSpeed: maxZoomSpeed,
		status:       "IDLE",
		lastCmd:      "NONE",
		presets:      make(map[string]Preset),
		ticker:       time.NewTicker(50 * time.Millisecond), // 20Hz
		stopCh:       make(chan struct{}),
		onUpdate:     onUpdate,
	}

	// Initialize default "Home" preset
	p.presets["preset_home"] = Preset{
		Token: "preset_home",
		Name:  "Home",
		Pan:   p.pan,
		Tilt:  p.tilt,
		Zoom:  p.zoom,
	}

	go p.kinematicsLoop()
	return p
}

func (p *VirtualPTZ) kinematicsLoop() {
	lastTick := time.Now()
	for {
		select {
		case <-p.stopCh:
			return
		case now := <-p.ticker.C:
			dt := now.Sub(lastTick).Seconds()
			lastTick = now
			p.updateKinematics(dt, now)
		}
	}
}

func (p *VirtualPTZ) updateKinematics(dt float64, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check timeout on continuous move
	if !p.moveTimeout.IsZero() && now.After(p.moveTimeout) {
		p.vx = 0
		p.vy = 0
		p.vz = 0
		p.moveTimeout = time.Time{}
		p.status = "IDLE"
	}

	// Absolute targeting
	if p.targetPan != nil || p.targetTilt != nil || p.targetZoom != nil {
		moved := false
		if p.targetPan != nil {
			diff := *p.targetPan - p.pan
			step := p.maxPanSpeed * dt
			if math.Abs(diff) <= step {
				p.pan = *p.targetPan
				p.targetPan = nil
			} else {
				p.pan += math.Copysign(step, diff)
				moved = true
			}
		}
		if p.targetTilt != nil {
			diff := *p.targetTilt - p.tilt
			step := p.maxTiltSpeed * dt
			if math.Abs(diff) <= step {
				p.tilt = *p.targetTilt
				p.targetTilt = nil
			} else {
				p.tilt += math.Copysign(step, diff)
				moved = true
			}
		}
		if p.targetZoom != nil {
			diff := *p.targetZoom - p.zoom
			step := p.maxZoomSpeed * dt
			if math.Abs(diff) <= step {
				p.zoom = *p.targetZoom
				p.targetZoom = nil
			} else {
				p.zoom += math.Copysign(step, diff)
				moved = true
			}
		}

		if !moved {
			p.status = "IDLE"
		} else {
			p.status = "MOVING"
		}
	} else if p.vx != 0 || p.vy != 0 || p.vz != 0 {
		// Velocity integration
		p.pan = clamp(p.pan+p.vx*p.maxPanSpeed*dt, -1.0, 1.0)
		p.tilt = clamp(p.tilt+p.vy*p.maxTiltSpeed*dt, -1.0, 1.0)
		p.zoom = clamp(p.zoom+p.vz*p.maxZoomSpeed*dt, 0.0, 1.0)
		p.status = "MOVING"
	} else {
		p.status = "IDLE"
	}

	if p.onUpdate != nil {
		p.onUpdate(p.pan, p.tilt, p.zoom, p.status, p.lastCmd)
	}
}

// ContinuousMove updates velocity vector and optional timeout.
func (p *VirtualPTZ) ContinuousMove(vx, vy, vz float64, timeout time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.vx = clamp(vx, -1.0, 1.0)
	p.vy = clamp(vy, -1.0, 1.0)
	p.vz = clamp(vz, -1.0, 1.0)
	p.targetPan = nil
	p.targetTilt = nil
	p.targetZoom = nil

	if timeout > 0 {
		p.moveTimeout = time.Now().Add(timeout)
	} else {
		p.moveTimeout = time.Time{}
	}

	if p.vx != 0 || p.vy != 0 || p.vz != 0 {
		p.status = "MOVING"
	} else {
		p.status = "IDLE"
	}
	p.lastCmd = "CONTINUOUS"
	metrics.PTZCommands.WithLabelValues("continuous").Inc()

	if p.onUpdate != nil {
		p.onUpdate(p.pan, p.tilt, p.zoom, p.status, p.lastCmd)
	}
}

// AbsoluteMove commands motors toward specific target coordinates.
func (p *VirtualPTZ) AbsoluteMove(pan, tilt, zoom float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tp := clamp(pan, -1.0, 1.0)
	tt := clamp(tilt, -1.0, 1.0)
	tz := clamp(zoom, 0.0, 1.0)

	p.targetPan = &tp
	p.targetTilt = &tt
	p.targetZoom = &tz
	p.vx = 0
	p.vy = 0
	p.vz = 0
	p.moveTimeout = time.Time{}
	p.status = "MOVING"
	p.lastCmd = "ABSOLUTE"
	metrics.PTZCommands.WithLabelValues("absolute").Inc()

	if p.onUpdate != nil {
		p.onUpdate(p.pan, p.tilt, p.zoom, p.status, p.lastCmd)
	}
}

// RelativeMove applies delta coordinates.
func (p *VirtualPTZ) RelativeMove(dPan, dTilt, dZoom float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tp := clamp(p.pan+dPan, -1.0, 1.0)
	tt := clamp(p.tilt+dTilt, -1.0, 1.0)
	tz := clamp(p.zoom+dZoom, 0.0, 1.0)

	p.targetPan = &tp
	p.targetTilt = &tt
	p.targetZoom = &tz
	p.vx = 0
	p.vy = 0
	p.vz = 0
	p.moveTimeout = time.Time{}
	p.status = "MOVING"
	p.lastCmd = "RELATIVE"
	metrics.PTZCommands.WithLabelValues("relative").Inc()

	if p.onUpdate != nil {
		p.onUpdate(p.pan, p.tilt, p.zoom, p.status, p.lastCmd)
	}
}

// Stop halts all motion immediately.
func (p *VirtualPTZ) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.vx = 0
	p.vy = 0
	p.vz = 0
	p.targetPan = nil
	p.targetTilt = nil
	p.targetZoom = nil
	p.moveTimeout = time.Time{}
	p.status = "IDLE"
	p.lastCmd = "STOP"
	metrics.PTZCommands.WithLabelValues("stop").Inc()

	if p.onUpdate != nil {
		p.onUpdate(p.pan, p.tilt, p.zoom, p.status, p.lastCmd)
	}
}

// GetStatus returns the current coordinates, status, and UTC timestamp.
func (p *VirtualPTZ) GetStatus() (float64, float64, float64, string, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	if !p.moveTimeout.IsZero() && now.After(p.moveTimeout) {
		p.vx = 0
		p.vy = 0
		p.vz = 0
		p.moveTimeout = time.Time{}
		p.status = "IDLE"
	}

	return p.pan, p.tilt, p.zoom, p.status, now.UTC()
}

// SetPreset stores the current position under a preset name.
func (p *VirtualPTZ) SetPreset(name string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	token := "preset_" + uuid.New().String()[:8]
	if name == "" {
		name = fmt.Sprintf("Preset %d", len(p.presets)+1)
	}

	p.presets[token] = Preset{
		Token: token,
		Name:  name,
		Pan:   p.pan,
		Tilt:  p.tilt,
		Zoom:  p.zoom,
	}
	return token
}

// GetPresets returns all registered presets.
func (p *VirtualPTZ) GetPresets() []Preset {
	p.mu.RLock()
	defer p.mu.RUnlock()

	list := make([]Preset, 0, len(p.presets))
	for _, pr := range p.presets {
		list = append(list, pr)
	}
	return list
}

// GotoPreset drives motors to the coordinates of a saved preset.
func (p *VirtualPTZ) GotoPreset(token string) error {
	p.mu.Lock()
	pr, ok := p.presets[token]
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("preset %q not found", token)
	}

	p.AbsoluteMove(pr.Pan, pr.Tilt, pr.Zoom)
	return nil
}

// RemovePreset deletes a saved preset.
func (p *VirtualPTZ) RemovePreset(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.presets, token)
}

func (p *VirtualPTZ) Close() {
	p.ticker.Stop()
	close(p.stopCh)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
