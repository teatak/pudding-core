// Package sherpa implements ASR with sherpa-onnx SenseVoice + Silero VAD.
package sherpa

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sherpaonnx "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/teatak/pudding-core/internal/audio/asr"
	"github.com/teatak/pudding-core/internal/audio/frame"
)

const (
	expectedSampleRate = 16000
	expectedChannels   = 1

	defaultFeatureDim    = 80
	defaultVADBufferSecs = 30.0
	defaultSegQueueSize  = 8
	defaultEventBuf      = 16
)

type Config struct {
	ModelPath    string
	TokensPath   string
	VADModelPath string

	Language                    string
	UseInverseTextNormalization bool

	VADThreshold       float64
	MinSilenceDuration time.Duration
	MinSpeechDuration  time.Duration
	VADWindowSize      int
	PrerollDuration    time.Duration

	NumThreads int
	Provider   string
}

type Client struct {
	cfg Config

	vad   *sherpaonnx.VoiceActivityDetector
	recog *sherpaonnx.OfflineRecognizer

	vadMu    sync.Mutex
	decodeMu sync.Mutex

	events chan asr.Event
	segs   chan []float32

	history        *sampleRing
	prerollSamples int
	vadSpeech      bool
	pendingPreroll []float32

	started atomic.Bool
	stopped atomic.Bool

	wg     sync.WaitGroup
	stopCh chan struct{}

	droppedSegs atomic.Uint64
}

func New(cfg Config) (*Client, error) {
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	prerollSamples := durationSamples(cfg.PrerollDuration)
	historySamples := int(defaultVADBufferSecs*expectedSampleRate) + expectedSampleRate + prerollSamples
	return &Client{
		cfg:            cfg,
		events:         make(chan asr.Event, defaultEventBuf),
		segs:           make(chan []float32, defaultSegQueueSize),
		history:        newSampleRing(historySamples),
		prerollSamples: prerollSamples,
		stopCh:         make(chan struct{}),
	}, nil
}

func (c *Client) Name() string { return "sherpa-sensevoice" }

func (c *Client) Start(context.Context) error {
	if c.stopped.Load() {
		return errors.New("sherpa asr: client already stopped")
	}
	if !c.started.CompareAndSwap(false, true) {
		return nil
	}
	vad := sherpaonnx.NewVoiceActivityDetector(&sherpaonnx.VadModelConfig{
		SileroVad: sherpaonnx.SileroVadModelConfig{
			Model:              c.cfg.VADModelPath,
			Threshold:          float32(c.cfg.VADThreshold),
			MinSilenceDuration: float32(c.cfg.MinSilenceDuration.Seconds()),
			MinSpeechDuration:  float32(c.cfg.MinSpeechDuration.Seconds()),
			WindowSize:         c.cfg.VADWindowSize,
		},
		SampleRate: expectedSampleRate,
		NumThreads: 1,
	}, float32(defaultVADBufferSecs))
	if vad == nil {
		c.started.Store(false)
		return fmt.Errorf("sherpa asr: failed to load silero VAD at %s", c.cfg.VADModelPath)
	}
	recog := sherpaonnx.NewOfflineRecognizer(&sherpaonnx.OfflineRecognizerConfig{
		FeatConfig: sherpaonnx.FeatureConfig{
			SampleRate: expectedSampleRate,
			FeatureDim: defaultFeatureDim,
		},
		ModelConfig: sherpaonnx.OfflineModelConfig{
			SenseVoice: sherpaonnx.OfflineSenseVoiceModelConfig{
				Model:                       c.cfg.ModelPath,
				Language:                    c.cfg.Language,
				UseInverseTextNormalization: boolToInt(c.cfg.UseInverseTextNormalization),
			},
			Tokens:     c.cfg.TokensPath,
			NumThreads: c.cfg.NumThreads,
			Provider:   c.cfg.Provider,
			Debug:      0,
		},
		DecodingMethod: "greedy_search",
	})
	if recog == nil {
		sherpaonnx.DeleteVoiceActivityDetector(vad)
		c.started.Store(false)
		return fmt.Errorf("sherpa asr: failed to load SenseVoice at %s", c.cfg.ModelPath)
	}
	c.vad = vad
	c.recog = recog
	c.wg.Add(1)
	go c.decodeLoop()
	slog.Info(
		"sherpa asr: started",
		"model", c.cfg.ModelPath,
		"vad", c.cfg.VADModelPath,
		"language", c.cfg.Language,
		"provider", c.cfg.Provider,
		"vadThreshold", c.cfg.VADThreshold,
		"minSilence", c.cfg.MinSilenceDuration,
		"preroll", c.cfg.PrerollDuration,
	)
	return nil
}

