package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

type LogsView struct {
	viewport   viewport.Model
	followLogs bool
}

func newLogsView(width, height int) LogsView {
	vp := viewport.New(width, height)
	return LogsView{
		viewport:   vp,
		followLogs: true,
	}
}

func (v *LogsView) updateContent(logEntries []telemetry.LogEntry) {
	var lines []string
	for _, e := range logEntries {
		ts := e.Timestamp.Format("15:04:05.000")
		var lvlStyle lipgloss.Style
		switch e.Level.String() {
		case "DEBUG":
			lvlStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))
		case "INFO":
			lvlStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981")).Bold(true)
		case "WARN":
			lvlStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true)
		case "ERROR":
			lvlStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444")).Bold(true)
		default:
			lvlStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1"))
		}

		lvlBadge := lvlStyle.Render(fmt.Sprintf("[%-5s]", e.Level.String()))
		attrStr := ""
		for k, val := range e.Attrs {
			attrStr += fmt.Sprintf(" %s=%v", lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Render(k), val)
		}

		lines = append(lines, fmt.Sprintf("%s %s %s%s", ts, lvlBadge, e.Message, attrStr))
	}

	content := strings.Join(lines, "\n")
	v.viewport.SetContent(content)
	if v.followLogs {
		v.viewport.GotoBottom()
	}
}

func renderLogsTab(m *Model) string {
	width := m.width
	if width < 80 {
		width = 80
	}

	followStatus := idleBadgeStyle.Render("FOLLOW: OFF")
	if m.logsView.followLogs {
		followStatus = activeBadgeStyle.Render("FOLLOW: AUTO-SCROLL (ON)")
	}

	header := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Render("System Event Log (Ring Buffer - Last 250 Records)"),
		"  ",
		followStatus,
		"  ",
		hotkeyBadgeStyle.Render("[f]"), " Toggle Follow   ",
		hotkeyBadgeStyle.Render("[c]"), " Clear Logs",
	)

	cardContent := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		m.logsView.viewport.View(),
	)

	return cardStyle.Width(width - 4).Render(cardContent)
}
