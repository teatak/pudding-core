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
	"github.com/teatak/pudding-core/internal/audio/dsp/resample"
	"github.com/teatak/pudding-core/internal/audio/frame"
	"github.com/teatak/pudding-core/internal/audio/prompt"
	audioqueue "github.com/teatak/pudding-core/internal/audio/queue"
	"github.com/teatak/pudding-core/internal/audio/tts"
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

const feedbackSuppressGrace = 1500 * time.Millisecond
const inputLevelScale = 10000
const inputLevelEventInterval = 100 * time.Millisecond

type Submitter interface {
	Submit(ctx context.Context, in engine.SubmitInput) (*engine.SubmitResult, error)
}

type AudioInputCapability interface {
	AudioInputSupported(ctx context.Context, sessionID string) (bool, error)
}

type Canceler interface {
	Cancel(sessionID string) error
}

type EventSubscriber interface {
	Subscribe(sessionID string) (<-chan event.Event, func())
}

type EventPublisher interface {
	Publish(event.Event)
}

type EventBus interface {
	EventSubscriber
	EventPublisher
}

type captureHealth interface {
	CaptureActive() bool
}

type ServiceConfig struct {
	Manager           *Manager
	Submitter         Submitter
	Canceler          Canceler
	Events            EventBus
	Driver            driver.Driver
	ASR               asr.Client
	AEC               aec.Processor
	NS                ns.Processor
	TTS               tts.Client
	HomeDir           string
	SaveAudio         bool
	MinEnergy         float64
	PlaybackMinEnergy float64
}

// Service is the daemon-owned voice orchestrator. It routes ASR text into the
// engine and routes session text deltas into TTS without owning session state.
type Service struct {
	manager           *Manager
	submitter         Submitter
	audioCapability   AudioInputCapability
	canceler          Canceler
	events            EventSubscriber
	publisher         EventPublisher
	driver            driver.Driver
	asr               asr.Client
	aec               aec.Processor
	ns                ns.Processor
	aecRender         *resample.Linear
	tts               tts.Client
	playback          *audioqueue.Queue
	homeDir           string
	saveAudio         bool
	minEnergy         float64
	playbackMinEnergy float64

	inputOpMu       sync.Mutex
	mu              sync.Mutex
	driverReady     bool
	asrStarted      bool
	inputSession    string
	inputStreamID   string
	inputCancel     context.CancelFunc
	asrLoopStarted  bool
	outputCancels   map[string]context.CancelFunc
	turnBuffers     map[string]*turnBuffer
	playbackCancel  context.CancelFunc
	playbackDone    chan struct{}
	ttsEventsCancel context.CancelFunc
	ttsEventsDone   chan struct{}
	currentSession  string
	currentTurn     string
	mutedTurns      map[string]bool

	ttsSpeaking           atomic.Bool
	feedbackSuppressUntil atomic.Int64
	inputLevel            atomic.Int64
	inputLevelEventAt     atomic.Int64
}

type turnBuffer struct {
	sessionID string
	splitter  *SentenceSplitter
}

