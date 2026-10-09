package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the complete simulator configuration schema.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Discovery DiscoveryConfig `yaml:"discovery"`
	Auth      AuthConfig      `yaml:"auth"`
	Fleet     FleetConfig     `yaml:"fleet"`
	Camera    CameraConfig    `yaml:"camera"`
	PTZ       PTZConfig       `yaml:"ptz"`
	Events    EventsConfig    `yaml:"events"`
	Streams   StreamsConfig   `yaml:"streams"`
}

type ServerConfig struct {
	BindAddress    string `yaml:"bind_address"`
	AdvertisedIP   string `yaml:"advertised_ip"`
	HTTPPort       int    `yaml:"http_port"`
	RTSPPort       int    `yaml:"rtsp_port"`
	TLSEnabled     bool   `yaml:"tls_enabled"`
	MetricsEnabled bool   `yaml:"metrics_enabled"`
	LogLevel       string `yaml:"log_level"`
	LogFormat      string `yaml:"log_format"`
}

type DiscoveryConfig struct {
	Enabled            bool   `yaml:"enabled"`
	MulticastInterface string `yaml:"multicast_interface"`
	MulticastAddress   string `yaml:"multicast_address"`
}

type AuthConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type FleetConfig struct {
	Enabled bool `yaml:"enabled"`
	Count   int  `yaml:"count"`
}

type CameraConfig struct {
	Manufacturer string `yaml:"manufacturer"`
	Model        string `yaml:"model"`
	Firmware     string `yaml:"firmware"`
	SerialNumber string `yaml:"serial_number"`
	HardwareID   string `yaml:"hardware_id"`
}

type PTZConfig struct {
	Enabled      bool    `yaml:"enabled"`
	MaxPanSpeed  float64 `yaml:"max_pan_speed"`
	MaxTiltSpeed float64 `yaml:"max_tilt_speed"`
	MaxZoomSpeed float64 `yaml:"max_zoom_speed"`
	HomePan      float64 `yaml:"home_pan"`
	HomeTilt     float64 `yaml:"home_tilt"`
	HomeZoom     float64 `yaml:"home_zoom"`
}

type EventsConfig struct {
	Enabled                     bool `yaml:"enabled"`
	ChaosMode                   bool `yaml:"chaos_mode"`
	ChaosIntervalSeconds        int  `yaml:"chaos_interval_seconds"`
	DefaultEventDurationSeconds int  `yaml:"default_event_duration_seconds"`
}

type StreamProfileConfig struct {
	Path         string `yaml:"path"`
	Width        int    `yaml:"width"`
	Height       int    `yaml:"height"`
	FPS          int    `yaml:"fps"`
	BitrateKbps  int    `yaml:"bitrate_kbps"`
	AudioEnabled bool   `yaml:"audio_enabled"`
	SourceFile   string `yaml:"source_file"`
}

type StreamsConfig struct {
	Main StreamProfileConfig `yaml:"main"`
	Sub  StreamProfileConfig `yaml:"sub"`
}

// DefaultConfig returns a production-grade default configuration.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			BindAddress:    "0.0.0.0",
			AdvertisedIP:   "127.0.0.1",
			HTTPPort:       8000,
			RTSPPort:       8554,
			TLSEnabled:     false,
			MetricsEnabled: true,
			LogLevel:       "info",
			LogFormat:      "text",
		},
		Discovery: DiscoveryConfig{
			Enabled:            true,
			MulticastInterface: "",
			MulticastAddress:   "239.255.255.250:3702",
		},
		Auth: AuthConfig{
			Enabled:  false,
			Username: "admin",
			Password: "password123",
		},
		Fleet: FleetConfig{
			Enabled: false,
			Count:   1,
		},
		Camera: CameraConfig{
			Manufacturer: "Simulated",
			Model:        "ONVIF-Go-SRE-Enterprise",
			Firmware:     "1.0.0",
			SerialNumber: "SIM-GO-005",
			HardwareID:   "REV-5",
		},
		PTZ: PTZConfig{
			Enabled:      true,
			MaxPanSpeed:  1.0,
			MaxTiltSpeed: 1.0,
			MaxZoomSpeed: 1.0,
			HomePan:      0.0,
			HomeTilt:     0.0,
			HomeZoom:     0.0,
		},
		Events: EventsConfig{
			Enabled:                     true,
			ChaosMode:                   false,
			ChaosIntervalSeconds:        30,
			DefaultEventDurationSeconds: 5,
		},
		Streams: StreamsConfig{
			Main: StreamProfileConfig{
				Path:         "/live/main",
				Width:        1920,
				Height:       1080,
				FPS:          20,
				BitrateKbps:  4096,
				AudioEnabled: true,
				SourceFile:   "",
			},
			Sub: StreamProfileConfig{
				Path:         "/live/sub",
				Width:        640,
				Height:       360,
				FPS:          10,
				BitrateKbps:  512,
				AudioEnabled: true,
				SourceFile:   "",
			},
		},
	}
}

