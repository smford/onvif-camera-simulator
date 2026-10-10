package fleet

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/onvif"
	"github.com/smford/onvif-camera-simulator/internal/ptz"
	"github.com/smford/onvif-camera-simulator/internal/rtsp"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// VirtualCamera encapsulates an individual simulated camera instance in the fleet.
type VirtualCamera struct {
	id    string
	uuid  string
	cfg   config.CameraConfig
	ptz   *ptz.VirtualPTZ
	hub   *telemetry.Hub
	evMgr *events.Manager
	mac   string
}

func (vc *VirtualCamera) ID() string {
	return vc.id
}

func (vc *VirtualCamera) UUID() string {
	return vc.uuid
}

func (vc *VirtualCamera) PTZ() *ptz.VirtualPTZ {
	return vc.ptz
}

func (vc *VirtualCamera) Config() config.CameraConfig {
	return vc.cfg
}

func (vc *VirtualCamera) MAC() string {
	return vc.mac
}

// Manager coordinates multi-instance fleet scaling and multiplexed routing.
type Manager struct {
	mu          sync.RWMutex
	cfg         *config.Config
	hub         *telemetry.Hub
	rtspServer  *rtsp.Server
	onvifServer *onvif.Server
	eventMgr    *events.Manager
	cameras     map[string]*VirtualCamera
	order       []string
}

// NewManager creates a fleet manager.
func NewManager(
	cfg *config.Config,
	hub *telemetry.Hub,
	rtspServer *rtsp.Server,
	onvifServer *onvif.Server,
	eventMgr *events.Manager,
) *Manager {
	return &Manager{
		cfg:         cfg,
		hub:         hub,
		rtspServer:  rtspServer,
		onvifServer: onvifServer,
		eventMgr:    eventMgr,
		cameras:     make(map[string]*VirtualCamera),
	}
}

// InitializeFleet sets up N virtual camera instances with shared underlying bitstream buffers.
func (m *Manager) InitializeFleet(count int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if count <= 0 {
		count = 1
	}

	m.hub.SetFleetSize(count)
	slog.Info("Initializing fleet simulation", "cameras", count)

	for i := 0; i < count; i++ {
		camID := fmt.Sprintf("cam%d", i)
		camUUID := uuid.New().String()
		mac := fmt.Sprintf("02:42:AC:11:%02X:%02X", i/256, i%256)

		camCfg := m.cfg.Camera
		camCfg.SerialNumber = fmt.Sprintf("%s-%03d", m.cfg.Camera.SerialNumber, i)
		camCfg.Model = fmt.Sprintf("%s-Fleet", m.cfg.Camera.Model)

		// Create independent PTZ kinematics engine
		camPTZ := ptz.NewVirtualPTZ(
			camID,
			m.cfg.PTZ.HomePan,
			m.cfg.PTZ.HomeTilt,
			m.cfg.PTZ.HomeZoom,
			m.cfg.PTZ.MaxPanSpeed,
			m.cfg.PTZ.MaxTiltSpeed,
			m.cfg.PTZ.MaxZoomSpeed,
			func(pan, tilt, zoom float64, status, lastCmd string) {
				m.hub.UpdatePTZ(camID, pan, tilt, zoom, status, lastCmd)
			},
		)

		vCam := &VirtualCamera{
			id:    camID,
			uuid:  camUUID,
			cfg:   camCfg,
			ptz:   camPTZ,
			hub:   m.hub,
			evMgr: m.eventMgr,
			mac:   mac,
		}

		m.cameras[camID] = vCam
		m.order = append(m.order, camID)

		// Register with ONVIF server
		m.onvifServer.RegisterCamera(vCam)

		// Register RTSP Mount Points:
		// Dynamic paths for Fleet: /live/cam{N}/main and /live/cam{N}/sub
		mainFleetPath := fmt.Sprintf("/live/%s/main", camID)
		subFleetPath := fmt.Sprintf("/live/%s/sub", camID)

		if err := m.rtspServer.RegisterRoute(
			mainFleetPath,
			camID,
			"main",
			true,
			m.cfg.Streams.Main.SourceFile,
			m.cfg.Streams.Main.FPS,
			m.cfg.Streams.Main.AudioEnabled,
		); err != nil {
			return err
		}

		if err := m.rtspServer.RegisterRoute(
			subFleetPath,
			camID,
			"sub",
			false,
			m.cfg.Streams.Sub.SourceFile,
			m.cfg.Streams.Sub.FPS,
			m.cfg.Streams.Sub.AudioEnabled,
		); err != nil {
			return err
		}

		// If single-camera fleet or cam0, also alias default paths /live/main and /live/sub
		if i == 0 {
			_ = m.rtspServer.RegisterAlias("/live/main", mainFleetPath)
			_ = m.rtspServer.RegisterAlias("/live/sub", subFleetPath)
		}
	}

	return nil
}

// GetCamera returns a camera by ID.
func (m *Manager) GetCamera(id string) (*VirtualCamera, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cam, ok := m.cameras[id]
	return cam, ok
}

// GetAllCameras returns all active camera instances in ordered sequence.
func (m *Manager) GetAllCameras() []*VirtualCamera {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*VirtualCamera, 0, len(m.order))
	for _, id := range m.order {
		if c, ok := m.cameras[id]; ok {
			res = append(res, c)
		}
	}
	return res
}

// Close terminates all virtual camera kinematics loops.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.cameras {
		c.ptz.Close()
	}
}