func NewService(cfg ServiceConfig) *Service {
	manager := cfg.Manager
	if manager == nil {
		manager = NewManager()
	}
	canceler := cfg.Canceler
	if canceler == nil {
		canceler, _ = cfg.Submitter.(Canceler)
	}
	audioCapability, _ := cfg.Submitter.(AudioInputCapability)
	svc := &Service{
		manager:           manager,
		submitter:         cfg.Submitter,
		audioCapability:   audioCapability,
		canceler:          canceler,
		events:            cfg.Events,
		publisher:         cfg.Events,
		driver:            cfg.Driver,
		asr:               cfg.ASR,
		aec:               cfg.AEC,
		ns:                cfg.NS,
		tts:               cfg.TTS,
		playback:          audioqueue.New(),
		homeDir:           strings.TrimSpace(cfg.HomeDir),
		saveAudio:         cfg.SaveAudio,
		minEnergy:         clamp01(cfg.MinEnergy),
		playbackMinEnergy: clamp01(cfg.PlaybackMinEnergy),
		outputCancels:     make(map[string]context.CancelFunc),
		turnBuffers:       make(map[string]*turnBuffer),
		mutedTurns:        make(map[string]bool),
	}
	if svc.tts != nil {
		if err := svc.tts.Start(context.Background()); err != nil {
			slog.Warn("voice: start tts failed", "err", err)
		}
		svc.startTTSEventLoop()
		svc.startPlayback()
	}
	if svc.driver != nil && svc.aec != nil {
		in := svc.driver.InputFormat()
		out := svc.driver.OutputFormat()
		if in.Valid() && out.Valid() && in.Channels == 1 && out.Channels == 1 {
			svc.aecRender = resample.NewLinear(out.SampleRate, in.SampleRate)
		}
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
			SessionID:   sessionID,
			Kind:        event.AudioBindings,
			InputOwner:  after.InputOwner,
			InputMode:   string(after.InputMode),
			OutputOwner: after.OutputOwner,
			InputLevel:  &level,
		})
	}
}

