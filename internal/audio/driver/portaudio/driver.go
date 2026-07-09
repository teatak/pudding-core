// Package portaudio captures PCM16 audio from the default input device.
package portaudio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/teatak/pudding-core/internal/audio/driver"
	"github.com/teatak/pudding-core/internal/audio/frame"
)

const (
	captureStopTimeout = 2 * time.Second
	captureFrameQueue  = 16
	deviceCloseTimeout = 2 * time.Second
	deviceOpenAttempts = 5
	deviceOpenRetry    = 300 * time.Millisecond
	deviceRefreshDelay = 300 * time.Millisecond
	deviceWatchEvery   = 500 * time.Millisecond
)

type Config struct {
	Format       frame.Format
	InputFormat  frame.Format
	OutputFormat frame.Format
	FrameMillis  int
}

type Driver struct {
	mu           sync.Mutex
	inputFormat  frame.Format
	outputFormat frame.Format
	frameMillis  int
	initialized  bool

	stream          *portaudio.Stream
	captureCtx      context.Context
	captureHandler  driver.CaptureHandler
	stop            chan struct{}
	done            chan struct{}
	closeCapture    func()
	deviceName      string
	playbackStream  *portaudio.Stream
	playbackBuffer  []int16
	playbackReady   bool
	playbackDevName string
	needsRefresh    bool
	watcherStop     chan struct{}
	watcherDone     chan struct{}
}

func New(cfg Config) *Driver {
	inputFormat := cfg.InputFormat
	if !inputFormat.Valid() {
		inputFormat = cfg.Format
	}
	if !inputFormat.Valid() {
		inputFormat = frame.Format{SampleRate: 16000, Channels: 1}
	}
	outputFormat := cfg.OutputFormat
	if !outputFormat.Valid() {
		outputFormat = cfg.Format
	}
	if !outputFormat.Valid() {
		outputFormat = frame.Format{SampleRate: 24000, Channels: 1}
	}
	frameMillis := cfg.FrameMillis
	if frameMillis <= 0 {
		frameMillis = 20
	}
	return &Driver{inputFormat: inputFormat, outputFormat: outputFormat, frameMillis: frameMillis}
}

func (d *Driver) Name() string { return "portaudio" }

func (d *Driver) Init(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.initialized {
		return nil
	}
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("portaudio init: %w", err)
	}
	installCoreAudioListener()
	slog.Info("portaudio: initialized", "input", d.inputFormat, "output", d.outputFormat)
	d.initialized = true
	return nil
}

func (d *Driver) Close() error {
	d.stopWatcher()
	_ = d.StopCapture(context.Background())
	_ = d.StopPlayback(context.Background())
	d.mu.Lock()
	initialized := d.initialized
	d.initialized = false
	d.needsRefresh = false
	d.mu.Unlock()
	if initialized {
		portaudio.Terminate()
		slog.Info("portaudio: terminated")
	}
	return nil
}

func (d *Driver) InputFormat() frame.Format  { return d.inputFormat }
func (d *Driver) OutputFormat() frame.Format { return d.outputFormat }

func (d *Driver) CaptureActive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stream != nil && d.done != nil && !isClosed(d.done)
}

func (d *Driver) InputDeviceName() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deviceName
}

