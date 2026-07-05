// Package aec provides acoustic echo cancellation processors.
package aec

import "github.com/teatak/pudding-core/internal/audio/frame"

type Processor interface {
	Name() string
	PushRender(frame.PCM16) error
	ProcessCapture(frame.PCM16) (frame.PCM16, error)
	Reset()
}

type Closer interface {
	Close() error
}
