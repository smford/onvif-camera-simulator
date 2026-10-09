package telemetry

import (
	"sync"
	"time"
)

// ClientSession tracks an active RTSP connection.
type ClientSession struct {
	ID          string
	RemoteIP    string
	Port        int
	Camera      string
	Stream      string // "main" or "sub"
	Transport   string // "TCP" or "UDP"
	UserAgent   string
	State       string // "INIT", "SETUP", "PLAY"
	ConnectTime time.Time
	BytesSent   uint64
	PacketsSent uint64
	BitrateKbps float64
	lastBytes   uint64
	lastCalc    time.Time
}

// SOAPAuditEntry records recent ONVIF SOAP interactions for the security audit log.
type SOAPAuditEntry struct {
	Timestamp   time.Time
	Camera      string
	Action      string
	ClientIP    string
	AuthOutcome string // "SUCCESS", "ANON", "FAILED"
}

// CameraPTZTelemetry records the live kinematics and motor state.
type CameraPTZTelemetry struct {
	Pan         float64
	Tilt        float64
	Zoom        float64
	MoveStatus  string // "IDLE" or "MOVING"
	LastCommand string
	LastUpdated time.Time
}

// CameraEventTelemetry records active simulated analytics events.
type CameraEventTelemetry struct {
	MotionActive  bool
	MotionExpiry  time.Time
	HumanActive   bool
	HumanExpiry   time.Time
	HumanBBox     [4]float64 // x, y, w, h
	VehicleActive bool
	VehicleExpiry time.Time
	VehicleBBox   [4]float64
}

// Hub is the central thread-safe telemetry registry.
type Hub struct {
	mu           sync.RWMutex
	startTime    time.Time
	fleetSize    int
	sessions     map[string]*ClientSession
	auditLog     []SOAPAuditEntry
	auditLimit   int
	ptzStates    map[string]*CameraPTZTelemetry
	eventStates  map[string]*CameraEventTelemetry
	subscribers  map[string]int // cam -> count of active pull-point subscribers
	eventsSent   map[string]int // topic -> count
	chaosFrozen  bool
	chaosReboot  bool
	chaosLossPct int
	mainFPS      float64
	subFPS       float64
}

// NewHub creates and initializes the Telemetry Hub.
func NewHub(fleetSize int) *Hub {
	if fleetSize <= 0 {
		fleetSize = 1
	}
	h := &Hub{
		startTime:    time.Now(),
		fleetSize:    fleetSize,
		sessions:     make(map[string]*ClientSession),
		auditLimit:   10,
		ptzStates:    make(map[string]*CameraPTZTelemetry),
		eventStates:  make(map[string]*CameraEventTelemetry),
		subscribers:  make(map[string]int),
		eventsSent:   make(map[string]int),
		mainFPS:      20.0,
		subFPS:       10.0,
	}

	for i := 0; i < fleetSize; i++ {
		camName := getCamName(i)
		h.ptzStates[camName] = &CameraPTZTelemetry{
			MoveStatus:  "IDLE",
			LastCommand: "NONE",
			LastUpdated: time.Now(),
		}
		h.eventStates[camName] = &CameraEventTelemetry{}
	}

	return h
}

func getCamName(idx int) string {
	return "cam" + string(rune('0'+idx))
}

func (h *Hub) SetFleetSize(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fleetSize = n
	for i := 0; i < n; i++ {
		camName := getCamName(i)
		if _, ok := h.ptzStates[camName]; !ok {
			h.ptzStates[camName] = &CameraPTZTelemetry{
				MoveStatus:  "IDLE",
				LastCommand: "NONE",
				LastUpdated: time.Now(),
			}
			h.eventStates[camName] = &CameraEventTelemetry{}
		}
	}
}

func (h *Hub) Uptime() time.Duration {
	return time.Since(h.startTime)
}

// RegisterSession records a newly opened RTSP session.
func (h *Hub) RegisterSession(id, remoteIP string, port int, userAgent string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sessions[id] = &ClientSession{
		ID:          id,
		RemoteIP:    remoteIP,
		Port:        port,
		Camera:      "cam0",
		Stream:      "main",
		Transport:   "TCP",
		UserAgent:   userAgent,
		State:       "INIT",
		ConnectTime: time.Now(),
		lastCalc:    time.Now(),
	}
}

// UpdateSessionPath updates stream routing info upon SETUP/PLAY.
func (h *Hub) UpdateSessionPath(id, camera, stream, transport, state string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.sessions[id]; ok {
		s.Camera = camera
		s.Stream = stream
		s.Transport = transport
		s.State = state
	}
}

