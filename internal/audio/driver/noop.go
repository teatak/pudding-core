package driver

import (
	"context"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

// Noop is a deterministic driver for wiring and tests.
type Noop struct {
	mu        sync.Mutex
	name      string
	format    frame.Format
	capturing bool
	playing   bool
	closed    bool
}

func NewNoop(name string, format frame.Format) *Noop {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "noop"
	}
	if !format.Valid() {
		format = frame.Format{SampleRate: 16000, Channels: 1}
	}
	return &Noop{name: name, format: format}
}

func (d *Noop) Name() string { return d.name }

func (d *Noop) Init(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = false
	return nil
}

func (d *Noop) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.capturing = false
	d.playing = false
	d.closed = true
	return nil
}

func (d *Noop) InputFormat() frame.Format  { return d.format }
func (d *Noop) OutputFormat() frame.Format { return d.format }

func (d *Noop) StartCapture(_ context.Context, onFrame CaptureHandler) error {
	if onFrame == nil {
		return ErrNilHandler
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.capturing = true
	return nil
}

func (d *Noop) StopCapture(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.capturing = false
	return nil
}

func (d *Noop) StartPlayback(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.playing = true
	return nil
}

func (d *Noop) WritePlayback(context.Context, frame.PCM16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.playing {
		return ErrNotStarted
	}
	return nil
}

func (d *Noop) StopPlayback(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.playing = false
	return nil
}
