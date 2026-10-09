package tui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5A56E0")).
			Padding(0, 1)

	headerSubStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A0A5B5")).
			Background(lipgloss.Color("#1E212B")).
			Padding(0, 1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3D4255")).
			Padding(0, 1)

	gaugeLabelStyle = lipgloss.NewStyle().
			Width(6).
			Foreground(lipgloss.Color("#E2E8F0"))

	activeBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#10B981")).
				Padding(0, 1)

	idleBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8")).
			Background(lipgloss.Color("#334155")).
			Padding(0, 1)

	dangerBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#EF4444")).
				Padding(0, 1)

	hotkeyBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))
)

func renderDashboardTab(m *Model) string {
	width := m.width
	if width < 80 {
		width = 80
	}

	// 1. Top Header Bar
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	activeClients, totalBitrate, fleetSize := m.hub.GetOverviewStats()

	uptime := m.hub.Uptime().Round(time.Second).String()
	goroutines := runtime.NumGoroutine()
	allocMB := float64(mem.Alloc) / (1024 * 1024)

	headerLeft := headerStyle.Render(" ONVIF & RTSP SRE SIMULATOR ")
	headerInfo := fmt.Sprintf(" Uptime: %s | CPU/Go: %d gr | Mem: %.1f MB | Fleet: %d cams | RTSP Clients: %d | Bitrate: %.1f kbps",
		uptime, goroutines, allocMB, fleetSize, activeClients, totalBitrate)
	headerRight := headerSubStyle.Render(headerInfo)
	topBar := lipgloss.JoinHorizontal(lipgloss.Top, headerLeft, headerRight)

	// 2. Camera URL Endpoints Card
	selectedCam := fmt.Sprintf("cam%d", m.selectedCam)
	endpointsPanel := renderEndpointsCard(m, selectedCam, width-4)

	// 3. Middle Row: PTZ Gauges (Left) and Events & Chaos (Right)
	ptzTelemetry := m.hub.GetPTZ(selectedCam)
	eventsTelemetry := m.hub.GetEvents(selectedCam)

	ptzPanel := renderPTZCard(selectedCam, ptzTelemetry, (width/2)-2)
	eventsPanel := renderEventsCard(m, eventsTelemetry, (width/2)-2)
	middleRow := lipgloss.JoinHorizontal(lipgloss.Top, ptzPanel, eventsPanel)

	// 4. Active Clients Table
	clientsTable := renderClientsTable(m, width-4)

	// 5. Audit Log
	auditPanel := renderAuditLog(m, width-4)

	return lipgloss.JoinVertical(lipgloss.Left,
		topBar,
		"",
		endpointsPanel,
		"",
		middleRow,
		"",
		cardStyle.Width(width-2).Render(clientsTable),
		"",
		cardStyle.Width(width-2).Render(auditPanel),
	)
}

func renderPTZCard(camID string, ptz telemetry.CameraPTZTelemetry, cardWidth int) string {
	title := fmt.Sprintf("Virtual PTZ Kinematics [%s] (Switch with '[' / ']')", camID)
	statusBadge := idleBadgeStyle.Render("STATUS: " + ptz.MoveStatus)
	if ptz.MoveStatus == "MOVING" {
		statusBadge = activeBadgeStyle.Render("STATUS: MOVING")
	}

	panGauge := renderGauge("PAN", ptz.Pan, -1.0, 1.0, cardWidth-24, "#38BDF8")
	tiltGauge := renderGauge("TILT", ptz.Tilt, -1.0, 1.0, cardWidth-24, "#818CF8")
	zoomGauge := renderGauge("ZOOM", ptz.Zoom, 0.0, 1.0, cardWidth-24, "#F472B6")

	lastCmd := fmt.Sprintf("Last Action: %-12s | Position: [%.2f, %.2f, %.2f]", ptz.LastCommand, ptz.Pan, ptz.Tilt, ptz.Zoom)

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Center, lipgloss.NewStyle().Bold(true).Render(title), "  ", statusBadge),
		"",
		panGauge,
		tiltGauge,
		zoomGauge,
		"",
		lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")).Render(lastCmd),
	)

	return cardStyle.Width(cardWidth).Render(content)
}

