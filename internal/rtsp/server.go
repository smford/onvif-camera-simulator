package rtsp

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/auth"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/media"
	"github.com/smford/onvif-camera-simulator/internal/metrics"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// StreamRoute binds a URL path to an active ServerStream and Media Pipeline.
type StreamRoute struct {
	Path         string
	Camera       string
	StreamType   string // "main" or "sub"
	ServerStream *gortsplib.ServerStream
	Pipeline     *media.Pipeline
}

// Server encapsulates gortsplib RTSP server with authentication, telemetry, and stream routing.
type Server struct {
	cfg             *config.Config
	hub             *telemetry.Hub
	server          *gortsplib.Server
	routes          map[string]*StreamRoute
	mu              sync.RWMutex
	nonce           string
	rebootActive    bool
	activeConns     map[*gortsplib.ServerConn]struct{}
	playingSessions map[*gortsplib.ServerSession]struct{}
}

// NewServer creates a new RTSP server instance.
func NewServer(cfg *config.Config, hub *telemetry.Hub) *Server {
	s := &Server{
		cfg:             cfg,
		hub:             hub,
		routes:          make(map[string]*StreamRoute),
		activeConns:     make(map[*gortsplib.ServerConn]struct{}),
		playingSessions: make(map[*gortsplib.ServerSession]struct{}),
	}

	n, _ := auth.GenerateNonce()
	s.nonce = n

	server := &gortsplib.Server{
		Handler:                  s,
		RTSPAddress:              fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.RTSPPort),
		UDPRTPAddress:            fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.RTSPPort+1000),
		UDPRTCPAddress:           fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.RTSPPort+1001),
		ReadTimeout:              10 * time.Second,
		WriteTimeout:             10 * time.Second,
		DisableRTCPSenderReports: false, // Automatic periodic RTCP Sender Reports enabled
	}
	s.server = server

	return s
}

// RegisterRoute registers a mount point and starts its packet transmitter.
func (s *Server) RegisterRoute(path, camera, streamType string, isMain bool, sourceFile string, fps int, audio bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanPath := normalizePath(path)

	setup, err := media.CreateMediaSetup(isMain, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to initialize media setup for %s: %w", cleanPath, err)
	}

	serverStream := gortsplib.NewServerStream(s.server, setup.Description)
	pipe := media.NewPipeline(camera, streamType, fps, audio, s.hub, serverStream, setup)
	pipe.Start()

	route := &StreamRoute{
		Path:         cleanPath,
		Camera:       camera,
		StreamType:   streamType,
		ServerStream: serverStream,
		Pipeline:     pipe,
	}

	s.routes[cleanPath] = route
	slog.Info("RTSP mount point published", "path", cleanPath, "cam", camera, "stream", streamType, "fps", fps)
	return nil
}

// RegisterAlias maps an alternate mount path to an existing stream route.
func (s *Server) RegisterAlias(aliasPath, targetPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanTarget := normalizePath(targetPath)
	route, ok := s.routes[cleanTarget]
	if !ok {
		return fmt.Errorf("target route not found for alias: %s", cleanTarget)
	}

	cleanAlias := normalizePath(aliasPath)
	s.routes[cleanAlias] = &StreamRoute{
		Path:         cleanAlias,
		Camera:       route.Camera,
		StreamType:   route.StreamType,
		ServerStream: route.ServerStream,
		Pipeline:     route.Pipeline,
	}
	slog.Info("RTSP mount point alias registered", "alias", cleanAlias, "target", cleanTarget)
	return nil
}

// Start launches the RTSP server.
func (s *Server) Start() error {
	slog.Info("Starting RTSP server", "addr", s.server.RTSPAddress)
	return s.server.Start()
}

// Close gracefully closes the RTSP server and associated streams.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	closedPipelines := make(map[*media.Pipeline]struct{})
	closedStreams := make(map[*gortsplib.ServerStream]struct{})
	for _, r := range s.routes {
		if r.Pipeline != nil {
			if _, ok := closedPipelines[r.Pipeline]; !ok {
				r.Pipeline.Stop()
				closedPipelines[r.Pipeline] = struct{}{}
			}
		}
		if r.ServerStream != nil {
			if _, ok := closedStreams[r.ServerStream]; !ok {
				r.ServerStream.Close()
				closedStreams[r.ServerStream] = struct{}{}
			}
		}
	}
	s.server.Close()
}

// DropConnections temporarily terminates all active connections to simulate a hard reboot.
func (s *Server) DropConnections(duration time.Duration) {
	s.mu.Lock()
	s.rebootActive = true
	conns := make([]*gortsplib.ServerConn, 0, len(s.activeConns))
	for c := range s.activeConns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	for _, c := range conns {
		c.Close()
	}

	go func() {
		time.Sleep(duration)
		s.mu.Lock()
		s.rebootActive = false
		s.mu.Unlock()
	}()
}

// gortsplib.ServerHandler implementation:

