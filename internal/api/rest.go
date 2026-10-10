package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/chaos"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/metrics"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// EventTriggerRequest is the JSON schema for POST /api/events/trigger.
type EventTriggerRequest struct {
	Camera          any        `json:"camera"` // can be int (0) or string ("cam0")
	Type            string     `json:"type"`   // "motion", "human", "vehicle"
	State           bool       `json:"state"`
	DurationSeconds int        `json:"duration_seconds"`
	BBox            [4]float64 `json:"bbox"` // [x, y, w, h]
}

// ChaosInjectRequest is the JSON schema for POST /api/chaos/inject.
type ChaosInjectRequest struct {
	Action          string `json:"action"` // "reboot", "freeze", "packet_loss"
	DurationSeconds int    `json:"duration_seconds"`
	LossPercentage  int    `json:"loss_percentage"`
}

// APIHandler wraps event and chaos triggers.
type APIHandler struct {
	hub        *telemetry.Hub
	eventMgr   *events.Manager
	chaosEng   *chaos.Engine
	logHandler *telemetry.RingBufferLogHandler
}

// NewAPIHandler creates the REST API controller.
func NewAPIHandler(hub *telemetry.Hub, eventMgr *events.Manager, chaosEng *chaos.Engine, logHandler *telemetry.RingBufferLogHandler) *APIHandler {
	return &APIHandler{
		hub:        hub,
		eventMgr:   eventMgr,
		chaosEng:   chaosEng,
		logHandler: logHandler,
	}
}

// RegisterRoutes registers endpoints on an http.ServeMux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/readyz", h.handleReadyz)
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/api/status", h.handleStatus)
	mux.HandleFunc("/api/cameras", h.handleCameras)
	mux.HandleFunc("/api/logs", h.handleLogs)
	mux.HandleFunc("/api/events/trigger", h.handleEventTrigger)
	mux.HandleFunc("/api/chaos/inject", h.handleChaosInject)
}

func (h *APIHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if h.logHandler == nil {
		json.NewEncoder(w).Encode([]any{})
		return
	}
	entries := h.logHandler.GetEntries()
	json.NewEncoder(w).Encode(entries)
}

func (h *APIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	activeClients, totalBitrate, fleetSize := h.hub.GetOverviewStats()
	sessions := h.hub.GetSessions()

	status := map[string]any{
		"uptime_seconds": int(h.hub.Uptime().Seconds()),
		"fleet_size":     fleetSize,
		"active_clients": activeClients,
		"total_bitrate":  totalBitrate,
		"chaos": map[string]any{
			"frozen":          h.hub.IsChaosFrozen(),
			"rebooting":       h.hub.IsChaosReboot(),
			"packet_loss_pct": h.hub.GetChaosLossPct(),
		},
		"sessions_count": len(sessions),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (h *APIHandler) handleCameras(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, _, fleetSize := h.hub.GetOverviewStats()
	cams := make([]map[string]any, 0, fleetSize)
	for i := 0; i < fleetSize; i++ {
		camID := fmt.Sprintf("cam%d", i)
		ptz := h.hub.GetPTZ(camID)
		events := h.hub.GetEvents(camID)

		camInfo := map[string]any{
			"id": camID,
			"ptz": map[string]any{
				"pan":         ptz.Pan,
				"tilt":        ptz.Tilt,
				"zoom":        ptz.Zoom,
				"move_status": ptz.MoveStatus,
				"last_cmd":    ptz.LastCommand,
			},
			"events": map[string]any{
				"motion_active":  events.MotionActive,
				"human_active":   events.HumanActive,
				"human_bbox":     events.HumanBBox,
				"vehicle_active": events.VehicleActive,
				"vehicle_bbox":   events.VehicleBBox,
			},
		}
		cams = append(cams, camInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cams)
}

func (h *APIHandler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK\n"))
}

func (h *APIHandler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// Ready if not currently in hard reboot
	if h.hub.IsChaosReboot() {
		http.Error(w, "Rebooting", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("READY\n"))
}

func (h *APIHandler) handleEventTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req EventTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	camStr := parseCamID(req.Camera)
	dur := time.Duration(req.DurationSeconds) * time.Second

	switch req.Type {
	case "motion":
		h.eventMgr.TriggerMotion(camStr, req.State, dur)
	case "human":
		h.eventMgr.TriggerHuman(camStr, req.State, dur, req.BBox)
	case "vehicle":
		h.eventMgr.TriggerVehicle(camStr, req.State, dur, req.BBox)
	default:
		http.Error(w, fmt.Sprintf("Unknown event type: %q (expected 'motion', 'human', 'vehicle')", req.Type), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "success",
		"camera":  camStr,
		"type":    req.Type,
		"state":   req.State,
		"expires": time.Now().Add(dur).Format(time.RFC3339),
	})
}

func (h *APIHandler) handleChaosInject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChaosInjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	dur := time.Duration(req.DurationSeconds) * time.Second

	switch req.Action {
	case "freeze":
		h.chaosEng.TriggerFreeze(dur)
	case "unfreeze":
		h.chaosEng.Unfreeze()
	case "reboot":
		h.chaosEng.TriggerReboot(dur)
	case "packet_loss":
		h.chaosEng.SetPacketLoss(req.LossPercentage, dur)
	default:
		http.Error(w, fmt.Sprintf("Unknown chaos action: %q (expected 'freeze', 'unfreeze', 'reboot', 'packet_loss')", req.Action), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "injected",
		"action": req.Action,
	})
}

func parseCamID(v any) string {
	switch val := v.(type) {
	case float64:
		return fmt.Sprintf("cam%d", int(val))
	case int:
		return fmt.Sprintf("cam%d", val)
	case string:
		if val == "" {
			return "cam0"
		}
		if !strings.HasPrefix(val, "cam") {
			return "cam" + val
		}
		return val
	default:
		return "cam0"
	}
}