func renderGauge(label string, val, min, max float64, barWidth int, hexColor string) string {
	if barWidth < 10 {
		barWidth = 10
	}
	ratio := (val - min) / (max - min)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	fill := int(ratio * float64(barWidth))
	empty := barWidth - fill

	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(hexColor)).Render(strings.Repeat("█", fill)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#334155")).Render(strings.Repeat("░", empty))

	return fmt.Sprintf("%s %s %6.2f", gaugeLabelStyle.Render(label), bar, val)
}

func renderEventsCard(m *Model, ev telemetry.CameraEventTelemetry, cardWidth int) string {
	title := lipgloss.NewStyle().Bold(true).Render("WS-BaseNotification Events & SRE Chaos")

	// Status Badges
	motionBadge := idleBadgeStyle.Render("MOTION: IDLE")
	if ev.MotionActive {
		motionBadge = activeBadgeStyle.Render("MOTION: ACTIVE")
	}

	humanBadge := idleBadgeStyle.Render("HUMAN: IDLE")
	if ev.HumanActive {
		humanBadge = activeBadgeStyle.Render("HUMAN: DETECTED")
	}

	vehicleBadge := idleBadgeStyle.Render("VEHICLE: IDLE")
	if ev.VehicleActive {
		vehicleBadge = activeBadgeStyle.Render("VEHICLE: DETECTED")
	}

	freezeBadge := idleBadgeStyle.Render("STREAM: RUNNING")
	if m.hub.IsChaosFrozen() {
		freezeBadge = dangerBadgeStyle.Render("STREAM: FROZEN")
	}

	rebootBadge := idleBadgeStyle.Render("SYSTEM: ONLINE")
	if m.hub.IsChaosReboot() {
		rebootBadge = dangerBadgeStyle.Render("SYSTEM: REBOOTING")
	}

	badgesRow1 := lipgloss.JoinHorizontal(lipgloss.Top, motionBadge, "  ", humanBadge, "  ", vehicleBadge)
	badgesRow2 := lipgloss.JoinHorizontal(lipgloss.Top, freezeBadge, "  ", rebootBadge)

	hotkeys := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top,
			hotkeyBadgeStyle.Render("[m]"), " Toggle Motion   ",
			hotkeyBadgeStyle.Render("[h]"), " Burst Human (5s)   ",
			hotkeyBadgeStyle.Render("[v]"), " Burst Vehicle (5s)",
		),
		lipgloss.JoinHorizontal(lipgloss.Top,
			hotkeyBadgeStyle.Render("[x]"), " Freeze Stream   ",
			hotkeyBadgeStyle.Render("[b]"), " Hard Reboot (5s)   ",
			hotkeyBadgeStyle.Render("[1..3]"), " Switch Tabs",
		),
	)

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		badgesRow1,
		"",
		badgesRow2,
		"",
		hotkeys,
	)

	return cardStyle.Width(cardWidth).Render(content)
}

func renderClientsTable(m *Model, width int) string {
	title := lipgloss.NewStyle().Bold(true).Render("Active RTSP Sessions & Connections")

	columns := []table.Column{
		{Title: "Client IP", Width: 18},
		{Title: "Cam", Width: 6},
		{Title: "Stream", Width: 8},
		{Title: "Transport", Width: 10},
		{Title: "User-Agent", Width: 20},
		{Title: "State", Width: 8},
		{Title: "Duration", Width: 10},
		{Title: "Bitrate (kbps)", Width: 14},
	}

	sessions := m.hub.GetSessions()
	rows := make([]table.Row, 0, len(sessions))
	now := time.Now()

	for _, s := range sessions {
		dur := now.Sub(s.ConnectTime).Round(time.Second).String()
		bitrateStr := fmt.Sprintf("%.1f", s.BitrateKbps)
		rows = append(rows, table.Row{
			fmt.Sprintf("%s:%d", s.RemoteIP, s.Port),
			s.Camera,
			s.Stream,
			s.Transport,
			s.UserAgent,
			s.State,
			dur,
			bitrateStr,
		})
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(5),
	)

	tStyle := table.DefaultStyles()
	tStyle.Header = tStyle.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#4B5563")).
		BorderBottom(true).
		Bold(true)
	t.SetStyles(tStyle)

	return lipgloss.JoinVertical(lipgloss.Left, title, t.View())
}

