package config

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestManagerPersistsSettingsAndProfiles(t *testing.T) {
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	settings, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings[SettingCompactTailInputTurns] != "2" ||
		settings[SettingCompactAutoThresholdPercent] != "80" ||
		settings[SettingShowCompactSummary] != "true" ||
		settings[SettingShowReasoning] != "true" ||
		settings[SettingShowRawToolInfo] != "true" ||
		settings[SettingShowAppPreviewVersions] != "false" {
		t.Fatalf("unexpected settings: %+v", settings)
	}

	if err := m.PutProviderProfile(ctx, &store.ProviderProfile{
		ID:          "openai",
		DisplayName: "OpenAI",
		Protocol:    "openai-responses",
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      "secret",
		Models: []store.ProviderModel{
			{ID: "gpt-5.5", DisplayName: "GPT 5.5", ContextWindow: 1050000, Capabilities: &store.ModelCaps{Image: true, Audio: false, Tools: true}, Limits: &store.ModelLimits{MaxOutputTokens: 8192}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	p, err := m.GetProviderProfile(ctx, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayLabel() != "OpenAI" || len(p.Models) != 1 || p.Models[0].ID != "gpt-5.5" || EffectiveAPIKey(p) != "secret" {
		t.Fatalf("unexpected profile: %+v", p)
	}
	profilesPath := home + "/config/profiles.yaml"
	if _, err := os.Stat(profilesPath); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(profilesPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "audio: false") {
		t.Fatalf("expected explicit false capability in profiles.yaml:\n%s", b)
	}
	if !strings.Contains(string(b), "display_name: OpenAI") || !strings.Contains(string(b), "protocol: openai-responses") || !strings.Contains(string(b), "max_output_tokens: 8192") {
		t.Fatalf("expected renamed provider keys in profiles.yaml:\n%s", b)
	}

	audio, err := m.Audio(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if audio.Driver.Type != "portaudio" ||
		audio.ASR.Engine != "sherpa-sensevoice" ||
		audio.ASR.Language != "zh" ||
		audio.ASRSaveAudio() ||
		audio.ASRUseITN() ||
		audio.ASR.VAD.Threshold != 0.6 ||
		audio.ASR.VAD.PrerollMillis != 500 ||
		!audio.AECEnabled() ||
		audio.AEC.Model != "webrtc" ||
		!audio.NSEnabled() ||
		audio.NS.Model != "webrtc" ||
		audio.NS.Level != "moderate" {
		t.Fatalf("unexpected audio config: %+v", audio)
	}
	audioPath := home + "/config/audio.yaml"
	b, err = os.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	audioYAML := string(b)
	for _, want := range []string{
		"type: portaudio",
		"engine: sherpa-sensevoice",
		"save_audio: false",
		"language: zh",
		"use_itn: false",
		"preroll_millis: 500",
		"level: moderate",
		"voice: zh-CN-YunxiaNeural",
	} {
		if !strings.Contains(audioYAML, want) {
			t.Fatalf("expected %q in audio.yaml:\n%s", want, audioYAML)
		}
	}

	useITN := true
	audio.ASR.Language = "en"
	audio.ASR.UseInverseTextNormalization = &useITN
	audio.ASR.VAD.Threshold = 0.55
	audio.ASR.VAD.PrerollMillis = 650
	audio.NS.Level = "high"
	edge := audio.TTS.Profiles["edge"]
	edge.Voice = "zh-CN-XiaoxiaoNeural"
	edge.Speed = 1.35
	audio.TTS.Profiles["edge"] = edge
	updatedAudio, err := m.SetAudio(ctx, audio)
	if err != nil {
		t.Fatal(err)
	}
	if updatedAudio.ASR.Language != "en" || !updatedAudio.ASRUseITN() || updatedAudio.ASR.VAD.Threshold != 0.55 || updatedAudio.ASR.VAD.PrerollMillis != 650 || updatedAudio.NS.Level != "high" || updatedAudio.TTS.Profiles["edge"].Speed != 1.35 {
		t.Fatalf("unexpected updated audio config: %+v", updatedAudio)
	}
	b, err = os.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	audioYAML = string(b)
	for _, want := range []string{
		"language: en",
		"use_itn: true",
		"threshold: 0.55",
		"preroll_millis: 650",
		"level: high",
		"voice: zh-CN-XiaoxiaoNeural",
		"speed: 1.35",
	} {
		if !strings.Contains(audioYAML, want) {
			t.Fatalf("expected %q in updated audio.yaml:\n%s", want, audioYAML)
		}
	}
}

func TestManagerPersistsSettingsAndUserPrompt(t *testing.T) {
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := m.SetSettings(ctx, map[string]string{
		SettingCompactTailInputTurns:       "3",
		SettingCompactAutoThresholdPercent: "70",
		SettingShowCompactSummary:          "false",
		SettingShowReasoning:               "false",
		SettingShowRawToolInfo:             "false",
		SettingShowAppPreviewVersions:      "true",
	}); err != nil {
		t.Fatal(err)
	}
	settings, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings[SettingCompactTailInputTurns] != "3" ||
		settings[SettingCompactAutoThresholdPercent] != "70" ||
		settings[SettingShowCompactSummary] != "false" ||
		settings[SettingShowReasoning] != "false" ||
		settings[SettingShowRawToolInfo] != "false" ||
		settings[SettingShowAppPreviewVersions] != "true" {
		t.Fatalf("unexpected settings: %+v", settings)
	}
	settingsPath := home + "/config/settings.yaml"
	b, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	settingsYAML := string(b)
	if !strings.Contains(settingsYAML, "tail_input_turns: 3") ||
		!strings.Contains(settingsYAML, "auto_threshold_percent: 70") ||
		!strings.Contains(settingsYAML, "reasoning: false") ||
		!strings.Contains(settingsYAML, "raw_tool_info: false") ||
		!strings.Contains(settingsYAML, "show_preview_versions: true") {
		t.Fatalf("expected settings in settings.yaml:\n%s", settingsYAML)
	}
	reloaded := NewManager(home)
	reloadedSettings, err := reloaded.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedSettings[SettingShowAppPreviewVersions] != "true" {
		t.Fatalf("reloaded preview setting = %q", reloadedSettings[SettingShowAppPreviewVersions])
	}
	if err := reloaded.SetSettings(ctx, map[string]string{SettingShowAppPreviewVersions: "false"}); err != nil {
		t.Fatal(err)
	}
	reloadedSettings, err = reloaded.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedSettings[SettingShowAppPreviewVersions] != "false" ||
		reloadedSettings[SettingCompactTailInputTurns] != "3" {
		t.Fatalf("unexpected settings after preview toggle: %+v", reloadedSettings)
	}

	initialPrompt, err := m.UserPrompt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initialPrompt.Exists || initialPrompt.Content != "" || initialPrompt.Path != home+"/pudding.md" {
		t.Fatalf("unexpected initial prompt: %+v", initialPrompt)
	}
	updatedPrompt, err := m.SetUserPrompt(ctx, "short replies")
	if err != nil {
		t.Fatal(err)
	}
	if !updatedPrompt.Exists || updatedPrompt.Content != "short replies" || updatedPrompt.Path != home+"/pudding.md" {
		t.Fatalf("unexpected updated prompt: %+v", updatedPrompt)
	}
	b, err = os.ReadFile(home + "/pudding.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "short replies" {
		t.Fatalf("unexpected pudding.md content: %q", b)
	}
}

func TestManagerResetSettingsOnlyRewritesSettingsYAML(t *testing.T) {
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.SetSettings(ctx, map[string]string{
		SettingCompactTailInputTurns:       "9",
		SettingCompactAutoThresholdPercent: "65",
		SettingShowCompactSummary:          "false",
		SettingShowReasoning:               "false",
		SettingShowRawToolInfo:             "false",
		SettingShowAppPreviewVersions:      "true",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetUserPrompt(ctx, "keep this prompt"); err != nil {
		t.Fatal(err)
	}
	audio, err := m.Audio(ctx)
	if err != nil {
		t.Fatal(err)
	}
	audio.ASR.Language = "en"
	if _, err := m.SetAudio(ctx, audio); err != nil {
		t.Fatal(err)
	}

	reset, err := m.ResetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reset[SettingCompactTailInputTurns] != "2" ||
		reset[SettingCompactAutoThresholdPercent] != "80" ||
		reset[SettingShowCompactSummary] != "true" ||
		reset[SettingShowReasoning] != "true" ||
		reset[SettingShowRawToolInfo] != "true" ||
		reset[SettingShowAppPreviewVersions] != "false" {
		t.Fatalf("unexpected reset settings: %+v", reset)
	}
	reloaded, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reset, reloaded) {
		t.Fatalf("persisted settings differ from reset result: reset=%+v reloaded=%+v", reset, reloaded)
	}
	prompt, err := m.UserPrompt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !prompt.Exists || prompt.Content != "keep this prompt" {
		t.Fatalf("settings reset must preserve pudding.md: %+v", prompt)
	}
	persistedAudio, err := m.Audio(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if persistedAudio.ASR.Language != "en" {
		t.Fatalf("settings reset must preserve audio.yaml: %+v", persistedAudio.ASR)
	}
	b, err := os.ReadFile(home + "/config/settings.yaml")
	if err != nil {
		t.Fatal(err)
	}
	settingsYAML := string(b)
	if !strings.Contains(settingsYAML, "version: 1") ||
		strings.Contains(settingsYAML, "tail_input_turns: 9") ||
		strings.Contains(settingsYAML, "show_preview_versions: true") {
		t.Fatalf("settings.yaml was not rewritten to defaults:\n%s", settingsYAML)
	}
}

func TestManagerResetAudioOnlyRewritesAudioYAML(t *testing.T) {
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.SetSettings(ctx, map[string]string{SettingCompactTailInputTurns: "9"}); err != nil {
		t.Fatal(err)
	}
	audio, err := m.Audio(ctx)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	audio.ASR.Language = "en"
	audio.ASR.SaveAudio = &enabled
	audio.ASR.VAD.Threshold = 0.42
	edge := audio.TTS.Profiles["edge"]
	edge.Voice = "en-US-AriaNeural"
	edge.Speed = 1.7
	audio.TTS.Profiles["edge"] = edge
	audio.TTS.Profiles["extra"] = AudioTTSProfile{Voice: "en-GB-SoniaNeural", Speed: 1.1}
	if _, err := m.SetAudio(ctx, audio); err != nil {
		t.Fatal(err)
	}

	reset, err := m.ResetAudio(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultAudioConfig()
	if !reflect.DeepEqual(reset, want) {
		t.Fatalf("unexpected reset audio:\n got: %+v\nwant: %+v", reset, want)
	}
	if _, ok := reset.TTS.Profiles["extra"]; ok {
		t.Fatalf("audio reset kept an extra TTS profile: %+v", reset.TTS.Profiles)
	}
	reloaded, err := m.Audio(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, want) {
		t.Fatalf("persisted audio differs from defaults:\n got: %+v\nwant: %+v", reloaded, want)
	}
	settings, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings[SettingCompactTailInputTurns] != "9" {
		t.Fatalf("audio reset must preserve settings.yaml: %+v", settings)
	}
}

func TestManagerResetReportsWriteErrors(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		reset func(*Manager) error
	}{
		{
			name: "settings",
			file: settingsFile,
			reset: func(m *Manager) error {
				_, err := m.ResetSettings(context.Background())
				return err
			},
		},
		{
			name: "audio",
			file: audioFile,
			reset: func(m *Manager) error {
				_, err := m.ResetAudio(context.Background())
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			m := NewManager(home)
			if err := m.Prepare(); err != nil {
				t.Fatal(err)
			}
			path := home + "/config/" + tt.file
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := tt.reset(m); err == nil {
				t.Fatal("reset succeeded despite an unwritable destination")
			}
		})
	}
}

func TestManagerPersistsWebTools(t *testing.T) {
	home := t.TempDir()
	m := NewManager(home)
	if err := m.Prepare(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	initial, err := m.WebTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial.SearchProvider != "" || initial.FetchProvider != "" || len(initial.Providers) != 1 {
		t.Fatalf("unexpected initial web tools: %+v", initial)
	}
	if initial.Providers[0].Name != "tavily" || initial.Providers[0].APIKeySet || initial.Providers[0].APIKey != "" {
		t.Fatalf("unexpected initial tavily provider: %+v", initial.Providers[0])
	}

	key := "  tvly-secret  "
	updated, err := m.PatchWebTools(ctx, WebToolsUpdate{
		Providers: map[string]WebToolProviderUpdate{
			"tavily": {APIKey: &key},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SearchProvider != "tavily" || updated.FetchProvider != "tavily" {
		t.Fatalf("saving key should enable tavily providers: %+v", updated)
	}
	if got := updated.Providers[0].APIKey; got != "tvly-secret" {
		t.Fatalf("api key should be trimmed, got %q", got)
	}
	if ok := updated.Providers[0].APIKeySet; !ok {
		t.Fatalf("api key should be marked configured: %+v", updated.Providers[0])
	}
	stored, ok, err := m.TavilyAPIKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || stored != "tvly-secret" {
		t.Fatalf("unexpected tavily api key: %q %v", stored, ok)
	}
	webToolsPath := home + "/config/web.yaml"
	b, err := os.ReadFile(webToolsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "api_key: tvly-secret") || !strings.Contains(string(b), "provider: tavily") {
		t.Fatalf("expected tavily config in web.yaml:\n%s", b)
	}

	empty := ""
	cleared, err := m.PatchWebTools(ctx, WebToolsUpdate{
		Providers: map[string]WebToolProviderUpdate{
			"tavily": {APIKey: &empty},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Providers[0].APIKeySet || cleared.Providers[0].APIKey != "" {
		t.Fatalf("clear should remove tavily api key: %+v", cleared.Providers[0])
	}
	if cleared.SearchProvider != "" || cleared.FetchProvider != "" {
		t.Fatalf("clear should disable tavily providers: %+v", cleared)
	}
	stored, ok, err = m.TavilyAPIKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ok || stored != "" {
		t.Fatalf("cleared tavily api key should be empty: %q %v", stored, ok)
	}
	b, err = os.ReadFile(webToolsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "tvly-secret") || strings.Contains(string(b), "api_key:") {
		t.Fatalf("cleared web.yaml must not keep api key:\n%s", b)
	}
}
