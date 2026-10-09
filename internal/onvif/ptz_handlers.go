package onvif

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/ptz"
)

// FormatGetPTZConfigurationsResponse returns bounds for Pan/Tilt and Zoom.
func FormatGetPTZConfigurationsResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
  <soap:Body>
    <tptz:GetConfigurationsResponse>
      <tptz:PTZConfiguration token="ptz_config">
        <tt:Name>PTZ Configuration 1</tt:Name>
        <tt:UseCount>1</tt:UseCount>
        <tt:NodeToken>ptz_node_0</tt:NodeToken>
        <tt:DefaultAbsolutePantTiltPositionSpace>http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace</tt:DefaultAbsolutePantTiltPositionSpace>
        <tt:DefaultAbsoluteZoomPositionSpace>http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace</tt:DefaultAbsoluteZoomPositionSpace>
        <tt:DefaultRelativePanTiltTranslationSpace>http://www.onvif.org/ver10/tptz/PanTiltSpaces/TranslationGenericSpace</tt:DefaultRelativePanTiltTranslationSpace>
        <tt:DefaultRelativeZoomTranslationSpace>http://www.onvif.org/ver10/tptz/ZoomSpaces/TranslationGenericSpace</tt:DefaultRelativeZoomTranslationSpace>
        <tt:DefaultContinuousPanTiltVelocitySpace>http://www.onvif.org/ver10/tptz/PanTiltSpaces/VelocityGenericSpace</tt:DefaultContinuousPanTiltVelocitySpace>
        <tt:DefaultContinuousZoomVelocitySpace>http://www.onvif.org/ver10/tptz/ZoomSpaces/VelocityGenericSpace</tt:DefaultContinuousZoomVelocitySpace>
        <tt:DefaultPTZSpeed>
          <tt:PanTilt x="1.0" y="1.0"/>
          <tt:Zoom x="1.0"/>
        </tt:DefaultPTZSpeed>
        <tt:DefaultPTZTimeout>PT10S</tt:DefaultPTZTimeout>
        <tt:PanTiltLimits>
          <tt:Range>
            <tt:URI>http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace</tt:URI>
            <tt:XRange><tt:Min>-1.0</tt:Min><tt:Max>1.0</tt:Max></tt:XRange>
            <tt:YRange><tt:Min>-1.0</tt:Min><tt:Max>1.0</tt:Max></tt:YRange>
          </tt:Range>
        </tt:PanTiltLimits>
        <tt:ZoomLimits>
          <tt:Range>
            <tt:URI>http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace</tt:URI>
            <tt:XRange><tt:Min>0.0</tt:Min><tt:Max>1.0</tt:Max></tt:XRange>
          </tt:Range>
        </tt:ZoomLimits>
      </tptz:PTZConfiguration>
    </tptz:GetConfigurationsResponse>
  </soap:Body>
</soap:Envelope>`
}

// FormatGetPTZNodesResponse advertises PTZ node capabilities.
func FormatGetPTZNodesResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
  <soap:Body>
    <tptz:GetNodesResponse>
      <tptz:PTZNode token="ptz_node_0" FixedHomePosition="false">
        <tt:Name>PTZ Node 0</tt:Name>
        <tt:SupportedPTZSpaces>
          <tt:AbsolutePanTiltPositionSpace>
            <tt:URI>http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace</tt:URI>
            <tt:XRange><tt:Min>-1.0</tt:Min><tt:Max>1.0</tt:Max></tt:XRange>
            <tt:YRange><tt:Min>-1.0</tt:Min><tt:Max>1.0</tt:Max></tt:YRange>
          </tt:AbsolutePanTiltPositionSpace>
          <tt:AbsoluteZoomPositionSpace>
            <tt:URI>http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace</tt:URI>
            <tt:XRange><tt:Min>0.0</tt:Min><tt:Max>1.0</tt:Max></tt:XRange>
          </tt:AbsoluteZoomPositionSpace>
          <tt:ContinuousPanTiltVelocitySpace>
            <tt:URI>http://www.onvif.org/ver10/tptz/PanTiltSpaces/VelocityGenericSpace</tt:URI>
            <tt:XRange><tt:Min>-1.0</tt:Min><tt:Max>1.0</tt:Max></tt:XRange>
            <tt:YRange><tt:Min>-1.0</tt:Min><tt:Max>1.0</tt:Max></tt:YRange>
          </tt:ContinuousPanTiltVelocitySpace>
        </tt:SupportedPTZSpaces>
        <tt:MaximumNumberOfPresets>32</tt:MaximumNumberOfPresets>
        <tt:HomeSupported>true</tt:HomeSupported>
      </tptz:PTZNode>
    </tptz:GetNodesResponse>
  </soap:Body>
</soap:Envelope>`
}

