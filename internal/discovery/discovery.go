package discovery

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/fleet"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

// Server implements WS-Discovery (UDP Multicast 239.255.255.250:3702).
type Server struct {
	cfg      *config.Config
	fleetMgr *fleet.Manager
	conn     *net.UDPConn
	stopCh   chan struct{}
	mu       sync.Mutex
	running  bool
}

// NewServer initializes the WS-Discovery service.
func NewServer(cfg *config.Config, fleetMgr *fleet.Manager) *Server {
	return &Server{
		cfg:      cfg,
		fleetMgr: fleetMgr,
		stopCh:   make(chan struct{}),
	}
}

// CheckContainerEnvironment performs SRE startup diagnostics for Docker/K8s networking.
func CheckContainerEnvironment() {
	inContainer := false
	if _, err := os.Stat("/.dockerenv"); err == nil {
		inContainer = true
	} else if cgroup, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		if strings.Contains(string(cgroup), "docker") || strings.Contains(string(cgroup), "kubepods") || strings.Contains(string(cgroup), "containerd") {
			inContainer = true
		}
	}

	if inContainer {
		slog.Warn("SRE Diagnostics: Container environment detected. Note that WS-Discovery multicast probes (239.255.255.250:3702) will not bridge to the physical host LAN unless started with `network_mode: host`.")
	}
}

// Start binds to the multicast group and listens for SOAP Probe envelopes.
func (s *Server) Start() error {
	if !s.cfg.Discovery.Enabled {
		slog.Info("WS-Discovery is disabled in configuration")
		return nil
	}

	CheckContainerEnvironment()

	mcastAddr, err := net.ResolveUDPAddr("udp4", s.cfg.Discovery.MulticastAddress)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address %s: %w", s.cfg.Discovery.MulticastAddress, err)
	}

	var iface *net.Interface
	if s.cfg.Discovery.MulticastInterface != "" {
		if i, err := net.InterfaceByName(s.cfg.Discovery.MulticastInterface); err == nil {
			iface = i
		} else {
			slog.Warn("Configured multicast interface not found, using default", "interface", s.cfg.Discovery.MulticastInterface, "err", err)
		}
	}

	// Socket configuration using SO_REUSEADDR and SO_REUSEPORT
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var opErr error
			err := c.Control(func(fd uintptr) {
				if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
					opErr = err
					return
				}
				_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
			if err != nil {
				return err
			}
			return opErr
		},
	}

	packetConn, err := lc.ListenPacket(nil, "udp4", fmt.Sprintf(":%d", mcastAddr.Port))
	if err != nil {
		return fmt.Errorf("failed to bind UDP port %d: %w", mcastAddr.Port, err)
	}

	udpConn := packetConn.(*net.UDPConn)
	pConn := ipv4.NewPacketConn(udpConn)
	if err := pConn.JoinGroup(iface, &net.UDPAddr{IP: mcastAddr.IP}); err != nil {
		slog.Warn("Failed to join multicast group (multicast reception may be limited)", "err", err)
	}

	s.conn = udpConn
	s.running = true
	slog.Info("WS-Discovery service listening", "group", s.cfg.Discovery.MulticastAddress, "iface", s.cfg.Discovery.MulticastInterface)

	go s.listenLoop()
	return nil
}

// Stop closes the discovery socket.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.running = false
		close(s.stopCh)
		if s.conn != nil {
			s.conn.Close()
		}
	}
}

func (s *Server) listenLoop() {
	buf := make([]byte, 8192)

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		n, srcAddr, err := s.conn.ReadFrom(buf)
		if err != nil {
			if !s.running {
				return
			}
			continue
		}

		packet := buf[:n]
		s.handlePacket(packet, srcAddr)
	}
}

func (s *Server) handlePacket(packet []byte, srcAddr net.Addr) {
	str := string(packet)
	// Match WS-Discovery Probe
	if !strings.Contains(str, ":Probe") && !strings.Contains(str, "Probe>") {
		return
	}

	// Match Types: dn:NetworkVideoTransmitter or wildcard (empty or omitted)
	isMatch := false
	if !strings.Contains(str, "Types>") || strings.Contains(str, "NetworkVideoTransmitter") || strings.Contains(str, "Device") {
		isMatch = true
	}

	if !isMatch {
		return
	}

	msgID := extractMessageID(str)
	responseXML := s.buildProbeMatchResponse(msgID)

	// Send ProbeMatch to the sender's unicast address
	if _, err := s.conn.WriteTo([]byte(responseXML), srcAddr); err != nil {
		slog.Warn("Failed to send WS-Discovery ProbeMatch", "to", srcAddr, "err", err)
	} else {
		slog.Debug("Sent WS-Discovery ProbeMatch", "to", srcAddr, "relatesTo", msgID)
	}
}

func (s *Server) buildProbeMatchResponse(relatesToMsgID string) string {
	scheme := "http"
	if s.cfg.Server.TLSEnabled {
		scheme = "https"
	}
	advIP := s.cfg.Server.AdvertisedIP
	httpPort := s.cfg.Server.HTTPPort

	cams := s.fleetMgr.GetAllCameras()
	if len(cams) == 0 {
		return ""
	}

	var matchesBuf bytes.Buffer
	for _, c := range cams {
		xaddr := fmt.Sprintf("%s://%s:%d/onvif/device_service", scheme, advIP, httpPort)
		if len(cams) > 1 {
			xaddr = fmt.Sprintf("%s://%s:%d/onvif/%s/device_service", scheme, advIP, httpPort, c.ID())
		}

		matchesBuf.WriteString(fmt.Sprintf(`      <d:ProbeMatch>
        <wsa:EndpointReference>
          <wsa:Address>urn:uuid:%s</wsa:Address>
        </wsa:EndpointReference>
        <d:Types>dn:NetworkVideoTransmitter tds:Device</d:Types>
        <d:Scopes>
          onvif://www.onvif.org/type/video_encoder
          onvif://www.onvif.org/type/ptz
          onvif://www.onvif.org/type/audio
          onvif://www.onvif.org/hardware/%s
          onvif://www.onvif.org/name/%s
          onvif://www.onvif.org/location/Default
        </d:Scopes>
        <d:XAddrs>%s</d:XAddrs>
        <d:MetadataVersion>1</d:MetadataVersion>
      </d:ProbeMatch>
`, c.UUID(), c.Config().Model, c.ID(), xaddr))
	}

	newMsgID := "urn:uuid:" + uuid.New().String()

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing"
               xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
               xmlns:dn="http://www.onvif.org/ver10/network/wsdl"
               xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <soap:Header>
    <wsa:MessageID>%s</wsa:MessageID>
    <wsa:RelatesTo>%s</wsa:RelatesTo>
    <wsa:To>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</wsa:To>
    <wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</wsa:Action>
  </soap:Header>
  <soap:Body>
    <d:ProbeMatches>
%s    </d:ProbeMatches>
  </soap:Body>
</soap:Envelope>`, newMsgID, relatesToMsgID, matchesBuf.String())
}

func extractMessageID(reqXML string) string {
	idx := strings.Index(reqXML, "MessageID>")
	if idx != -1 {
		rest := reqXML[idx+len("MessageID>"):]
		if end := strings.Index(rest, "<"); end != -1 {
			return strings.TrimSpace(rest[:end])
		}
	}
	return "urn:uuid:" + uuid.New().String()
}
