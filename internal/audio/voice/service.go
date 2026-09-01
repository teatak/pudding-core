package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/audio/asr"
	"github.com/teatak/pudding-core/internal/audio/driver"
	"github.com/teatak/pudding-core/internal/audio/dsp/aec"
	"github.com/teatak/pudding-core/internal/audio/dsp/ns"
	"github.com/teatak/pudding-core/internal/audio/frame"
	"github.com/teatak/pudding-core/internal/audio/prompt"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

var (
	ErrNoInputBinding        = errors.New("voice: session does not own audio input")
	ErrInputUnavailable      = errors.New("voice: input backend unavailable")
	ErrInputRouteUnavailable = errors.New("voice: input route unavailable")
	ErrRawInputUnsupported   = errors.New("voice: current model does not support raw audio input")
	ErrEmptySentence         = errors.New("voice: empty sentence")
)

const inputLevelScale = 10000
const inputLevelEventInterval = 100 * time.Millisecond

type Submitter interface {
	Submit(ctx context.Context, in engine.SubmitInput) (*engine.SubmitResult, error)
}

type AudioInputCapability interface {
	AudioInputSupported(ctx context.Context, sessionID string) (bool, error)
}

type EventPublisher interface {
	Publish(event.Event)
}

type captureHealth interface {
	CaptureActive() bool
}

type ServiceConfig struct {
	Manager   *Manager
	Submitter Submitter
	Events    EventPublisher
	Driver    driver.Driver
	ASR       asr.Client
	AEC       aec.Processor
	NS        ns.Processor
	HomeDir   string
	SaveAudio bool
	MinEnergy float64
}

// Service is the daemon-owned voice orchestrator. It routes ASR text into the
// engine without owning session state.
type Service struct {
	manager         *Manager
	submitter       Submitter
	audioCapability AudioInputCapability
	publisher       EventPublisher
	driver          driver.Driver
	asr             asr.Client
	aec             aec.Processor
	ns              ns.Processor
	homeDir         string
	saveAudio       bool
	minEnergy       float64

	inputOpMu         sync.Mutex
	mu                sync.Mutex
	driverReady       bool
	asrStarted        bool
	inputSession      string
	inputStreamID     string
	inputCancel       context.CancelFunc
	asrLoopStarted    bool
	inputLevel        atomic.Int64
	inputLevelEventAt atomic.Int64
}

func NewService(cfg ServiceConfig) *Service {
	manager := cfg.Manager
	if manager == nil {
		manager = NewManager()
	}
	audioCapability, _ := cfg.Submitter.(AudioInputCapability)
	svc := &Service{
		manager:         manager,
		submitter:       cfg.Submitter,
		audioCapability: audioCapability,
		publisher:       cfg.Events,
		driver:          cfg.Driver,
		asr:             cfg.ASR,
		aec:             cfg.AEC,
		ns:              cfg.NS,
		homeDir:         strings.TrimSpace(cfg.HomeDir),
		saveAudio:       cfg.SaveAudio,
		minEnergy:       clamp01(cfg.MinEnergy),
	}
	return svc
}

func (s *Service) Snapshot() Bindings {
	bindings := s.manager.Snapshot()
	if bindings.InputOwner != "" {
		bindings.InputLevel = s.currentInputLevel()
	}
	return bindings
}

func (s *Service) withCurrentInputLevel(bindings Bindings) Bindings {
	if bindings.InputOwner != "" {
		bindings.InputLevel = s.currentInputLevel()
	} else {
		bindings.InputLevel = 0
	}
	return bindings
}

func (s *Service) publishAudioBindings(before, after Bindings) {
	if s.publisher == nil {
		return
	}
	for _, sessionID := range audioBindingEventSessionIDs(before, after) {
		level := after.InputLevel
		s.publisher.Publish(event.Event{
			SessionID:  sessionID,
			Kind:       event.AudioBindings,
			InputOwner: after.InputOwner,
			InputMode:  string(after.InputMode),
			InputLevel: &level,
		})
	}
}

