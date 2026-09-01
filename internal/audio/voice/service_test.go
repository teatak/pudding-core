package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/audio/asr"
	"github.com/teatak/pudding-core/internal/audio/driver"
	"github.com/teatak/pudding-core/internal/audio/frame"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestServiceSubmitsSentenceThroughEngine(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"voice reply"}), mock.WithDelay(time.Millisecond))), ms)
	if err := ms.CreateSession(ctx, &store.Session{ID: "sess_voice", Title: "Voice", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	drv, fakeASR := testInputBackend()
	svc := NewService(ServiceConfig{Submitter: eng, Events: hub, Driver: drv, ASR: fakeASR})
	defer svc.Close()
	if _, err := svc.BindInput("sess_voice", true); err != nil {
		t.Fatal(err)
	}

	res, err := svc.HandleASREvent(ctx, "sess_voice", asr.Event{Kind: asr.EventSentence, Text: "hello by voice"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.TurnID == "" {
		t.Fatalf("unexpected submit result: %+v", res)
	}
	eng.Wait()

	msgs, err := ms.ListMessages(ctx, "sess_voice", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != store.RoleUser || msgs[0].Text != "hello by voice" {
		t.Fatalf("unexpected user message: %+v", msgs[0])
	}
	if msgs[1].Role != store.RoleAssistant || msgs[1].Text != "voice reply" {
		t.Fatalf("unexpected assistant message: %+v", msgs[1])
	}
}

func TestServiceSubmitsSavedASRAudioAttachment(t *testing.T) {
	ctx := context.Background()
	drv, fakeASR := testInputBackend()
	var submitted engine.SubmitInput
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(_ context.Context, in engine.SubmitInput) (*engine.SubmitResult, error) {
			submitted = in
			return &engine.SubmitResult{TurnID: "turn_audio"}, nil
		}),
		Driver:    drv,
		ASR:       fakeASR,
		HomeDir:   t.TempDir(),
		SaveAudio: true,
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_voice", true); err != nil {
		t.Fatal(err)
	}
	_, err := svc.HandleASREvent(ctx, "sess_voice", asr.Event{
		Kind: asr.EventSentence,
		Text: "hello by voice",
		Audio: frame.PCM16{
			Format: frame.Format{SampleRate: 16000, Channels: 1},
			Data:   []byte{0x00, 0x00, 0xff, 0x7f, 0x00, 0x80, 0x00, 0x00},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Text != "hello by voice" || len(submitted.Parts) != 1 {
		t.Fatalf("unexpected submitted input: %+v", submitted)
	}
	part := submitted.Parts[0]
	if part.Type != store.ContentPartAttachment || part.Origin != attachment.OriginASRAudio || part.MIME != "audio/wav" || part.AudioTranscript != "hello by voice" {
		t.Fatalf("unexpected audio part: %+v", part)
	}
	path, ok, err := attachment.NewService(svc.homeDir).Path("sess_voice", part.AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("audio path ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 44 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WAVE")) {
		t.Fatalf("saved audio is not wav: %q", data[:min(len(data), 12)])
	}
}

func TestServiceSubmitsRawVoiceAudioWithASRTranscript(t *testing.T) {
	ctx := context.Background()
	drv, fakeASR := testInputBackend()
	var submitted engine.SubmitInput
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(_ context.Context, in engine.SubmitInput) (*engine.SubmitResult, error) {
			submitted = in
			return &engine.SubmitResult{TurnID: "turn_voice"}, nil
		}),
		Driver:  drv,
		ASR:     fakeASR,
		HomeDir: t.TempDir(),
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_voice", true, InputModeRaw); err != nil {
		t.Fatal(err)
	}
	_, err := svc.HandleASREvent(ctx, "sess_voice", asr.Event{
		Kind: asr.EventSentence,
		Text: "原音回显",
		Audio: frame.PCM16{
			Format: frame.Format{SampleRate: 16000, Channels: 1},
			Data:   []byte{0x00, 0x00, 0xff, 0x7f},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Text != "原音回显" || !strings.HasPrefix(submitted.ClientMessageID, "voicemsg") || len(submitted.Parts) != 1 {
		t.Fatalf("unexpected raw voice input: %+v", submitted)
	}
	part := submitted.Parts[0]
	if part.Origin != attachment.OriginVoiceAudio || part.AudioTranscript != "原音回显" || part.MIME != "audio/wav" {
		t.Fatalf("unexpected raw voice attachment: %+v", part)
	}
}

func TestServiceRejectsSentenceWithoutInputOwner(t *testing.T) {
	svc := NewService(ServiceConfig{Submitter: submitterFunc(func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error) {
		t.Fatal("submit should not be called")
		return nil, nil
	})})
	defer svc.Close()
	if _, err := svc.SubmitSentence(context.Background(), "sess_voice", "hello"); err != ErrNoInputBinding {
		t.Fatalf("err = %v, want %v", err, ErrNoInputBinding)
	}
}

func TestBindInputStartsCaptureAndRoutesASR(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	hub := event.NewHub()
	fakeASR := asr.NewFake(4)
	drv := &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}
	eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithScript([]string{"ok"}), mock.WithDelay(time.Millisecond))), ms)
	if err := ms.CreateSession(ctx, &store.Session{ID: "sess_input", Title: "Voice", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(ServiceConfig{Submitter: eng, Events: hub, Driver: drv, ASR: fakeASR})
	defer svc.Close()
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	if !drv.started {
		t.Fatal("capture driver was not started")
	}
	if drv.handler == nil {
		t.Fatal("capture handler was not registered")
	}
	drv.handler(frame.PCM16{
		Format: drv.format,
		Data:   []byte{0, 64, 0, 64, 0, 64, 0, 64},
	})
	if got := svc.Snapshot().InputLevel; got <= 0 {
		t.Fatalf("input level = %v, want > 0", got)
	}

	fakeASR.EmitSentence("from mic")
	msgs := waitMessages(t, ctx, ms, "sess_input", 2)
	eng.Wait()
	if len(msgs) != 2 || msgs[0].Text != "from mic" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	if _, err := svc.BindInput("sess_input", false); err != nil {
		t.Fatal(err)
	}
	if drv.started {
		t.Fatal("capture driver should be stopped")
	}
	if got := svc.Snapshot().InputLevel; got != 0 {
		t.Fatalf("input level after stop = %v, want 0", got)
	}
}

func TestBindInputPrimesRouteBeforeCapture(t *testing.T) {
	drv := &primingCaptureDriver{captureDriver: captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}}
	svc := NewService(ServiceConfig{Driver: drv, ASR: asr.NewFake(1)})
	defer svc.Close()

	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(drv.calls, ","); got != "prime,capture" {
		t.Fatalf("audio call order while active = %q, want prime,capture", got)
	}
	if drv.prompt.Format != drv.OutputFormat() || len(drv.prompt.Data) == 0 {
		t.Fatalf("route prompt = %+v, want non-empty output-format PCM", drv.prompt)
	}
	if _, err := svc.BindInput("sess_input", false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(drv.calls, ","); got != "prime,capture,stop-playback" {
		t.Fatalf("audio call order after stop = %q, want prime,capture,stop-playback", got)
	}
}

func TestCaptureFeedsLowEnergyFramesToVAD(t *testing.T) {
	drv := &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}
	recASR := newRecordingASR()
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error) { return nil, nil }),
		Driver:    drv,
		ASR:       recASR,
		MinEnergy: 0.5,
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}

	drv.handler(frame.PCM16{
		Format: drv.format,
		Data:   []byte{100, 0, 100, 0, 100, 0, 100, 0},
	})

	feeds := recASR.Feeds()
	if len(feeds) != 1 {
		t.Fatalf("feeds len = %d, want 1", len(feeds))
	}
	if !bytes.Equal(feeds[0].Data, []byte{100, 0, 100, 0, 100, 0, 100, 0}) {
		t.Fatalf("low energy frame changed before VAD: %v", feeds[0].Data)
	}
	if got := svc.Snapshot().InputLevel; got <= 0 {
		t.Fatalf("input level = %v, want visible low-level activity", got)
	}
}

