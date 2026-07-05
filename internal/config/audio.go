package config

import (
	"context"
	"strings"
)

const audioFile = "audio.yaml"

type AudioConfig struct {
	Version int               `yaml:"version"`
	Driver  AudioDriverConfig `yaml:"driver"`
	ASR     AudioASRConfig    `yaml:"asr"`
	TTS     AudioTTSConfig    `yaml:"tts"`
}

type AudioDriverConfig struct {
	Type               string `yaml:"type"`
	CaptureSampleRate  int    `yaml:"capture_sample_rate"`
	PlaybackSampleRate int    `yaml:"playback_sample_rate"`
	Channels           int    `yaml:"channels"`
	PeriodMillis       int    `yaml:"period_millis"`
}

type AudioASRConfig struct {
	Enabled                     *bool             `yaml:"enabled,omitempty"`
	Engine                      string            `yaml:"engine"`
	ModelPath                   string            `yaml:"model_path"`
	TokensPath                  string            `yaml:"tokens_path"`
	Language                    string            `yaml:"language"`
	UseInverseTextNormalization *bool             `yaml:"use_itn,omitempty"`
	NumThreads                  int               `yaml:"num_threads"`
	Provider                    string            `yaml:"provider"`
	VAD                         AudioASRVADConfig `yaml:"vad"`
}

type AudioASRVADConfig struct {
	ModelPath        string  `yaml:"model_path"`
	Threshold        float64 `yaml:"threshold"`
	MinSilenceMillis int     `yaml:"min_silence_millis"`
	MinSpeechMillis  int     `yaml:"min_speech_millis"`
	WindowSize       int     `yaml:"window_size"`
}

type AudioTTSConfig struct {
	Enabled  *bool                      `yaml:"enabled,omitempty"`
	Profile  string                     `yaml:"profile"`
	Profiles map[string]AudioTTSProfile `yaml:"profiles"`
}

type AudioTTSProfile struct {
	Voice string  `yaml:"voice,omitempty"`
	Speed float64 `yaml:"speed,omitempty"`
}

func DefaultAudioConfig() AudioConfig {
	on := true
	off := false
	return AudioConfig{
		Version: 1,
		Driver: AudioDriverConfig{
			Type:               "portaudio",
			CaptureSampleRate:  16000,
			PlaybackSampleRate: 24000,
			Channels:           1,
			PeriodMillis:       20,
		},
		ASR: AudioASRConfig{
			Enabled:                     &on,
			Engine:                      "sherpa-sensevoice",
			ModelPath:                   "runtime/models/asr/model.int8.onnx",
			TokensPath:                  "runtime/models/asr/tokens.txt",
			Language:                    "zh",
			UseInverseTextNormalization: &off,
			NumThreads:                  2,
			Provider:                    "cpu",
			VAD: AudioASRVADConfig{
				ModelPath:        "runtime/models/vad/silero_vad.onnx",
				Threshold:        0.6,
				MinSilenceMillis: 400,
				MinSpeechMillis:  300,
				WindowSize:       512,
			},
		},
		TTS: AudioTTSConfig{
			Enabled: &on,
			Profile: "edge",
			Profiles: map[string]AudioTTSProfile{
				"edge": {
					Voice: "zh-CN-YunxiaNeural",
					Speed: 1.2,
				},
			},
		},
	}
}

func (m *Manager) Audio(_ context.Context) (AudioConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readAudio()
}

