package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/smford/onvif-camera-simulator/internal/api"
	"github.com/smford/onvif-camera-simulator/internal/chaos"
	"github.com/smford/onvif-camera-simulator/internal/config"
	"github.com/smford/onvif-camera-simulator/internal/discovery"
	"github.com/smford/onvif-camera-simulator/internal/events"
	"github.com/smford/onvif-camera-simulator/internal/fleet"
	"github.com/smford/onvif-camera-simulator/internal/onvif"
	"github.com/smford/onvif-camera-simulator/internal/rtsp"
	"github.com/smford/onvif-camera-simulator/internal/scenario"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
	"github.com/smford/onvif-camera-simulator/internal/tui"
	"golang.org/x/term"
)

var (
	// Version, GitCommit, and BuildDate are populated during build via ldflags
	Version   = "dev"
	GitCommit = "none"
	BuildDate = "unknown"
)

func main() {
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	vFlag := flag.Bool("v", false, "Print version information and exit (shorthand)")
	configPath := flag.String("config", "config.yaml", "Path to YAML configuration file")
	initFlag := flag.Bool("init", false, "Generate a fully annotated config.yaml template and exit")
	noTUIFlag := flag.Bool("no-tui", false, "Force headless mode without interactive TUI")
	fleetCountFlag := flag.Int("fleet", 0, "Override number of simulated cameras in fleet (e.g. 1..32)")
	scenarioPath := flag.String("scenario", "", "Path to scripted scenario playbook YAML for automated CI/CD runs")
	logLevelFlag := flag.String("log-level", "", "Override logging level (debug, info, warn, error)")
	flag.Parse()

	// 0. Handle version flag
	if *versionFlag || *vFlag {
		fmt.Printf("onvif-camera-simulator version %s (commit: %s, built at: %s)\n", Version, GitCommit, BuildDate)
		os.Exit(0)
	}

	// 1. Handle --init flag
	if *initFlag {
		target := *configPath
		if target == "" {
			target = "config.yaml"
		}
		if err := config.GenerateInitConfig(target); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing config template: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Successfully generated initial configuration file: %s\n", target)
		os.Exit(0)
	}

	// 2. Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config loading error: %v\n", err)
		os.Exit(1)
	}

	// CLI overrides
	if *fleetCountFlag > 0 {
		cfg.Fleet.Count = *fleetCountFlag
		cfg.Fleet.Enabled = true
	}
	if *logLevelFlag != "" {
		cfg.Server.LogLevel = *logLevelFlag
	}

	// Determine log level
	var slogLevel slog.Level
	switch strings.ToLower(cfg.Server.LogLevel) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	// 3. Terminal detection & headless fallback
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	headless := *noTUIFlag || *scenarioPath != "" || !isTTY

	// Configure logging:
	// If headless, write directly to stdout. If TUI is active, route through ring buffer to avoid screen tearing.
	ringHandler := telemetry.NewRingBufferLogHandler(250, slogLevel)
	if headless {
		var handler slog.Handler
		if cfg.Server.LogFormat == "json" {
			handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
		} else {
			handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})
		}
		// Multi-handler: stdout + ring buffer for REST inspection
		slog.SetDefault(slog.New(&multiHandler{stdoutHandler: handler, ringHandler: ringHandler}))
	} else {
		slog.SetDefault(slog.New(ringHandler))
	}

	slog.Info("Starting ONVIF & RTSP Camera Simulator",
		"version", Version,
		"commit", GitCommit,
		"advertised_ip", cfg.Server.AdvertisedIP,
		"http_port", cfg.Server.HTTPPort,
		"rtsp_port", cfg.Server.RTSPPort,
		"headless", headless,
		"fleet", cfg.Fleet.Count,
	)

	// 4. Root lifecycle context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("Termination signal received, beginning graceful teardown...")
		cancel()
	}()

	// 5. Initialize core subsystems
	hub := telemetry.NewHub(cfg.Fleet.Count)
	eventMgr := events.NewManager(hub)

	// Forward-declare reboot callback for chaos engine
	var onvifServer *onvif.Server
	var rtspServer *rtsp.Server

	chaosEng := chaos.NewEngine(hub, func(duration time.Duration) {
		if onvifServer != nil {
			onvifServer.DropConnections(duration)
		}
		if rtspServer != nil {
			rtspServer.DropConnections(duration)
		}
	})

	// Start ONVIF HTTP/HTTPS Server
	onvifServer = onvif.NewServer(cfg, hub, eventMgr)

	// Attach REST API handler
	apiHandler := api.NewAPIHandler(hub, eventMgr, chaosEng, ringHandler)
	onvifServer.SetAPIRoutes(func(mux *http.ServeMux) {
		apiHandler.RegisterRoutes(mux)
	})

	if err := onvifServer.Start(); err != nil {
		slog.Error("Fatal: Failed to start ONVIF HTTP server", "err", err)
		os.Exit(1)
	}

	// Start RTSP Server
	rtspServer = rtsp.NewServer(cfg, hub)
	if err := rtspServer.Start(); err != nil {
		slog.Error("Fatal: Failed to start RTSP server", "err", err)
		os.Exit(1)
	}

	// Initialize Multi-Camera Fleet
	fleetMgr := fleet.NewManager(cfg, hub, rtspServer, onvifServer, eventMgr)
	if err := fleetMgr.InitializeFleet(cfg.Fleet.Count); err != nil {
		slog.Error("Fatal: Failed to initialize camera fleet", "err", err)
		os.Exit(1)
	}

	// Start WS-Discovery UDP Multicast Server
	discoveryServer := discovery.NewServer(cfg, fleetMgr)
	if err := discoveryServer.Start(); err != nil {
		slog.Warn("WS-Discovery startup warning", "err", err)
	}

	// 6. Automated Scenario Runner Mode
	if *scenarioPath != "" {
		runner := scenario.NewRunner(*scenarioPath, eventMgr, chaosEng)
		if err := runner.Run(ctx); err != nil {
			slog.Error("Scenario runner failed", "err", err)
			os.Exit(1)
		}
		slog.Info("Scenario execution finished. Exiting.")
		return
	}

	// 7. Interactive TUI or Headless Wait
	if !headless {
		tuiModel := tui.NewModel(cfg, *configPath, hub, ringHandler, eventMgr, chaosEng, func(newCfg *config.Config) {
			slog.Info("Hot-reload applied from TUI")
		})
		if err := tui.RunTUI(ctx, tuiModel); err != nil {
			slog.Error("TUI terminated with error", "err", err)
		}
		cancel()
	} else {
		slog.Info("Running in headless mode. Press Ctrl+C to terminate.")
		<-ctx.Done()
	}

	// 8. Graceful shutdown
	slog.Info("Cleaning up network listeners and virtual motors...")
	discoveryServer.Stop()
	rtspServer.Close()
	onvifServer.Stop()
	eventMgr.Close()
	fleetMgr.Close()
	slog.Info("Simulator stopped cleanly.")
}

type multiHandler struct {
	stdoutHandler slog.Handler
	ringHandler   slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return m.stdoutHandler.Enabled(ctx, level) || m.ringHandler.Enabled(ctx, level)
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	_ = m.stdoutHandler.Handle(ctx, r)
	_ = m.ringHandler.Handle(ctx, r)
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &multiHandler{
		stdoutHandler: m.stdoutHandler.WithAttrs(attrs),
		ringHandler:   m.ringHandler.WithAttrs(attrs),
	}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	return &multiHandler{
		stdoutHandler: m.stdoutHandler.WithGroup(name),
		ringHandler:   m.ringHandler.WithGroup(name),
	}
}

// DiscardWriter
type discardWriter struct{}

func (d discardWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

var _ io.Writer = discardWriter{}
