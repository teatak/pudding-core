//go:build !windows

package daemon

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/audio/driver/portaudio"
	"github.com/teatak/pudding-core/internal/config"
)

func TestNativeVoiceWiringKeepsCaptureAndInstaller(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultAudioConfig()
	driver := defaultCaptureDriver(cfg)
	if _, ok := driver.(*portaudio.Driver); !ok {
		t.Fatalf("native capture driver = %T", driver)
	}
	service, installer := newVoiceRuntime(dir, config.NewManager(dir), cfg, nil, nil)
	if service == nil || installer == nil {
		t.Fatal("native voice runtime was removed")
	}
	t.Cleanup(func() { _ = service.Close() })
	if status := installer.Status(context.Background(), cfg); status.State == "unsupported" {
		t.Fatal("native voice marked unsupported")
	}
}
