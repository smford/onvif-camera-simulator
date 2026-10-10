package onvif

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/ptz"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// CameraDelegate provides camera-specific subsystems.
type CameraDelegate interface {
	ID() string
	PTZ() *ptz.VirtualPTZ
	Config() config.CameraConfig
}

// Server handles ONVIF SOAP 1.2 and HTTP Snapshot requests.
type Server struct {
	cfg          *config.Config
	hub          *telemetry.Hub
	eventMgr     *events.Manager
	cameras      map[string]CameraDelegate
	defaultCam   string
	httpServer   *http.Server
	listener     net.Listener
	mu           sync.RWMutex
	tlsCert      *tls.Certificate
	rebootActive bool
	apiRoutes    func(mux *http.ServeMux)
}

// NewServer initializes the ONVIF HTTP/HTTPS server.
func NewServer(cfg *config.Config, hub *telemetry.Hub, eventMgr *events.Manager) *Server {
	s := &Server{
		cfg:        cfg,
		hub:        hub,
		eventMgr:   eventMgr,
		cameras:    make(map[string]CameraDelegate),
		defaultCam: "cam0",
	}

	if cfg.Server.TLSEnabled {
		cert, err := generateSelfSignedCert()
		if err != nil {
			slog.Error("Failed to generate ephemeral TLS certificate", "err", err)
		} else {
			s.tlsCert = &cert
			slog.Info("Ephemeral self-signed TLS certificate generated successfully")
		}
	}

	return s
}

// RegisterCamera registers a virtual camera instance with the SOAP router.
func (s *Server) RegisterCamera(cam CameraDelegate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cameras[cam.ID()] = cam
	if s.defaultCam == "" || s.defaultCam == "cam0" {
		s.defaultCam = cam.ID()
	}
}

// SetAPIRoutes allows registering additional REST API routes on the HTTP server.
func (s *Server) SetAPIRoutes(fn func(mux *http.ServeMux)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiRoutes = fn
}

// Start begins listening on the configured HTTP/HTTPS port.
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.BindAddress, s.cfg.Server.HTTPPort)
	mux := http.NewServeMux()

	// Register catch-all ONVIF handler
	mux.HandleFunc("/onvif/", s.handleONVIF)
	mux.HandleFunc("/onvif", s.handleONVIF)

	if s.apiRoutes != nil {
		s.apiRoutes(mux)
	}

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	var ln net.Listener
	var err error
	if s.tlsCert != nil {
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{*s.tlsCert},
		}
		ln, err = tls.Listen("tcp", addr, tlsConfig)
	} else {
		ln, err = net.Listen("tcp", addr)
	}

	if err != nil {
		return fmt.Errorf("failed to bind ONVIF HTTP listener on %s: %w", addr, err)
	}

	s.listener = ln
	slog.Info("ONVIF HTTP service listening", "addr", addr, "tls", s.tlsCert != nil)

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("ONVIF HTTP server terminated unexpectedly", "err", err)
		}
	}()

	return nil
}

