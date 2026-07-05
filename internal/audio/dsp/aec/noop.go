package aec

import "github.com/teatak/pudding-core/internal/audio/frame"

type Noop struct{}

func NewNoop() *Noop { return &Noop{} }

func (*Noop) Name() string { return "noop" }

func (*Noop) PushRender(frame.PCM16) error { return nil }

func (*Noop) ProcessCapture(f frame.PCM16) (frame.PCM16, error) { return f, nil }

func (*Noop) Reset() {}
