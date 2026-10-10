# ONVIF & RTSP Camera Simulator

[![Release](https://img.shields.io/github/v/release/smford/onvif-camera-simulator?color=blue&logo=github)](https://github.com/smford/onvif-camera-simulator/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/smford/onvif-camera-simulator)](https://goreportcard.com/report/github.com/smford/onvif-camera-simulator)
[![Documentation](https://img.shields.io/badge/Docs-GitHub%20Pages-38bdf8)](https://smford.github.io/onvif-camera-simulator/)

A production-ready **ONVIF Profile S/T/M** and **Dual RTSP Camera Simulator** written in **100% pure Go** with zero Cgo and zero external dependencies (no FFmpeg required).

Built for SREs, QA engineers, and video pipeline developers to test and stress-test Network Video Recorders (NVRs) such as **Frigate**, **Home Assistant**, **Shinobi**, **Milestone XProtect**, and **Synology Surveillance Station**.

---

## ⚡ Quick Start

### 1. Install

**macOS (Homebrew):**
```bash
brew tap smford/tap
brew install onvif-camera-simulator
```

**Docker:**
```bash
# Run with host networking for UDP WS-Discovery broadcast
docker run -d --name onvif-simulator --network host ghcr.io/smford/onvif-camera-simulator:latest --no-tui
```

**Linux (Debian / Ubuntu / Raspberry Pi OS):**
```bash
curl -LO https://github.com/smford/onvif-camera-simulator/releases/latest/download/onvif-camera-simulator_0.2.0_linux_amd64.deb
sudo dpkg -i onvif-camera-simulator_0.2.0_linux_amd64.deb
```

*(Fedora `.rpm`, Alpine `.apk`, Arch `.pkg.tar.zst`, and Windows `.zip` releases are available on the [Releases page](https://github.com/smford/onvif-camera-simulator/releases) and [Documentation Site](https://smford.github.io/onvif-camera-simulator/).)*

### 2. Run

```bash
# Launch interactive terminal UI (default single camera)
onvif-camera-simulator

# Or run 4 cameras headless in the background
onvif-camera-simulator --no-tui --fleet 4
```

---

## 📸 Interactive Terminal UI (TUI)

The simulator includes a zero-dependency interactive dashboard built with Charm's `bubbletea` and `lipgloss`:

- **Tab 1: Dashboard**: Live PTZ coordinate gauges, active RTSP client sessions table, event triggers, and SOAP audit trail.
- **Tab 2: Configuration**: In-terminal configuration editor with hot-reload.
- **Tab 3: Event Log**: Real-time color-coded syslog/event stream backed by a 250-record ring buffer.

### Keyboard Shortcuts

| Key | Action | Description |
| :--- | :--- | :--- |
| <kbd>Tab</kbd> / <kbd>Shift+Tab</kbd> | Switch Tabs | Cycle between `[1] Dashboard`, `[2] Configuration`, and `[3] Event Log` |
| <kbd>1</kbd>, <kbd>2</kbd>, <kbd>3</kbd> | Jump to Tab | Jump directly to Tab 1, 2, or 3 |
| <kbd>[</kbd> / <kbd>]</kbd> | Camera Select | Cycle active camera (`cam0` .. `camN`) in Fleet Mode |
| <kbd>m</kbd> | Motion Event | Toggle Profile S cell motion detection event |
| <kbd>h</kbd> | Human Detection | Burst Profile T/M AI human detection (5s) with bounding box |
| <kbd>v</kbd> | Vehicle Detection | Burst Profile T/M AI vehicle detection (5s) with bounding box |
| <kbd>x</kbd> | Freeze Stream | Toggle RTSP stream freeze (holds TCP connection, pauses RTP) |
| <kbd>b</kbd> | Camera Reboot | Simulate hardware drop (disconnects sockets for 5s) |
| <kbd>f</kbd> / <kbd>c</kbd> | Log Controls | Toggle auto-scroll follow (<kbd>f</kbd>) or clear log buffer (<kbd>c</kbd>) |
| <kbd>q</kbd> / <kbd>Ctrl+C</kbd> | Exit | Graceful shutdown with WS-Discovery Bye multicast |

---

## 📡 Endpoints & Stream Layout

The simulator runs on two primary ports:
- **HTTP/SOAP (Port `8000`)**: ONVIF Web Services, Dynamic JPEG Snapshot generator, REST API, Prometheus `/metrics`, and `/healthz`.
- **RTSP (Port `8554`)**: Dual H.264 streams (`1080p` Main + `360p` Sub) with synchronized G.711 u-law audio.

### Single Camera Mode (`fleet: 1`)

| Service | Protocol | Endpoint URL | Description |
| :--- | :--- | :--- | :--- |
| **Main Stream** | RTSP | `rtsp://<ip>:8554/live/main` | 1080p @ 20 FPS, H.264 Baseline + PCMU |
| **Sub Stream** | RTSP | `rtsp://<ip>:8554/live/sub` | 360p @ 10 FPS, H.264 Baseline + PCMU |
| **ONVIF Device** | HTTP | `http://<ip>:8000/onvif/device_service` | Profile S/T/M Device & Discovery |
| **ONVIF Media** | HTTP | `http://<ip>:8000/onvif/media_service` | Stream URI resolution & Profiles |
| **ONVIF Events** | HTTP | `http://<ip>:8000/onvif/event_service` | WS-BaseNotification Pull-Point worker |
| **ONVIF PTZ** | HTTP | `http://<ip>:8000/onvif/ptz_service` | ContinuousMove & Presets |
| **Live Snapshot** | HTTP | `http://<ip>:8000/onvif/snapshot` | Dynamic JPEG with HUD & bounding boxes |

### Multi-Camera Fleet Mode (`--fleet N`)

When running with `--fleet N` (e.g. `--fleet 4`), streams and services are cleanly namespaced under `/cam{i}/`:
- **RTSP Main**: `rtsp://<ip>:8554/live/cam0/main` ... `/live/cam3/main`
- **RTSP Sub**: `rtsp://<ip>:8554/live/cam0/sub` ... `/live/cam3/sub`
- **ONVIF Core**: `http://<ip>:8000/onvif/cam0/device_service` ... `/cam3/device_service`
- **Snapshot**: `http://<ip>:8000/onvif/cam0/snapshot` ... `/cam3/snapshot`
*(Note: `/live/main` and `/onvif/device_service` automatically alias to `cam0` for backward compatibility).*

---

## 🛠️ NVR Integrations

### 1. Frigate NVR

Add the simulator to your `config.yml`:

```yaml
cameras:
  simulated_cam0:
    ffmpeg:
      inputs:
        # High-resolution 1080p stream for recording
        - path: rtsp://127.0.0.1:8554/live/cam0/main
          roles:
            - record
        # Low-resolution 360p stream for motion & AI detection
        - path: rtsp://127.0.0.1:8554/live/cam0/sub
          roles:
            - detect
    detect:
      width: 640
      height: 360
      fps: 10
    onvif:
      host: 127.0.0.1
      port: 8000
      user: admin
      password: admin
```

### 2. Home Assistant

1. Deploy the simulator with host networking on your LAN.
2. Home Assistant will automatically detect the camera via **WS-Discovery (UDP 3702)**.
3. Alternatively, manually add via **Settings &rarr; Devices &amp; Services &rarr; Add Integration &rarr; ONVIF**:
   - **Host:** `127.0.0.1` (or server LAN IP)
   - **Port:** `8000`
   - **Username:** `admin` | **Password:** `admin`

### 3. Quick Video Playback Test

```bash
# Play Main Stream in VLC or ffplay
ffplay -rtsp_transport tcp rtsp://127.0.0.1:8554/live/main

# Fetch live stamped JPEG snapshot
curl -o snapshot.jpg http://127.0.0.1:8000/onvif/snapshot
```

---

## ⚡ REST API & SRE Chaos Engineering

Programmatically trigger analytics events and simulate production camera failures in automated tests:

### Inspect System Status
```bash
# Query active cameras, fleet health, and active sessions
curl -s http://127.0.0.1:8000/api/status | jq .

# List all camera stream URLs
curl -s http://127.0.0.1:8000/api/cameras | jq .

# Fetch recent log ring buffer
curl -s http://127.0.0.1:8000/api/logs | jq .
```

### Trigger Analytics Events
```bash
# Trigger Cell Motion event (cam0)
curl -X POST http://127.0.0.1:8000/api/events/trigger \
  -H "Content-Type: application/json" \
  -d '{"topic":"motion", "state":true}'

# Trigger AI Person detection with custom duration
curl -X POST http://127.0.0.1:8000/api/events/trigger \
  -H "Content-Type: application/json" \
  -d '{"topic":"person", "state":true, "duration_seconds":10}'
```

### Inject Faults (Chaos Testing)
```bash
# 1. Stream Freeze: Holds TCP connection open without sending RTP frames
curl -X POST http://127.0.0.1:8000/api/chaos/inject \
  -H "Content-Type: application/json" \
  -d '{"type":"freeze", "duration":"10s"}'

# 2. Simulated Reboot: Drops TCP sockets and rejects reconnects
curl -X POST http://127.0.0.1:8000/api/chaos/inject \
  -H "Content-Type: application/json" \
  -d '{"type":"reboot", "duration":"5s"}'

# 3. Packet Loss: Simulates 25% packet drop rate
curl -X POST http://127.0.0.1:8000/api/chaos/inject \
  -H "Content-Type: application/json" \
  -d '{"type":"packet_loss", "percentage":25, "duration":"20s"}'

# 4. Clear all active chaos conditions
curl -X POST http://127.0.0.1:8000/api/chaos/clear
```

---

## 🤖 Automated CI/CD Scenarios

Execute scripted end-to-end playbooks in headless mode:

```bash
# Run a scripted test scenario
onvif-camera-simulator --scenario playbook.yaml --no-tui
```

Example `playbook.yaml`:
```yaml
scenario:
  - at: 5s
    action: "event"
    camera: 0
    type: "motion"
    state: true
  - at: 10s
    action: "chaos"
    type: "freeze"
    duration: 5s
  - at: 20s
    action: "event"
    camera: 0
    type: "motion"
    state: false
```

---

## 📊 Observability

- **Prometheus Metrics**: Scrape `http://127.0.0.1:8000/metrics` for client count, bitrates, frame rates, and fault counters.
- **Health Probes**: `GET /healthz` returns `200 OK`. `GET /readyz` returns `503 Service Unavailable` during simulated reboots.

---

## 📄 License

Created by **[Stephen Ford](https://github.com/smford)**. Distributed under the [MIT License](LICENSE).