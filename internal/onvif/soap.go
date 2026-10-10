package onvif

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/config"
)

// SOAPEnvelope represents an incoming SOAP 1.2 or 1.1 envelope.
type SOAPEnvelope struct {
	XMLName xml.Name   `xml:"Envelope"`
	Header  SOAPHeader `xml:"Header"`
	Body    SOAPBody   `xml:"Body"`
}

type SOAPHeader struct {
	Security *WSSecurity `xml:"Security"`
	Action   string      `xml:"Action"`
}

type WSSecurity struct {
	UsernameToken *WSUsernameToken `xml:"UsernameToken"`
}

type WSUsernameToken struct {
	Username string     `xml:"Username"`
	Password WSPassword `xml:"Password"`
	Nonce    WSNonce    `xml:"Nonce"`
	Created  string     `xml:"Created"`
}

type WSPassword struct {
	Type  string `xml:"Type,attr"`
	Value string `xml:",chardata"`
}

type WSOldNonce struct {
	EncodingType string `xml:"EncodingType,attr"`
	Value        string `xml:",chardata"`
}

type WSNonce struct {
	EncodingType string `xml:"EncodingType,attr"`
	Value        string `xml:",chardata"`
}

type SOAPBody struct {
	Content []byte `xml:",innerxml"`
}

// Authenticate verifies the SOAP request against configured credentials.
func Authenticate(authCfg config.AuthConfig, env *SOAPEnvelope) (bool, string) {
	if !authCfg.Enabled {
		return true, "ANON"
	}

	if env.Header.Security == nil || env.Header.Security.UsernameToken == nil {
		return false, "MISSING_SECURITY_HEADER"
	}

	token := env.Header.Security.UsernameToken
	if token.Username != authCfg.Username {
		return false, "INVALID_USERNAME"
	}

	passType := token.Password.Type
	if passType == "" || strings.HasSuffix(passType, "#PasswordText") {
		// Plaintext password comparison
		if token.Password.Value == authCfg.Password {
			return true, "SUCCESS"
		}
		return false, "INVALID_PASSWORD"
	}

	if strings.HasSuffix(passType, "#PasswordDigest") {
		// PasswordDigest = Base64(SHA-1(DecodedNonce + Created + Password))
		nonceRaw, err := base64.StdEncoding.DecodeString(token.Nonce.Value)
		if err != nil {
			return false, "INVALID_NONCE_ENCODING"
		}

		h := sha1.New()
		h.Write(nonceRaw)
		h.Write([]byte(token.Created))
		h.Write([]byte(authCfg.Password))
		expectedDigest := base64.StdEncoding.EncodeToString(h.Sum(nil))

		if token.Password.Value == expectedDigest {
			return true, "SUCCESS"
		}
		return false, "INVALID_DIGEST"
	}

	return false, "UNSUPPORTED_AUTH_TYPE"
}

// FormatSOAPFault creates a standard SOAP Fault response.
func FormatSOAPFault(code, reason string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">
  <soap:Body>
    <soap:Fault>
      <soap:Code>
        <soap:Value>%s</soap:Value>
      </soap:Code>
      <soap:Reason>
        <soap:Text xml:lang="en">%s</soap:Text>
      </soap:Reason>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`, code, reason)
}

// FormatGetSystemDateAndTimeResponse builds the clock synchronization response.
func FormatGetSystemDateAndTimeResponse() string {
	now := time.Now().UTC()
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <soap:Body>
    <tds:GetSystemDateAndTimeResponse>
      <tds:SystemDateAndTime>
        <tt:DateTimeType>Manual</tt:DateTimeType>
        <tt:DaylightSavings>false</tt:DaylightSavings>
        <tt:TimeZone>
          <tt:TZ>UTC</tt:TZ>
        </tt:TimeZone>
        <tt:UTCDateTime>
          <tt:Time>
            <tt:Hour>%02d</tt:Hour>
            <tt:Minute>%02d</tt:Minute>
            <tt:Second>%02d</tt:Second>
          </tt:Time>
          <tt:Date>
            <tt:Year>%04d</tt:Year>
            <tt:Month>%02d</tt:Month>
            <tt:Day>%02d</tt:Day>
          </tt:Date>
        </tt:UTCDateTime>
      </tds:SystemDateAndTime>
    </tds:GetSystemDateAndTimeResponse>
  </soap:Body>
</soap:Envelope>`, now.Hour(), now.Minute(), now.Second(), now.Year(), int(now.Month()), now.Day())
}