func audioBindingEventSessionIDs(before, after Bindings) []string {
	seen := make(map[string]bool, 2)
	out := make([]string, 0, 2)
	for _, sessionID := range []string{before.InputOwner, after.InputOwner} {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" || seen[sessionID] {
			continue
		}
		seen[sessionID] = true
		out = append(out, sessionID)
	}
	return out
}

func (s *Service) BindInput(sessionID string, enabled bool, modes ...InputMode) (Bindings, error) {
	s.inputOpMu.Lock()
	defer s.inputOpMu.Unlock()

	mode := InputModeTranscribe
	if len(modes) > 0 {
		var err error
		mode, err = NormalizeInputMode(modes[0])
		if err != nil {
			return Bindings{}, err
		}
	}
	if enabled && mode == InputModeRaw && s.audioCapability != nil {
		supported, err := s.audioCapability.AudioInputSupported(context.Background(), sessionID)
		if err != nil {
			return Bindings{}, err
		}
		if !supported {
			return Bindings{}, ErrRawInputUnsupported
		}
	}
	before := s.manager.Snapshot()
	slog.Info("voice: input bind requested", "sessionID", sessionID, "enabled", enabled, "mode", mode, "previousOwner", before.InputOwner)
	if enabled {
		if before.InputOwner != sessionID || !s.inputCaptureActive(sessionID) {
			s.stopInput()
			if err := s.startInput(sessionID); err != nil {
				slog.Warn("voice: input start failed", "sessionID", sessionID, "err", err)
				return Bindings{}, err
			}
		}
		bindings, err := s.manager.BindInput(sessionID, true, mode)
		if err == nil {
			bindings = s.withCurrentInputLevel(bindings)
			s.publishAudioBindings(before, bindings)
			slog.Info("voice: input bound", "sessionID", sessionID, "inputOwner", bindings.InputOwner)
		}
		return bindings, err
	}
	bindings, err := s.manager.BindInput(sessionID, false)
	if err != nil {
		return Bindings{}, err
	}
	if before.InputOwner != bindings.InputOwner && before.InputOwner != "" {
		s.stopInput()
	}
	bindings = s.withCurrentInputLevel(bindings)
	s.publishAudioBindings(before, bindings)
	slog.Info("voice: input released", "sessionID", sessionID, "inputOwner", bindings.InputOwner)
	return bindings, nil
}

func (s *Service) ReplaceASR(next asr.Client) error {
	if next == nil {
		return fmt.Errorf("%w: ASR backend unavailable", ErrInputUnavailable)
	}
	s.inputOpMu.Lock()
	defer s.inputOpMu.Unlock()
	s.stopInput()
	s.mu.Lock()
	old := s.asr
	s.asr = next
	s.asrStarted = false
	s.asrLoopStarted = false
	s.mu.Unlock()
	if old != nil {
		_ = old.Stop(context.Background())
	}
	return nil
}

func (s *Service) inputCaptureActive(sessionID string) bool {
	s.mu.Lock()
	active := s.inputSession == sessionID && s.inputStreamID != "" && s.inputCancel != nil
	driver := s.driver
	s.mu.Unlock()
	if !active {
		return false
	}
	health, ok := driver.(captureHealth)
	if !ok {
		return true
	}
	return health.CaptureActive()
}

func (s *Service) ReleaseSession(sessionID string) Bindings {
	s.inputOpMu.Lock()
	defer s.inputOpMu.Unlock()

	before := s.manager.Snapshot()
	bindings := s.manager.ReleaseSession(sessionID)
	if before.InputOwner != bindings.InputOwner && before.InputOwner != "" {
		s.stopInput()
	}
	bindings = s.withCurrentInputLevel(bindings)
	s.publishAudioBindings(before, bindings)
	return bindings
}

func (s *Service) Close() error {
	s.inputOpMu.Lock()
	defer s.inputOpMu.Unlock()

	s.mu.Lock()
	inputCancel := s.inputCancel
	s.inputCancel = nil
	s.inputSession = ""
	s.inputStreamID = ""
	s.mu.Unlock()
	if inputCancel != nil {
		inputCancel()
	}
	s.setInputLevel(0)
	if s.driver != nil {
		_ = s.driver.StopCapture(context.Background())
	}
	var err error
	s.closeDSP()
	if s.driver != nil {
		_ = s.driver.Close()
	}
	if s.asr != nil {
		if asrErr := s.asr.Stop(context.Background()); err == nil {
			err = asrErr
		}
	}
	return err
}

