package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/smford/onvif-camera-simulator/internal/config"
)

type ConfigForm struct {
	inputs  []textinput.Model
	focused int
	status  string
}

const (
	fieldManufacturer = iota
	fieldModel
	fieldFirmware
	fieldSerial
	fieldAdvertisedIP
	fieldHTTPPort
	fieldRTSPPort
	fieldMainFPS
	fieldSubFPS
	fieldFleetCount
	fieldCount
)

func newConfigForm(cfg *config.Config) ConfigForm {
	inputs := make([]textinput.Model, fieldCount)

	labels := []string{
		"Manufacturer",
		"Model",
		"Firmware",
		"Serial Number",
		"Advertised IP",
		"HTTP Port",
		"RTSP Port",
		"Main Stream FPS",
		"Sub Stream FPS",
		"Fleet Camera Count",
	}

	defaults := []string{
		cfg.Camera.Manufacturer,
		cfg.Camera.Model,
		cfg.Camera.Firmware,
		cfg.Camera.SerialNumber,
		cfg.Server.AdvertisedIP,
		strconv.Itoa(cfg.Server.HTTPPort),
		strconv.Itoa(cfg.Server.RTSPPort),
		strconv.Itoa(cfg.Streams.Main.FPS),
		strconv.Itoa(cfg.Streams.Sub.FPS),
		strconv.Itoa(cfg.Fleet.Count),
	}

	for i := range inputs {
		t := textinput.New()
		t.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8"))
		t.Prompt = fmt.Sprintf("%-20s: ", labels[i])
		t.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
		t.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
		t.SetValue(defaults[i])
		inputs[i] = t
	}

	inputs[0].Focus()

	return ConfigForm{
		inputs:  inputs,
		focused: 0,
		status:  "Ready. Use Up/Down arrows to navigate. Press [Ctrl+S] to Save, [Ctrl+R] to Reload.",
	}
}

func (f *ConfigForm) applyToConfig(cfg *config.Config) error {
	cfg.Camera.Manufacturer = f.inputs[fieldManufacturer].Value()
	cfg.Camera.Model = f.inputs[fieldModel].Value()
	cfg.Camera.Firmware = f.inputs[fieldFirmware].Value()
	cfg.Camera.SerialNumber = f.inputs[fieldSerial].Value()
	cfg.Server.AdvertisedIP = f.inputs[fieldAdvertisedIP].Value()

	if port, err := strconv.Atoi(f.inputs[fieldHTTPPort].Value()); err == nil {
		cfg.Server.HTTPPort = port
	}
	if port, err := strconv.Atoi(f.inputs[fieldRTSPPort].Value()); err == nil {
		cfg.Server.RTSPPort = port
	}
	if fps, err := strconv.Atoi(f.inputs[fieldMainFPS].Value()); err == nil {
		cfg.Streams.Main.FPS = fps
	}
	if fps, err := strconv.Atoi(f.inputs[fieldSubFPS].Value()); err == nil {
		cfg.Streams.Sub.FPS = fps
	}
	if cnt, err := strconv.Atoi(f.inputs[fieldFleetCount].Value()); err == nil {
		cfg.Fleet.Count = cnt
	}
	return nil
}

func renderConfigTab(m *Model) string {
	width := m.width
	if width < 80 {
		width = 80
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#2563EB")).
		Padding(0, 1).
		Render(" Camera Configuration & Live Parameter Tuning ")

	scheme := "http"
	if m.cfg.Server.TLSEnabled {
		scheme = "https"
	}
	urlSummary := lipgloss.NewStyle().Foreground(lipgloss.Color("#38BDF8")).Render(fmt.Sprintf(
		"Active Endpoints: %s://%s:%d/onvif/device_service  |  rtsp://%s:%d/live/main",
		scheme, m.cfg.Server.AdvertisedIP, m.cfg.Server.HTTPPort, m.cfg.Server.AdvertisedIP, m.cfg.Server.RTSPPort,
	))

	var fieldViews []string
	for i := range m.form.inputs {
		style := lipgloss.NewStyle().Padding(0, 1)
		if i == m.form.focused {
			style = style.
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("#38BDF8")).
				Background(lipgloss.Color("#1E293B"))
		}
		fieldViews = append(fieldViews, style.Render(m.form.inputs[i].View()))
	}

	help := lipgloss.JoinHorizontal(lipgloss.Top,
		hotkeyBadgeStyle.Render("[Up/Down]"), " Navigate fields  ",
		hotkeyBadgeStyle.Render("[Ctrl+S]"), " Save config.yaml  ",
		hotkeyBadgeStyle.Render("[Ctrl+R]"), " Hot-Reload settings  ",
	)

	statusBar := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981")).
		Bold(true).
		Render(m.form.status)

	cardContent := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		urlSummary,
		"",
		strings.Join(fieldViews, "\n"),
		"",
		help,
		"",
		statusBar,
	)

	return cardStyle.Width(width - 4).Render(cardContent)
}