// Stop terminates the ONVIF HTTP listener cleanly.
func (s *Server) Stop() error {
	if s.httpServer != nil {
		ctx, cancel := contextWithTimeout(5 * time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// DropConnections temporarily closes the listener to simulate a hard reboot.
func (s *Server) DropConnections(duration time.Duration) {
	s.mu.Lock()
	s.rebootActive = true
	s.mu.Unlock()

	if s.listener != nil {
		s.listener.Close()
	}

	go func() {
		time.Sleep(duration)
		s.mu.Lock()
		s.rebootActive = false
		s.mu.Unlock()
		if err := s.Start(); err != nil {
			slog.Error("Failed to rebind ONVIF HTTP listener after reboot", "err", err)
		}
	}()
}

func (s *Server) handleONVIF(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	if s.rebootActive {
		s.mu.RUnlock()
		http.Error(w, "Camera Rebooting", http.StatusServiceUnavailable)
		return
	}
	s.mu.RUnlock()

	path := r.URL.Path

	// Parse camera instance from path if fleet mode (/onvif/cam{N}/...)
	camID := s.defaultCam
	for id := range s.cameras {
		if strings.HasPrefix(path, "/onvif/"+id+"/") || path == "/onvif/"+id {
			camID = id
			break
		}
	}

	s.mu.RLock()
	cam, ok := s.cameras[camID]
	s.mu.RUnlock()
	if !ok && len(s.cameras) > 0 {
		for _, c := range s.cameras {
			cam = c
			break
		}
	}

	// 1. Snapshot endpoint: /onvif/snapshot or /onvif/{cam}/snapshot (or GET /onvif/{cam}/device_service)
	if strings.HasSuffix(path, "/snapshot") || (r.Method == http.MethodGet && strings.HasSuffix(path, "/device_service")) {
		s.handleSnapshot(w, r, cam)
		return
	}

	// 2. SOAP XML processing
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var env SOAPEnvelope
	if err := xml.Unmarshal(bodyBytes, &env); err != nil {
		// Attempt non-strict parsing or return fault
		slog.Warn("Failed to unmarshal SOAP Envelope", "err", err)
	}

	clientIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(clientIP); err == nil {
		clientIP = host
	}

	// Authenticate
	authOK, authOutcome := Authenticate(s.cfg.Auth, &env)
	action := extractSOAPAction(r, bodyBytes)
	s.hub.RecordSOAPAction(camID, action, clientIP, authOutcome)

	if !authOK {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(FormatSOAPFault("wsse:FailedAuthentication", "The security token could not be authenticated or authorized")))
		return
	}

	// Dispatch request
	s.dispatchSOAP(w, r, cam, action, bodyBytes, path)
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request, cam CameraDelegate) {
	var pan, tilt, zoom float64
	var camID = "cam0"
	if cam != nil {
		camID = cam.ID()
		pan, tilt, zoom, _, _ = cam.PTZ().GetStatus()
	}

	ev := s.hub.GetEvents(camID)
	data, err := GenerateSnapshot(camID, 1280, 720, pan, tilt, zoom, ev)
	if err != nil {
		http.Error(w, "Failed to render snapshot: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) dispatchSOAP(w http.ResponseWriter, r *http.Request, cam CameraDelegate, action string, bodyBytes []byte, path string) {
	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")

	scheme := "http"
	if s.tlsCert != nil {
		scheme = "https"
	}
	advIP := s.cfg.Server.AdvertisedIP
	httpPort := s.cfg.Server.HTTPPort
	rtspPort := s.cfg.Server.RTSPPort
	camID := "cam0"
	if cam != nil {
		camID = cam.ID()
	}

	baseURL := fmt.Sprintf("%s://%s:%d/onvif", scheme, advIP, httpPort)
	if s.cfg.Fleet.Count > 1 {
		baseURL = fmt.Sprintf("%s://%s:%d/onvif/%s", scheme, advIP, httpPort, camID)
	}

	rtspMain := fmt.Sprintf("rtsp://%s:%d/live/main", advIP, rtspPort)
	rtspSub := fmt.Sprintf("rtsp://%s:%d/live/sub", advIP, rtspPort)
	if s.cfg.Fleet.Count > 1 {
		rtspMain = fmt.Sprintf("rtsp://%s:%d/live/%s/main", advIP, rtspPort, camID)
		rtspSub = fmt.Sprintf("rtsp://%s:%d/live/%s/sub", advIP, rtspPort, camID)
	}
	snapshotURL := fmt.Sprintf("%s://%s:%d/onvif/snapshot", scheme, advIP, httpPort)
	if s.cfg.Fleet.Count > 1 {
		snapshotURL = fmt.Sprintf("%s://%s:%d/onvif/%s/snapshot", scheme, advIP, httpPort, camID)
	}

	// 1. Device Service
	switch {
	case strings.Contains(action, "GetSystemDateAndTime"):
		w.Write([]byte(FormatGetSystemDateAndTimeResponse()))
		return
	case strings.Contains(action, "GetDeviceInformation"):
		camCfg := s.cfg.Camera
		if cam != nil {
			camCfg = cam.Config()
		}
		w.Write([]byte(FormatGetDeviceInformationResponse(camCfg)))
		return
	case strings.Contains(action, "GetScopes"):
		camCfg := s.cfg.Camera
		if cam != nil {
			camCfg = cam.Config()
		}
		w.Write([]byte(FormatGetScopesResponse(camCfg)))
		return
	case strings.Contains(action, "SystemReboot"):
		w.Write([]byte(FormatSystemRebootResponse()))
		go func() {
			time.Sleep(500 * time.Millisecond)
			s.DropConnections(5 * time.Second)
		}()
		return
	case strings.Contains(action, "GetCapabilities"):
		w.Write([]byte(FormatGetCapabilitiesResponse(baseURL)))
		return
	case strings.Contains(action, "GetServices"):
		w.Write([]byte(FormatGetServicesResponse(baseURL)))
		return
	case strings.Contains(action, "GetHostname"):
		w.Write([]byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <soap:Body>
    <tds:GetHostnameResponse>
      <tds:HostnameInformation><tt:FromDHCP>false</tt:FromDHCP><tt:Name>%s</tt:Name></tds:HostnameInformation>
    </tds:GetHostnameResponse>
  </soap:Body>
</soap:Envelope>`, camID)))
		return
	case strings.Contains(action, "GetNetworkInterfaces"):
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">
  <soap:Body>
    <tds:GetNetworkInterfacesResponse>
      <tds:NetworkInterfaces token="eth0">
        <tt:Enabled>true</tt:Enabled>
        <tt:Info><tt:Name>eth0</tt:Name><tt:HwAddress>00:1A:2B:3C:4D:5E</tt:HwAddress></tt:Info>
        <tt:IPv4><tt:Enabled>true</tt:Enabled><tt:Config><tt:Manual><tt:Address>127.0.0.1</tt:Address><tt:PrefixLength>24</tt:PrefixLength></tt:Manual><tt:DHCP>false</tt:DHCP></tt:Config></tt:IPv4>
      </tds:NetworkInterfaces>
    </tds:GetNetworkInterfacesResponse>
  </soap:Body>
</soap:Envelope>`))
		return

	// 2. Media Service
	case strings.Contains(action, "GetProfiles") || strings.Contains(action, "GetProfile"):
		w.Write([]byte(FormatGetProfilesResponse()))
		return
	case strings.Contains(action, "GetStreamUri"):
		streamURL := rtspMain
		lowerBody := strings.ToLower(string(bodyBytes))
		if strings.Contains(lowerBody, "sub") {
			streamURL = rtspSub
		}
		w.Write([]byte(FormatGetStreamUriResponse(streamURL)))
		return
	case strings.Contains(action, "GetSnapshotUri"):
		w.Write([]byte(FormatGetSnapshotUriResponse(snapshotURL)))
		return
	case strings.Contains(action, "GetVideoEncoderConfigurationOptions"):
		w.Write([]byte(FormatGetVideoEncoderConfigurationOptionsResponse()))
		return

	// 3. PTZ Service
	case strings.Contains(action, "GetConfigurations") || strings.Contains(action, "GetConfiguration"):
		w.Write([]byte(FormatGetPTZConfigurationsResponse()))
		return
	case strings.Contains(action, "GetNodes") || strings.Contains(action, "GetNode"):
		w.Write([]byte(FormatGetPTZNodesResponse()))
		return
	case strings.Contains(action, "GetStatus"):
		pan, tilt, zoom, status, utc := 0.0, 0.0, 0.0, "IDLE", time.Now().UTC()
		if cam != nil {
			pan, tilt, zoom, status, utc = cam.PTZ().GetStatus()
		}
		w.Write([]byte(FormatGetPTZStatusResponse(pan, tilt, zoom, status, utc)))
		return
	case strings.Contains(action, "ContinuousMove"):
		vx, vy, vz, _ := ParsePanTiltZoom(bodyBytes)
		if cam != nil {
			cam.PTZ().ContinuousMove(vx, vy, vz, 10*time.Second)
		}
		w.Write([]byte(FormatPTZSuccessResponse("ContinuousMove")))
		return
	case strings.Contains(action, "AbsoluteMove"):
		px, py, pz, _ := ParsePanTiltZoom(bodyBytes)
		if cam != nil {
			cam.PTZ().AbsoluteMove(px, py, pz)
		}
		w.Write([]byte(FormatPTZSuccessResponse("AbsoluteMove")))
		return
	case strings.Contains(action, "RelativeMove"):
		dx, dy, dz, _ := ParsePanTiltZoom(bodyBytes)
		if cam != nil {
			cam.PTZ().RelativeMove(dx, dy, dz)
		}
		w.Write([]byte(FormatPTZSuccessResponse("RelativeMove")))
		return
	case strings.Contains(action, "Stop"):
		if cam != nil {
			cam.PTZ().Stop()
		}
		w.Write([]byte(FormatPTZSuccessResponse("Stop")))
		return
	case strings.Contains(action, "GetPresets"):
		presets := []ptz.Preset{}
		if cam != nil {
			presets = cam.PTZ().GetPresets()
		}
		w.Write([]byte(FormatGetPresetsResponse(presets)))
		return
	case strings.Contains(action, "SetPreset"):
		tok := "preset_1"
		if cam != nil {
			tok = cam.PTZ().SetPreset("Preset")
		}
		w.Write([]byte(FormatSetPresetResponse(tok)))
		return
	case strings.Contains(action, "GotoPreset"):
		tok := ParsePresetToken(bodyBytes)
		if cam != nil {
			_ = cam.PTZ().GotoPreset(tok)
		}
		w.Write([]byte(FormatPTZSuccessResponse("GotoPreset")))
		return
	case strings.Contains(action, "RemovePreset"):
		tok := ParsePresetToken(bodyBytes)
		if cam != nil {
			cam.PTZ().RemovePreset(tok)
		}
		w.Write([]byte(FormatPTZSuccessResponse("RemovePreset")))
		return

	// 4. Events Service
	case strings.Contains(action, "GetEventProperties"):
		w.Write([]byte(events.FormatGetEventPropertiesResponse()))
		return
	case strings.Contains(action, "CreatePullPointSubscription"):
		sub := s.eventMgr.CreateSubscription(camID, 60*time.Second)
		subURL := fmt.Sprintf("%s/events/%s", baseURL, sub.ID)
		w.Write([]byte(events.FormatCreatePullPointSubscriptionResponse(subURL, time.Now().UTC(), sub.Expires)))
		return
	case strings.Contains(action, "PullMessages"):
		s.handlePullMessages(w, r, camID, bodyBytes, path)
		return
	case strings.Contains(action, "Renew"):
		s.handleRenew(w, r, path)
		return
	case strings.Contains(action, "Unsubscribe"):
		s.handleUnsubscribe(w, r, path)
		return

	default:
		// Fallback generic response
		w.Write([]byte(FormatSOAPFault("soap:ActionNotSupported", fmt.Sprintf("Action %q is not implemented", action))))
	}
}

func (s *Server) handlePullMessages(w http.ResponseWriter, r *http.Request, camID string, bodyBytes []byte, path string) {
	subID := extractSubID(path)
	sub, ok := s.eventMgr.GetSubscription(subID)
	if !ok {
		// If subscription not found directly, create or find any subscription for camera
		sub = s.eventMgr.CreateSubscription(camID, 60*time.Second)
	}

	// Long-polling: default timeout 5s
	timeout := 5 * time.Second
	if strings.Contains(string(bodyBytes), "PT") {
		// simple timeout heuristic
		timeout = 3 * time.Second
	}

	evList := sub.Pull(10, timeout)
	now := time.Now().UTC()
	w.Write([]byte(events.FormatPullMessagesResponse(now, sub.Expires, evList)))
}

func (s *Server) handleRenew(w http.ResponseWriter, r *http.Request, path string) {
	subID := extractSubID(path)
	expires, err := s.eventMgr.Renew(subID, 60*time.Second)
	if err != nil {
		expires = time.Now().UTC().Add(60 * time.Second)
	}
	w.Write([]byte(events.FormatRenewResponse(time.Now().UTC(), expires)))
}

func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request, path string) {
	subID := extractSubID(path)
	s.eventMgr.Unsubscribe(subID)
	w.Write([]byte(events.FormatUnsubscribeResponse()))
}

func extractSubID(path string) string {
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "sub_") {
			return p
		}
	}
	return ""
}