func TestASRSentenceEnergyGateSuppressesOnlyCompletedLowEnergyAudio(t *testing.T) {
	drv := &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}
	fakeASR := asr.NewFake(4)
	submits := make(chan engine.SubmitInput, 2)
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(_ context.Context, input engine.SubmitInput) (*engine.SubmitResult, error) {
			submits <- input
			return &engine.SubmitResult{}, nil
		}),
		Driver:    drv,
		ASR:       fakeASR,
		MinEnergy: 0.5,
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}

	fakeASR.Emit(asr.Event{Kind: asr.EventSentence, Text: "quiet", Audio: constantPCM16(drv.format, 100, 500*time.Millisecond)})
	select {
	case input := <-submits:
		t.Fatalf("low energy sentence was submitted: %+v", input)
	case <-time.After(100 * time.Millisecond):
	}

	fakeASR.Emit(asr.Event{Kind: asr.EventSentence, Text: "loud", Audio: constantPCM16(drv.format, 20000, 500*time.Millisecond)})
	select {
	case input := <-submits:
		if input.Text != "loud" {
			t.Fatalf("submitted text = %q, want loud", input.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("high energy sentence was not submitted")
	}
}

func TestInputRestartDropsStaleASREvents(t *testing.T) {
	drv := &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}
	fakeASR := asr.NewFake(4)
	submits := make(chan engine.SubmitInput, 1)
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(_ context.Context, input engine.SubmitInput) (*engine.SubmitResult, error) {
			submits <- input
			return &engine.SubmitResult{}, nil
		}),
		Driver: drv,
		ASR:    fakeASR,
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	oldStreamID := svc.inputStreamID
	if _, err := svc.BindInput("sess_input", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	if oldStreamID == "" || oldStreamID == svc.inputStreamID {
		t.Fatalf("stream IDs were not rotated: old=%q new=%q", oldStreamID, svc.inputStreamID)
	}

	fakeASR.Emit(asr.Event{Kind: asr.EventSentence, StreamID: oldStreamID, Text: "stale"})
	select {
	case input := <-submits:
		t.Fatalf("stale ASR event was submitted: %+v", input)
	case <-time.After(100 * time.Millisecond):
	}
	fakeASR.EmitSentence("current")
	select {
	case input := <-submits:
		if input.Text != "current" {
			t.Fatalf("submitted text = %q, want current", input.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("current ASR event was not submitted")
	}
}

func TestInputRestartDropsStaleCaptureCallbacks(t *testing.T) {
	drv := &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}
	recASR := newRecordingASR()
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error) { return nil, nil }),
		Driver:    drv,
		ASR:       recASR,
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	staleHandler := drv.handler
	if _, err := svc.BindInput("sess_input", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	currentHandler := drv.handler
	pcm := constantPCM16(drv.format, 1000, 20*time.Millisecond)
	staleHandler(pcm)
	currentHandler(pcm)
	if feeds := recASR.Feeds(); len(feeds) != 1 {
		t.Fatalf("feeds len = %d, want only current callback", len(feeds))
	}
}

