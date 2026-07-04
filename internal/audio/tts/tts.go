// Package tts defines text-to-speech contracts for assistant turn audio.
package tts

import (
	"context"
	"errors"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

var ErrBusy = errors.New("tts: speak queue full")

type EventKind string

const (
	EventStarted EventKind = "started"
	EventAudio   EventKind = "audio"
	EventEnded   EventKind = "ended"
	EventError   EventKind = "error"
)

type Request struct {
	SessionID string
	TurnID    string
	SegmentID string
	Text      string
}

type Event struct {
	Kind      EventKind
	SessionID string
	TurnID    string
	SegmentID string
	Audio     frame.PCM16
	Err       error
}

type Client interface {
	Name() string
	Start(ctx context.Context) error
	Speak(ctx context.Context, req Request) error
	Cancel(ctx context.Context, turnID string) error
	Events() <-chan Event
	Stop(ctx context.Context) error
}
