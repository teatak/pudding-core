package daemon

import (
	"github.com/teatak/pudding-core/internal/config"
	"testing"
)

func TestWindowsDoesNotCreateVoiceOrModelInstaller(t *testing.T) {
	dir := t.TempDir()
	voice, installer := newVoiceRuntime(dir, config.NewManager(dir), config.DefaultAudioConfig(), nil, nil)
	if voice != nil || installer != nil {
		t.Fatal("Windows audio must remain unavailable until capture and ASR are implemented")
	}
}
