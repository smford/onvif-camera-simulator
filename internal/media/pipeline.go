package media

import (
	"bytes"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v4/pkg/format/rtplpcm"
	"github.com/bluenviron/mediacommon/pkg/codecs/h264"
	"github.com/smford/onvif-camera-simulator/internal/assets"
	"github.com/smford/onvif-camera-simulator/internal/metrics"
	"github.com/smford/onvif-camera-simulator/internal/telemetry"
)

// Pipeline manages RTP packetization and high-precision emission for an RTSP stream.
type Pipeline struct {
	streamName   string
	cameraName   string
	fps          int
	audioEnabled bool
	hub          *telemetry.Hub
	serverStream *gortsplib.ServerStream
	videoMedia   *description.Media
	audioMedia   *description.Media
	h264Encoder  *rtph264.Encoder
	g711Encoder  *rtplpcm.Encoder
	sps          []byte
	pps          []byte
	idr          []byte
	pFrames      [][]byte
	stopCh       chan struct{}
}

// MediaSetup returns the session description configured with Constrained Baseline H.264 and G.711 PCMU.
type MediaSetup struct {
	Description *description.Session
	VideoMedia  *description.Media
	AudioMedia  *description.Media
	H264Encoder *rtph264.Encoder
	G711Encoder *rtplpcm.Encoder
	SPS         []byte
	PPS         []byte
	IDR         []byte
	PFrames     [][]byte
}

// CreateMediaSetup prepares formats, encoders, and session descriptions for main or sub stream.
func CreateMediaSetup(isMain bool, sourceFile string) (*MediaSetup, error) {
	var rawData []byte
	if sourceFile != "" {
		data, err := os.ReadFile(sourceFile)
		if err == nil && len(data) > 0 {
			rawData = data
		} else {
			slog.Warn("Failed to read source_file, falling back to embedded pattern", "file", sourceFile, "err", err)
		}
	}

	if len(rawData) == 0 {
		if isMain {
			rawData = assets.Pattern1080pH264
		} else {
			rawData = assets.Pattern360pH264
		}
	}

	nalus, err := h264.AnnexBUnmarshal(rawData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Annex-B stream: %w", err)
	}

	var sps, pps, idr []byte
	var pFrames [][]byte

	for _, n := range nalus {
		if len(n) == 0 {
			continue
		}
		typ := h264.NALUType(n[0] & 0x1f)
		switch typ {
		case h264.NALUTypeSPS:
			sps = n
		case h264.NALUTypePPS:
			pps = n
		case h264.NALUTypeIDR:
			idr = n
		case h264.NALUTypeNonIDR:
			pFrames = append(pFrames, n)
		}
	}

	if sps == nil || pps == nil || idr == nil {
		return nil, fmt.Errorf("Annex-B bitstream missing required SPS, PPS, or IDR NAL units")
	}
	if len(pFrames) == 0 {
		// Synthesize a generic P-frame skip unit if missing
		pFrames = append(pFrames, []byte{0x41, 0x9a, 0x21, 0x6c, 0x45, 0xff})
	}

	// 1. Constrained Baseline H.264 RTP format
	h264Format := &format.H264{
		PayloadTyp:        96,
		SPS:               sps,
		PPS:               pps,
		PacketizationMode: 1,
	}

	h264Enc, err := h264Format.CreateEncoder()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize H264 RTP encoder: %w", err)
	}

	videoMedia := &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{h264Format},
	}

	// 2. Synthetic G.711 PCMU (mu-law, PT 0, 8000Hz, 1 channel)
	g711Format := &format.G711{
		PayloadTyp:   0,
		MULaw:        true,
		SampleRate:   8000,
		ChannelCount: 1,
	}

	g711Enc, err := g711Format.CreateEncoder()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize G711 PCMU encoder: %w", err)
	}

	audioMedia := &description.Media{
		Type:    description.MediaTypeAudio,
		Formats: []format.Format{g711Format},
	}

	desc := &description.Session{
		Medias: []*description.Media{videoMedia, audioMedia},
	}

	return &MediaSetup{
		Description: desc,
		VideoMedia:  videoMedia,
		AudioMedia:  audioMedia,
		H264Encoder: h264Enc,
		G711Encoder: g711Enc,
		SPS:         sps,
		PPS:         pps,
		IDR:         idr,
		PFrames:     pFrames,
	}, nil
}