func (c *Client) Feed(_ context.Context, f frame.PCM16) error {
	if !c.started.Load() || c.stopped.Load() {
		return errors.New("sherpa asr: not started")
	}
	if f.Format.SampleRate != expectedSampleRate || f.Format.Channels != expectedChannels {
		return fmt.Errorf("sherpa asr: unsupported format %dHz %dch, need %dHz mono",
			f.Format.SampleRate, f.Format.Channels, expectedSampleRate)
	}
	if len(f.Data) == 0 {
		return nil
	}
	samples := pcm16BytesToFloat32(f.Data)
	c.vadMu.Lock()
	c.vad.AcceptWaveform(samples)
	speechNow := c.vad.IsSpeech()
	if !c.vadSpeech && speechNow && c.prerollSamples > 0 && c.history != nil {
		c.pendingPreroll = c.history.Tail(c.prerollSamples)
	}
	c.vadSpeech = speechNow
	c.history.Append(samples)
	for !c.vad.IsEmpty() {
		seg := c.vad.Front()
		c.vad.Pop()
		if seg == nil || len(seg.Samples) == 0 {
			continue
		}
		samples := c.samplesWithPreroll(seg)
		select {
		case c.segs <- samples:
			slog.Debug("sherpa asr: vad segment queued", "samples", len(samples), "prerollSamples", len(samples)-len(seg.Samples), "segmentStart", seg.Start)
		default:
			dropped := c.droppedSegs.Add(1)
			slog.Warn("sherpa asr: vad segment dropped", "samples", len(samples), "dropped", dropped)
		}
		c.pendingPreroll = nil
	}
	c.vadMu.Unlock()
	return nil
}

func (c *Client) samplesWithPreroll(seg *sherpaonnx.SpeechSegment) []float32 {
	if c.prerollSamples <= 0 || seg == nil || len(seg.Samples) == 0 {
		return seg.Samples
	}
	preroll := c.history.Slice(int64(seg.Start-c.prerollSamples), int64(seg.Start))
	if len(preroll) == 0 && len(c.pendingPreroll) > 0 {
		preroll = append([]float32(nil), c.pendingPreroll...)
	}
	if len(preroll) == 0 {
		return seg.Samples
	}
	out := make([]float32, 0, len(preroll)+len(seg.Samples))
	out = append(out, preroll...)
	out = append(out, seg.Samples...)
	return out
}

func (c *Client) Events() <-chan asr.Event { return c.events }

func (c *Client) Stop(context.Context) error {
	if !c.stopped.CompareAndSwap(false, true) {
		return nil
	}
	if !c.started.Load() {
		close(c.events)
		return nil
	}
	close(c.stopCh)
	c.wg.Wait()

	c.vadMu.Lock()
	if c.vad != nil {
		sherpaonnx.DeleteVoiceActivityDetector(c.vad)
		c.vad = nil
	}
	c.decodeMu.Lock()
	if c.recog != nil {
		sherpaonnx.DeleteOfflineRecognizer(c.recog)
		c.recog = nil
	}
	c.decodeMu.Unlock()
	c.vadMu.Unlock()
	close(c.events)
	return nil
}

func (c *Client) DroppedSegments() uint64 { return c.droppedSegs.Load() }

func (c *Client) decodeLoop() {
	defer c.wg.Done()
	for {
		select {
		case <-c.stopCh:
			return
		case samples := <-c.segs:
			c.decodeSegment(samples)
		}
	}
}

func (c *Client) decodeSegment(samples []float32) {
	start := time.Now()
	result := c.decodeSamples(samples)
	elapsed := time.Since(start)
	if result == nil {
		return
	}
	audioDur := time.Duration(float64(len(samples)) / float64(expectedSampleRate) * float64(time.Second))
	ev := asr.Event{
		Kind:           asr.EventSentence,
		Text:           strings.TrimSpace(result.Text),
		Language:       stripSenseVoiceTag(result.Lang),
		Emotion:        stripSenseVoiceTag(result.Emotion),
		Audio:          samplesToPCM16(samples),
		AudioDuration:  audioDur,
		DecodeDuration: elapsed,
	}
	if ev.Text == "" {
		slog.Debug("sherpa asr: decoded empty segment", "audioDuration", audioDur, "decodeDuration", elapsed)
		return
	}
	slog.Info(
		"sherpa asr: sentence decoded",
		"text", previewText(ev.Text, 80),
		"language", ev.Language,
		"audioDuration", audioDur,
		"decodeDuration", elapsed,
	)
	select {
	case c.events <- ev:
	case <-c.stopCh:
	}
}

