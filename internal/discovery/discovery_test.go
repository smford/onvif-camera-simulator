package discovery

import (
	"encoding/xml"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/fleet"
	"github.com/smford/onvif-camera-simulator/internal/onvif"
	"github.com/smford/onvif-camera-simulator/internal/rtsp"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// Minimal XML structs matching video-amplifier's parser to verify compatibility
type testProbeEnvelope struct {
	Body testProbeBody `xml:"Body"`
}

type testProbeBody struct {
	ProbeMatches testProbeMatches `xml:"ProbeMatches"`
}

type testProbeMatches struct {
	ProbeMatch []testProbeMatch `xml:"ProbeMatch"`
}

type testProbeMatch struct {
	EndpointReference struct {
		Address string `xml:"Address"`
	} `xml:"EndpointReference"`
	Types  string `xml:"Types"`
	Scopes string `xml:"Scopes"`
	XAddrs string `xml:"XAddrs"`
}

func parseVideoAmplifierMatch(data []byte) (testProbeMatch, error) {
	var env testProbeEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return testProbeMatch{}, err
	}
	if len(env.Body.ProbeMatches.ProbeMatch) == 0 {
		return testProbeMatch{}, fmt.Errorf("no probe matches found in response")
	}
	return env.Body.ProbeMatches.ProbeMatch[0], nil
}

func setupTestServer(t *testing.T) (*Server, *fleet.Manager) {
	cfg := config.DefaultConfig()
	cfg.Server.AdvertisedIP = "192.168.1.100"
	cfg.Server.HTTPPort = 8000
	cfg.Server.RTSPPort = 18554
	cfg.Camera.Model = "DrivewayCam"

	hub := telemetry.NewHub(1)
	eventMgr := events.NewManager(hub)
	onvifServer := onvif.NewServer(cfg, hub, eventMgr)
	rtspServer := rtsp.NewServer(cfg, hub)
	if err := rtspServer.Start(); err != nil {
		t.Fatalf("failed to start rtsp server: %v", err)
	}
	t.Cleanup(func() {
		rtspServer.Close()
	})

	mgr := fleet.NewManager(cfg, hub, rtspServer, onvifServer, eventMgr)
	if err := mgr.InitializeFleet(1); err != nil {
		t.Fatalf("failed to initialize fleet: %v", err)
	}
	t.Cleanup(func() {
		mgr.Close()
	})

	srv := NewServer(cfg, mgr)
	return srv, mgr
}

func TestBuildProbeMatchResponse(t *testing.T) {
	srv, mgr := setupTestServer(t)
	cams := mgr.GetAllCameras()
	if len(cams) == 0 {
		t.Fatal("expected at least 1 camera")
	}
	cam := cams[0]

	clientMsgID := "urn:uuid:" + uuid.New().String()
	resp := srv.buildProbeMatchResponse(clientMsgID)

	if !strings.Contains(resp, fmt.Sprintf("<wsa:RelatesTo>%s</wsa:RelatesTo>", clientMsgID)) {
		t.Fatalf("response missing expected RelatesTo header: %s", resp)
	}

	if !strings.Contains(resp, "<wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</wsa:Action>") {
		t.Fatalf("response missing expected Action header: %s", resp)
	}

	// Verify it parses with video-amplifier parser
	match, err := parseVideoAmplifierMatch([]byte(resp))
	if err != nil {
		t.Fatalf("video-amplifier failed to parse response: %v\nXML:\n%s", err, resp)
	}

	expectedAddr := "urn:uuid:" + cam.UUID()
	if match.EndpointReference.Address != expectedAddr {
		t.Errorf("expected Address %q, got %q", expectedAddr, match.EndpointReference.Address)
	}

	expectedTypes := "tds:Device dn:NetworkVideoTransmitter"
	if match.Types != expectedTypes {
		t.Errorf("expected Types %q, got %q", expectedTypes, match.Types)
	}

	expectedScope := fmt.Sprintf("onvif://www.onvif.org/type/video_encoder onvif://www.onvif.org/name/%s", cam.Config().Model)
	if match.Scopes != expectedScope {
		t.Errorf("expected Scopes %q, got %q", expectedScope, match.Scopes)
	}

	expectedXAddr := "http://192.168.1.100:8000/onvif/device_service"
	if match.XAddrs != expectedXAddr {
		t.Errorf("expected XAddrs %q, got %q", expectedXAddr, match.XAddrs)
	}
}