func (d *Driver) StartCapture(ctx context.Context, onFrame driver.CaptureHandler) error {
	if onFrame == nil {
		return driver.ErrNilHandler
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !d.inputFormat.Valid() {
		return errors.New("portaudio capture: invalid format")
	}
	framesPerBuffer := d.framesPerBuffer()
	if framesPerBuffer <= 0 {
		return errors.New("portaudio capture: invalid frame duration")
	}
	if err := requestMicrophonePermission(ctx); err != nil {
		return fmt.Errorf("portaudio microphone permission: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.initialized {
		return driver.ErrNotStarted
	}
	if d.stream != nil {
		captureDied := d.done != nil && isClosed(d.done)
		captureChanged := d.deviceName != "" && defaultInputChanged(d.deviceName)
		coreAudioChanged := coreAudioDeviceChanged()
		if !d.needsRefresh && !captureDied && !captureChanged && !coreAudioChanged {
			slog.Info("portaudio capture: already running", "device", d.deviceName)
			return nil
		}
		d.captureCtx = ctx
		d.captureHandler = onFrame
		slog.Info(
			"portaudio capture: refreshing existing stream",
			"device", d.deviceName,
			"pending", d.needsRefresh,
			"captureDied", captureDied,
			"captureChanged", captureChanged,
			"coreAudioChanged", coreAudioChanged,
		)
		captureErr, playbackErr := d.fallbackRefreshOpenLocked("capture restart")
		if playbackErr != nil {
			slog.Warn("portaudio playback: recovery reopen failed", "err", playbackErr)
		}
		if captureErr != nil {
			d.captureCtx = nil
			d.captureHandler = nil
			return fmt.Errorf("portaudio capture open: %w", captureErr)
		}
		return nil
	}

	d.captureCtx = ctx
	d.captureHandler = onFrame
	if d.needsRefresh {
		if err := d.refreshDeviceListLocked("pending refresh"); err != nil {
			d.captureCtx = nil
			d.captureHandler = nil
			return err
		}
	}
	err := d.openCaptureLocked()
	if err != nil {
		slog.Warn("portaudio capture: initial open failed, refreshing devices", "err", err)
		captureErr, playbackErr := d.fallbackRefreshOpenLocked("capture open failure")
		if playbackErr != nil {
			slog.Warn("portaudio playback: recovery reopen failed", "err", playbackErr)
		}
		err = captureErr
	}
	if err != nil {
		d.captureCtx = nil
		d.captureHandler = nil
		return fmt.Errorf("portaudio capture open: %w", err)
	}
	d.ensureWatcherLocked()
	return nil
}

func (d *Driver) StopCapture(context.Context) error {
	d.mu.Lock()
	d.captureCtx = nil
	d.captureHandler = nil
	stream, stop, done, closeCapture := d.detachCaptureLocked()
	d.mu.Unlock()

	if err := stopDetachedCapture(stream, stop, done, closeCapture); err != nil {
		d.mu.Lock()
		d.needsRefresh = true
		d.mu.Unlock()
		return err
	}
	if stop != nil {
		slog.Info("portaudio capture: stopped")
	}
	return nil
}

func (d *Driver) StartPlayback(context.Context) error {
	if !d.outputFormat.Valid() {
		return errors.New("portaudio playback: invalid format")
	}
	framesPerBuffer := d.outputFramesPerBuffer()
	if framesPerBuffer <= 0 {
		return errors.New("portaudio playback: invalid frame duration")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.initialized {
		return driver.ErrNotStarted
	}
	if d.playbackStream != nil {
		d.playbackReady = true
		return nil
	}
	d.playbackReady = true
	if d.needsRefresh {
		if err := d.refreshDeviceListLocked("pending refresh"); err != nil {
			d.playbackReady = false
			return err
		}
	}
	err := d.openPlaybackLocked()
	if err != nil {
		slog.Warn("portaudio playback: initial open failed, refreshing devices", "err", err)
		captureErr, playbackErr := d.fallbackRefreshOpenLocked("playback open failure")
		if captureErr != nil {
			slog.Warn("portaudio capture: recovery reopen failed", "err", captureErr)
		}
		err = playbackErr
	}
	if err != nil {
		d.playbackReady = false
		return fmt.Errorf("portaudio playback open: %w", err)
	}
	d.ensureWatcherLocked()
	return nil
}

func (d *Driver) WritePlayback(ctx context.Context, pcm frame.PCM16) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !pcm.Format.Valid() {
		return errors.New("portaudio playback: invalid frame")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.playbackReady || d.playbackStream == nil || d.playbackBuffer == nil {
		return driver.ErrNotStarted
	}
	if pcm.Format != d.outputFormat {
		return fmt.Errorf("portaudio playback format mismatch: got=%+v want=%+v", pcm.Format, d.outputFormat)
	}
	bytesPerBuffer := len(d.playbackBuffer) * 2
	for offset := 0; offset < len(pcm.Data); offset += bytesPerBuffer {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		end := offset + bytesPerBuffer
		if end > len(pcm.Data) {
			end = len(pcm.Data)
		}
		bytesToInt16LE(pcm.Data[offset:end], d.playbackBuffer)
		if err := d.playbackStream.Write(); err != nil {
			if isOutputUnderflow(err) {
				slog.Debug("portaudio playback: output underflow")
				continue
			}
			d.needsRefresh = true
			return fmt.Errorf("portaudio playback write: %w", err)
		}
	}
	return nil
}

func (d *Driver) StopPlayback(context.Context) error {
	d.mu.Lock()
	stream := d.detachPlaybackLocked()
	d.playbackReady = false
	d.mu.Unlock()
	if err := stopDetachedPlayback(stream); err != nil {
		d.mu.Lock()
		d.needsRefresh = true
		d.mu.Unlock()
		return err
	}
	return nil
}

func (d *Driver) captureLoop(ctx context.Context, frames <-chan []int16, stop <-chan struct{}, done chan<- struct{}, onFrame driver.CaptureHandler) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case samples := <-frames:
			onFrame(frame.PCM16{
				Format:    d.inputFormat,
				Data:      int16ToBytes(samples),
				Timestamp: time.Now(),
			})
		}
	}
}

