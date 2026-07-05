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
	input           []int16
	stop            chan struct{}
	done            chan struct{}
	deviceName      string
	playbackStream  *portaudio.Stream
	playbackBuffer  []int16
	playbackReady   bool
	playbackDevName string
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
	slog.Info("portaudio: initialized", "input", d.inputFormat, "output", d.outputFormat)
	d.initialized = true
	return nil
}

func (d *Driver) Close() error {
	_ = d.StopCapture(context.Background())
	_ = d.StopPlayback(context.Background())
	d.mu.Lock()
	initialized := d.initialized
	d.initialized = false
	d.mu.Unlock()
	if initialized {
		portaudio.Terminate()
		slog.Info("portaudio: terminated")
	}
	return nil
}

func (d *Driver) InputFormat() frame.Format  { return d.inputFormat }
func (d *Driver) OutputFormat() frame.Format { return d.outputFormat }

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
		slog.Info("portaudio capture: already running", "device", d.deviceName)
		return nil
	}

	input := make([]int16, framesPerBuffer*d.inputFormat.Channels)
	stream, err := portaudio.OpenDefaultStream(
		d.inputFormat.Channels,
		0,
		float64(d.inputFormat.SampleRate),
		framesPerBuffer,
		input,
	)
	if err != nil {
		return fmt.Errorf("portaudio capture open: %w", err)
	}
	if err := stream.Start(); err != nil {
		_ = stream.Close()
		return fmt.Errorf("portaudio capture start: %w", err)
	}
	deviceName := ""
	if dev, err := portaudio.DefaultInputDevice(); err == nil && dev != nil {
		deviceName = dev.Name
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	d.stream = stream
	d.input = input
	d.stop = stop
	d.done = done
	d.deviceName = deviceName
	go d.captureLoop(ctx, stream, input, stop, done, onFrame)
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

func (d *Driver) StopCapture(context.Context) error {
	d.mu.Lock()
	stop := d.stop
	done := d.done
	d.stream = nil
	d.input = nil
	d.stop = nil
	d.done = nil
	d.deviceName = ""
	d.mu.Unlock()

	if stop == nil {
		return nil
	}
	close(stop)
	<-done
	slog.Info("portaudio capture: stopped")
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
	output := make([]int16, framesPerBuffer*d.outputFormat.Channels)
	stream, err := portaudio.OpenDefaultStream(
		0,
		d.outputFormat.Channels,
		float64(d.outputFormat.SampleRate),
		framesPerBuffer,
		output,
	)
	if err != nil {
		return fmt.Errorf("portaudio playback open: %w", err)
	}
	if err := stream.Start(); err != nil {
		_ = stream.Close()
		return fmt.Errorf("portaudio playback start: %w", err)
	}
	deviceName := ""
	if dev, err := portaudio.DefaultOutputDevice(); err == nil && dev != nil {
		deviceName = dev.Name
	}
	d.playbackStream = stream
	d.playbackBuffer = output
	d.playbackReady = true
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
			return fmt.Errorf("portaudio playback write: %w", err)
		}
	}
	return nil
}

func (d *Driver) StopPlayback(context.Context) error {
	d.mu.Lock()
	stream := d.playbackStream
	d.playbackStream = nil
	d.playbackBuffer = nil
	d.playbackReady = false
	d.playbackDevName = ""
	d.mu.Unlock()
	if stream == nil {
		return nil
	}
	_ = stream.Stop()
	_ = stream.Close()
	slog.Info("portaudio playback: stopped")
	return nil
}

func (d *Driver) captureLoop(ctx context.Context, stream *portaudio.Stream, input []int16, stop <-chan struct{}, done chan<- struct{}, onFrame driver.CaptureHandler) {
	defer close(done)
	defer func() {
		_ = stream.Stop()
		_ = stream.Close()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		default:
		}
		if err := stream.Read(); err != nil {
			if isInputOverflow(err) {
				slog.Debug("portaudio capture: input overflow, dropping frame")
				continue
			}
			slog.Warn("portaudio capture: read failed", "err", err)
			return
		}
		onFrame(frame.PCM16{
			Format:    d.inputFormat,
			Data:      int16ToBytes(input),
			Timestamp: time.Now(),
		})
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
