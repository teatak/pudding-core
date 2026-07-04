package asr

import (
	"context"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

// Fake is a controllable ASR client for service tests and wiring.
type Fake struct {
	events chan Event
}

func NewFake(buffer int) *Fake {
	if buffer <= 0 {
		buffer = 16
	}
	return &Fake{events: make(chan Event, buffer)}
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Start(context.Context) error { return nil }

func (f *Fake) Feed(context.Context, frame.PCM16) error { return nil }

func (f *Fake) Events() <-chan Event { return f.events }

func (f *Fake) Stop(context.Context) error {
	close(f.events)
	return nil
}

func (f *Fake) EmitPartial(text string) {
	f.events <- Event{Kind: EventPartial, Text: text}
}

func (f *Fake) EmitSentence(text string) {
	f.events <- Event{Kind: EventSentence, Text: text}
}

func (f *Fake) EmitError(err error) {
	f.events <- Event{Kind: EventError, Err: err}
}
