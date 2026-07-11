package asr

import (
	"context"
	"sync"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

// Fake is a controllable ASR client for service tests and wiring.
type Fake struct {
	mu       sync.Mutex
	events   chan Event
	streamID string
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

func (f *Fake) ResetStream(_ context.Context, streamID string) error {
	f.mu.Lock()
	f.streamID = streamID
	f.mu.Unlock()
	return nil
}

func (f *Fake) Events() <-chan Event { return f.events }

func (f *Fake) Stop(context.Context) error {
	close(f.events)
	return nil
}

func (f *Fake) EmitPartial(text string) {
	f.Emit(Event{Kind: EventPartial, Text: text})
}

func (f *Fake) EmitSentence(text string) {
	f.Emit(Event{Kind: EventSentence, Text: text})
}

func (f *Fake) EmitError(err error) {
	f.Emit(Event{Kind: EventError, Err: err})
}

func (f *Fake) Emit(event Event) {
	if event.StreamID == "" {
		event.StreamID = f.currentStreamID()
	}
	f.events <- event
}

func (f *Fake) currentStreamID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamID
}