func (s *Service) HandleASREvent(ctx context.Context, sessionID string, ev asr.Event) (*engine.SubmitResult, error) {
	switch ev.Kind {
	case asr.EventSentence:
		return s.submitSentence(ctx, sessionID, ev.Text, ev)
	case asr.EventError:
		if ev.Err != nil {
			return nil, ev.Err
		}
	}
	return nil, nil
}

func (s *Service) SubmitSentence(ctx context.Context, sessionID, text string) (*engine.SubmitResult, error) {
	return s.submitSentence(ctx, sessionID, text, asr.Event{})
}

func (s *Service) submitSentence(ctx context.Context, sessionID, text string, ev asr.Event) (*engine.SubmitResult, error) {
	if s.submitter == nil {
		return nil, errors.New("voice: submitter unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptySentence
	}
	bindings := s.manager.Snapshot()
	if bindings.InputOwner != sessionID {
		return nil, ErrNoInputBinding
	}
	rawInput := bindings.InputMode == InputModeRaw
	if rawInput && s.audioCapability != nil {
		supported, err := s.audioCapability.AudioInputSupported(ctx, sessionID)
		if err != nil {
			slog.Warn("voice: raw audio capability check failed; falling back to transcript", "sessionID", sessionID, "err", err)
			rawInput = false
			s.fallbackInputMode(sessionID)
		} else if !supported {
			slog.Info("voice: raw audio no longer supported; falling back to transcript", "sessionID", sessionID)
			rawInput = false
			s.fallbackInputMode(sessionID)
		}
	}
	clientMessageID := store.NewID("audmsg")
	parts := s.asrAudioParts(sessionID, clientMessageID, text, ev)
	if rawInput {
		clientMessageID = store.NewID("voicemsg")
		parts = s.voiceAudioParts(sessionID, clientMessageID, text, ev)
		if len(parts) == 0 {
			slog.Warn("voice: raw audio unavailable; falling back to transcript", "sessionID", sessionID, "clientMessageID", clientMessageID)
			clientMessageID = store.NewID("audmsg")
			parts = s.asrAudioParts(sessionID, clientMessageID, text, ev)
			rawInput = false
			s.fallbackInputMode(sessionID)
		}
	}
	inputMode := InputModeTranscribe
	if rawInput {
		inputMode = InputModeRaw
	}
	slog.Info("voice: submitting sentence", "sessionID", sessionID, "clientMessageID", clientMessageID, "inputMode", inputMode, "textLength", len([]rune(text)))
	result, err := s.submitter.Submit(ctx, engine.SubmitInput{
		SessionID:       sessionID,
		ClientMessageID: clientMessageID,
		Text:            text,
		Parts:           parts,
	})
	if err != nil {
		slog.Warn("voice: submit asr sentence failed", "sessionID", sessionID, "clientMessageID", clientMessageID, "err", err)
		return nil, err
	}
	if result != nil {
		slog.Info(
			"voice: submit asr sentence accepted",
			"sessionID", sessionID,
			"clientMessageID", clientMessageID,
			"turnID", result.TurnID,
			"queued", result.Queued,
			"duplicate", result.Duplicate,
			"status", result.Status,
		)
	}
	return result, nil
}

func (s *Service) fallbackInputMode(sessionID string) {
	before := s.manager.Snapshot()
	bindings, err := s.manager.BindInput(sessionID, true, InputModeTranscribe)
	if err != nil {
		return
	}
	bindings = s.withCurrentInputLevel(bindings)
	s.publishAudioBindings(before, bindings)
}

func (s *Service) asrAudioParts(sessionID, clientMessageID, text string, ev asr.Event) []store.ContentPart {
	if !s.saveAudio {
		return nil
	}
	return s.storeSentenceAudio(sessionID, clientMessageID, text, ev, attachment.OriginASRAudio, "asr")
}

func (s *Service) voiceAudioParts(sessionID, clientMessageID, text string, ev asr.Event) []store.ContentPart {
	return s.storeSentenceAudio(sessionID, clientMessageID, text, ev, attachment.OriginVoiceAudio, "voice")
}

func (s *Service) storeSentenceAudio(sessionID, clientMessageID, text string, ev asr.Event, origin, namePrefix string) []store.ContentPart {
	if strings.TrimSpace(s.homeDir) == "" || len(ev.Audio.Data) == 0 || !ev.Audio.Format.Valid() {
		return nil
	}
	wav, err := wavFromPCM16(ev.Audio)
	if err != nil {
		slog.Warn("voice: encode sentence audio failed", "sessionID", sessionID, "clientMessageID", clientMessageID, "err", err)
		return nil
	}
	name := namePrefix + "-" + time.Now().Format("20060102-150405") + ".wav"
	item, err := attachment.NewService(s.homeDir).StoreReader(sessionID, name, "audio/wav", bytes.NewReader(wav))
	if err != nil {
		slog.Warn("voice: store sentence audio failed", "sessionID", sessionID, "clientMessageID", clientMessageID, "err", err)
		return nil
	}
	item.Origin = origin
	item.AudioTranscript = text
	return []store.ContentPart{store.AttachmentPart(item)}
}

func wavFromPCM16(pcm frame.PCM16) ([]byte, error) {
	if !pcm.Format.Valid() {
		return nil, errors.New("invalid pcm format")
	}
	data := pcm.Data
	if len(data) == 0 {
		return nil, errors.New("empty pcm data")
	}
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	if len(data) == 0 {
		return nil, errors.New("empty pcm data")
	}
	channels := pcm.Format.Channels
	sampleRate := pcm.Format.SampleRate
	blockAlign := channels * 2
	byteRate := sampleRate * blockAlign
	if channels <= 0 || sampleRate <= 0 || blockAlign <= 0 || byteRate <= 0 {
		return nil, fmt.Errorf("invalid pcm format %dHz %dch", sampleRate, channels)
	}
	var buf bytes.Buffer
	buf.Grow(44 + len(data))
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes(), nil
}

func (s *Service) startInput(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ErrSessionRequired
	}
	if s.driver == nil && s.asr == nil {
		return fmt.Errorf("%w: audio driver and ASR backend unavailable", ErrInputUnavailable)
	}
	if s.driver == nil {
		return fmt.Errorf("%w: audio driver unavailable", ErrInputUnavailable)
	}
	if s.asr == nil {
		return fmt.Errorf("%w: ASR backend unavailable", ErrInputUnavailable)
	}
	s.mu.Lock()
	if !s.asrStarted {
		slog.Info("voice: starting asr", "sessionID", sessionID, "asr", s.asr.Name())
		if err := s.asr.Start(context.Background()); err != nil {
			s.mu.Unlock()
			return err
		}
		s.asrStarted = true
	}
	if !s.driverReady {
		slog.Info("voice: initializing audio driver", "sessionID", sessionID, "driver", s.driver.Name())
		if err := s.driver.Init(context.Background()); err != nil {
			s.mu.Unlock()
			return err
		}
		s.driverReady = true
	}
	if !s.asrLoopStarted {
		s.asrLoopStarted = true
		go s.asrEventLoop(s.asr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	streamID := store.NewID("audstream")
	s.inputSession = sessionID
	s.inputStreamID = streamID
	s.inputCancel = cancel
	client := s.asr
	s.mu.Unlock()

	s.setInputLevel(0)
	if err := resetASRStream(ctx, client, streamID); err != nil {
		cancel()
		s.clearInputStream(streamID)
		return err
	}
	s.resetCaptureDSP()
	s.primeInputRoute(ctx, sessionID)
	if err := s.driver.StartCapture(ctx, func(pcm frame.PCM16) {
		if !s.inputStreamActive(sessionID, streamID) {
			return
		}
		processed, err := s.processCapture(pcm)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: capture dsp failed", "sessionID", sessionID, "err", err)
			processed = pcm
		}
		level := pcmRMSLevel(processed)
		s.setInputLevel(level)
		if !s.inputStreamActive(sessionID, streamID) {
			return
		}
		if err := client.Feed(ctx, processed); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: asr feed failed", "sessionID", sessionID, "err", err)
		}
	}); err != nil {
		s.stopInputRoutePlayback()
		cancel()
		s.clearInputStream(streamID)
		s.setInputLevel(0)
		if stopErr := s.driver.StopCapture(context.Background()); stopErr != nil && !errors.Is(stopErr, context.Canceled) {
			slog.Warn("voice: cleanup failed capture after start error", "sessionID", sessionID, "err", stopErr)
		}
		s.resetCaptureDSP()
		if resetErr := resetASRStream(context.Background(), client, ""); resetErr != nil {
			slog.Warn("voice: reset asr after capture start error failed", "sessionID", sessionID, "err", resetErr)
		}
		if errors.Is(err, driver.ErrCaptureNoSignal) {
			return fmt.Errorf("%w: %v", ErrInputRouteUnavailable, err)
		}
		return err
	}
	slog.Info("voice: input capture started", "sessionID", sessionID, "streamID", streamID, "driver", s.driver.Name(), "asr", client.Name())
	return nil
}

