package prompt

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/audio/frame"
)

func TestStartUsesBuiltInPrompt(t *testing.T) {
	format := frame.Format{SampleRate: 24000, Channels: 1}
	pcm, err := Start(format)
	if err != nil {
		t.Fatal(err)
	}
	if pcm.Format != format {
		t.Fatalf("format = %+v, want %+v", pcm.Format, format)
	}
	if got := pcm.Duration(); got < 1580*time.Millisecond || got > 1590*time.Millisecond {
		t.Fatalf("duration = %v, want about 1.58s", got)
	}
	nonZero := false
	for offset := 0; offset+1 < len(pcm.Data); offset += 2 {
		if int16(binary.LittleEndian.Uint16(pcm.Data[offset:offset+2])) != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatal("built-in prompt is silent")
	}
}

func TestStartConvertsToPlaybackFormat(t *testing.T) {
	format := frame.Format{SampleRate: 16000, Channels: 2}
	pcm, err := Start(format)
	if err != nil {
		t.Fatal(err)
	}
	if pcm.Format != format {
		t.Fatalf("format = %+v, want %+v", pcm.Format, format)
	}
	for offset := 0; offset+3 < len(pcm.Data); offset += 4 {
		if pcm.Data[offset] != pcm.Data[offset+2] || pcm.Data[offset+1] != pcm.Data[offset+3] {
			t.Fatalf("stereo channels differ at byte %d", offset)
		}
	}
}