// FormatGetDeviceInformationResponse builds the manufacturer and model payload.
func FormatGetDeviceInformationResponse(camCfg config.CameraConfig) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <soap:Body>
    <tds:GetDeviceInformationResponse>
      <tds:Manufacturer>%s</tds:Manufacturer>
      <tds:Model>%s</tds:Model>
      <tds:FirmwareVersion>%s</tds:FirmwareVersion>
      <tds:SerialNumber>%s</tds:SerialNumber>
      <tds:HardwareId>%s</tds:HardwareId>
    </tds:GetDeviceInformationResponse>
  </soap:Body>
</soap:Envelope>`, camCfg.Manufacturer, camCfg.Model, camCfg.Firmware, camCfg.SerialNumber, camCfg.HardwareID)
}

// FormatGetCapabilitiesResponse advertises Device, Media, PTZ, and Events endpoints.
func FormatGetCapabilitiesResponse(baseURL string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <soap:Body>
    <tds:GetCapabilitiesResponse>
      <tds:Capabilities>
        <tt:Device>
          <tt:XAddr>%s/device_service</tt:XAddr>
          <tt:System>
            <tt:DiscoveryResolve>true</tt:DiscoveryResolve>
            <tt:DiscoveryBye>true</tt:DiscoveryBye>
            <tt:RemoteDiscovery>false</tt:RemoteDiscovery>
            <tt:SystemBackup>false</tt:SystemBackup>
            <tt:SystemLogging>true</tt:SystemLogging>
            <tt:FirmwareUpgrade>false</tt:FirmwareUpgrade>
            <tt:SupportedVersions>
              <tt:Major>2</tt:Major>
              <tt:Minor>0</tt:Minor>
            </tt:SupportedVersions>
          </tt:System>
        </tt:Device>
        <tt:Events>
          <tt:XAddr>%s/event_service</tt:XAddr>
          <tt:WSSubscriptionPolicySupport>true</tt:WSSubscriptionPolicySupport>
          <tt:WSPullPointSupport>true</tt:WSPullPointSupport>
          <tt:WSPausableSubscriptionManagerInterfaceSupport>false</tt:WSPausableSubscriptionManagerInterfaceSupport>
        </tt:Events>
        <tt:Media>
          <tt:XAddr>%s/media_service</tt:XAddr>
          <tt:StreamingCapabilities>
            <tt:RTPMulticast>false</tt:RTPMulticast>
            <tt:RTP_TCP>true</tt:RTP_TCP>
            <tt:RTP_RTSP_TCP>true</tt:RTP_RTSP_TCP>
          </tt:StreamingCapabilities>
        </tt:Media>
        <tt:PTZ>
          <tt:XAddr>%s/ptz_service</tt:XAddr>
        </tt:PTZ>
      </tds:Capabilities>
    </tds:GetCapabilitiesResponse>
  </soap:Body>
</soap:Envelope>`, baseURL, baseURL, baseURL, baseURL)
}