func samplesToPCM16(samples []float32) frame.PCM16 {
	if len(samples) == 0 {
		return frame.PCM16{}
	}
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(floatSampleToInt16(sample)))
	}
	return frame.PCM16{
		Format: frame.Format{
			SampleRate: expectedSampleRate,
			Channels:   expectedChannels,
		},
		Data: data,
	}
}

func floatSampleToInt16(sample float32) int16 {
	if sample >= 1 {
		return 32767
	}
	if sample <= -1 {
		return -32768
	}
	if sample < 0 {
		return int16(sample * 32768)
	}
	return int16(sample * 32767)
}

func (c *Client) decodeSamples(samples []float32) *sherpaonnx.OfflineRecognizerResult {
	c.decodeMu.Lock()
	defer c.decodeMu.Unlock()
	if c.recog == nil {
		return nil
	}
	stream := sherpaonnx.NewOfflineStream(c.recog)
	defer sherpaonnx.DeleteOfflineStream(stream)
	stream.AcceptWaveform(expectedSampleRate, samples)
	c.recog.Decode(stream)
	return stream.GetResult()
}

func validateConfig(cfg *Config) error {
	for label, path := range map[string]string{
		"ModelPath":    cfg.ModelPath,
		"TokensPath":   cfg.TokensPath,
		"VADModelPath": cfg.VADModelPath,
	} {
		if path == "" {
			return fmt.Errorf("sherpa asr: %s is required", label)
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("sherpa asr: stat %s=%s: %w", label, path, err)
		}
	}
	if cfg.Language == "" {
		cfg.Language = "auto"
	}
	if cfg.VADThreshold <= 0 {
		cfg.VADThreshold = 0.5
	}
	if cfg.MinSilenceDuration <= 0 {
		cfg.MinSilenceDuration = 400 * time.Millisecond
	}
	if cfg.MinSpeechDuration <= 0 {
		cfg.MinSpeechDuration = 250 * time.Millisecond
	}
	if cfg.VADWindowSize <= 0 {
		cfg.VADWindowSize = 512
	}
	if cfg.PrerollDuration < 0 {
		return errors.New("sherpa asr: preroll duration must not be negative")
	}
	if cfg.NumThreads <= 0 {
		cfg.NumThreads = 2
	}
	if cfg.Provider == "" {
		cfg.Provider = "cpu"
	}
	return nil
}

func durationSamples(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d.Seconds() * expectedSampleRate)
}

type sampleRing struct {
	data   []float32
	start  int
	length int
	base   int64
	next   int64
}

func newSampleRing(capacity int) *sampleRing {
	if capacity <= 0 {
		return nil
	}
	return &sampleRing{data: make([]float32, capacity)}
}

func (r *sampleRing) Append(samples []float32) {
	if r == nil || len(r.data) == 0 || len(samples) == 0 {
		return
	}
	for _, sample := range samples {
		if r.length < len(r.data) {
			r.data[(r.start+r.length)%len(r.data)] = sample
			r.length++
		} else {
			r.data[r.start] = sample
			r.start = (r.start + 1) % len(r.data)
			r.base++
		}
		r.next++
	}
}

func (r *sampleRing) Tail(maxSamples int) []float32 {
	if r == nil || maxSamples <= 0 {
		return nil
	}
	end := r.next
	start := end - int64(maxSamples)
	return r.Slice(start, end)
}

func (r *sampleRing) Slice(start, end int64) []float32 {
	if r == nil || len(r.data) == 0 || r.length == 0 || end <= start {
		return nil
	}
	if start < r.base {
		start = r.base
	}
	if end > r.next {
		end = r.next
	}
	if end <= start {
		return nil
	}
	n := int(end - start)
	out := make([]float32, n)
	offset := int(start - r.base)
	for i := range out {
		out[i] = r.data[(r.start+offset+i)%len(r.data)]
	}
	return out
}

func stripSenseVoiceTag(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<|") && strings.HasSuffix(s, "|>") && len(s) > 4 {
		return s[2 : len(s)-2]
	}
	return s
}

func pcm16BytesToFloat32(data []byte) []float32 {
	n := len(data) / 2
	out := make([]float32, n)
	for i := range out {
		v := int16(uint16(data[i*2]) | uint16(data[i*2+1])<<8)
		out[i] = float32(v) / 32768.0
	}
	return out
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func previewText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}
