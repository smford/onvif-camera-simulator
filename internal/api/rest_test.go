package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/chaos"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

func setupTestAPI() (*APIHandler, *http.ServeMux, *telemetry.Hub, *events.Manager, *chaos.Engine, *telemetry.RingBufferLogHandler) {
	hub := telemetry.NewHub(2)
	eventMgr := events.NewManager(hub)
	chaosEng := chaos.NewEngine(hub, nil)
	ring := telemetry.NewRingBufferLogHandler(10, slog.LevelDebug)

	// Record a sample log
	rec := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "REST test log entry",
	}
	_ = ring.Handle(context.Background(), rec)

	handler := NewAPIHandler(hub, eventMgr, chaosEng, ring)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	return handler, mux, hub, eventMgr, chaosEng, ring
}

func TestHealthAndReadyEndpoints(t *testing.T) {
	_, mux, hub, _, _, _ := setupTestAPI()

	// Test /healthz
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for healthz, got %d", w.Code)
	}

	// Test /readyz (online)
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for readyz, got %d", w.Code)
	}

	// Test /readyz during reboot
	hub.SetChaosReboot(true)
	req = httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for readyz during reboot, got %d", w.Code)
	}
}

func TestStatusAndCamerasEndpoints(t *testing.T) {
	_, mux, _, _, _, _ := setupTestAPI()

	// Test /api/status
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/status, got %d", w.Code)
	}

	var status map[string]any
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode /api/status JSON: %v", err)
	}
	if status["fleet_size"] != float64(2) {
		t.Errorf("expected fleet_size 2, got %v", status["fleet_size"])
	}

	// Test /api/cameras
	req = httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/cameras, got %d", w.Code)
	}

	var cams []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&cams); err != nil {
		t.Fatalf("failed to decode /api/cameras JSON: %v", err)
	}
	if len(cams) != 2 {
		t.Errorf("expected 2 cameras, got %d", len(cams))
	}
}

func TestLogsEndpoint(t *testing.T) {
	_, mux, _, _, _, _ := setupTestAPI()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/logs, got %d", w.Code)
	}

	var entries []telemetry.LogEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode logs JSON: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	if entries[0].Message != "REST test log entry" {
		t.Errorf("unexpected log message: %s", entries[0].Message)
	}
}

func TestEventTriggerEndpoint(t *testing.T) {
	_, mux, hub, _, _, _ := setupTestAPI()

	payload := EventTriggerRequest{
		Camera:          0,
		Type:            "motion",
		State:           true,
		DurationSeconds: 10,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/events/trigger", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	ev := hub.GetEvents("cam0")
	if !ev.MotionActive {
		t.Errorf("expected motion to be active on cam0")
	}

	// Trigger human event
	payloadHuman := EventTriggerRequest{
		Camera:          "cam1",
		Type:            "human",
		State:           true,
		DurationSeconds: 10,
		BBox:            [4]float64{0.2, 0.2, 0.3, 0.5},
	}
	bodyHuman, _ := json.Marshal(payloadHuman)
	req = httptest.NewRequest(http.MethodPost, "/api/events/trigger", bytes.NewReader(bodyHuman))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	ev1 := hub.GetEvents("cam1")
	if !ev1.HumanActive {
		t.Errorf("expected human to be active on cam1")
	}
}

func TestChaosInjectEndpoint(t *testing.T) {
	_, mux, hub, _, _, _ := setupTestAPI()

	payload := ChaosInjectRequest{
		Action:          "freeze",
		DurationSeconds: 5,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/chaos/inject", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if !hub.IsChaosFrozen() {
		t.Errorf("expected chaos freeze to be active")
	}

	// Unfreeze
	payloadUnfreeze := ChaosInjectRequest{
		Action: "unfreeze",
	}
	bodyUnfreeze, _ := json.Marshal(payloadUnfreeze)
	req = httptest.NewRequest(http.MethodPost, "/api/chaos/inject", bytes.NewReader(bodyUnfreeze))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if hub.IsChaosFrozen() {
		t.Errorf("expected chaos freeze to be cleared")
	}
}