func audioBindingEventSessionIDs(before, after Bindings) []string {
	seen := make(map[string]bool, 4)
	out := make([]string, 0, 4)
	for _, sessionID := range []string{before.InputOwner, before.OutputOwner, after.InputOwner, after.OutputOwner} {
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

func (s *Service) BindOutput(sessionID string, enabled bool) (Bindings, error) {
	before := s.manager.Snapshot()
	slog.Info("voice: output bind requested", "sessionID", sessionID, "enabled", enabled, "previousOwner", before.OutputOwner)
	bindings, err := s.manager.BindOutput(sessionID, enabled)
	if err != nil {
		return Bindings{}, err
	}
	if before.OutputOwner != bindings.OutputOwner {
		if before.OutputOwner != "" {
			s.stopOutput(before.OutputOwner)
		}
		if bindings.OutputOwner != "" {
			s.startOutput(bindings.OutputOwner)
		}
	}
	bindings = s.withCurrentInputLevel(bindings)
	s.publishAudioBindings(before, bindings)
	slog.Info("voice: output binding updated", "sessionID", sessionID, "outputOwner", bindings.OutputOwner)
	return bindings, nil
}

func (s *Service) ReleaseSession(sessionID string) Bindings {
	s.inputOpMu.Lock()
	defer s.inputOpMu.Unlock()

	before := s.manager.Snapshot()
	bindings := s.manager.ReleaseSession(sessionID)
	if before.OutputOwner != bindings.OutputOwner && before.OutputOwner != "" {
		s.stopOutput(before.OutputOwner)
	}
	if before.InputOwner != bindings.InputOwner && before.InputOwner != "" {
		s.stopInput()
	}
	bindings = s.withCurrentInputLevel(bindings)
	s.publishAudioBindings(before, bindings)
	return bindings
}

func (s *Service) CancelSession(ctx context.Context, sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cancelled := s.cancelSessionPlayback(ctx, sessionID)
	slog.Info("voice: session audio cancelled", "sessionID", sessionID, "cancelled", cancelled)
	return cancelled
}

func (s *Service) Close() error {
	s.inputOpMu.Lock()
	defer s.inputOpMu.Unlock()

	s.mu.Lock()
	inputCancel := s.inputCancel
	s.inputCancel = nil
	s.inputSession = ""
	s.inputStreamID = ""
	cancels := make([]context.CancelFunc, 0, len(s.outputCancels))
	for sessionID, cancel := range s.outputCancels {
		cancels = append(cancels, cancel)
		delete(s.outputCancels, sessionID)
	}
	playbackCancel := s.playbackCancel
	playbackDone := s.playbackDone
	ttsEventsCancel := s.ttsEventsCancel
	ttsEventsDone := s.ttsEventsDone
	s.playbackCancel = nil
	s.playbackDone = nil
	s.ttsEventsCancel = nil
	s.ttsEventsDone = nil
	s.mu.Unlock()
	if inputCancel != nil {
		inputCancel()
	}
	s.setInputLevel(0)
	if s.driver != nil {
		_ = s.driver.StopCapture(context.Background())
	}
	for _, cancel := range cancels {
		cancel()
	}
	if s.playback != nil {
		s.playback.Close()
	}
	if playbackCancel != nil {
		playbackCancel()
	}
	if playbackDone != nil {
		<-playbackDone
	}
	if ttsEventsCancel != nil {
		ttsEventsCancel()
	}
	var err error
	if s.tts != nil {
		err = s.tts.Stop(context.Background())
	}
	if ttsEventsDone != nil {
		<-ttsEventsDone
	}
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
		s.stopUnownedPlayback()
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

func (s *Service) stopUnownedPlayback() {
	if s.manager.Snapshot().OutputOwner != "" {
		return
	}
	if err := s.driver.StopPlayback(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("voice: stop unowned playback failed", "err", err)
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
		s.stopUnownedPlayback()
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
			threshold := s.captureEnergyThreshold(owner)
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
			bargeIn := s.bargeInIfPlaying(context.Background(), owner)
			if !bargeIn && s.feedbackSuppressed() {
				slog.Debug("voice: suppress asr sentence during tts feedback window", "sessionID", owner)
				continue
			}
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

func (s *Service) bargeInIfPlaying(ctx context.Context, sessionID string) bool {
	turnID, playing := s.currentPlayback(sessionID)
	if !playing {
		return false
	}
	slog.Info("voice: barge-in detected during tts", "sessionID", sessionID, "turnID", turnID)
	s.cancelSessionPlayback(ctx, sessionID)
	if s.canceler != nil {
		if err := s.canceler.Cancel(sessionID); err != nil && !errors.Is(err, engine.ErrNoRunningTurn) {
			slog.Warn("voice: cancel running turn for barge-in failed", "sessionID", sessionID, "turnID", turnID, "err", err)
		}
	}
	return true
}

func (s *Service) startOutput(sessionID string) {
	if s.events == nil || s.tts == nil {
		return
	}
	s.mu.Lock()
	if cancel, ok := s.outputCancels[sessionID]; ok {
		cancel()
	}
	ch, unsubscribe := s.events.Subscribe(sessionID)
	ctx, cancel := context.WithCancel(context.Background())
	s.outputCancels[sessionID] = cancel
	s.mu.Unlock()

	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				s.handleOutputEvent(ctx, ev)
			}
		}
	}()
}

func (s *Service) stopOutput(sessionID string) {
	s.mu.Lock()
	cancel, ok := s.outputCancels[sessionID]
	if ok {
		delete(s.outputCancels, sessionID)
	}
	s.mu.Unlock()
	if ok {
		cancel()
	}
	s.cancelSessionPlayback(context.Background(), sessionID)
	s.stopDriverPlayback(sessionID)
}

func (s *Service) handleOutputEvent(ctx context.Context, ev event.Event) {
	if s.manager.Snapshot().OutputOwner != ev.SessionID {
		return
	}
	switch ev.Kind {
	case event.TurnDelta:
		if ev.Part != "" && ev.Part != "text" {
			return
		}
		if strings.TrimSpace(ev.Delta) == "" {
			return
		}
		for _, segment := range s.pushDelta(ev.SessionID, ev.TurnID, ev.Delta) {
			s.enqueueSegment(ev.SessionID, ev.TurnID, segment)
		}
	case event.TurnCompleted:
		for _, segment := range s.flushTurn(ev.TurnID) {
			s.enqueueSegment(ev.SessionID, ev.TurnID, segment)
		}
	case event.TurnFailed, event.TurnCancelled:
		s.discardTurn(ev.TurnID)
		s.markTurnMuted(ev.TurnID)
		if s.playback != nil {
			s.playback.ClearTurn(ev.TurnID)
		}
		if err := s.tts.Cancel(ctx, ev.TurnID); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: tts cancel failed", "sessionID", ev.SessionID, "turnID", ev.TurnID, "err", err)
		}
	}
}

func (s *Service) startDriverPlayback(sessionID string) {
	if s.driver == nil {
		return
	}
	if err := s.driver.Init(context.Background()); err != nil {
		slog.Warn("voice: initialize playback driver failed", "sessionID", sessionID, "driver", s.driver.Name(), "err", err)
		return
	}
	if err := s.driver.StartPlayback(context.Background()); err != nil {
		slog.Warn("voice: start playback driver failed", "sessionID", sessionID, "driver", s.driver.Name(), "err", err)
	}
}

func (s *Service) stopDriverPlayback(sessionID string) {
	if s.driver == nil {
		return
	}
	if err := s.driver.StopPlayback(context.Background()); err != nil {
		slog.Warn("voice: stop playback driver failed", "sessionID", sessionID, "driver", s.driver.Name(), "err", err)
	}
}

func (s *Service) startTTSEventLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.ttsEventsCancel = cancel
	s.ttsEventsDone = done
	go func() {
		defer close(done)
		events := s.tts.Events()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				s.handleTTSEvent(ctx, ev)
			}
		}
	}()
}

