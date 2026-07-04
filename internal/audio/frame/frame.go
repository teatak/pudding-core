// Package frame defines small audio frame contracts shared by capture, ASR,
// TTS, and playback code. It intentionally stays transport-agnostic.
package frame

import "time"

// Format describes interleaved PCM16 audio.
type Format struct {
	SampleRate int `json:"sampleRate"`
	Channels   int `json:"channels"`
}

func (f Format) Valid() bool {
	return f.SampleRate > 0 && f.Channels > 0
}

func (f Format) BytesPerSecond() int {
	if !f.Valid() {
		return 0
	}
	return f.SampleRate * f.Channels * 2
}

func (f Format) DurationForBytes(n int) time.Duration {
	bps := f.BytesPerSecond()
	if n <= 0 || bps == 0 {
		return 0
	}
	return time.Duration(n) * time.Second / time.Duration(bps)
}

// PCM16 is one timestamped chunk of signed 16-bit little-endian PCM audio.
type PCM16 struct {
	Format    Format
	Data      []byte
	Timestamp time.Time
}

func (p PCM16) Duration() time.Duration {
	return p.Format.DurationForBytes(len(p.Data))
}