func extractSOAPAction(r *http.Request, body []byte) string {
	if a := r.Header.Get("SOAPAction"); a != "" {
		return strings.Trim(a, `"`)
	}

	// SOAP 1.2 action parameter in Content-Type header
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "action=") {
		parts := strings.Split(ct, "action=")
		if len(parts) > 1 {
			actVal := strings.Trim(strings.Split(parts[1], ";")[0], `" `)
			if actVal != "" {
				subParts := strings.Split(actVal, "/")
				return subParts[len(subParts)-1]
			}
		}
	}

	// Extract from Body XML tag
	str := string(body)
	for _, action := range []string{
		"GetSystemDateAndTime", "GetDeviceInformation", "GetScopes", "SystemReboot", "GetCapabilities", "GetServices",
		"GetProfiles", "GetProfile", "GetStreamUri", "GetSnapshotUri", "GetVideoEncoderConfigurationOptions",
		"GetConfigurations", "GetNodes", "GetStatus", "ContinuousMove", "AbsoluteMove",
		"RelativeMove", "Stop", "GetPresets", "SetPreset", "GotoPreset", "RemovePreset",
		"GetEventProperties", "CreatePullPointSubscription", "PullMessages", "Renew", "Unsubscribe",
		"GetHostname", "GetNetworkInterfaces",
	} {
		if strings.Contains(str, action) {
			return action
		}
	}
	return "Unknown"
}

func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Simulated ONVIF SRE Camera"},
			CommonName:   "localhost",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("0.0.0.0")},
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}, nil
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