func (s *Service) handleTTSEvent(ctx context.Context, ev tts.Event) {
	switch ev.Kind {
	case tts.EventAudio:
		if s.driver == nil || ev.Audio.Data == nil {
			return
		}
		if s.manager.Snapshot().OutputOwner != ev.SessionID || s.isTurnMuted(ev.TurnID) {
			return
		}
		s.startDriverPlayback(ev.SessionID)
		audio := ev.Audio
		if len(audio.Data) == 0 {
			return
		}
		if !audio.Format.Valid() || audio.Format != s.driver.OutputFormat() {
			slog.Warn("voice: tts audio format mismatch", "sessionID", ev.SessionID, "turnID", ev.TurnID, "got", audio.Format, "want", s.driver.OutputFormat())
			return
		}
		if err := s.pushRenderReference(audio); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: aec render reference failed", "sessionID", ev.SessionID, "turnID", ev.TurnID, "err", err)
		}
		if err := s.driver.WritePlayback(ctx, audio); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, driver.ErrNotStarted) {
			slog.Warn("voice: playback write failed", "sessionID", ev.SessionID, "turnID", ev.TurnID, "err", err)
		}
	case tts.EventEnded, tts.EventError:
		s.cleanupTTSPlayback(ev)
	}
}

func (s *Service) cleanupTTSPlayback(ev tts.Event) {
	s.mu.Lock()
	delete(s.mutedTurns, ev.TurnID)
	s.mu.Unlock()
}

func (s *Service) startPlayback() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.playbackCancel = cancel
	s.playbackDone = done
	go func() {
		defer close(done)
		for {
			item, ok := s.playback.Next(ctx)
			if !ok {
				return
			}
			if s.manager.Snapshot().OutputOwner != item.SessionID {
				continue
			}
			s.beginFeedbackSuppression()
			s.setCurrentPlayback(item.SessionID, item.TurnID)
			if err := s.tts.Speak(ctx, tts.Request{
				SessionID: item.SessionID,
				TurnID:    item.TurnID,
				SegmentID: item.SegmentID,
				Text:      item.Text,
			}); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("voice: tts speak failed", "sessionID", item.SessionID, "turnID", item.TurnID, "err", err)
			}
			s.clearCurrentPlayback(item.SessionID, item.TurnID)
			s.endFeedbackSuppression()
		}
	}()
}

