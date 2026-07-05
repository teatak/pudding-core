package aec

import (
	"errors"
	"fmt"
	"sync"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

var errBridgeNotLinked = errors.New("webrtc aec native bridge not linked")

const webrtcBlockMillis = 10

type WebRTCAECConfig struct {
	SampleRate   int
	Channels     int
	PeriodMillis int
}

type WebRTCAEC struct {
	cfg          WebRTCAECConfig
	bridge       webrtcAECBridge
	blockSamples int

	mu            sync.Mutex
	scratch       []int16
	renderPending []int16
}

func NewWebRTCAEC(cfg WebRTCAECConfig) (*WebRTCAEC, error) {
	if cfg.SampleRate <= 0 {
		return nil, fmt.Errorf("webrtc aec sample rate must be positive")
	}
	if cfg.Channels != 1 {
		return nil, fmt.Errorf("webrtc aec currently only supports mono input")
	}
	if cfg.PeriodMillis <= 0 {
		return nil, fmt.Errorf("webrtc aec period millis must be positive")
	}
	if cfg.PeriodMillis%webrtcBlockMillis != 0 {
		return nil, fmt.Errorf("webrtc aec requires period millis to be divisible by %d", webrtcBlockMillis)
	}
	blockSamples := cfg.SampleRate * webrtcBlockMillis / 1000 * cfg.Channels
	if blockSamples <= 0 {
		return nil, fmt.Errorf("webrtc aec computed empty processing block")
	}
	bridge, err := newWebRTCAECBridge(cfg)
	if err != nil {
		return nil, err
	}
	return &WebRTCAEC{
		cfg:           cfg,
		bridge:        bridge,
		blockSamples:  blockSamples,
		scratch:       make([]int16, 0, blockSamples*16),
		renderPending: make([]int16, 0, blockSamples*4),
	}, nil
}

func (w *WebRTCAEC) Name() string {
	if w.bridge == nil {
		return "webrtc(unlinked)"
	}
	return "webrtc"
}

func (w *WebRTCAEC) BridgeLinked() bool { return w.bridge != nil }

func (w *WebRTCAEC) PushRender(f frame.PCM16) error {
	if len(f.Data) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	samples := pcm16BytesToInt16(f.Data, &w.scratch)
	w.renderPending = append(w.renderPending, samples...)
	if w.bridge == nil {
		w.renderPending = w.renderPending[:0]
		return nil
	}
	full := (len(w.renderPending) / w.blockSamples) * w.blockSamples
	for offset := 0; offset < full; offset += w.blockSamples {
		if err := w.bridge.AnalyzeRender(w.renderPending[offset : offset+w.blockSamples]); err != nil {
			return err
		}
	}
	if full > 0 {
		remain := copy(w.renderPending, w.renderPending[full:])
		w.renderPending = w.renderPending[:remain]
	}
	return nil
}

func (w *WebRTCAEC) ProcessCapture(f frame.PCM16) (frame.PCM16, error) {
	if len(f.Data) == 0 {
		return f, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	samples := pcm16BytesToInt16(f.Data, &w.scratch)
	if len(samples)%w.blockSamples != 0 {
		return f, fmt.Errorf("webrtc aec capture frame has %d samples, expected multiple of %d", len(samples), w.blockSamples)
	}
	if w.bridge == nil {
		return f, nil
	}
	for offset := 0; offset < len(samples); offset += w.blockSamples {
		block := samples[offset : offset+w.blockSamples]
		if err := w.bridge.ProcessCapture(block, block); err != nil {
			return f, err
		}
	}
	out := make([]byte, len(f.Data))
	int16ToPCM16Bytes(samples, out)
	return frame.PCM16{Format: f.Format, Data: out, Timestamp: f.Timestamp}, nil
}

func (w *WebRTCAEC) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.renderPending = w.renderPending[:0]
	if w.bridge != nil {
		w.bridge.Reset()
	}
}

func (w *WebRTCAEC) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.bridge == nil {
		return nil
	}
	return w.bridge.Close()
}

type webrtcAECBridge interface {
	AnalyzeRender(frame []int16) error
	ProcessCapture(in []int16, out []int16) error
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
