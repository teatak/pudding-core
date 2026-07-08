package voice

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teatak/pudding-core/internal/audio/asr"
	"github.com/teatak/pudding-core/internal/audio/driver"
	"github.com/teatak/pudding-core/internal/audio/dsp/aec"
	"github.com/teatak/pudding-core/internal/audio/dsp/ns"
	"github.com/teatak/pudding-core/internal/audio/dsp/resample"
	"github.com/teatak/pudding-core/internal/audio/frame"
	audioqueue "github.com/teatak/pudding-core/internal/audio/queue"
	"github.com/teatak/pudding-core/internal/audio/tts"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

var (
	ErrNoInputBinding   = errors.New("voice: session does not own audio input")
	ErrInputUnavailable = errors.New("voice: input backend unavailable")
	ErrEmptySentence    = errors.New("voice: empty sentence")
)

const feedbackSuppressGrace = 1500 * time.Millisecond
const inputLevelScale = 10000
const inputLevelEventInterval = 100 * time.Millisecond

type Submitter interface {
	Submit(ctx context.Context, in engine.SubmitInput) (*engine.SubmitResult, error)
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
	Manager   *Manager
	Submitter Submitter
	Canceler  Canceler
	Events    EventBus
	Driver    driver.Driver
	ASR       asr.Client
	AEC       aec.Processor
	NS        ns.Processor
	TTS       tts.Client
}

// Service is the daemon-owned voice orchestrator. It routes ASR text into the
// engine and routes session text deltas into TTS without owning session state.
type Service struct {
	manager   *Manager
	submitter Submitter
	canceler  Canceler
	events    EventSubscriber
	publisher EventPublisher
	driver    driver.Driver
	asr       asr.Client
	aec       aec.Processor
	ns        ns.Processor
	aecRender *resample.Linear
	tts       tts.Client
	playback  *audioqueue.Queue

	mu              sync.Mutex
	driverReady     bool
	asrStarted      bool
	inputSession    string
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
	svc := &Service{
		manager:       manager,
		submitter:     cfg.Submitter,
		canceler:      canceler,
		events:        cfg.Events,
		publisher:     cfg.Events,
		driver:        cfg.Driver,
		asr:           cfg.ASR,
		aec:           cfg.AEC,
		ns:            cfg.NS,
		tts:           cfg.TTS,
		playback:      audioqueue.New(),
		outputCancels: make(map[string]context.CancelFunc),
		turnBuffers:   make(map[string]*turnBuffer),
		mutedTurns:    make(map[string]bool),
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

func (s *Service) BindInput(sessionID string, enabled bool) (Bindings, error) {
	before := s.manager.Snapshot()
	slog.Info("voice: input bind requested", "sessionID", sessionID, "enabled", enabled, "previousOwner", before.InputOwner)
	if enabled {
		if before.InputOwner != sessionID || !s.inputCaptureActive(sessionID) {
			s.stopInput()
			if err := s.startInput(sessionID); err != nil {
				slog.Warn("voice: input start failed", "sessionID", sessionID, "err", err)
				return Bindings{}, err
			}
		}
		bindings, err := s.manager.BindInput(sessionID, true)
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

func (s *Service) inputCaptureActive(sessionID string) bool {
	s.mu.Lock()
	active := s.inputSession == sessionID && s.inputCancel != nil
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
	s.mu.Lock()
	inputCancel := s.inputCancel
	s.inputCancel = nil
	s.inputSession = ""
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
		return s.SubmitSentence(ctx, sessionID, ev.Text)
	case asr.EventError:
		if ev.Err != nil {
			return nil, ev.Err
		}
	}
	return nil, nil
}

func (s *Service) SubmitSentence(ctx context.Context, sessionID, text string) (*engine.SubmitResult, error) {
	if s.submitter == nil {
		return nil, errors.New("voice: submitter unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrEmptySentence
	}
	if s.manager.Snapshot().InputOwner != sessionID {
		return nil, ErrNoInputBinding
	}
	clientMessageID := store.NewID("audmsg")
	slog.Info("voice: submitting asr sentence", "sessionID", sessionID, "clientMessageID", clientMessageID, "text", previewText(text, 80))
	result, err := s.submitter.Submit(ctx, engine.SubmitInput{
		SessionID:       sessionID,
		ClientMessageID: clientMessageID,
		Text:            text,
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

func (s *Service) startInput(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ErrSessionRequired
	}
	if s.driver == nil || s.asr == nil {
		return ErrInputUnavailable
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
		go s.asrEventLoop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.inputSession = sessionID
	s.inputCancel = cancel
	s.mu.Unlock()

	s.setInputLevel(0)
	s.resetDSP()
	if err := s.driver.StartCapture(ctx, func(pcm frame.PCM16) {
		if s.manager.Snapshot().InputOwner != sessionID {
			return
		}
		processed, err := s.processCapture(pcm)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: capture dsp failed", "sessionID", sessionID, "err", err)
			processed = pcm
		}
		s.setInputLevel(pcmRMSLevel(processed))
		if err := s.asr.Feed(ctx, processed); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("voice: asr feed failed", "sessionID", sessionID, "err", err)
		}
	}); err != nil {
		cancel()
		s.mu.Lock()
		s.inputCancel = nil
		s.inputSession = ""
		s.mu.Unlock()
		return err
	}
	slog.Info("voice: input capture started", "sessionID", sessionID, "driver", s.driver.Name(), "asr", s.asr.Name())
	return nil
}

func (s *Service) stopInput() {
	s.mu.Lock()
	cancel := s.inputCancel
	s.inputCancel = nil
	s.inputSession = ""
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.setInputLevel(0)
	s.resetDSP()
	if s.driver != nil {
		_ = s.driver.StopCapture(context.Background())
	}
	if cancel != nil {
		slog.Info("voice: input capture stopped")
	}
}

func (s *Service) asrEventLoop() {
	for ev := range s.asr.Events() {
		owner := s.manager.Snapshot().InputOwner
		if owner == "" {
			continue
		}
		if ev.Kind == asr.EventSentence {
			slog.Info(
				"voice: asr sentence received",
				"sessionID", owner,
				"text", previewText(ev.Text, 80),
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

func (s *Service) resetDSP() {
	if s.aec != nil {
		s.aec.Reset()
	}
	if s.ns != nil {
		s.ns.Reset()
	}
	if s.aecRender != nil {
		s.aecRender.Reset()
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
