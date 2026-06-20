package config

import (
	"context"
	"os"
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

	if err := m.SetSettings(ctx, map[string]string{
		store.SettingSystemPrompt: "hi",
	}); err != nil {
		t.Fatal(err)
	}
	settings, err := m.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings[store.SettingSystemPrompt] != "hi" {
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
}
