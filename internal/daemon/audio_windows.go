package daemon

import (
	"github.com/teatak/pudding-core/internal/audio/runtimeassets"
	"github.com/teatak/pudding-core/internal/audio/voice"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
)

// Windows ships without audio until the Electron capture bridge and local ASR
// are delivered together. No native driver, fake recorder or model downloader.
func newVoiceRuntime(string, *config.Manager, config.AudioConfig, *engine.Engine, *event.Hub) (*voice.Service, *runtimeassets.Installer) {
	return nil, nil
}