// FormatGetPTZStatusResponse returns the live coordinate vector and move status.
func FormatGetPTZStatusResponse(pan, tilt, zoom float64, status string, utc time.Time) string {
	moveStatusStr := "IDLE"
	if status == "MOVING" {
		moveStatusStr = "MOVING"
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
  <soap:Body>
    <tptz:GetStatusResponse>
      <tptz:PTZStatus>
        <tt:Position>
          <tt:PanTilt x="%.4f" y="%.4f" space="http://www.onvif.org/ver10/tptz/PanTiltSpaces/PositionGenericSpace"/>
          <tt:Zoom x="%.4f" space="http://www.onvif.org/ver10/tptz/ZoomSpaces/PositionGenericSpace"/>
        </tt:Position>
        <tt:MoveStatus>
          <tt:PanTilt>%s</tt:PanTilt>
          <tt:Zoom>%s</tt:Zoom>
        </tt:MoveStatus>
        <tt:UtcTime>%s</tt:UtcTime>
      </tptz:PTZStatus>
    </tptz:GetStatusResponse>
  </soap:Body>
</soap:Envelope>`, pan, tilt, zoom, moveStatusStr, moveStatusStr, utc.Format(time.RFC3339))
}

// FormatPTZSuccessResponse returns an empty body for ContinuousMove, AbsoluteMove, Stop, etc.
func FormatPTZSuccessResponse(action string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
  <soap:Body>
    <tptz:%sResponse/>
  </soap:Body>
</soap:Envelope>`, action)
}

// FormatGetPresetsResponse lists all configured presets.
func FormatGetPresetsResponse(presets []ptz.Preset) string {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tt="http://www.onvif.org/ver10/schema"
               xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
  <soap:Body>
    <tptz:GetPresetsResponse>
`)
	for _, pr := range presets {
		buf.WriteString(fmt.Sprintf(`      <tptz:Preset token="%s">
        <tt:Name>%s</tt:Name>
        <tt:PTZPosition>
          <tt:PanTilt x="%.4f" y="%.4f"/>
          <tt:Zoom x="%.4f"/>
        </tt:PTZPosition>
      </tptz:Preset>
`, pr.Token, pr.Name, pr.Pan, pr.Tilt, pr.Zoom))
	}
	buf.WriteString(`    </tptz:GetPresetsResponse>
  </soap:Body>
</soap:Envelope>`)
	return buf.String()
}

// FormatSetPresetResponse returns the created preset token.
func FormatSetPresetResponse(token string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:tptz="http://www.onvif.org/ver20/ptz/wsdl">
  <soap:Body>
    <tptz:SetPresetResponse>
      <tptz:PTZPresetToken>%s</tptz:PTZPresetToken>
    </tptz:SetPresetResponse>
  </soap:Body>
</soap:Envelope>`, token)
}

// ParsePanTiltZoom extracts coordinates from SOAP XML bodies.
func ParsePanTiltZoom(body []byte) (x, y, z float64, found bool) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			local := se.Name.Local
			if local == "PanTilt" || local == "Translation" || local == "Velocity" {
				for _, attr := range se.Attr {
					if attr.Name.Local == "x" {
						if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
							x = v
							found = true
						}
					} else if attr.Name.Local == "y" {
						if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
							y = v
							found = true
						}
					}
				}
			} else if local == "Zoom" {
				for _, attr := range se.Attr {
					if attr.Name.Local == "x" {
						if v, err := strconv.ParseFloat(attr.Value, 64); err == nil {
							z = v
							found = true
						}
					}
				}
			}
		}
	}
	return
}

// ParsePresetToken extracts preset token from GotoPreset/RemovePreset requests.
func ParsePresetToken(body []byte) string {
	str := string(body)
	if idx := strings.Index(str, "PresetToken>"); idx != -1 {
		rest := str[idx+len("PresetToken>"):]
		if end := strings.Index(rest, "<"); end != -1 {
			return strings.TrimSpace(rest[:end])
		}
	}
	return ""
}
