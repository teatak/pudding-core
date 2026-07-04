package tts

import (
	"context"
	"sync"
)

type Fake struct {
	mu        sync.Mutex
	requests  []Request
	cancelled []string
	events    chan Event
	requestCh chan Request
}

func NewFake(buffer int) *Fake {
	if buffer <= 0 {
		buffer = 16
	}
	return &Fake{
		events:    make(chan Event, buffer),
		requestCh: make(chan Request, buffer),
	}
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Start(context.Context) error { return nil }

func (f *Fake) Speak(ctx context.Context, req Request) error {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	select {
	case f.requestCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	f.emit(Event{Kind: EventStarted, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID})
	f.emit(Event{Kind: EventEnded, SessionID: req.SessionID, TurnID: req.TurnID, SegmentID: req.SegmentID})
	return nil
}

func (f *Fake) Cancel(ctx context.Context, turnID string) error {
	f.mu.Lock()
	f.cancelled = append(f.cancelled, turnID)
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (f *Fake) Events() <-chan Event { return f.events }

func (f *Fake) Stop(context.Context) error { return nil }

func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

func (f *Fake) Cancelled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelled...)
}

func (f *Fake) WaitRequest(ctx context.Context) (Request, bool) {
	select {
	case req := <-f.requestCh:
		return req, true
	case <-ctx.Done():
		return Request{}, false
	}
}

func (f *Fake) emit(ev Event) {
	select {
	case f.events <- ev:
	default:
	}
}
