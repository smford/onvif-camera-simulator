package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	ConnectedClients = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "onvif_connected_clients",
			Help: "Number of connected clients by protocol, stream, and camera.",
		},
		[]string{"protocol", "stream", "camera"},
	)

	StreamBandwidthBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "onvif_stream_bandwidth_bytes_total",
			Help: "Total bytes transmitted over RTSP streams.",
		},
		[]string{"stream", "camera"},
	)

	RTPPacketsSent = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "onvif_rtp_packets_sent_total",
			Help: "Total count of RTP packets emitted.",
		},
		[]string{"stream", "type"},
	)

	EventsEmitted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "onvif_events_emitted_total",
			Help: "Total ONVIF WS-BaseNotification events dispatched.",
		},
		[]string{"topic"},
	)

	PTZCommands = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "onvif_ptz_commands_total",
			Help: "Total PTZ motion commands processed.",
		},
		[]string{"action"},
	)

	ChaosFaultsInjected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "onvif_chaos_faults_injected_total",
			Help: "Total chaos fault injection events triggered.",
		},
		[]string{"action"},
	)
)

var registry = prometheus.NewRegistry()

func init() {
	registry.MustRegister(
		ConnectedClients,
		StreamBandwidthBytes,
		RTPPacketsSent,
		EventsEmitted,
		PTZCommands,
		ChaosFaultsInjected,
	)
}

// Handler returns the HTTP handler for Prometheus scraping.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}