func (d *Driver) openCaptureLocked() error {
	if d.stream != nil {
		return nil
	}
	if d.captureHandler == nil {
		return nil
	}
	framesPerBuffer := d.framesPerBuffer()
	dev, err := portaudio.DefaultInputDevice()
	if err != nil {
		return err
	}
	params := portaudio.LowLatencyParameters(dev, nil)
	params.Input.Channels = d.inputFormat.Channels
	params.Output.Device = nil
	params.Output.Channels = 0
	params.SampleRate = float64(d.inputFormat.SampleRate)
	params.FramesPerBuffer = framesPerBuffer
	frameCh := make(chan []int16, captureFrameQueue)
	stream, err := portaudio.OpenStream(params, func(in []int16, _ portaudio.StreamCallbackTimeInfo, flags portaudio.StreamCallbackFlags) {
		if flags&portaudio.InputOverflow != 0 {
			return
		}
		samples := make([]int16, len(in))
		copy(samples, in)
		select {
		case frameCh <- samples:
		default:
		}
	})
	if err != nil {
		return fmt.Errorf("open capture device %q: %w", dev.Name, err)
	}
	if err := stream.Start(); err != nil {
		_ = stream.Close()
		return fmt.Errorf("start capture device %q: %w", dev.Name, err)
	}
	deviceName := dev.Name

	stop := make(chan struct{})
	done := make(chan struct{})
	closeCapture := closePortAudioStreamOnce("capture", stream)
	d.stream = stream
	d.stop = stop
	d.done = done
	d.closeCapture = closeCapture
	d.deviceName = deviceName
	go d.captureLoop(d.captureCtx, frameCh, stop, done, d.captureHandler)
	slog.Info(
		"portaudio capture: started",
		"device", deviceName,
		"sampleRate", d.inputFormat.SampleRate,
		"channels", d.inputFormat.Channels,
		"frameMillis", d.frameMillis,
		"framesPerBuffer", framesPerBuffer,
	)
	return nil
}

func (d *Driver) openPlaybackLocked() error {
	if d.playbackStream != nil {
		return nil
	}
	framesPerBuffer := d.outputFramesPerBuffer()
	output := make([]int16, framesPerBuffer*d.outputFormat.Channels)
	dev, err := portaudio.DefaultOutputDevice()
	if err != nil {
		return err
	}
	params := portaudio.LowLatencyParameters(nil, dev)
	params.Input.Device = nil
	params.Input.Channels = 0
	params.Output.Channels = d.outputFormat.Channels
	params.SampleRate = float64(d.outputFormat.SampleRate)
	params.FramesPerBuffer = framesPerBuffer
	stream, err := portaudio.OpenStream(params, output)
	if err != nil {
		return fmt.Errorf("open playback device %q: %w", dev.Name, err)
	}
	if err := stream.Start(); err != nil {
		_ = stream.Close()
		return fmt.Errorf("start playback device %q: %w", dev.Name, err)
	}
	deviceName := dev.Name
	d.playbackStream = stream
	d.playbackBuffer = output
	d.playbackDevName = deviceName
	slog.Info(
		"portaudio playback: started",
		"device", deviceName,
		"sampleRate", d.outputFormat.SampleRate,
		"channels", d.outputFormat.Channels,
		"frameMillis", d.frameMillis,
		"framesPerBuffer", framesPerBuffer,
	)
	return nil
}

