package ns

import "github.com/teatak/pudding-core/internal/audio/frame"

type Noop struct{}

func NewNoop() *Noop { return &Noop{} }

func (*Noop) Name() string { return "noop" }

func (*Noop) Process(f frame.PCM16) (frame.PCM16, error) { return f, nil }

func (*Noop) Reset() {}
