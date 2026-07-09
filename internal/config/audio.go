package config

import (
	"context"
	"fmt"
	"strings"
)

const audioFile = "audio.yaml"

type AudioConfig struct {
	Version int               `yaml:"version" json:"version"`
	Driver  AudioDriverConfig `yaml:"driver" json:"driver"`
	ASR     AudioASRConfig    `yaml:"asr" json:"asr"`
	AEC     AudioAECConfig    `yaml:"aec" json:"aec"`
	NS      AudioNSConfig     `yaml:"ns" json:"ns"`
	TTS     AudioTTSConfig    `yaml:"tts" json:"tts"`
}

type AudioDriverConfig struct {
	Type               string `yaml:"type" json:"type"`
	CaptureSampleRate  int    `yaml:"capture_sample_rate" json:"captureSampleRate"`
	PlaybackSampleRate int    `yaml:"playback_sample_rate" json:"playbackSampleRate"`
	Channels           int    `yaml:"channels" json:"channels"`
	PeriodMillis       int    `yaml:"period_millis" json:"periodMillis"`
}

type AudioASRConfig struct {
	Enabled                     *bool             `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	SaveAudio                   *bool             `yaml:"save_audio,omitempty" json:"saveAudio,omitempty"`
	Engine                      string            `yaml:"engine" json:"engine"`
	ModelPath                   string            `yaml:"model_path" json:"modelPath"`
	TokensPath                  string            `yaml:"tokens_path" json:"tokensPath"`
	Language                    string            `yaml:"language" json:"language"`
	UseInverseTextNormalization *bool             `yaml:"use_itn,omitempty" json:"useITN,omitempty"`
	NumThreads                  int               `yaml:"num_threads" json:"numThreads"`
	Provider                    string            `yaml:"provider" json:"provider"`
	VAD                         AudioASRVADConfig `yaml:"vad" json:"vad"`
}

type AudioASRVADConfig struct {
	ModelPath         string  `yaml:"model_path" json:"modelPath"`
	Threshold         float64 `yaml:"threshold" json:"threshold"`
	MinEnergy         float64 `yaml:"min_energy" json:"minEnergy"`
	PlaybackMinEnergy float64 `yaml:"playback_min_energy" json:"playbackMinEnergy"`
	MinSilenceMillis  int     `yaml:"min_silence_millis" json:"minSilenceMillis"`
	MinSpeechMillis   int     `yaml:"min_speech_millis" json:"minSpeechMillis"`
	WindowSize        int     `yaml:"window_size" json:"windowSize"`
	PrerollMillis     int     `yaml:"preroll_millis" json:"prerollMillis"`
}

type AudioAECConfig struct {
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Model   string `yaml:"model" json:"model"`
}

type AudioNSConfig struct {
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Model   string `yaml:"model" json:"model"`
	Level   string `yaml:"level" json:"level"`
}

type AudioTTSConfig struct {
	Enabled  *bool                      `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Profile  string                     `yaml:"profile" json:"profile"`
	Profiles map[string]AudioTTSProfile `yaml:"profiles" json:"profiles"`
}