func (d *Driver) fallbackRefreshOpenLocked(reason string) (error, error) {
	if err := d.refreshDeviceListLocked(reason); err != nil {
		return err, err
	}
	var captureErr error
	if d.captureHandler != nil {
		captureErr = d.openCaptureWithRetryLocked()
		if captureErr != nil {
			slog.Warn("portaudio capture: reopen failed after refresh", "err", captureErr)
		}
	}
	var playbackErr error
	if d.playbackReady {
		playbackErr = d.openPlaybackWithRetryLocked()
		if playbackErr != nil {
			slog.Warn("portaudio playback: reopen failed after refresh", "err", playbackErr)
		}
	}
	return captureErr, playbackErr
}

func (d *Driver) refreshDeviceListLocked(reason string) error {
	stream, stop, done, closeCapture := d.detachCaptureLocked()
	playback := d.detachPlaybackLocked()
	wasInitialized := d.initialized
	d.initialized = false
	d.needsRefresh = false

	d.mu.Unlock()
	captureErr := stopDetachedCapture(stream, stop, done, closeCapture)
	if err := stopDetachedPlayback(playback); err != nil {
		slog.Warn("portaudio playback: close failed during refresh", "err", err)
	}
	if wasInitialized {
		portaudio.Terminate()
		time.Sleep(deviceRefreshDelay)
	}
	initErr := portaudio.Initialize()
	d.mu.Lock()

	if captureErr != nil {
		slog.Warn("portaudio capture: close failed during refresh", "err", captureErr)
	}
	if initErr != nil {
		d.initialized = false
		d.needsRefresh = true
		return fmt.Errorf("portaudio reinit after %s: %w", reason, initErr)
	}
	d.initialized = true
	slog.Info("portaudio: device list refreshed", "reason", reason)
	return nil
}

func (d *Driver) openCaptureWithRetryLocked() error {
	var errs []error
	for attempt := 0; attempt < deviceOpenAttempts; attempt++ {
		if err := d.openCaptureLocked(); err != nil {
			errs = append(errs, err)
		} else {
			return nil
		}
		if attempt == deviceOpenAttempts-1 {
			break
		}
		d.mu.Unlock()
		time.Sleep(deviceOpenRetry * time.Duration(attempt+1))
		d.mu.Lock()
		if !d.initialized || d.captureHandler == nil {
			return errors.Join(errs...)
		}
	}
	return errors.Join(errs...)
}

func (d *Driver) openPlaybackWithRetryLocked() error {
	var errs []error
	for attempt := 0; attempt < deviceOpenAttempts; attempt++ {
		if err := d.openPlaybackLocked(); err != nil {
			errs = append(errs, err)
		} else {
			return nil
		}
		if attempt == deviceOpenAttempts-1 {
			break
		}
		d.mu.Unlock()
		time.Sleep(deviceOpenRetry * time.Duration(attempt+1))
		d.mu.Lock()
		if !d.initialized || !d.playbackReady {
			return errors.Join(errs...)
		}
	}
	return errors.Join(errs...)
}

func (d *Driver) detachCaptureLocked() (*portaudio.Stream, chan struct{}, chan struct{}, func()) {
	stream := d.stream
	stop := d.stop
	done := d.done
	closeCapture := d.closeCapture
	d.stream = nil
	d.stop = nil
	d.done = nil
	d.closeCapture = nil
	d.deviceName = ""
	return stream, stop, done, closeCapture
}

func (d *Driver) detachPlaybackLocked() *portaudio.Stream {
	stream := d.playbackStream
	d.playbackStream = nil
	d.playbackBuffer = nil
	d.playbackDevName = ""
	return stream
}

func (d *Driver) markNeedsRefresh() {
	d.mu.Lock()
	d.needsRefresh = true
	d.mu.Unlock()
}

func stopDetachedCapture(stream *portaudio.Stream, stop chan struct{}, done chan struct{}, closeStream func()) error {
	if stop == nil {
		if closeStream != nil {
			closeStream()
		}
		return nil
	}
	close(stop)
	closeDone := make(chan struct{})
	if closeStream != nil {
		go func() {
			defer close(closeDone)
			closeStream()
		}()
	} else {
		close(closeDone)
	}
	timeout := time.NewTimer(captureStopTimeout)
	defer timeout.Stop()
	frameLoopDone := done == nil
	streamClosed := false
	var timedOut bool
	for !frameLoopDone || !streamClosed {
		if timedOut {
			err := fmt.Errorf("portaudio capture stop timed out after %s", captureStopTimeout)
			slog.Warn("portaudio capture: stop timed out", "err", err, "frameLoopDone", frameLoopDone, "streamClosed", streamClosed)
			return err
		}
		var doneCh <-chan struct{}
		if !frameLoopDone {
			doneCh = done
		}
		var closeCh <-chan struct{}
		if !streamClosed {
			closeCh = closeDone
		}
		select {
		case <-doneCh:
			frameLoopDone = true
		case <-closeCh:
			streamClosed = true
		case <-timeout.C:
			timedOut = true
		}
	}
	return nil
}

