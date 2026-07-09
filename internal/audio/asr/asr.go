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