// LoadConfig reads YAML config from the given path, falling back to defaults for unspecified fields.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		path = "config.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Auto-detect advertised IP if default
			cfg.ResolveAdvertisedIP()
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml config %q: %w", path, err)
	}

	cfg.ResolveAdvertisedIP()
	return cfg, nil
}

// SaveConfig serializes the configuration to disk.
func SaveConfig(path string, cfg *Config) error {
	if path == "" {
		path = "config.yaml"
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// ResolveAdvertisedIP detects the preferred local non-loopback IP if AdvertisedIP is empty or 0.0.0.0.
func (c *Config) ResolveAdvertisedIP() {
	if c.Server.AdvertisedIP != "" && c.Server.AdvertisedIP != "0.0.0.0" {
		return
	}

	ip := DetectLocalIP()
	if ip != "" {
		c.Server.AdvertisedIP = ip
	} else {
		c.Server.AdvertisedIP = "127.0.0.1"
	}
}

// DetectLocalIP attempts to find an active outbound IP address.
func DetectLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String()
	}

	// Fallback to iterating network interfaces
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}

	return "127.0.0.1"
}

// GenerateInitConfig writes a complete, commented config.yaml file.
func GenerateInitConfig(path string) error {
	defaultYAML := `# ONVIF & RTSP Camera Simulator Configuration
server:
  bind_address: "0.0.0.0"
  advertised_ip: "127.0.0.1" # Used in ONVIF XAddrs and RTSP SDP; auto-detected if blank
  http_port: 8000
  rtsp_port: 8554
  tls_enabled: false # Generates ephemeral self-signed certificate on port 8443
  metrics_enabled: true
  log_level: "info"
  log_format: "text" # "text" or "json" (used when headless)

discovery:
  enabled: true
  multicast_interface: "" # e.g., "eth0", "en0"; blank uses default route
  multicast_address: "239.255.255.250:3702"

auth:
  enabled: false
  username: "admin"
  password: "password123"

fleet:
  enabled: false
  count: 1 # Number of virtual cameras (cam0..camN)

camera:
  manufacturer: "Simulated"
  model: "ONVIF-Go-SRE-Enterprise"
  firmware: "1.0.0"
  serial_number: "SIM-GO-005"
  hardware_id: "REV-5"

ptz:
  enabled: true
  max_pan_speed: 1.0
  max_tilt_speed: 1.0
  max_zoom_speed: 1.0
  home_pan: 0.0
  home_tilt: 0.0
  home_zoom: 0.0

events:
  enabled: true
  chaos_mode: false
  chaos_interval_seconds: 30
  default_event_duration_seconds: 5

streams:
  main:
    path: "/live/main"
    width: 1920
    height: 1080
    fps: 20
    bitrate_kbps: 4096
    audio_enabled: true
    source_file: "" # Empty uses embedded synthetic stream
  sub:
    path: "/live/sub"
    width: 640
    height: 360
    fps: 10
    bitrate_kbps: 512
    audio_enabled: true
    source_file: "" # Empty uses embedded synthetic stream
`
	if path == "" {
		path = "config.yaml"
	}
	return os.WriteFile(path, []byte(defaultYAML), 0644)
}
