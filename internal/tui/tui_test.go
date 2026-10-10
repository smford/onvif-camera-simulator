package tui

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/chaos"
	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

func TestRenderTUI(t *testing.T) {
	cfg := config.DefaultConfig()
	hub := telemetry.NewHub(4)
	ringHandler := telemetry.NewRingBufferLogHandler(250, slog.LevelDebug)
	eventMgr := events.NewManager(hub)
	chaosEng := chaos.NewEngine(hub, func(d time.Duration) {})

	// Simulate sessions
	hub.RegisterSession("sess-1", "192.168.1.104", 54210, "Frigate/0.14.1")
	hub.UpdateSessionPath("sess-1", "cam0", "main", "TCP", "PLAY")
	hub.RecordSessionBytes("sess-1", 12500000, 9500)

	hub.RegisterSession("sess-2", "192.168.1.150", 49182, "HomeAssistant/2026.3")
	hub.UpdateSessionPath("sess-2", "cam0", "sub", "TCP", "PLAY")
	hub.RecordSessionBytes("sess-2", 3400000, 2600)

	hub.RegisterSession("sess-3", "127.0.0.1", 58920, "go2rtc/1.9.4")
	hub.UpdateSessionPath("sess-3", "cam1", "main", "TCP", "PLAY")
	hub.RecordSessionBytes("sess-3", 11200000, 8900)

	// Simulate SOAP audit
	hub.RecordSOAPAction("cam0", "GetDeviceInformation", "192.168.1.104:54210", "SUCCESS")
	hub.RecordSOAPAction("cam0", "GetStreamUri", "192.168.1.104:54210", "SUCCESS")
	hub.RecordSOAPAction("cam0", "PullMessages", "192.168.1.150:49182", "SUCCESS")
	hub.RecordSOAPAction("cam0", "SystemReboot", "192.168.1.188:59100", "ANON")

	// Simulate PTZ & Events
	hub.UpdatePTZ("cam0", 0.45, -0.20, 0.60, "MOVING", "ContinuousMove")
	hub.SetEventActive("cam0", "motion", true, time.Now().Add(5*time.Second), [4]float64{0, 0, 0, 0})
	hub.SetEventActive("cam0", "human", true, time.Now().Add(5*time.Second), [4]float64{0.2, 0.3, 0.15, 0.45})

	model := NewModel(cfg, "config.yaml", hub, ringHandler, eventMgr, chaosEng, nil)
	model.width = 112
	model.height = 36

	out := model.View()
	if !strings.Contains(out, "ONVIF & RTSP SRE SIMULATOR") {
		t.Errorf("expected dashboard view to contain header title, got: %s", out)
	}
	if !strings.Contains(out, "Camera Endpoints & URLs") {
		t.Errorf("expected dashboard view to contain endpoints card, got: %s", out)
	}
	if !strings.Contains(out, "Virtual PTZ Kinematics") {
		t.Errorf("expected dashboard view to contain PTZ card, got: %s", out)
	}

	// Render Logs tab
	model.activeTab = 2
	logsOut := model.View()
	if !strings.Contains(logsOut, "System Event Log") {
		t.Errorf("expected logs view to contain Event Log title, got: %s", logsOut)
	}

	// Render Config tab
	model.activeTab = 1
	configOut := model.View()
	if !strings.Contains(configOut, "Camera Configuration & Live Parameter Tuning") {
		t.Errorf("expected config view to contain Configuration title, got: %s", configOut)
	}
}
