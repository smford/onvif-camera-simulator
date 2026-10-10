# ONVIF & RTSP Camera Simulator (Pure Go)

A standalone, production-ready **ONVIF Profile S/T/M** and **RTSP Camera Simulator** written in **100% pure Go** (zero Cgo, zero runtime dependencies like FFmpeg).

Designed for SREs, Systems Engineers, and QA pipelines to test and benchmark Network Video Recorders (NVRs) such as **Frigate**, **Home Assistant**, **Shinobi**, **Milestone XProtect**, and **Synology Surveillance Station**.

---

## 🌟 Key Architectural Features

- **Pure Go Media Stack**: Native RTP/RTCP/H.264/G.711 pipeline using [`github.com/bluenviron/gortsplib/v4`](https://github.com/bluenviron/gortsplib) and [`github.com/bluenviron/mediacommon`](https://github.com/bluenviron/mediacommon).
- **Dual RTSP Streams**:
  - `Profile_Main` (`/live/main`): High-resolution recording stream (`1920x1080` @ 20 FPS, 4096 kbps, H.264 + PCMU audio).
  - `Profile_Sub` (`/live/sub`): Low-resolution analytics stream (`640x360` @ 10 FPS, 512 kbps, H.264 + PCMU audio).
- **WebRTC & go2rtc Compatibility**: Pre-encoded Constrained Baseline Profile (`profile-level-id=42e01f`, `packetization-mode=1`) guaranteeing zero black-screen errors in browsers (Chrome/Safari) and direct hardware decoding.
- **Audio & Clock Drift Synchronization**: Synthetic G.711 u-law (PCMU) 8000Hz silence frames emitted every 20ms and periodic RTCP Sender Reports correlated with NTP wall-clock time to prevent NVR stream timeouts.
- **Interactive Terminal User Interface (TUI)**: Built with Charm's `bubbletea`, `lipgloss`, and `bubbles`:
  - **Tab 1: Dashboard**: Live PTZ gauges, active sessions table, event triggers, and SOAP audit trail.
  - **Tab 2: Configuration**: Live form tuning with `[Ctrl+S]` Save and `[Ctrl+R]` Hot-Reload.
  - **Tab 3: System Event Log**: Colorized ring-buffer log viewer with auto-scroll follow mode.
- **Headless Fallback**: Automatic terminal detection (`golang.org/x/term`); drops TUI and switches to structured JSON/Text `slog` output in non-interactive CI/CD pipelines or headless Docker containers.
- **Virtual PTZ Engine (`/onvif/ptz_service`)**: 20Hz kinematics engine simulating Pan, Tilt, and Zoom with velocity clamping, continuous move timeouts, and preset storage.
- **WS-BaseNotification Pull-Point Events (`/onvif/event_service`)**: Long-polling pull-point worker delivering real-time cell motion, human detection (with normalized bounding boxes), and vehicle detection events.
- **SRE Chaos Engineering Engine**:
  - **Stream Freeze**: Keep RTSP TCP sockets open while pausing RTP packet emissions.
  - **Simulated Reboot**: Drop active TCP sockets and HTTP listeners for $N$ seconds to test NVR reconnection backoff.
  - **Packet Loss & Jitter**: Random non-IDR packet dropping.
- **Multi-Camera Fleet Scaling**: Simulate $N$ cameras concurrently (`--fleet 8`) multiplexed across the same ports with negligible CPU and memory overhead (`/live/cam{i}/main`, `/onvif/cam{i}/...`).
- **Automated Scenario Playbook Runner**: Execute automated YAML test scripts (`--scenario playbook.yaml`) for automated CI/CD testing.
- **Prometheus Observability**: Native metrics exposed at `/metrics`, along with `/healthz` and `/readyz` probes.

---

## 🚀 Quick Start

### 1. Build and Run Locally

```bash
# Clone and build
go build -o simulator ./cmd/simulator

# Generate default config.yaml
./simulator --init

# Launch with interactive TUI
./simulator

# Launch in headless mode
./simulator --no-tui
```

### 2. Multi-Camera Fleet Mode

```bash
# Simulate 4 independent cameras (cam0..cam3)
./simulator --fleet 4
```

### 3. Docker & Docker Compose

> [!IMPORTANT]
> `network_mode: host` is configured in `docker-compose.yml` so WS-Discovery multicast probes (`239.255.255.250:3702`) bridge to physical LAN NVRs.

```bash
# Build and run with docker compose
docker compose up -d

# Inspect container logs
docker compose logs -f
```

---

## 🎮 TUI Keybindings

| Key | Description |
| :--- | :--- |
| `Tab` / `Shift+Tab` | Cycle through Tabs (Dashboard, Config, Event Log) |
| `1`, `2`, `3` | Jump directly to Tab 1, 2, or 3 |
| `[` / `]` | Select active camera in Fleet Mode |
| `m` | Toggle Cell Motion event on selected camera |
| `h` | Burst Human AI Detection event (5 seconds) with bounding box |
| `v` | Burst Vehicle AI Detection event (5 seconds) with bounding box |
| `x` | Toggle RTSP Stream Freeze (SRE Chaos) |
| `b` | Trigger Simulated Camera Reboot (5 seconds) |
| `q` / `Ctrl+C` | Graceful exit and socket cleanup |

---

## 🧪 Verification & Integration Commands

### 1. RTSP Video Streams (`ffplay` & `vlc`)

```bash
# Main High-Resolution Stream (1080p @ 20 FPS)
ffplay -rtsp_transport tcp rtsp://127.0.0.1:8554/live/main

# Sub Analytics Stream (360p @ 10 FPS)
ffplay -rtsp_transport tcp rtsp://127.0.0.1:8554/live/sub

# Fleet Camera 2 Stream
ffplay -rtsp_transport tcp rtsp://127.0.0.1:8554/live/cam2/main
```

### 2. Live Dynamic Snapshot

```bash
# Fetch live stamped JPEG with PTZ crosshairs and active detection bounding boxes
curl -o snapshot.jpg http://127.0.0.1:8000/onvif/snapshot
```

### 3. ONVIF SOAP Services

#### Clock Synchronization (`GetSystemDateAndTime`):
```bash
curl -s -X POST http://127.0.0.1:8000/onvif/device_service \
  -H "Content-Type: application/soap+xml; charset=utf-8" \
  -d '<?xml version="1.0" encoding="utf-8"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"/></soap:Body></soap:Envelope>'
```

#### Device Information (`GetDeviceInformation`):
```bash
curl -s -X POST http://127.0.0.1:8000/onvif/device_service \
  -H "Content-Type: application/soap+xml; charset=utf-8" \
  -d '<?xml version="1.0" encoding="utf-8"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><GetDeviceInformation xmlns="http://www.onvif.org/ver10/device/wsdl"/></soap:Body></soap:Envelope>'
```

#### Media Profiles (`GetProfiles`):
```bash
curl -s -X POST http://127.0.0.1:8000/onvif/media_service \
  -H "Content-Type: application/soap+xml; charset=utf-8" \
  -d '<?xml version="1.0" encoding="utf-8"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/></soap:Body></soap:Envelope>'
```

#### Virtual PTZ Continuous Move:
```bash
curl -s -X POST http://127.0.0.1:8000/onvif/ptz_service \
  -H "Content-Type: application/soap+xml; charset=utf-8" \
  -d '<?xml version="1.0" encoding="utf-8"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><ContinuousMove xmlns="http://www.onvif.org/ver20/ptz/wsdl"><ProfileToken>profile_main</ProfileToken><Velocity><PanTilt x="0.5" y="0.2"/></Velocity></ContinuousMove></soap:Body></soap:Envelope>'
```

---

## 📡 REST API & SRE Chaos Injection

### Inspect System & Camera Status via REST

```bash
# Query overall system & chaos status
curl -s http://127.0.0.1:8000/api/status

# Query detailed per-camera telemetry (PTZ coordinates, active analytics events)
curl -s http://127.0.0.1:8000/api/cameras

# Query recent structured log entries from the ring buffer
curl -s http://127.0.0.1:8000/api/logs
```

### Trigger Events via REST

```bash
# Burst Human Detection Event on cam0 with custom bounding box
curl -X POST http://127.0.0.1:8000/api/events/trigger \
  -H "Content-Type: application/json" \
  -d '{"camera": 0, "type": "human", "state": true, "duration_seconds": 10, "bbox": [0.35, 0.25, 0.20, 0.50]}'

# Trigger Cell Motion
curl -X POST http://127.0.0.1:8000/api/events/trigger \
  -H "Content-Type: application/json" \
  -d '{"camera": 0, "type": "motion", "state": true, "duration_seconds": 5}'
```

### Inject Chaos / Faults

```bash
# Freeze RTSP stream for 10 seconds (keep TCP connection open)
curl -X POST http://127.0.0.1:8000/api/chaos/inject \
  -H "Content-Type: application/json" \
  -d '{"action": "freeze", "duration_seconds": 10}'

# Simulate Camera Hardware Reboot (drop sockets for 5 seconds)
curl -X POST http://127.0.0.1:8000/api/chaos/inject \
  -H "Content-Type: application/json" \
  -d '{"action": "reboot", "duration_seconds": 5}'

# Introduce 25% non-IDR packet loss for 15 seconds
curl -X POST http://127.0.0.1:8000/api/chaos/inject \
  -H "Content-Type: application/json" \
  -d '{"action": "packet_loss", "duration_seconds": 15, "loss_percentage": 25}'
```

---

## 📊 Prometheus Observability

Metrics are scraped at `http://127.0.0.1:8000/metrics`:

| Metric | Type | Labels | Description |
| :--- | :--- | :--- | :--- |
| `onvif_connected_clients` | Gauge | `protocol`, `stream`, `camera` | Active RTSP/SOAP client sessions |
| `onvif_stream_bandwidth_bytes_total` | Counter | `stream`, `camera` | Total stream bytes transmitted |
| `onvif_rtp_packets_sent_total` | Counter | `stream`, `type` | Total video/audio RTP packets emitted |
| `onvif_events_emitted_total` | Counter | `topic` | Dispatched WS-BaseNotification events |
| `onvif_ptz_commands_total` | Counter | `action` | PTZ kinematics commands executed |
| `onvif_chaos_faults_injected_total` | Counter | `action` | Fault injections triggered |

Health probes:
- `GET /healthz` -> Returns `200 OK`
- `GET /readyz`  -> Returns `200 OK` (returns `503 Service Unavailable` during simulated reboots)

---

## 🤖 CI/CD Scripted Scenario Runner

Create a scenario YAML file (e.g., `playbook.yaml`):

```yaml
scenario:
  - at: 5s
    action: "event"
    camera: 0
    type: "motion"
    state: true
  - at: 10s
    action: "event"
    camera: 0
    type: "human"
    state: true
    bbox: [0.35, 0.25, 0.20, 0.50]
  - at: 15s
    action: "chaos"
    camera: 0
    type: "freeze"
    duration: 5s
  - at: 25s
    action: "event"
    camera: 0
    type: "human"
    state: false
```

Execute automatically in your CI pipeline:

```bash
./simulator --scenario playbook.yaml
```

---

## 📜 License

MIT License. Designed with standard Go concurrency, clean domain boundaries, and zero external runtime dependencies.