type AudioTTSProfile struct {
	Voice string  `yaml:"voice,omitempty" json:"voice,omitempty"`
	Speed float64 `yaml:"speed,omitempty" json:"speed,omitempty"`
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
			SaveAudio:                   &off,
			Engine:                      "sherpa-sensevoice",
			ModelPath:                   "runtime/models/asr/model.int8.onnx",
			TokensPath:                  "runtime/models/asr/tokens.txt",
			Language:                    "zh",
			UseInverseTextNormalization: &off,
			NumThreads:                  2,
			Provider:                    "cpu",
			VAD: AudioASRVADConfig{
				ModelPath:         "runtime/models/vad/silero_vad.onnx",
				Threshold:         0.6,
				MinEnergy:         0.01,
				PlaybackMinEnergy: 0.015,
				MinSilenceMillis:  400,
				MinSpeechMillis:   300,
				WindowSize:        512,
				PrerollMillis:     500,
			},
		},
		AEC: AudioAECConfig{
			Enabled: &on,
			Model:   "webrtc",
		},
		NS: AudioNSConfig{
			Enabled: &on,
			Model:   "webrtc",
			Level:   "moderate",
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

func (m *Manager) SetAudio(_ context.Context, cfg AudioConfig) (AudioConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cfg.WithDefaults()
	if err := validateAudio(next); err != nil {
		return AudioConfig{}, err
	}
	if err := m.writeAudio(next); err != nil {
		return AudioConfig{}, err
	}
	return next, nil
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
	if c.ASR.SaveAudio == nil {
		c.ASR.SaveAudio = d.ASR.SaveAudio
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
	if c.ASR.VAD.MinEnergy <= 0 {
		c.ASR.VAD.MinEnergy = d.ASR.VAD.MinEnergy
	}
	if c.ASR.VAD.PlaybackMinEnergy <= 0 {
		c.ASR.VAD.PlaybackMinEnergy = d.ASR.VAD.PlaybackMinEnergy
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
	if c.ASR.VAD.PrerollMillis <= 0 {
		c.ASR.VAD.PrerollMillis = d.ASR.VAD.PrerollMillis
	}
	if c.AEC.Enabled == nil {
		c.AEC.Enabled = d.AEC.Enabled
	}
	if strings.TrimSpace(c.AEC.Model) == "" {
		c.AEC.Model = d.AEC.Model
	}
	if c.NS.Enabled == nil {
		c.NS.Enabled = d.NS.Enabled
	}
	if strings.TrimSpace(c.NS.Model) == "" {
		c.NS.Model = d.NS.Model
	}
	if strings.TrimSpace(c.NS.Level) == "" {
		c.NS.Level = d.NS.Level
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

func (c AudioConfig) ASRSaveAudio() bool {
	return boolSetting(c.ASR.SaveAudio, false)
}

func (c AudioConfig) TTSEnabled() bool {
	return boolSetting(c.TTS.Enabled, true)
}

func (c AudioConfig) AECEnabled() bool {
	return boolSetting(c.AEC.Enabled, true)
}

func (c AudioConfig) NSEnabled() bool {
	return boolSetting(c.NS.Enabled, true)
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

func validateAudio(cfg AudioConfig) error {
	if strings.ToLower(strings.TrimSpace(cfg.Driver.Type)) != "portaudio" {
		return fmt.Errorf("%w: unsupported audio driver", ErrInvalidSetting)
	}
	if cfg.Driver.CaptureSampleRate <= 0 || cfg.Driver.PlaybackSampleRate <= 0 || cfg.Driver.Channels <= 0 || cfg.Driver.PeriodMillis <= 0 {
		return fmt.Errorf("%w: invalid audio driver format", ErrInvalidSetting)
	}
	if strings.ToLower(strings.TrimSpace(cfg.ASR.Engine)) != "sherpa-sensevoice" {
		return fmt.Errorf("%w: unsupported asr engine", ErrInvalidSetting)
	}
	if strings.TrimSpace(cfg.ASR.ModelPath) == "" || strings.TrimSpace(cfg.ASR.TokensPath) == "" || strings.TrimSpace(cfg.ASR.VAD.ModelPath) == "" {
		return fmt.Errorf("%w: missing asr model path", ErrInvalidSetting)
	}
	if strings.TrimSpace(cfg.ASR.Language) == "" {
		return fmt.Errorf("%w: missing asr language", ErrInvalidSetting)
	}
	if cfg.ASR.NumThreads <= 0 {
		return fmt.Errorf("%w: invalid asr threads", ErrInvalidSetting)
	}
	if strings.TrimSpace(cfg.ASR.Provider) == "" {
		return fmt.Errorf("%w: missing asr provider", ErrInvalidSetting)
	}
	if cfg.ASR.VAD.Threshold < 0.01 || cfg.ASR.VAD.Threshold > 0.99 {
		return fmt.Errorf("%w: vad threshold must be 0.01..0.99", ErrInvalidSetting)
	}
	if cfg.ASR.VAD.MinEnergy < 0 || cfg.ASR.VAD.MinEnergy > 1 {
		return fmt.Errorf("%w: vad min energy must be 0..1", ErrInvalidSetting)
	}
	if cfg.ASR.VAD.PlaybackMinEnergy < 0 || cfg.ASR.VAD.PlaybackMinEnergy > 1 {
		return fmt.Errorf("%w: vad playback min energy must be 0..1", ErrInvalidSetting)
	}
	if cfg.ASR.VAD.MinSilenceMillis < 100 || cfg.ASR.VAD.MinSilenceMillis > 5000 {
		return fmt.Errorf("%w: vad min silence must be 100..5000", ErrInvalidSetting)
	}
	if cfg.ASR.VAD.MinSpeechMillis < 100 || cfg.ASR.VAD.MinSpeechMillis > 5000 {
		return fmt.Errorf("%w: vad min speech must be 100..5000", ErrInvalidSetting)
	}
	if cfg.ASR.VAD.WindowSize <= 0 {
		return fmt.Errorf("%w: invalid vad window size", ErrInvalidSetting)
	}
	if cfg.ASR.VAD.PrerollMillis < 0 || cfg.ASR.VAD.PrerollMillis > 2000 {
		return fmt.Errorf("%w: vad preroll must be 0..2000", ErrInvalidSetting)
	}
	if cfg.AECEnabled() && strings.ToLower(strings.TrimSpace(cfg.AEC.Model)) != "webrtc" {
		return fmt.Errorf("%w: unsupported aec model", ErrInvalidSetting)
	}
	if cfg.NSEnabled() && strings.ToLower(strings.TrimSpace(cfg.NS.Model)) != "webrtc" {
		return fmt.Errorf("%w: unsupported ns model", ErrInvalidSetting)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.NS.Level)) {
	case "low", "moderate", "high", "very_high":
	default:
		return fmt.Errorf("%w: invalid ns level", ErrInvalidSetting)
	}
	profileName, profile := cfg.ActiveTTSProfile()
	if strings.ToLower(strings.TrimSpace(profileName)) != "edge" {
		return fmt.Errorf("%w: unsupported tts profile", ErrInvalidSetting)
	}
	if strings.TrimSpace(profile.Voice) == "" {
		return fmt.Errorf("%w: missing tts voice", ErrInvalidSetting)
	}
	if profile.Speed < 0.5 || profile.Speed > 2 {
		return fmt.Errorf("%w: tts speed must be 0.5..2", ErrInvalidSetting)
	}
	return nil
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