// NewPipeline creates an active media transmitter attached to a ServerStream.
func NewPipeline(
	cameraName string,
	streamName string,
	fps int,
	audioEnabled bool,
	hub *telemetry.Hub,
	serverStream *gortsplib.ServerStream,
	setup *MediaSetup,
) *Pipeline {
	if fps <= 0 {
		fps = 20
	}

	return &Pipeline{
		cameraName:   cameraName,
		streamName:   streamName,
		fps:          fps,
		audioEnabled: audioEnabled,
		hub:          hub,
		serverStream: serverStream,
		videoMedia:   setup.VideoMedia,
		audioMedia:   setup.AudioMedia,
		h264Encoder:  setup.H264Encoder,
		g711Encoder:  setup.G711Encoder,
		sps:          setup.SPS,
		pps:          setup.PPS,
		idr:          setup.IDR,
		pFrames:      setup.PFrames,
		stopCh:       make(chan struct{}),
	}
}

// Start launches background goroutines emitting video and audio frames.
func (p *Pipeline) Start() {
	go p.videoLoop()
	if p.audioEnabled {
		go p.audioLoop()
	}
}

// Stop terminates the background emitters.
func (p *Pipeline) Stop() {
	close(p.stopCh)
}

func (p *Pipeline) videoLoop() {
	interval := time.Duration(1000/p.fps) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// H.264 clock rate = 90kHz
	ptsStep := uint32(90000 / p.fps)
	var pts uint32
	frameIdx := 0
	keyframeInterval := p.fps // Send IDR every 1 second

	for {
		select {
		case <-p.stopCh:
			return
		case now := <-ticker.C:
			pts += ptsStep

			// SRE Chaos: Stream freeze check
			if p.hub.IsChaosFrozen() || p.hub.IsChaosReboot() {
				continue
			}

			// SRE Chaos: Packet loss injection for non-keyframe packets
			lossPct := p.hub.GetChaosLossPct()

			isKeyframe := (frameIdx % keyframeInterval) == 0
			frameIdx++

			var au [][]byte
			if isKeyframe {
				// Access Unit: SPS + PPS + IDR
				au = [][]byte{p.sps, p.pps, p.idr}
			} else {
				if lossPct > 0 && rand.Intn(100) < lossPct {
					// Drop non-IDR frame
					continue
				}
				pIdx := (frameIdx - 1) % len(p.pFrames)
				au = [][]byte{p.pFrames[pIdx]}
			}

			pkts, err := p.h264Encoder.Encode(au)
			if err != nil {
				continue
			}

			for _, pkt := range pkts {
				pkt.Timestamp = pts
				if err := p.serverStream.WritePacketRTPWithNTP(p.videoMedia, pkt, now); err != nil {
					// Client write error or no listeners
					continue
				}
				metrics.RTPPacketsSent.WithLabelValues(p.streamName, "video").Inc()
				metrics.StreamBandwidthBytes.WithLabelValues(p.streamName, p.cameraName).Add(float64(len(pkt.Payload)))
			}
		}
	}
}

func (p *Pipeline) audioLoop() {
	// G.711 PCMU: 8000Hz, 160 samples (160 bytes) every 20ms
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	silenceFrame := bytes.Repeat([]byte{0xFF}, 160)
	var pts uint32
	ptsStep := uint32(160)

	for {
		select {
		case <-p.stopCh:
			return
		case now := <-ticker.C:
			pts += ptsStep

			if p.hub.IsChaosFrozen() || p.hub.IsChaosReboot() {
				continue
			}

			pkts, err := p.g711Encoder.Encode(silenceFrame)
			if err != nil {
				continue
			}

			for _, pkt := range pkts {
				pkt.Timestamp = pts
				if err := p.serverStream.WritePacketRTPWithNTP(p.audioMedia, pkt, now); err != nil {
					continue
				}
				metrics.RTPPacketsSent.WithLabelValues(p.streamName, "audio").Inc()
				metrics.StreamBandwidthBytes.WithLabelValues(p.streamName, p.cameraName).Add(float64(len(pkt.Payload)))
			}
		}
	}
}