func (s *Service) primeInputRoute(ctx context.Context, sessionID string) {
	primer, ok := s.driver.(driver.InputRoutePrimer)
	if !ok {
		return
	}
	pcm, err := prompt.Start(s.driver.OutputFormat())
	if err != nil {
		slog.Warn("voice: prepare input route prompt failed", "sessionID", sessionID, "err", err)
		return
	}
	if err := primer.PrimeInputRoute(ctx, pcm); err != nil {
		slog.Warn("voice: prime input route failed", "sessionID", sessionID, "err", err)
		return
	}
	slog.Info("voice: input route primed", "sessionID", sessionID, "duration", pcm.Duration())
}

func (s *Service) stopInputRoutePlayback() {
	if err := s.driver.StopPlayback(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("voice: stop input route playback failed", "err", err)
	}
}

func (s *Service) stopInput() {
	s.mu.Lock()
	cancel := s.inputCancel
	streamID := s.inputStreamID
	client := s.asr
	s.inputCancel = nil
	s.inputSession = ""
	s.inputStreamID = ""
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.setInputLevel(0)
	if s.driver != nil {
		if cancel != nil || streamID != "" {
			s.stopInputRoutePlayback()
		}
		if err := s.driver.StopCapture(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: stop input capture failed", "streamID", streamID, "err", err)
		}
	}
	if streamID != "" {
		s.resetCaptureDSP()
		if err := resetASRStream(context.Background(), client, ""); err != nil {
			slog.Warn("voice: reset asr after capture stop failed", "streamID", streamID, "err", err)
		}
	}
	if cancel != nil {
		slog.Info("voice: input capture stopped", "streamID", streamID)
	}
}

