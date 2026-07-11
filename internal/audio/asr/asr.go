// Package asr defines speech-to-text contracts for the session voice pipeline.
package asr

import (
	"context"
	"time"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

type EventKind string

const (
	EventPartial  EventKind = "partial"
	EventSentence EventKind = "sentence"
	EventError    EventKind = "error"
)

type Event struct {
	Kind           EventKind
	StreamID       string
	Text           string
	Language       string
	Emotion        string
	Audio          frame.PCM16
	AudioDuration  time.Duration
	DecodeDuration time.Duration
	Err            error
}

type Client interface {
	Name() string
	Start(ctx context.Context) error
	Feed(ctx context.Context, pcm frame.PCM16) error
	Events() <-chan Event
	Stop(ctx context.Context) error
}

// StreamResetter isolates consecutive capture runs without restarting the
// usually expensive ASR model. Events produced for a stream carry StreamID.
type StreamResetter interface {
	ResetStream(ctx context.Context, streamID string) error
}
