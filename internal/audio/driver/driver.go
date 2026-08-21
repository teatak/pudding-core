// Package driver defines daemon-owned audio device contracts.
package driver

import (
	"context"
	"errors"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

var (
	ErrNotStarted      = errors.New("audio driver: not started")
	ErrNilHandler      = errors.New("audio driver: nil capture handler")
	ErrCaptureNoSignal = errors.New("audio driver: capture has no signal")
)

type CaptureHandler func(frame.PCM16)

// InputRoutePrimer prepares a wireless voice route before capture. It may keep
// writing pcm after returning and keeps the playback route open until StopPlayback.
type InputRoutePrimer interface {
	PrimeInputRoute(ctx context.Context, pcm frame.PCM16) error
}

// Driver owns local hardware access. Sessions may bind to capture/playback,
// but they do not own the device implementation.
type Driver interface {
	Name() string
	Init(ctx context.Context) error
	Close() error

	InputFormat() frame.Format
	OutputFormat() frame.Format

	StartCapture(ctx context.Context, onFrame CaptureHandler) error
	StopCapture(ctx context.Context) error

	StartPlayback(ctx context.Context) error
	WritePlayback(ctx context.Context, pcm frame.PCM16) error
	StopPlayback(ctx context.Context) error
}