func TestInputRestartPreservesAECAndResetsNSOnlyWhileCaptureStopped(t *testing.T) {
	drv := &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}
	aecProcessor := &lifecycleAEC{}
	nsProcessor := &lifecycleNS{driver: drv}
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error) { return nil, nil }),
		Driver:    drv,
		ASR:       asr.NewFake(1),
		AEC:       aecProcessor,
		NS:        nsProcessor,
	})
	defer svc.Close()
	for i := 0; i < 2; i++ {
		if _, err := svc.BindInput("sess_input", true); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.BindInput("sess_input", false); err != nil {
			t.Fatal(err)
		}
	}
	if got := aecProcessor.resetCount.Load(); got != 0 {
		t.Fatalf("AEC reset count = %d, want 0 to preserve convergence", got)
	}
	if got := nsProcessor.resetCount.Load(); got != 4 {
		t.Fatalf("NS reset count = %d, want 4", got)
	}
	if nsProcessor.resetWhileActive.Load() {
		t.Fatal("NS was reset while the capture callback could still be running")
	}
}

func TestBindInputRestartsUnhealthyCapture(t *testing.T) {
	fakeASR := asr.NewFake(1)
	drv := &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error) { return nil, nil }),
		Driver:    drv,
		ASR:       fakeASR,
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	if drv.startCount != 1 {
		t.Fatalf("startCount = %d, want 1", drv.startCount)
	}
	drv.active = false
	if _, err := svc.BindInput("sess_input", true); err != nil {
		t.Fatal(err)
	}
	if drv.startCount != 2 {
		t.Fatalf("startCount after unhealthy restart = %d, want 2", drv.startCount)
	}
	if got := svc.manager.Snapshot().InputOwner; got != "sess_input" {
		t.Fatalf("input owner = %q, want sess_input", got)
	}
}