func (s *Service) asrEventLoop(client asr.Client) {
	for ev := range client.Events() {
		owner, currentStreamID, ok := s.inputForASREvent(ev.StreamID)
		if !ok {
			if ev.StreamID != "" {
				slog.Debug("voice: stale asr event dropped", "eventStreamID", ev.StreamID, "currentStreamID", currentStreamID)
			}
			continue
		}
		if ev.Kind == asr.EventSentence {
			peakLevel := pcmPeakFrameRMSLevel(ev.Audio, 20*time.Millisecond)
			threshold := s.captureEnergyThreshold()
			if len(ev.Audio.Data) > 0 && threshold > 0 && peakLevel < threshold {
				slog.Info(
					"voice: low-energy asr sentence suppressed",
					"sessionID", owner,
					"streamID", currentStreamID,
					"peakLevel", peakLevel,
					"threshold", threshold,
				)
				continue
			}
			slog.Info(
				"voice: asr sentence received",
				"sessionID", owner,
				"streamID", currentStreamID,
				"textLength", len([]rune(ev.Text)),
				"language", ev.Language,
				"audioDuration", ev.AudioDuration,
				"decodeDuration", ev.DecodeDuration,
			)
		}
		if _, err := s.HandleASREvent(context.Background(), owner, ev); err != nil && !errors.Is(err, ErrEmptySentence) && !errors.Is(err, ErrNoInputBinding) {
			slog.Warn("voice: handle asr event failed", "sessionID", owner, "err", err)
		}
	}
}

