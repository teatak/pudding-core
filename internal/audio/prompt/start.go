// Package prompt provides the built-in sounds used by the audio runtime.
package prompt

import (
	_ "embed"
	"errors"

	"github.com/teatak/pudding-core/internal/audio/dsp/resample"
	"github.com/teatak/pudding-core/internal/audio/frame"
)

const startSampleRate = 24000

// startPCM is the PCM16 payload from the predecessor runtime's
// runtime/assets/prompt/start.wav.
//
//go:embed assets/start.pcm16
var startPCM []byte

// Start returns the input-start prompt in the requested playback format.
func Start(format frame.Format) (frame.PCM16, error) {
	if !format.Valid() {
		return frame.PCM16{}, errors.New("audio prompt: invalid output format")
	}
	data := append([]byte(nil), startPCM...)
	if format.SampleRate != startSampleRate {
		converter := resample.NewLinear(startSampleRate, format.SampleRate)
		data = converter.Process(data)
	}
	if format.Channels > 1 {
		data = duplicateMonoChannels(data, format.Channels)
	}
	return frame.PCM16{Format: format, Data: data}, nil
}

func duplicateMonoChannels(mono []byte, channels int) []byte {
	out := make([]byte, len(mono)*channels)
	for src := 0; src+1 < len(mono); src += 2 {
		dst := src * channels
		for channel := 0; channel < channels; channel++ {
			out[dst+channel*2] = mono[src]
			out[dst+channel*2+1] = mono[src+1]
		}
	}
	return out
}
