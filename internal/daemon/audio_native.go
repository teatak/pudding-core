//go:build !windows

package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	audioasr "github.com/teatak/pudding-core/internal/audio/asr"
	sherpaasr "github.com/teatak/pudding-core/internal/audio/asr/sherpa"
	audiodriver "github.com/teatak/pudding-core/internal/audio/driver"
	portaudiodriver "github.com/teatak/pudding-core/internal/audio/driver/portaudio"
	aecproc "github.com/teatak/pudding-core/internal/audio/dsp/aec"
	nsproc "github.com/teatak/pudding-core/internal/audio/dsp/ns"
	"github.com/teatak/pudding-core/internal/audio/frame"
	"github.com/teatak/pudding-core/internal/audio/runtimeassets"
	"github.com/teatak/pudding-core/internal/audio/voice"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
)

func newVoiceRuntime(dir string, cfg *config.Manager, audioCfg config.AudioConfig, eng *engine.Engine, hub *event.Hub) (*voice.Service, *runtimeassets.Installer) {
	audioDriver := defaultCaptureDriver(audioCfg)
	voiceService := voice.NewService(voice.ServiceConfig{
		Manager:   voice.NewManager(),
		Submitter: eng,
		Events:    hub,
		Driver:    audioDriver,
		ASR:       defaultASR(dir, audioCfg),
		AEC:       defaultAEC(audioCfg, audioDriver),
		NS:        defaultNS(audioCfg, audioDriver),
		HomeDir:   dir,
		SaveAudio: audioCfg.ASRSaveAudio(),
		MinEnergy: audioCfg.ASR.VAD.MinEnergy,
	})
	audioRuntime := runtimeassets.NewInstaller(dir, func(ctx context.Context) error {
		currentAudio, err := cfg.Audio(ctx)
		if err != nil {
			return err
		}
		return voiceService.ReplaceASR(defaultASR(dir, currentAudio))
	})
	return voiceService, audioRuntime
}

func defaultCaptureDriver(cfg config.AudioConfig) audiodriver.Driver {
	cfg = cfg.WithDefaults()
	if strings.ToLower(strings.TrimSpace(cfg.Driver.Type)) != "portaudio" {
		slog.Warn("daemon: unsupported audio driver", "driver", cfg.Driver.Type)
		return nil
	}
	return portaudiodriver.New(portaudiodriver.Config{
		InputFormat:  frame.Format{SampleRate: cfg.Driver.CaptureSampleRate, Channels: cfg.Driver.Channels},
		OutputFormat: frame.Format{SampleRate: cfg.Driver.PlaybackSampleRate, Channels: cfg.Driver.Channels},
		FrameMillis:  cfg.Driver.PeriodMillis,
	})
}

func defaultAEC(cfg config.AudioConfig, drv audiodriver.Driver) aecproc.Processor {
	cfg = cfg.WithDefaults()
	if !cfg.AECEnabled() {
		return nil
	}
	if drv == nil {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(cfg.AEC.Model)) != "webrtc" {
		slog.Warn("daemon: unsupported aec model", "model", cfg.AEC.Model)
		return nil
	}
	format := drv.InputFormat()
	client, err := aecproc.NewWebRTCAEC(aecproc.WebRTCAECConfig{
		SampleRate:   format.SampleRate,
		Channels:     format.Channels,
		PeriodMillis: cfg.Driver.PeriodMillis,
	})
	if err != nil {
		slog.Warn("daemon: WebRTC AEC unavailable", "err", err)
		return nil
	}
	slog.Info("daemon: WebRTC AEC configured", "name", client.Name(), "sampleRate", format.SampleRate, "channels", format.Channels)
	return client
}

func defaultNS(cfg config.AudioConfig, drv audiodriver.Driver) nsproc.Processor {
	cfg = cfg.WithDefaults()
	if !cfg.NSEnabled() {
		return nil
	}
	if drv == nil {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(cfg.NS.Model)) != "webrtc" {
		slog.Warn("daemon: unsupported ns model", "model", cfg.NS.Model)
		return nil
	}
	format := drv.InputFormat()
	client, err := nsproc.NewWebRTCNS(nsproc.WebRTCNSConfig{
		SampleRate:   format.SampleRate,
		Channels:     format.Channels,
		PeriodMillis: cfg.Driver.PeriodMillis,
		Level:        cfg.NS.Level,
	})
	if err != nil {
		slog.Warn("daemon: WebRTC NS unavailable", "err", err)
		return nil
	}
	slog.Info("daemon: WebRTC NS configured", "name", client.Name(), "level", client.Level(), "sampleRate", format.SampleRate, "channels", format.Channels)
	return client
}

func defaultASR(homeDir string, audioCfg config.AudioConfig) audioasr.Client {
	audioCfg = audioCfg.WithDefaults()
	if !audioCfg.ASREnabled() {
		slog.Info("daemon: asr disabled by config")
		return nil
	}
	cfg, ok := defaultSherpaConfig(homeDir, audioCfg)
	if !ok {
		slog.Warn("daemon: sherpa ASR models unavailable")
		return nil
	}
	client, err := sherpaasr.New(cfg)
	if err != nil {
		slog.Warn("daemon: sherpa ASR unavailable", "err", err)
		return nil
	}
	return client
}

func defaultSherpaConfig(homeDir string, audioCfg config.AudioConfig) (sherpaasr.Config, bool) {
	audioCfg = audioCfg.WithDefaults()
	asrCfg := audioCfg.ASR
	cfg := sherpaasr.Config{
		ModelPath:                   resolveAudioPath(homeDir, asrCfg.ModelPath, "asr"),
		TokensPath:                  resolveAudioPath(homeDir, asrCfg.TokensPath, "asr"),
		VADModelPath:                resolveAudioPath(homeDir, asrCfg.VAD.ModelPath, "vad"),
		Language:                    asrCfg.Language,
		UseInverseTextNormalization: audioCfg.ASRUseITN(),
		VADThreshold:                asrCfg.VAD.Threshold,
		MinSilenceDuration:          time.Duration(asrCfg.VAD.MinSilenceMillis) * time.Millisecond,
		MinSpeechDuration:           time.Duration(asrCfg.VAD.MinSpeechMillis) * time.Millisecond,
		VADWindowSize:               asrCfg.VAD.WindowSize,
		PrerollDuration:             time.Duration(asrCfg.VAD.PrerollMillis) * time.Millisecond,
		Provider:                    asrCfg.Provider,
		NumThreads:                  asrCfg.NumThreads,
	}
	if strings.ToLower(strings.TrimSpace(asrCfg.Engine)) != "sherpa-sensevoice" {
		slog.Warn("daemon: unsupported asr engine", "engine", asrCfg.Engine)
		return cfg, false
	}
	if fileExists(cfg.ModelPath) && fileExists(cfg.TokensPath) && fileExists(cfg.VADModelPath) {
		return cfg, true
	}
	return cfg, false
}

func resolveAudioPath(homeDir, raw, modelSubdir string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) {
		return raw
	}
	clean := filepath.Clean(raw)
	if clean == "." {
		return ""
	}
	if strings.ContainsRune(clean, filepath.Separator) || strings.HasPrefix(clean, "runtime") {
		return filepath.Join(homeDir, clean)
	}
	return filepath.Join(homeDir, "runtime", "models", modelSubdir, clean)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