func (s *Service) inputStreamActive(sessionID, streamID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return streamID != "" && s.inputSession == sessionID && s.inputStreamID == streamID && s.inputCancel != nil
}

func (s *Service) clearInputStream(streamID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inputStreamID != streamID {
		return
	}
	s.inputCancel = nil
	s.inputSession = ""
	s.inputStreamID = ""
}

func (s *Service) inputForASREvent(eventStreamID string) (string, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inputSession == "" || s.inputStreamID == "" || s.inputCancel == nil {
		return "", s.inputStreamID, false
	}
	if eventStreamID != "" && eventStreamID != s.inputStreamID {
		return "", s.inputStreamID, false
	}
	return s.inputSession, s.inputStreamID, true
}

func resetASRStream(ctx context.Context, client asr.Client, streamID string) error {
	resetter, ok := client.(asr.StreamResetter)
	if !ok {
		return nil
	}
	return resetter.ResetStream(ctx, streamID)
}

func (s *Service) processCapture(pcm frame.PCM16) (frame.PCM16, error) {
	var err error
	out := pcm
	if s.aec != nil {
		out, err = s.aec.ProcessCapture(out)
		if err != nil {
			return out, err
		}
	}
	if s.ns != nil {
		out, err = s.ns.Process(out)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Service) resetCaptureDSP() {
	// Reset only capture-local NS after PortAudio has stopped invoking callbacks.
	if s.ns != nil {
		s.ns.Reset()
	}
}

func (s *Service) closeDSP() {
	if closer, ok := s.aec.(aec.Closer); ok {
		_ = closer.Close()
	}
	if closer, ok := s.ns.(ns.Closer); ok {
		_ = closer.Close()
	}
}

func (s *Service) setInputLevel(level float64) {
	if level < 0 || math.IsNaN(level) || math.IsInf(level, 0) {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	s.inputLevel.Store(int64(level * inputLevelScale))
	s.publishInputLevel(level)
}

func (s *Service) currentInputLevel() float64 {
	return float64(s.inputLevel.Load()) / inputLevelScale
}

func (s *Service) captureEnergyThreshold() float64 {
	return s.minEnergy
}

func (s *Service) publishInputLevel(level float64) {
	if s.publisher == nil {
		return
	}
	now := time.Now().UnixNano()
	interval := int64(inputLevelEventInterval)
	for {
		last := s.inputLevelEventAt.Load()
		if now-last < interval {
			return
		}
		if s.inputLevelEventAt.CompareAndSwap(last, now) {
			break
		}
	}
	owner := s.manager.Snapshot().InputOwner
	if owner == "" {
		return
	}
	s.publisher.Publish(event.Event{
		SessionID:  owner,
		Kind:       event.AudioInputLevel,
		InputLevel: &level,
	})
}

func pcmRMSLevel(pcm frame.PCM16) float64 {
	if len(pcm.Data) < 2 {
		return 0
	}
	samples := len(pcm.Data) / 2
	var sumSquares float64
	for i := 0; i < samples; i++ {
		sample := int16(uint16(pcm.Data[i*2]) | uint16(pcm.Data[i*2+1])<<8)
		value := float64(sample) / 32768.0
		sumSquares += value * value
	}
	return math.Sqrt(sumSquares / float64(samples))
}

func pcmPeakFrameRMSLevel(pcm frame.PCM16, window time.Duration) float64 {
	if len(pcm.Data) < 2 || !pcm.Format.Valid() || window <= 0 {
		return 0
	}
	samplesPerWindow := pcm.Format.SampleRate * pcm.Format.Channels * int(window) / int(time.Second)
	if samplesPerWindow <= 0 {
		return pcmRMSLevel(pcm)
	}
	bytesPerWindow := samplesPerWindow * 2
	peak := 0.0
	for offset := 0; offset < len(pcm.Data); offset += bytesPerWindow {
		end := offset + bytesPerWindow
		if end > len(pcm.Data) {
			end = len(pcm.Data)
		}
		part := pcm
		part.Data = pcm.Data[offset:end]
		if level := pcmRMSLevel(part); level > peak {
			peak = level
		}
	}
	return peak
}

func clamp01(value float64) float64 {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
