package ns

import (
	"errors"
	"fmt"
	"sync"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

var errBridgeNotLinked = errors.New("webrtc ns native bridge not linked")

const webrtcBlockMillis = 10

type WebRTCNSConfig struct {
	SampleRate   int
	Channels     int
	PeriodMillis int
	Level        string
}

type WebRTCNS struct {
	cfg          WebRTCNSConfig
	bridge       webrtcNSBridge
	level        string
	blockSamples int

	mu      sync.Mutex
	scratch []int16
}

func NewWebRTCNS(cfg WebRTCNSConfig) (*WebRTCNS, error) {
	if cfg.SampleRate <= 0 {
		return nil, fmt.Errorf("webrtc ns sample rate must be positive")
	}
	if cfg.Channels != 1 {
		return nil, fmt.Errorf("webrtc ns currently only supports mono input")
	}
	if cfg.PeriodMillis <= 0 {
		return nil, fmt.Errorf("webrtc ns period millis must be positive")
	}
	if cfg.PeriodMillis%webrtcBlockMillis != 0 {
		return nil, fmt.Errorf("webrtc ns requires period millis to be divisible by %d", webrtcBlockMillis)
	}
	blockSamples := cfg.SampleRate * webrtcBlockMillis / 1000 * cfg.Channels
	if blockSamples <= 0 {
		return nil, fmt.Errorf("webrtc ns computed empty processing block")
	}
	level := NormalizeLevel(cfg.Level)
	bridge, err := newWebRTCNSBridge(cfg.SampleRate, cfg.Channels, level)
	if err != nil {
		return nil, err
	}
	return &WebRTCNS{
		cfg:          cfg,
		bridge:       bridge,
		level:        level,
		blockSamples: blockSamples,
		scratch:      make([]int16, 0, blockSamples*16),
	}, nil
}

func (w *WebRTCNS) Name() string {
	if w.bridge == nil {
		return "webrtc(unlinked)"
	}
	return "webrtc"
}

func (w *WebRTCNS) BridgeLinked() bool { return w.bridge != nil }

func (w *WebRTCNS) Level() string { return w.level }

func (w *WebRTCNS) Process(f frame.PCM16) (frame.PCM16, error) {
	if len(f.Data) == 0 {
		return f, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	samples := pcm16BytesToInt16(f.Data, &w.scratch)
	if len(samples)%w.blockSamples != 0 {
		return f, fmt.Errorf("webrtc ns frame has %d samples, expected multiple of %d", len(samples), w.blockSamples)
	}
	if w.bridge == nil || w.level == LevelOff {
		return f, nil
	}
	for offset := 0; offset < len(samples); offset += w.blockSamples {
		block := samples[offset : offset+w.blockSamples]
		if err := w.bridge.Process(block, block); err != nil {
			return f, err
		}
	}
	out := make([]byte, len(f.Data))
	int16ToPCM16Bytes(samples, out)
	return frame.PCM16{Format: f.Format, Data: out, Timestamp: f.Timestamp}, nil
}

func (w *WebRTCNS) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.bridge != nil {
		w.bridge.Reset()
	}
}

func (w *WebRTCNS) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.bridge == nil {
		return nil
	}
	return w.bridge.Close()
}

type webrtcNSBridge interface {
	Process(in []int16, out []int16) error
	SetLevel(level string) error
	Reset()
	Close() error
}

func pcm16BytesToInt16(data []byte, scratch *[]int16) []int16 {
	n := len(data) / 2
	if cap(*scratch) < n {
		*scratch = make([]int16, n)
	} else {
		*scratch = (*scratch)[:n]
	}
	out := *scratch
	for i := 0; i < n; i++ {
		out[i] = int16(uint16(data[i*2]) | uint16(data[i*2+1])<<8)
	}
	return out
}

func int16ToPCM16Bytes(samples []int16, out []byte) {
	for i, s := range samples {
		out[i*2] = byte(s)
		out[i*2+1] = byte(s >> 8)
	}
}