func closePortAudioStreamOnce(label string, stream *portaudio.Stream) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if stream == nil {
				return
			}
			if err := stream.Abort(); err != nil {
				slog.Debug("portaudio "+label+": abort failed", "err", err)
			}
			if err := stream.Close(); err != nil {
				slog.Debug("portaudio "+label+": close failed", "err", err)
			}
		})
	}
}

func stopDetachedPlayback(stream *portaudio.Stream) error {
	if stream == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = stream.Abort()
		_ = stream.Close()
	}()
	select {
	case <-done:
		slog.Info("portaudio playback: stopped")
		return nil
	case <-time.After(deviceCloseTimeout):
		err := fmt.Errorf("portaudio playback close timed out after %s", deviceCloseTimeout)
		slog.Warn("portaudio playback: close timed out", "err", err)
		return err
	}
}

func (d *Driver) ensureWatcherLocked() {
	if d.watcherStop != nil {
		return
	}
	d.watcherStop = make(chan struct{})
	d.watcherDone = make(chan struct{})
	go d.watchDevices(d.watcherStop, d.watcherDone)
}

func (d *Driver) stopWatcher() {
	d.mu.Lock()
	stop := d.watcherStop
	done := d.watcherDone
	d.watcherStop = nil
	d.watcherDone = nil
	d.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

func (d *Driver) watchDevices(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(deviceWatchEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			d.checkDeviceChange()
		}
	}
}

func (d *Driver) checkDeviceChange() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.initialized {
		return
	}
	captureDied := d.captureHandler != nil && d.done != nil && isClosed(d.done)
	captureMissing := d.captureHandler != nil && d.stream == nil
	playbackMissing := d.playbackReady && d.playbackStream == nil
	captureChanged := d.captureHandler != nil && d.deviceName != "" && defaultInputChanged(d.deviceName)
	playbackChanged := d.playbackReady && d.playbackDevName != "" && defaultOutputChanged(d.playbackDevName)
	coreAudioChanged := coreAudioDeviceChanged()
	if !d.needsRefresh && !captureDied && !captureMissing && !playbackMissing && !captureChanged && !playbackChanged && !coreAudioChanged {
		return
	}
	slog.Info(
		"portaudio: device change detected, refreshing",
		"pending", d.needsRefresh,
		"captureDied", captureDied,
		"captureMissing", captureMissing,
		"captureChanged", captureChanged,
		"playbackMissing", playbackMissing,
		"playbackChanged", playbackChanged,
		"coreAudioChanged", coreAudioChanged,
	)
	_, _ = d.fallbackRefreshOpenLocked("device change")
}

func defaultInputChanged(current string) bool {
	dev, err := portaudio.DefaultInputDevice()
	if err != nil || dev == nil {
		return true
	}
	return dev.Name != current
}

func defaultOutputChanged(current string) bool {
	dev, err := portaudio.DefaultOutputDevice()
	if err != nil || dev == nil {
		return true
	}
	return dev.Name != current
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (d *Driver) framesPerBuffer() int {
	return d.inputFormat.SampleRate * d.frameMillis / 1000
}

func (d *Driver) outputFramesPerBuffer() int {
	return d.outputFormat.SampleRate * d.frameMillis / 1000
}

func int16ToBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, sample := range samples {
		out[i*2] = byte(sample)
		out[i*2+1] = byte(uint16(sample) >> 8)
	}
	return out
}

func bytesToInt16LE(src []byte, dst []int16) {
	for i := range dst {
		base := i * 2
		if base+1 >= len(src) {
			dst[i] = 0
			continue
		}
		dst[i] = int16(uint16(src[base]) | uint16(src[base+1])<<8)
	}
}

func isInputOverflow(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "input overflow")
}

func isOutputUnderflow(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "output underflow")
}