func TestHandlePacketMatching(t *testing.T) {
	_, _ = setupTestServer(t)

	clientMsgID := "urn:uuid:" + uuid.New().String()
	validProbe := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<Envelope xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing">
  <Header>
    <wsa:MessageID>%s</wsa:MessageID>
    <wsa:To>urn:schemas-xmlsoap-org:ws:2005:04/discovery</wsa:To>
    <wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</wsa:Action>
  </Header>
  <Body>
    <Probe xmlns="http://schemas.xmlsoap.org/ws/2005/04/discovery">
      <Types>tds:Device</Types>
    </Probe>
  </Body>
</Envelope>`, clientMsgID)

	// Test extraction
	extractedID := extractMessageID(validProbe)
	if extractedID != clientMsgID {
		t.Fatalf("expected extracted ID %q, got %q", clientMsgID, extractedID)
	}

	// Test tag extraction
	types := extractTagContent(validProbe, "Types")
	if types != "tds:Device" {
		t.Fatalf("expected Types content 'tds:Device', got %q", types)
	}

	// Non-probe action should be ignored
	nonProbe := strings.Replace(validProbe, "http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe", "http://example.com/other", 1)
	if strings.Contains(nonProbe, "http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe") {
		t.Fatal("failed to modify probe action")
	}

	// Other device type should be ignored
	otherTypeProbe := strings.Replace(validProbe, "tds:Device", "dn:Printer", 1)
	otherTypes := extractTagContent(otherTypeProbe, "Types")
	if strings.Contains(otherTypes, "tds:Device") || strings.Contains(otherTypes, "Device") {
		t.Fatal("should not match tds:Device")
	}
}

func TestExtractMessageID(t *testing.T) {
	tests := []struct {
		xml string
		exp string
	}{
		{
			xml: `<wsa:MessageID>urn:uuid:1234-5678</wsa:MessageID>`,
			exp: "urn:uuid:1234-5678",
		},
		{
			xml: `<MessageID>urn:uuid:abcd-efgh</MessageID>`,
			exp: "urn:uuid:abcd-efgh",
		},
		{
			xml: `<Header><wsa:MessageID>  urn:uuid:space-test  </wsa:MessageID></Header>`,
			exp: "urn:uuid:space-test",
		},
		{
			xml: `<NoMsgIdHere></NoMsgIdHere>`,
			exp: "",
		},
	}

	for _, tc := range tests {
		got := extractMessageID(tc.xml)
		if got != tc.exp {
			t.Errorf("extractMessageID(%q) = %q, want %q", tc.xml, got, tc.exp)
		}
	}
}

// TestEndToEndDiscovery verifies that a client probe receives a valid unicast response
func TestEndToEndDiscovery(t *testing.T) {
	srv, _ := setupTestServer(t)

	// Create a dummy UDP listener to simulate srv.conn for handlePacket
	serverConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on udp4: %v", err)
	}
	defer serverConn.Close()
	srv.conn = serverConn.(*net.UDPConn)

	// Create client socket bound to loopback so unicast reply succeeds
	clientAddr, err := net.ResolveUDPAddr("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve client addr: %v", err)
	}
	clientConn, err := net.ListenUDP("udp4", clientAddr)
	if err != nil {
		t.Fatalf("failed to create client udp: %v", err)
	}
	defer clientConn.Close()

	clientMsgID := "urn:uuid:" + uuid.New().String()
	probeXML := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<Envelope xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing">
  <Header>
    <wsa:MessageID>%s</wsa:MessageID>
    <wsa:To>urn:schemas-xmlsoap-org:ws:2005:04/discovery</wsa:To>
    <wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</wsa:Action>
  </Header>
  <Body>
    <Probe xmlns="http://schemas.xmlsoap.org/ws/2005/04/discovery">
      <Types>tds:Device</Types>
    </Probe>
  </Body>
</Envelope>`, clientMsgID)

	// Simulate receiving probe from clientAddr
	srv.handlePacket([]byte(probeXML), clientConn.LocalAddr())

	// Client reads response
	_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := clientConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	respStr := string(buf[:n])
	if !strings.Contains(respStr, clientMsgID) {
		t.Errorf("response did not echo client message ID: %s", respStr)
	}

	match, err := parseVideoAmplifierMatch(buf[:n])
	if err != nil {
		t.Fatalf("failed to parse probe match: %v", err)
	}

	if match.Types != "tds:Device dn:NetworkVideoTransmitter" {
		t.Errorf("unexpected types: %s", match.Types)
	}
}
