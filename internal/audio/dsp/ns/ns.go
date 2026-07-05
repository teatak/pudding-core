// Package ns provides noise suppression processors.
package ns

import "github.com/teatak/pudding-core/internal/audio/frame"

const (
	LevelOff      = "off"
	LevelLow      = "low"
	LevelModerate = "moderate"
	LevelHigh     = "high"
	LevelVeryHigh = "very_high"
)

type Processor interface {
	Name() string
	Process(frame.PCM16) (frame.PCM16, error)
	Reset()
}

type Closer interface {
	Close() error
}

func NormalizeLevel(level string) string {
	switch level {
	case LevelOff, LevelLow, LevelModerate, LevelHigh, LevelVeryHigh:
		return level
	default:
		return LevelModerate
	}
}