func TestBindInputCleansUpCaptureAfterStartFailure(t *testing.T) {
	fakeASR := asr.NewFake(1)
	drv := &captureDriver{
		format:   frame.Format{SampleRate: 16000, Channels: 1},
		startErr: errors.New("boom"),
	}
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error) { return nil, nil }),
		Driver:    drv,
		ASR:       fakeASR,
	})
	defer svc.Close()
	if _, err := svc.BindInput("sess_input", true); err == nil {
		t.Fatal("BindInput error = nil, want start failure")
	}
	if drv.stopCount != 2 {
		t.Fatalf("stopCount = %d, want 2", drv.stopCount)
	}
	if drv.started || drv.active {
		t.Fatalf("driver still active after failed start: started=%v active=%v", drv.started, drv.active)
	}
	if got := svc.manager.Snapshot().InputOwner; got != "" {
		t.Fatalf("input owner = %q, want empty", got)
	}
}

func TestBindInputClassifiesCaptureWithoutSignal(t *testing.T) {
	fakeASR := asr.NewFake(1)
	drv := &captureDriver{
		format:   frame.Format{SampleRate: 16000, Channels: 1},
		startErr: driver.ErrCaptureNoSignal,
	}
	svc := NewService(ServiceConfig{
		Submitter: submitterFunc(func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error) { return nil, nil }),
		Driver:    drv,
		ASR:       fakeASR,
	})
	defer svc.Close()

	_, err := svc.BindInput("sess_input", true)
	if !errors.Is(err, ErrInputRouteUnavailable) {
		t.Fatalf("BindInput error = %v, want ErrInputRouteUnavailable", err)
	}
}

func waitMessages(t *testing.T, ctx context.Context, ms *memstore.Memstore, sessionID string, want int) []*store.Message {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		msgs, err := ms.ListMessages(ctx, sessionID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) >= want {
			return msgs
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d messages, got %d: %+v", want, len(msgs), msgs)
		case <-tick.C:
		}
	}
}

func testInputBackend() (*captureDriver, *asr.Fake) {
	return &captureDriver{format: frame.Format{SampleRate: 16000, Channels: 1}}, asr.NewFake(4)
}

type recordingASR struct {
	mu     sync.Mutex
	events chan asr.Event
	feeds  []frame.PCM16
	once   sync.Once
}

type lifecycleAEC struct {
	resetCount atomic.Int32
}