func renderAuditLog(m *Model, width int) string {
	title := lipgloss.NewStyle().Bold(true).Render("Recent ONVIF SOAP Security & Audit Trail (Last 8 Requests)")
	entries := m.hub.GetAuditLog()

	var lines []string
	if len(entries) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B")).Render("  (No SOAP requests recorded yet)"))
	} else {
		for _, e := range entries {
			ts := e.Timestamp.Format("15:04:05.000")
			outcomeColor := "#10B981"
			if e.AuthOutcome == "FAILED" {
				outcomeColor = "#EF4444"
			} else if e.AuthOutcome == "ANON" {
				outcomeColor = "#F59E0B"
			}

			badge := lipgloss.NewStyle().Foreground(lipgloss.Color(outcomeColor)).Render(fmt.Sprintf("[%-7s]", e.AuthOutcome))
			lines = append(lines, fmt.Sprintf("  %s %s | %-6s | %-16s | %s",
				ts, badge, e.Camera, e.ClientIP, e.Action))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(lines, "\n"))
}

func renderEndpointsCard(m *Model, camID string, cardWidth int) string {
	scheme := "http"
	if m.cfg.Server.TLSEnabled {
		scheme = "https"
	}
	advIP := m.cfg.Server.AdvertisedIP
	httpPort := m.cfg.Server.HTTPPort
	rtspPort := m.cfg.Server.RTSPPort

	// Format endpoints for the active camera
	onvifURL := fmt.Sprintf("%s://%s:%d/onvif/%s/device_service", scheme, advIP, httpPort, camID)
	snapshotURL := fmt.Sprintf("%s://%s:%d/onvif/%s/snapshot", scheme, advIP, httpPort, camID)
	rtspMain := fmt.Sprintf("rtsp://%s:%d/live/%s/main", advIP, rtspPort, camID)
	rtspSub := fmt.Sprintf("rtsp://%s:%d/live/%s/sub", advIP, rtspPort, camID)

	var aliasNote string
	if m.cfg.Fleet.Count <= 1 {
		onvifURL = fmt.Sprintf("%s://%s:%d/onvif/device_service", scheme, advIP, httpPort)
		snapshotURL = fmt.Sprintf("%s://%s:%d/onvif/snapshot", scheme, advIP, httpPort)
		rtspMain = fmt.Sprintf("rtsp://%s:%d/live/main", advIP, rtspPort)
		rtspSub = fmt.Sprintf("rtsp://%s:%d/live/sub", advIP, rtspPort)
	} else if camID == "cam0" {
		aliasNote = " (root alias: /live/main)"
	}

	title := fmt.Sprintf("Camera Endpoints & URLs [%s]  (Switch camera with '[' / ']')", camID)

	lblStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38BDF8")).Width(13)
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true)
	tagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))

	row1 := lipgloss.JoinHorizontal(lipgloss.Left,
		lblStyle.Render("ONVIF XAddr:"), valStyle.Render(onvifURL),
		"  ", tagStyle.Render("(SOAP Profile S/T/M Core & Pull-Point)"),
	)
	row2 := lipgloss.JoinHorizontal(lipgloss.Left,
		lblStyle.Render("Snapshot:"), valStyle.Render(snapshotURL),
		"  ", tagStyle.Render("(Live JPEG with PTZ crosshairs & event boxes)"),
	)
	row3 := lipgloss.JoinHorizontal(lipgloss.Left,
		lblStyle.Render("RTSP Main:"), valStyle.Render(rtspMain),
		"  ", tagStyle.Render(fmt.Sprintf("(%dx%d @ %dfps, H.264+PCMU%s)", m.cfg.Streams.Main.Width, m.cfg.Streams.Main.Height, m.cfg.Streams.Main.FPS, aliasNote)),
	)
	row4 := lipgloss.JoinHorizontal(lipgloss.Left,
		lblStyle.Render("RTSP Sub:"), valStyle.Render(rtspSub),
		"  ", tagStyle.Render(fmt.Sprintf("(%dx%d @ %dfps, H.264+PCMU)", m.cfg.Streams.Sub.Width, m.cfg.Streams.Sub.Height, m.cfg.Streams.Sub.FPS)),
	)

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Render(title),
		"",
		row1,
		row2,
		row3,
		row4,
	)

	return cardStyle.Width(cardWidth).Render(content)
}