// RecordSessionBytes updates byte counters for bitrate calculation.
func (h *Hub) RecordSessionBytes(id string, bytes uint64, packets uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.sessions[id]; ok {
		s.BytesSent += bytes
		s.PacketsSent += packets
		now := time.Now()
		elapsed := now.Sub(s.lastCalc).Seconds()
		if elapsed >= 1.0 {
			diffBytes := s.BytesSent - s.lastBytes
			s.BitrateKbps = (float64(diffBytes) * 8.0) / (elapsed * 1000.0)
			s.lastBytes = s.BytesSent
			s.lastCalc = now
		}
	}
}

// UnregisterSession removes a closed session.
func (h *Hub) UnregisterSession(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, id)
}

// GetSessions returns a snapshot copy of active sessions.
func (h *Hub) GetSessions() []ClientSession {
	h.mu.RLock()
	defer h.mu.RUnlock()

	list := make([]ClientSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		list = append(list, *s)
	}
	return list
}

// RecordSOAPAction adds an entry to the audit log.
func (h *Hub) RecordSOAPAction(camera, action, clientIP, outcome string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := SOAPAuditEntry{
		Timestamp:   time.Now(),
		Camera:      camera,
		Action:      action,
		ClientIP:    clientIP,
		AuthOutcome: outcome,
	}

	h.auditLog = append(h.auditLog, entry)
	if len(h.auditLog) > h.auditLimit {
		h.auditLog = h.auditLog[len(h.auditLog)-h.auditLimit:]
	}
}

// GetAuditLog returns a snapshot of recent SOAP actions.
func (h *Hub) GetAuditLog() []SOAPAuditEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	res := make([]SOAPAuditEntry, len(h.auditLog))
	copy(res, h.auditLog)
	return res
}

// UpdatePTZ sets coordinates and motion status for a camera.
func (h *Hub) UpdatePTZ(camera string, pan, tilt, zoom float64, status, lastCmd string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	st, ok := h.ptzStates[camera]
	if !ok {
		st = &CameraPTZTelemetry{}
		h.ptzStates[camera] = st
	}
	st.Pan = pan
	st.Tilt = tilt
	st.Zoom = zoom
	st.MoveStatus = status
	st.LastCommand = lastCmd
	st.LastUpdated = time.Now()
}

// GetPTZ returns PTZ telemetry for a camera.
func (h *Hub) GetPTZ(camera string) CameraPTZTelemetry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if st, ok := h.ptzStates[camera]; ok {
		return *st
	}
	return CameraPTZTelemetry{MoveStatus: "IDLE", LastCommand: "NONE"}
}

// SetEventActive updates event telemetry states.
func (h *Hub) SetEventActive(camera, eventType string, active bool, expiry time.Time, bbox [4]float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	st, ok := h.eventStates[camera]
	if !ok {
		st = &CameraEventTelemetry{}
		h.eventStates[camera] = st
	}

	switch eventType {
	case "motion":
		st.MotionActive = active
		st.MotionExpiry = expiry
	case "human":
		st.HumanActive = active
		st.HumanExpiry = expiry
		st.HumanBBox = bbox
	case "vehicle":
		st.VehicleActive = active
		st.VehicleExpiry = expiry
		st.VehicleBBox = bbox
	}
}

// GetEvents returns the event state for a camera.
func (h *Hub) GetEvents(camera string) CameraEventTelemetry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if st, ok := h.eventStates[camera]; ok {
		// Auto-clear expired events
		now := time.Now()
		res := *st
		if res.MotionActive && !res.MotionExpiry.IsZero() && now.After(res.MotionExpiry) {
			res.MotionActive = false
		}
		if res.HumanActive && !res.HumanExpiry.IsZero() && now.After(res.HumanExpiry) {
			res.HumanActive = false
		}
		if res.VehicleActive && !res.VehicleExpiry.IsZero() && now.After(res.VehicleExpiry) {
			res.VehicleActive = false
		}
		return res
	}
	return CameraEventTelemetry{}
}

// Chaos controls
func (h *Hub) SetChaosFrozen(frozen bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chaosFrozen = frozen
}

func (h *Hub) IsChaosFrozen() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.chaosFrozen
}

func (h *Hub) SetChaosReboot(rebooting bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chaosReboot = rebooting
}

func (h *Hub) IsChaosReboot() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.chaosReboot
}

func (h *Hub) SetChaosLossPct(pct int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chaosLossPct = pct
}

func (h *Hub) GetChaosLossPct() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.chaosLossPct
}

// Stats summary
func (h *Hub) GetOverviewStats() (activeClients int, totalBitrateKbps float64, fleetSize int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	activeClients = len(h.sessions)
	for _, s := range h.sessions {
		totalBitrateKbps += s.BitrateKbps
	}
	fleetSize = h.fleetSize
	return
}