func (*lifecycleAEC) Name() string                                        { return "lifecycle-aec" }
func (*lifecycleAEC) PushRender(frame.PCM16) error                        { return nil }
func (*lifecycleAEC) ProcessCapture(pcm frame.PCM16) (frame.PCM16, error) { return pcm, nil }
func (p *lifecycleAEC) Reset()                                            { p.resetCount.Add(1) }

type lifecycleNS struct {
	driver           *captureDriver
	resetCount       atomic.Int32
	resetWhileActive atomic.Bool
}

func (*lifecycleNS) Name() string { return "lifecycle-ns" }
func (*lifecycleNS) Process(pcm frame.PCM16) (frame.PCM16, error) {
	return pcm, nil
}
func (p *lifecycleNS) Reset() {
	p.resetCount.Add(1)
	if p.driver != nil && p.driver.active {
		p.resetWhileActive.Store(true)
	}
}

func newRecordingASR() *recordingASR {
	return &recordingASR{events: make(chan asr.Event)}
}

func (a *recordingASR) Name() string { return "recording" }

func (a *recordingASR) Start(context.Context) error { return nil }

func (a *recordingASR) Feed(_ context.Context, pcm frame.PCM16) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copied := pcm
	copied.Data = append([]byte(nil), pcm.Data...)
	a.feeds = append(a.feeds, copied)
	return nil
}

func (a *recordingASR) Events() <-chan asr.Event { return a.events }

func (a *recordingASR) Stop(context.Context) error {
	a.once.Do(func() { close(a.events) })
	return nil
}

func (a *recordingASR) Feeds() []frame.PCM16 {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]frame.PCM16, len(a.feeds))
	copy(out, a.feeds)
	return out
}

func constantPCM16(format frame.Format, sample int16, duration time.Duration) frame.PCM16 {
	samples := format.SampleRate * format.Channels * int(duration) / int(time.Second)
	data := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	return frame.PCM16{Format: format, Data: data}
}

type submitterFunc func(context.Context, engine.SubmitInput) (*engine.SubmitResult, error)

func (f submitterFunc) Submit(ctx context.Context, in engine.SubmitInput) (*engine.SubmitResult, error) {
	return f(ctx, in)
}

type captureDriver struct {
	format     frame.Format
	started    bool
	active     bool
	startCount int
	stopCount  int
	startErr   error
	handler    driver.CaptureHandler
}

func (d *captureDriver) Name() string               { return "capture-test" }
func (d *captureDriver) Init(context.Context) error { return nil }
func (d *captureDriver) Close() error {
	d.started = false
	d.active = false
	return nil
}
func (d *captureDriver) InputFormat() frame.Format  { return d.format }
func (d *captureDriver) OutputFormat() frame.Format { return d.format }
func (d *captureDriver) StartCapture(_ context.Context, handler driver.CaptureHandler) error {
	d.started = true
	d.active = true
	d.startCount++
	d.handler = handler
	return d.startErr
}
func (d *captureDriver) StopCapture(context.Context) error {
	d.stopCount++
	d.started = false
	d.active = false
	return nil
}
func (d *captureDriver) CaptureActive() bool                              { return d.active }
func (d *captureDriver) StartPlayback(context.Context) error              { return nil }
func (d *captureDriver) WritePlayback(context.Context, frame.PCM16) error { return nil }
func (d *captureDriver) StopPlayback(context.Context) error               { return nil }

type primingCaptureDriver struct {
	captureDriver
	calls  []string
	prompt frame.PCM16
}

func (d *primingCaptureDriver) PrimeInputRoute(_ context.Context, pcm frame.PCM16) error {
	d.calls = append(d.calls, "prime")
	d.prompt = pcm
	return nil
}

func (d *primingCaptureDriver) StartCapture(ctx context.Context, handler driver.CaptureHandler) error {
	d.calls = append(d.calls, "capture")
	return d.captureDriver.StartCapture(ctx, handler)
}

func (d *primingCaptureDriver) StopPlayback(context.Context) error {
	d.calls = append(d.calls, "stop-playback")
	return nil
}