func (s *Server) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx) {
	s.mu.Lock()
	s.activeConns[ctx.Conn] = struct{}{}
	s.mu.Unlock()

	remote := ctx.Conn.NetConn().RemoteAddr().String()
	host, portStr, _ := net.SplitHostPort(remote)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	s.hub.RegisterSession(remote, host, port, "RTSP Client")
}

func (s *Server) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	s.mu.Lock()
	delete(s.activeConns, ctx.Conn)
	s.mu.Unlock()

	remote := ctx.Conn.NetConn().RemoteAddr().String()
	s.hub.UnregisterSession(remote)
}

func (s *Server) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	// Session established
}

func (s *Server) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	if ctx.Session != nil {
		s.mu.Lock()
		_, wasPlaying := s.playingSessions[ctx.Session]
		delete(s.playingSessions, ctx.Session)
		s.mu.Unlock()

		if wasPlaying {
			path := normalizePath(ctx.Session.SetuppedPath())
			route := s.findRoute(path)
			if route != nil {
				metrics.ConnectedClients.WithLabelValues("rtsp", route.StreamType, route.Camera).Dec()
			}
		}
	}
}

func (s *Server) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	s.mu.RLock()
	reboot := s.rebootActive
	s.mu.RUnlock()
	if reboot {
		return &base.Response{StatusCode: base.StatusServiceUnavailable}, nil, nil
	}

	// 1. Authenticate if enabled
	if s.cfg.Auth.Enabled {
		if err := auth.Validate(
			ctx.Request,
			s.cfg.Auth.Username,
			s.cfg.Auth.Password,
			[]auth.ValidateMethod{auth.ValidateMethodDigestMD5, auth.ValidateMethodBasic},
			"ONVIF-Camera-Simulator",
			s.nonce,
		); err != nil {
			return &base.Response{
				StatusCode: base.StatusUnauthorized,
				Header: base.Header{
					"WWW-Authenticate": auth.GenerateWWWAuthenticate(
						[]auth.ValidateMethod{auth.ValidateMethodDigestMD5, auth.ValidateMethodBasic},
						"ONVIF-Camera-Simulator",
						s.nonce,
					),
				},
			}, nil, nil
		}
	}

	// 2. Route matching
	path := normalizePath(ctx.Path)
	route := s.findRoute(path)
	if route == nil {
		slog.Warn("RTSP DESCRIBE 404 Not Found", "path", ctx.Path)
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, route.ServerStream, nil
}

func (s *Server) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	s.mu.RLock()
	reboot := s.rebootActive
	s.mu.RUnlock()
	if reboot {
		return &base.Response{StatusCode: base.StatusServiceUnavailable}, nil, nil
	}

	// Authenticate if enabled
	if s.cfg.Auth.Enabled {
		if err := auth.Validate(
			ctx.Request,
			s.cfg.Auth.Username,
			s.cfg.Auth.Password,
			[]auth.ValidateMethod{auth.ValidateMethodDigestMD5, auth.ValidateMethodBasic},
			"ONVIF-Camera-Simulator",
			s.nonce,
		); err != nil {
			return &base.Response{
				StatusCode: base.StatusUnauthorized,
				Header: base.Header{
					"WWW-Authenticate": auth.GenerateWWWAuthenticate(
						[]auth.ValidateMethod{auth.ValidateMethodDigestMD5, auth.ValidateMethodBasic},
						"ONVIF-Camera-Simulator",
						s.nonce,
					),
				},
			}, nil, nil
		}
	}

	path := normalizePath(ctx.Path)
	route := s.findRoute(path)
	if route == nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}

	remote := ctx.Conn.NetConn().RemoteAddr().String()
	trans := "TCP"
	if ctx.Transport == gortsplib.TransportUDP {
		trans = "UDP"
	}
	s.hub.UpdateSessionPath(remote, route.Camera, route.StreamType, trans, "SETUP")

	return &base.Response{
		StatusCode: base.StatusOK,
	}, route.ServerStream, nil
}

func (s *Server) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	path := normalizePath(ctx.Path)
	route := s.findRoute(path)
	if route != nil {
		remote := ctx.Conn.NetConn().RemoteAddr().String()
		s.hub.UpdateSessionPath(remote, route.Camera, route.StreamType, "TCP", "PLAY")
		s.mu.Lock()
		if _, alreadyPlaying := s.playingSessions[ctx.Session]; !alreadyPlaying {
			s.playingSessions[ctx.Session] = struct{}{}
			metrics.ConnectedClients.WithLabelValues("rtsp", route.StreamType, route.Camera).Inc()
		}
		s.mu.Unlock()
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

func (s *Server) findRoute(path string) *StreamRoute {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if r, ok := s.routes[path]; ok {
		return r
	}
	// Try prefix matching or trimmed
	for k, v := range s.routes {
		if strings.TrimPrefix(path, "/") == strings.TrimPrefix(k, "/") {
			return v
		}
	}
	return nil
}

func normalizePath(p string) string {
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}
