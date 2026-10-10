package onvif

import (
	"crypto/sha1"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/config"
)

func TestWSAuthenticatePasswordText(t *testing.T) {
	authCfg := config.AuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "password123",
	}

	env := &SOAPEnvelope{
		Header: SOAPHeader{
			Security: &WSSecurity{
				UsernameToken: &WSUsernameToken{
					Username: "admin",
					Password: WSPassword{
						Type:  "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordText",
						Value: "password123",
					},
				},
			},
		},
	}

	ok, outcome := Authenticate(authCfg, env)
	if !ok || outcome != "SUCCESS" {
		t.Fatalf("expected SUCCESS, got ok=%v, outcome=%s", ok, outcome)
	}

	// Bad password
	env.Header.Security.UsernameToken.Password.Value = "wrong"
	ok, outcome = Authenticate(authCfg, env)
	if ok || outcome != "INVALID_PASSWORD" {
		t.Fatalf("expected INVALID_PASSWORD, got ok=%v, outcome=%s", ok, outcome)
	}
}

func TestWSAuthenticatePasswordDigest(t *testing.T) {
	authCfg := config.AuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "password123",
	}

	rawNonce := []byte("randomnonce12345")
	created := time.Now().UTC().Format(time.RFC3339)

	h := sha1.New()
	h.Write(rawNonce)
	h.Write([]byte(created))
	h.Write([]byte("password123"))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))

	env := &SOAPEnvelope{
		Header: SOAPHeader{
			Security: &WSSecurity{
				UsernameToken: &WSUsernameToken{
					Username: "admin",
					Password: WSPassword{
						Type:  "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest",
						Value: digest,
					},
					Nonce: WSNonce{
						Value: base64.StdEncoding.EncodeToString(rawNonce),
					},
					Created: created,
				},
			},
		},
	}

	ok, outcome := Authenticate(authCfg, env)
	if !ok || outcome != "SUCCESS" {
		t.Fatalf("expected SUCCESS, got ok=%v, outcome=%s", ok, outcome)
	}

	// Corrupted digest
	env.Header.Security.UsernameToken.Password.Value = "corruptedDigest="
	ok, outcome = Authenticate(authCfg, env)
	if ok || outcome != "INVALID_DIGEST" {
		t.Fatalf("expected INVALID_DIGEST, got ok=%v, outcome=%s", ok, outcome)
	}
}

func TestGetSystemDateAndTimeResponse(t *testing.T) {
	resp := FormatGetSystemDateAndTimeResponse()
	if !strings.Contains(resp, "<tt:DateTimeType>Manual</tt:DateTimeType>") {
		t.Errorf("missing DateTimeType element in response")
	}
	if !strings.Contains(resp, "<tt:UTCDateTime>") {
		t.Errorf("missing UTCDateTime element in response")
	}
}

func TestGetScopesResponse(t *testing.T) {
	cfg := config.CameraConfig{
		Model:      "TestCam-4K",
		HardwareID: "HW-REV-2",
	}
	resp := FormatGetScopesResponse(cfg)
	if !strings.Contains(resp, "onvif://www.onvif.org/type/video_encoder") {
		t.Errorf("missing video_encoder scope")
	}
	if !strings.Contains(resp, "onvif://www.onvif.org/Profile/S") {
		t.Errorf("missing Profile/S scope")
	}
	if !strings.Contains(resp, "onvif://www.onvif.org/name/TestCam-4K") {
		t.Errorf("missing model name scope")
	}
}

func TestSystemRebootResponse(t *testing.T) {
	resp := FormatSystemRebootResponse()
	if !strings.Contains(resp, "<tds:SystemRebootResponse>") {
		t.Errorf("missing SystemRebootResponse element")
	}
}

func TestFormatSOAPFault(t *testing.T) {
	fault := FormatSOAPFault("wsse:FailedAuthentication", "Auth error")
	if !strings.Contains(fault, "<soap:Value>soap:Sender</soap:Value>") {
		t.Errorf("expected soap:Sender in soap:Value, got %s", fault)
	}
	if !strings.Contains(fault, "<soap:Value>wsse:FailedAuthentication</soap:Value>") {
		t.Errorf("expected wsse:FailedAuthentication in Subcode, got %s", fault)
	}
}
