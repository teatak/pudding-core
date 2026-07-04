package tts

import "context"

type Noop struct {
	events chan Event
}

func NewNoop() *Noop {
	events := make(chan Event)
	close(events)
	return &Noop{events: events}
}

func (n *Noop) Name() string { return "noop" }

func (n *Noop) Start(context.Context) error { return nil }

func (n *Noop) Speak(context.Context, Request) error { return nil }

func (n *Noop) Cancel(context.Context, string) error { return nil }

func (n *Noop) Events() <-chan Event { return n.events }

func (n *Noop) Stop(context.Context) error { return nil }
