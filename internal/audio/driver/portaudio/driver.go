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
	Format      frame.Format
	FrameMillis int
}

type Driver struct {
	mu          sync.Mutex
	format      frame.Format
	frameMillis int
	initialized bool

	stream     *portaudio.Stream
	input      []int16
	stop       chan struct{}
	done       chan struct{}
	deviceName string
}

func New(cfg Config) *Driver {
	format := cfg.Format
	if !format.Valid() {
		format = frame.Format{SampleRate: 16000, Channels: 1}
	}
	frameMillis := cfg.FrameMillis
	if frameMillis <= 0 {
		frameMillis = 20
	}
	return &Driver{format: format, frameMillis: frameMillis}
}

func (d *Driver) Name() string { return "portaudio" }

func (d *Driver) Init(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.initialized {
		return nil
	}
	if err := requestMicrophonePermission(ctx); err != nil {
		return fmt.Errorf("portaudio microphone permission: %w", err)
	}
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("portaudio init: %w", err)
	}
	slog.Info("portaudio: initialized", "sampleRate", d.format.SampleRate, "channels", d.format.Channels)
	d.initialized = true
	return nil
}

func (d *Driver) Close() error {
	_ = d.StopCapture(context.Background())
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

func (d *Driver) InputFormat() frame.Format  { return d.format }
func (d *Driver) OutputFormat() frame.Format { return d.format }

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
	if !d.format.Valid() {
		return errors.New("portaudio capture: invalid format")
	}
	framesPerBuffer := d.framesPerBuffer()
	if framesPerBuffer <= 0 {
		return errors.New("portaudio capture: invalid frame duration")
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

	input := make([]int16, framesPerBuffer*d.format.Channels)
	stream, err := portaudio.OpenDefaultStream(
		d.format.Channels,
		0,
		float64(d.format.SampleRate),
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
		"sampleRate", d.format.SampleRate,
		"channels", d.format.Channels,
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

func (d *Driver) StartPlayback(context.Context) error { return driver.ErrNotStarted }
func (d *Driver) WritePlayback(context.Context, frame.PCM16) error {
	return driver.ErrNotStarted
}
func (d *Driver) StopPlayback(context.Context) error { return nil }

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
			Format:    d.format,
			Data:      int16ToBytes(input),
			Timestamp: time.Now(),
		})
	}
}

func (d *Driver) framesPerBuffer() int {
	return d.format.SampleRate * d.frameMillis / 1000
}

func int16ToBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, sample := range samples {
		out[i*2] = byte(sample)
		out[i*2+1] = byte(uint16(sample) >> 8)
	}
	return out
}

func isInputOverflow(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "input overflow")
}