func (s *Service) cancelSessionPlayback(ctx context.Context, sessionID string) bool {
	cancelled := false
	if s.playback != nil {
		if removed := s.playback.ClearSession(sessionID); removed > 0 {
			cancelled = true
		}
	}
	if s.clearSessionBuffers(sessionID) > 0 {
		cancelled = true
	}
	turnID, current := s.currentPlayback(sessionID)
	if current && s.tts != nil {
		s.markTurnMuted(turnID)
		if err := s.tts.Cancel(ctx, turnID); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: tts cancel failed", "sessionID", sessionID, "turnID", turnID, "err", err)
		}
		cancelled = true
	}
	return cancelled
}

func (s *Service) setCurrentPlayback(sessionID, turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentSession = sessionID
	s.currentTurn = turnID
}

func (s *Service) clearCurrentPlayback(sessionID, turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentSession == sessionID && s.currentTurn == turnID {
		s.currentSession = ""
		s.currentTurn = ""
	}
}

func (s *Service) markTurnMuted(turnID string) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	s.mu.Lock()
	s.mutedTurns[turnID] = true
	s.mu.Unlock()
}

func (s *Service) isTurnMuted(turnID string) bool {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutedTurns[turnID]
}

func (s *Service) currentPlayback(sessionID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentSession != sessionID {
		return "", false
	}
	return s.currentTurn, true
}

func (s *Service) beginFeedbackSuppression() {
	s.ttsSpeaking.Store(true)
}

func (s *Service) endFeedbackSuppression() {
	s.ttsSpeaking.Store(false)
	s.feedbackSuppressUntil.Store(time.Now().Add(feedbackSuppressGrace).UnixNano())
}

func (s *Service) feedbackSuppressed() bool {
	if s.ttsSpeaking.Load() {
		return true
	}
	until := s.feedbackSuppressUntil.Load()
	return until > 0 && time.Now().UnixNano() < until
}

func (s *Service) pushDelta(sessionID, turnID, delta string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := s.turnBuffers[turnID]
	if buf == nil {
		buf = &turnBuffer{sessionID: sessionID, splitter: NewSentenceSplitter(0)}
		s.turnBuffers[turnID] = buf
	}
	return append([]string(nil), buf.splitter.Push(delta)...)
}

func (s *Service) flushTurn(turnID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := s.turnBuffers[turnID]
	if buf == nil {
		return nil
	}
	delete(s.turnBuffers, turnID)
	return append([]string(nil), buf.splitter.Flush()...)
}

func (s *Service) discardTurn(turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.turnBuffers, turnID)
}

func (s *Service) clearSessionBuffers(sessionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for turnID, buf := range s.turnBuffers {
		if buf.sessionID == sessionID {
			delete(s.turnBuffers, turnID)
			removed++
		}
	}
	return removed
}

func (s *Service) enqueueSegment(sessionID, turnID, text string) {
	if s.playback == nil {
		return
	}
	text = tts.SanitizeText(text)
	if text == "" || !tts.HasSpeakableText(text) {
		return
	}
	s.playback.Enqueue(audioqueue.Item{
		SessionID: sessionID,
		TurnID:    turnID,
		SegmentID: store.NewID("seg"),
		Text:      text,
	})
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

func (s *Service) pushRenderReference(pcm frame.PCM16) error {
	if s.aec == nil {
		return nil
	}
	ref := pcm
	if s.aecRender != nil {
		ref = frame.PCM16{
			Format: frame.Format{
				SampleRate: s.aecRender.DstRate(),
				Channels:   pcm.Format.Channels,
			},
			Data:      s.aecRender.Process(pcm.Data),
			Timestamp: pcm.Timestamp,
		}
	}
	return s.aec.PushRender(ref)
}

func (s *Service) resetCaptureDSP() {
	// WebRTC AEC and its render resampler must remain converged across turns.
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

func (s *Service) captureEnergyThreshold(sessionID string) float64 {
	threshold := s.minEnergy
	if _, playing := s.currentPlayback(sessionID); playing && s.playbackMinEnergy > threshold {
		threshold = s.playbackMinEnergy
	}
	return threshold
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
