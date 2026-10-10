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
	if iface != nil {
		if err := pConn.JoinGroup(iface, &net.UDPAddr{IP: mcastAddr.IP}); err != nil {
			slog.Warn("Failed to join multicast group", "interface", iface.Name, "err", err)
		}
	} else {
		// On macOS/Darwin, JoinGroup(nil) returns EADDRNOTAVAIL ("can't assign requested address").
		// Enumerate all active multicast-capable IPv4 interfaces and join each one.
		joined := 0
		if ifaces, err := net.Interfaces(); err == nil {
			for _, ifi := range ifaces {
				if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
					continue
				}
				addrs, err := ifi.Addrs()
				if err != nil {
					continue
				}
				hasIPv4 := false
				for _, addr := range addrs {
					if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
						hasIPv4 = true
						break
					}
				}
				if !hasIPv4 {
					continue
				}
				if err := pConn.JoinGroup(&ifi, &net.UDPAddr{IP: mcastAddr.IP}); err == nil {
					joined++
				}
			}
		}
		if joined == 0 {
			if err := pConn.JoinGroup(nil, &net.UDPAddr{IP: mcastAddr.IP}); err != nil {
				slog.Warn("Failed to join multicast group (multicast reception may be limited)", "err", err)
			}
		}
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

	// Validate WS-Discovery Probe Action: http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe
	if !strings.Contains(str, "http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe") {
		return
	}

	// Validate Requested Types: TDS device (tds:Device or wildcard if Types is empty/omitted)
	if strings.Contains(str, "Types>") {
		typesContent := extractTagContent(str, "Types")
		if typesContent != "" && !strings.Contains(typesContent, "tds:Device") && !strings.Contains(typesContent, "Device") {
			return
		}
	}

	msgID := extractMessageID(str)
	if msgID == "" {
		return
	}

	responseXML := s.buildProbeMatchResponse(msgID)
	if responseXML == "" {
		return
	}

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

		camName := c.ID()
		if c.Config().Model != "" {
			camName = c.Config().Model
		}

		matchesBuf.WriteString(fmt.Sprintf(`      <ProbeMatch>
        <EndpointReference>
          <Address>urn:uuid:%s</Address>
        </EndpointReference>
        <Types>tds:Device dn:NetworkVideoTransmitter</Types>
        <Scopes>onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/%s</Scopes>
        <XAddrs>%s</XAddrs>
      </ProbeMatch>
`, c.UUID(), camName, xaddr))
	}

	newMsgID := "urn:uuid:" + uuid.New().String()

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing"
               xmlns="http://schemas.xmlsoap.org/ws/2005/04/discovery"
               xmlns:dn="http://www.onvif.org/ver10/network/wsdl"
               xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <soap:Header>
    <wsa:MessageID>%s</wsa:MessageID>
    <wsa:RelatesTo>%s</wsa:RelatesTo>
    <wsa:To>http://schemas.xmlsoap.org/ws/2004/08/addressing/role/anonymous</wsa:To>
    <wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</wsa:Action>
  </soap:Header>
  <soap:Body>
    <ProbeMatches>
%s    </ProbeMatches>
  </soap:Body>
</soap:Envelope>`, newMsgID, relatesToMsgID, matchesBuf.String())
}

func extractTagContent(xmlStr, tag string) string {
	openTag := "<" + tag
	idx := strings.Index(xmlStr, openTag)
	if idx == -1 {
		// Try with namespace prefix e.g. <d:Types
		openTag = ":" + tag
		idx = strings.Index(xmlStr, openTag)
	}
	if idx != -1 {
		rest := xmlStr[idx:]
		gt := strings.Index(rest, ">")
		if gt != -1 {
			body := rest[gt+1:]
			closeIdx := strings.Index(body, "</")
			if closeIdx != -1 {
				return strings.TrimSpace(body[:closeIdx])
			}
		}
	}
	return ""
}

func extractMessageID(reqXML string) string {
	idx := strings.Index(reqXML, "MessageID>")
	if idx != -1 {
		rest := reqXML[idx+len("MessageID>"):]
		if end := strings.Index(rest, "<"); end != -1 {
			return strings.TrimSpace(rest[:end])
		}
	}
	return ""
}