// FormatGetServicesResponse returns enumerations of active service endpoints.
func FormatGetServicesResponse(baseURL string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
  <soap:Body>
    <tds:GetServicesResponse>
      <tds:Service>
        <tds:Namespace>http://www.onvif.org/ver10/device/wsdl</tds:Namespace>
        <tds:XAddr>%s/device_service</tds:XAddr>
        <tds:Version><tds:Major>2</tds:Major><tds:Minor>4</tds:Minor></tds:Version>
      </tds:Service>
      <tds:Service>
        <tds:Namespace>http://www.onvif.org/ver10/media/wsdl</tds:Namespace>
        <tds:XAddr>%s/media_service</tds:XAddr>
        <tds:Version><tds:Major>2</tds:Major><tds:Minor>6</tds:Minor></tds:Version>
      </tds:Service>
      <tds:Service>
        <tds:Namespace>http://www.onvif.org/ver20/ptz/wsdl</tds:Namespace>
        <tds:XAddr>%s/ptz_service</tds:XAddr>
        <tds:Version><tds:Major>2</tds:Major><tds:Minor>4</tds:Minor></tds:Version>
      </tds:Service>
      <tds:Service>
        <tds:Namespace>http://www.onvif.org/ver10/events/wsdl</tds:Namespace>
        <tds:XAddr>%s/event_service</tds:XAddr>
        <tds:Version><tds:Major>2</tds:Major><tds:Minor>4</tds:Minor></tds:Version>
      </tds:Service>
    </tds:GetServicesResponse>
  </soap:Body>
</soap:Envelope>`, baseURL, baseURL, baseURL, baseURL)
}

// FormatGetProfilesResponse advertises Profile_Main (1080p) and Profile_Sub (360p).
func FormatGetProfilesResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <soap:Body>
    <trt:GetProfilesResponse>
      <!-- Main High-Res Profile -->
      <trt:Profiles token="profile_main" fixed="true">
        <tt:Name>Profile_Main</tt:Name>
        <tt:VideoSourceConfiguration token="vsc_main">
          <tt:Name>VideoSourceConfig_Main</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:SourceToken>vs_0</tt:SourceToken>
          <tt:Bounds x="0" y="0" width="1920" height="1080"/>
        </tt:VideoSourceConfiguration>
        <tt:VideoEncoderConfiguration token="vec_main">
          <tt:Name>VideoEncoderConfig_Main</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution>
            <tt:Width>1920</tt:Width>
            <tt:Height>1080</tt:Height>
          </tt:Resolution>
          <tt:Quality>5.0</tt:Quality>
          <tt:RateControl>
            <tt:FrameRateLimit>20</tt:FrameRateLimit>
            <tt:EncodingInterval>1</tt:EncodingInterval>
            <tt:BitrateLimit>4096</tt:BitrateLimit>
          </tt:RateControl>
          <tt:H264>
            <tt:GovLength>20</tt:GovLength>
            <tt:H264Profile>Baseline</tt:H264Profile>
          </tt:H264>
        </tt:VideoEncoderConfiguration>
        <tt:AudioEncoderConfiguration token="aec_main">
          <tt:Name>AudioEncoderConfig_Main</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:Encoding>G711</tt:Encoding>
          <tt:Bitrate>64</tt:Bitrate>
          <tt:SampleRate>8000</tt:SampleRate>
        </tt:AudioEncoderConfiguration>
        <tt:PTZConfiguration token="ptz_config">
          <tt:Name>PTZConfig</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:NodeToken>ptz_node_0</tt:NodeToken>
        </tt:PTZConfiguration>
      </trt:Profiles>

      <!-- Sub Low-Res Analytics Profile -->
      <trt:Profiles token="profile_sub" fixed="true">
        <tt:Name>Profile_Sub</tt:Name>
        <tt:VideoSourceConfiguration token="vsc_sub">
          <tt:Name>VideoSourceConfig_Sub</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:SourceToken>vs_0</tt:SourceToken>
          <tt:Bounds x="0" y="0" width="640" height="360"/>
        </tt:VideoSourceConfiguration>
        <tt:VideoEncoderConfiguration token="vec_sub">
          <tt:Name>VideoEncoderConfig_Sub</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:Encoding>H264</tt:Encoding>
          <tt:Resolution>
            <tt:Width>640</tt:Width>
            <tt:Height>360</tt:Height>
          </tt:Resolution>
          <tt:Quality>4.0</tt:Quality>
          <tt:RateControl>
            <tt:FrameRateLimit>10</tt:FrameRateLimit>
            <tt:EncodingInterval>1</tt:EncodingInterval>
            <tt:BitrateLimit>512</tt:BitrateLimit>
          </tt:RateControl>
          <tt:H264>
            <tt:GovLength>10</tt:GovLength>
            <tt:H264Profile>Baseline</tt:H264Profile>
          </tt:H264>
        </tt:VideoEncoderConfiguration>
        <tt:AudioEncoderConfiguration token="aec_sub">
          <tt:Name>AudioEncoderConfig_Sub</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:Encoding>G711</tt:Encoding>
          <tt:Bitrate>64</tt:Bitrate>
          <tt:SampleRate>8000</tt:SampleRate>
        </tt:AudioEncoderConfiguration>
        <tt:PTZConfiguration token="ptz_config">
          <tt:Name>PTZConfig</tt:Name>
          <tt:UseCount>1</tt:UseCount>
          <tt:NodeToken>ptz_node_0</tt:NodeToken>
        </tt:PTZConfiguration>
      </trt:Profiles>
    </trt:GetProfilesResponse>
  </soap:Body>
</soap:Envelope>`
}