func (c AudioConfig) WithDefaults() AudioConfig {
	d := DefaultAudioConfig()
	if c.Version == 0 {
		c.Version = d.Version
	}
	if strings.TrimSpace(c.Driver.Type) == "" {
		c.Driver.Type = d.Driver.Type
	}
	if c.Driver.CaptureSampleRate <= 0 {
		c.Driver.CaptureSampleRate = d.Driver.CaptureSampleRate
	}
	if c.Driver.PlaybackSampleRate <= 0 {
		c.Driver.PlaybackSampleRate = d.Driver.PlaybackSampleRate
	}
	if c.Driver.Channels <= 0 {
		c.Driver.Channels = d.Driver.Channels
	}
	if c.Driver.PeriodMillis <= 0 {
		c.Driver.PeriodMillis = d.Driver.PeriodMillis
	}
	if c.ASR.Enabled == nil {
		c.ASR.Enabled = d.ASR.Enabled
	}
	if strings.TrimSpace(c.ASR.Engine) == "" {
		c.ASR.Engine = d.ASR.Engine
	}
	if strings.TrimSpace(c.ASR.ModelPath) == "" {
		c.ASR.ModelPath = d.ASR.ModelPath
	}
	if strings.TrimSpace(c.ASR.TokensPath) == "" {
		c.ASR.TokensPath = d.ASR.TokensPath
	}
	if strings.TrimSpace(c.ASR.Language) == "" {
		c.ASR.Language = d.ASR.Language
	}
	if c.ASR.UseInverseTextNormalization == nil {
		c.ASR.UseInverseTextNormalization = d.ASR.UseInverseTextNormalization
	}
	if c.ASR.NumThreads <= 0 {
		c.ASR.NumThreads = d.ASR.NumThreads
	}
	if strings.TrimSpace(c.ASR.Provider) == "" {
		c.ASR.Provider = d.ASR.Provider
	}
	if strings.TrimSpace(c.ASR.VAD.ModelPath) == "" {
		c.ASR.VAD.ModelPath = d.ASR.VAD.ModelPath
	}
	if c.ASR.VAD.Threshold <= 0 {
		c.ASR.VAD.Threshold = d.ASR.VAD.Threshold
	}
	if c.ASR.VAD.MinSilenceMillis <= 0 {
		c.ASR.VAD.MinSilenceMillis = d.ASR.VAD.MinSilenceMillis
	}
	if c.ASR.VAD.MinSpeechMillis <= 0 {
		c.ASR.VAD.MinSpeechMillis = d.ASR.VAD.MinSpeechMillis
	}
	if c.ASR.VAD.WindowSize <= 0 {
		c.ASR.VAD.WindowSize = d.ASR.VAD.WindowSize
	}
	if c.TTS.Enabled == nil {
		c.TTS.Enabled = d.TTS.Enabled
	}
	if strings.TrimSpace(c.TTS.Profile) == "" {
		c.TTS.Profile = d.TTS.Profile
	}
	if c.TTS.Profiles == nil {
		c.TTS.Profiles = map[string]AudioTTSProfile{}
	}
	for name, profile := range d.TTS.Profiles {
		current := c.TTS.Profiles[name]
		if strings.TrimSpace(current.Voice) == "" {
			current.Voice = profile.Voice
		}
		if current.Speed <= 0 {
			current.Speed = profile.Speed
		}
		c.TTS.Profiles[name] = current
	}
	return c
}

func (c AudioConfig) ASREnabled() bool {
	return boolSetting(c.ASR.Enabled, true)
}

func (c AudioConfig) ASRUseITN() bool {
	return boolSetting(c.ASR.UseInverseTextNormalization, false)
}

func (c AudioConfig) TTSEnabled() bool {
	return boolSetting(c.TTS.Enabled, true)
}

func (c AudioConfig) ActiveTTSProfile() (string, AudioTTSProfile) {
	c = c.WithDefaults()
	name := strings.TrimSpace(c.TTS.Profile)
	profile := c.TTS.Profiles[name]
	return name, profile
}

func boolSetting(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func (m *Manager) readAudio() (AudioConfig, error) {
	var cfg AudioConfig
	if err := readYAML(m.path(audioFile), &cfg); err != nil {
		return cfg, err
	}
	return cfg.WithDefaults(), nil
}

func (m *Manager) writeAudio(cfg AudioConfig) error {
	return writeYAML(m.path(audioFile), cfg.WithDefaults())
}
