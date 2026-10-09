package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/smford/onvif-camera-simulator/internal/chaos"
	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

type tickMsg time.Time

// Model is the Bubble Tea application state model.
type Model struct {
	cfg         *config.Config
	configPath  string
	hub         *telemetry.Hub
	logHandler  *telemetry.RingBufferLogHandler
	eventMgr    *events.Manager
	chaosEng    *chaos.Engine
	activeTab   int // 0: Dashboard, 1: Config, 2: Logs
	selectedCam int // cam index 0..N-1
	width       int
	height      int
	form        ConfigForm
	logsView    LogsView
	quitting    bool
	onReload    func(*config.Config)
}

// NewModel creates the root TUI model.
func NewModel(
	cfg *config.Config,
	configPath string,
	hub *telemetry.Hub,
	logHandler *telemetry.RingBufferLogHandler,
	eventMgr *events.Manager,
	chaosEng *chaos.Engine,
	onReload func(*config.Config),
) *Model {
	form := newConfigForm(cfg)
	logsView := newLogsView(100, 20)

	return &Model{
		cfg:         cfg,
		configPath:  configPath,
		hub:         hub,
		logHandler:  logHandler,
		eventMgr:    eventMgr,
		chaosEng:    chaosEng,
		activeTab:   0,
		selectedCam: 0,
		width:       100,
		height:      30,
		form:        form,
		logsView:    logsView,
		onReload:    onReload,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logsView.viewport.Width = msg.Width - 6
		m.logsView.viewport.Height = msg.Height - 8

	case tickMsg:
		// Refresh log buffer
		if m.activeTab == 2 {
			m.logsView.updateContent(m.logHandler.GetEntries())
		}
		cmds = append(cmds, tickCmd())

	case tea.KeyMsg:
		// Global quit
		if msg.String() == "ctrl+c" || (msg.String() == "q" && m.activeTab != 1) {
			m.quitting = true
			return m, tea.Quit
		}

		// Tab switching
		switch msg.String() {
		case "tab":
			m.activeTab = (m.activeTab + 1) % 3
			return m, nil
		case "shift+tab":
			m.activeTab = (m.activeTab + 2) % 3
			return m, nil
		case "1":
			if m.activeTab != 1 {
				m.activeTab = 0
				return m, nil
			}
		case "2":
			if m.activeTab != 1 {
				m.activeTab = 1
				return m, nil
			}
		case "3":
			if m.activeTab != 1 {
				m.activeTab = 2
				return m, nil
			}
		}

		// Tab-specific key processing
		switch m.activeTab {
		case 0:
			m.handleDashboardKeys(msg)
		case 1:
			m.handleConfigKeys(msg)
		case 2:
			m.handleLogsKeys(msg)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleDashboardKeys(msg tea.KeyMsg) {
	camStr := fmt.Sprintf("cam%d", m.selectedCam)

	switch msg.String() {
	case "[":
		if m.selectedCam > 0 {
			m.selectedCam--
		}
	case "]":
		activeClients, _, fleetSize := m.hub.GetOverviewStats()
		_ = activeClients
		if m.selectedCam < fleetSize-1 {
			m.selectedCam++
		}
	case "m":
		// Toggle motion
		ev := m.hub.GetEvents(camStr)
		m.eventMgr.TriggerMotion(camStr, !ev.MotionActive, 10*time.Second)
	case "h":
		// Burst Human
		m.eventMgr.TriggerHuman(camStr, true, 5*time.Second, [4]float64{0.35, 0.25, 0.20, 0.50})
	case "v":
		// Burst Vehicle
		m.eventMgr.TriggerVehicle(camStr, true, 5*time.Second, [4]float64{0.10, 0.40, 0.45, 0.35})
	case "x":
		// Toggle Freeze
		m.chaosEng.ToggleFreeze()
	case "b":
		// Hard Reboot (5s)
		m.chaosEng.TriggerReboot(5 * time.Second)
	}
}

func (m *Model) handleConfigKeys(msg tea.KeyMsg) {
	switch msg.String() {
	case "up":
		if m.form.focused > 0 {
			m.form.inputs[m.form.focused].Blur()
			m.form.focused--
			m.form.inputs[m.form.focused].Focus()
		}
	case "down", "enter":
		if m.form.focused < len(m.form.inputs)-1 {
			m.form.inputs[m.form.focused].Blur()
			m.form.focused++
			m.form.inputs[m.form.focused].Focus()
		}
	case "ctrl+s":
		_ = m.form.applyToConfig(m.cfg)
		if err := config.SaveConfig(m.configPath, m.cfg); err != nil {
			m.form.status = fmt.Sprintf("Save failed: %v", err)
		} else {
			m.form.status = fmt.Sprintf("Successfully saved to %s at %s", m.configPath, time.Now().Format("15:04:05"))
		}
	case "ctrl+r":
		_ = m.form.applyToConfig(m.cfg)
		if m.onReload != nil {
			m.onReload(m.cfg)
		}
		m.form.status = fmt.Sprintf("Configuration reloaded live at %s", time.Now().Format("15:04:05"))
	default:
		// Route key message to active text input
		var cmd tea.Cmd
		m.form.inputs[m.form.focused], cmd = m.form.inputs[m.form.focused].Update(msg)
		_ = cmd
	}
}

func (m *Model) handleLogsKeys(msg tea.KeyMsg) {
	switch msg.String() {
	case "f":
		m.logsView.followLogs = !m.logsView.followLogs
	case "c":
		m.logHandler.Clear()
		m.logsView.updateContent(nil)
	default:
		var cmd tea.Cmd
		m.logsView.viewport, cmd = m.logsView.viewport.Update(msg)
		_ = cmd
	}
}

func (m *Model) View() string {
	if m.quitting {
		return "Shutting down ONVIF Camera Simulator...\n"
	}

	tabBar := renderTabs(m.activeTab)

	var currentTabContent string
	switch m.activeTab {
	case 0:
		currentTabContent = renderDashboardTab(m)
	case 1:
		currentTabContent = renderConfigTab(m)
	case 2:
		currentTabContent = renderLogsTab(m)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		tabBar,
		"",
		currentTabContent,
	)
}

func renderTabs(activeTab int) string {
	tabs := []string{" [1] Dashboard ", " [2] Configuration ", " [3] Event Log "}
	var renderedTabs []string

	for i, t := range tabs {
		style := lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#94A3B8")).
			Background(lipgloss.Color("#1E293B"))

		if i == activeTab {
			style = style.
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#2563EB"))
		}
		renderedTabs = append(renderedTabs, style.Render(t))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
}

// RunTUI launches the interactive terminal interface.
func RunTUI(ctx context.Context, m *Model) error {
	p := tea.NewProgram(m, tea.WithAltScreen())

	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	_, err := p.Run()
	return err
}