// FormatGetStreamUriResponse returns matching RTSP stream URL.
func FormatGetStreamUriResponse(rtspURI string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <soap:Body>
    <trt:GetStreamUriResponse>
      <trt:MediaUri>
        <tt:Uri>%s</tt:Uri>
        <tt:InvalidAfterConnect>false</tt:InvalidAfterConnect>
        <tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>
        <tt:Timeout>PT30S</tt:Timeout>
      </trt:MediaUri>
    </trt:GetStreamUriResponse>
  </soap:Body>
</soap:Envelope>`, rtspURI)
}

// FormatGetSnapshotUriResponse returns the HTTP snapshot URL.
func FormatGetSnapshotUriResponse(snapshotURI string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <soap:Body>
    <trt:GetSnapshotUriResponse>
      <trt:MediaUri>
        <tt:Uri>%s</tt:Uri>
        <tt:InvalidAfterConnect>false</tt:InvalidAfterConnect>
        <tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>
        <tt:Timeout>PT10S</tt:Timeout>
      </trt:MediaUri>
    </trt:GetSnapshotUriResponse>
  </soap:Body>
</soap:Envelope>`, snapshotURI)
}

// FormatGetVideoEncoderConfigurationOptionsResponse returns valid resolution and FPS bounds.
func FormatGetVideoEncoderConfigurationOptionsResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <soap:Body>
    <trt:GetVideoEncoderConfigurationOptionsResponse>
      <trt:Options>
        <tt:QualityRange>
          <tt:Min>1</tt:Min>
          <tt:Max>6</tt:Max>
        </tt:QualityRange>
        <tt:H264>
          <tt:ResolutionsAvailable>
            <tt:Width>1920</tt:Width>
            <tt:Height>1080</tt:Height>
          </tt:ResolutionsAvailable>
          <tt:ResolutionsAvailable>
            <tt:Width>640</tt:Width>
            <tt:Height>360</tt:Height>
          </tt:ResolutionsAvailable>
          <tt:GovLengthRange>
            <tt:Min>5</tt:Min>
            <tt:Max>60</tt:Max>
          </tt:GovLengthRange>
          <tt:FrameRateRange>
            <tt:Min>5</tt:Min>
            <tt:Max>30</tt:Max>
          </tt:FrameRateRange>
          <tt:H264ProfilesSupported>Baseline</tt:H264ProfilesSupported>
          <tt:H264ProfilesSupported>Main</tt:H264ProfilesSupported>
        </tt:H264>
      </trt:Options>
    </trt:GetVideoEncoderConfigurationOptionsResponse>
  </soap:Body>
</soap:Envelope>`
}
