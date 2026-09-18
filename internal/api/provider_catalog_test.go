package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func TestProviderCatalogMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, path, body string
		want                       []provider.ModelCandidate
	}{
		{"OpenRouter", "openai-compatible", "/models", `{"data":[{"id":" vendor/model ","name":"Model","context_length":65536,"architecture":{"input_modalities":["text","image"]},"supported_parameters":["tools"],"top_provider":{"max_completion_tokens":8192}},{"id":"text","architecture":{"input_modalities":["text"]},"supported_parameters":[]}]}`, []provider.ModelCandidate{
			{ID: "vendor/model", DisplayName: "Model", ContextWindow: 65536, Capabilities: map[string]bool{"image": true, "audio": false, "tools": true}, Limits: &provider.ModelLimits{MaxOutputTokens: 8192}},
			{ID: "text", Capabilities: map[string]bool{"image": false, "audio": false, "tools": false}},
		}},
		{"BuzzHive Responses", "openai-responses", "/models", `{"data":[{"id":"qwen3.6","name":"Qwen3.6","context_length":32768,"max_output_tokens":4096,"cost_multiplier":0.1,"capabilities":{"vision":false,"audio_input":true,"tools":false}},{"id":"free-model","name":"Free Model","cost_multiplier":0}]}`, []provider.ModelCandidate{
			{ID: "qwen3.6", DisplayName: "Qwen3.6", ContextWindow: 32768, CostMultiplier: float64Ptr(0.1), Capabilities: map[string]bool{"image": false, "audio": true, "tools": false}, Limits: &provider.ModelLimits{MaxOutputTokens: 4096}},
			{ID: "free-model", DisplayName: "Free Model", CostMultiplier: float64Ptr(0)},
		}},
		{"ID only and unknown limits", "openai-compatible", "/models", `{"data":[{"id":"id-only"},{"id":"unknown","context_length":-1,"max_output_tokens":0,"top_provider":{"max_completion_tokens":null},"architecture":{"input_modalities":null},"supported_parameters":null},{"id":" "}]}`, []provider.ModelCandidate{{ID: "id-only"}, {ID: "unknown"}}},
		{"Anthropic", "anthropic", "/v1/models", `{"data":[{"id":"claude-custom","display_name":"Custom Claude","max_input_tokens":200000,"max_tokens":64000,"capabilities":{"image_input":{"supported":false}}},{"id":"unknown","max_input_tokens":null,"max_tokens":0}]}`, []provider.ModelCandidate{
			{ID: "claude-custom", DisplayName: "Custom Claude", ContextWindow: 200000, Capabilities: map[string]bool{"image": false}, Limits: &provider.ModelLimits{MaxOutputTokens: 64000}}, {ID: "unknown"},
		}},
		{"Google", "google", "/v1beta/models", `{"models":[{"name":"models/gemini-custom","displayName":"Custom Gemini","inputTokenLimit":1048576,"outputTokenLimit":65536,"supportedGenerationMethods":["generateContent"]},{"name":"models/embedding","supportedGenerationMethods":["embedContent"]},{"name":"models/unknown","supportedGenerationMethods":["generateContent"]}]}`, []provider.ModelCandidate{
			{ID: "gemini-custom", DisplayName: "Custom Gemini", ContextWindow: 1048576, Limits: &provider.ModelLimits{MaxOutputTokens: 65536}}, {ID: "unknown"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("path = %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			got, err := fetchProviderModels(context.Background(), tc.protocol, upstream.URL, "fixture-key")
			if err != nil {
				t.Fatal(err)
			}
			// Compare the public JSON contract, including absence vs explicit false.
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if !reflect.DeepEqual(gotJSON, wantJSON) {
				t.Fatalf("got %s; want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestSyncBuzzHiveModels(t *testing.T) {
	existing := []store.ProviderModel{
		{
			ID:             "deepseek-flash",
			DisplayName:    "My Local Alias",
			ContextWindow:  64000,
			CostMultiplier: float64Ptr(0.15),
			ProviderOptions: &store.ProviderOptions{
				OpenAI: map[string]any{"temperature": 0.7},
			},
		},
		{
			ID:          "deprecated-model",
			DisplayName: "Old Model",
		},
	}

	candidates := []provider.ModelCandidate{
		{
			ID:             "deepseek-flash",
			DisplayName:    "DeepSeek Flash (Upstream)",
			ContextWindow:  128000,
			CostMultiplier: float64Ptr(0.1),
			Capabilities:   map[string]bool{"tools": true, "image": true},
			Limits:         &provider.ModelLimits{MaxOutputTokens: 8192},
		},
		{
			ID:             "owl-alpha",
			DisplayName:    "Owl Alpha",
			CostMultiplier: float64Ptr(0),
		},
	}

	synced := syncBuzzHiveModels(existing, candidates)
	if len(synced) != 3 {
		t.Fatalf("expected 3 models, got %d: %+v", len(synced), synced)
	}

	// 1. Preserved existing model with local DisplayName and ProviderOptions, but updated objective metadata
	flash := synced[0]
	if flash.ID != "deepseek-flash" {
		t.Errorf("expected deepseek-flash first, got %s", flash.ID)
	}
	if flash.DisplayName != "My Local Alias" {
		t.Errorf("expected local alias preserved, got %s", flash.DisplayName)
	}
	if flash.ContextWindow != 128000 {
		t.Errorf("expected context window updated to 128000, got %d", flash.ContextWindow)
	}
	if flash.CostMultiplier == nil || *flash.CostMultiplier != 0.1 {
		t.Errorf("expected cost multiplier updated to 0.1, got %v", flash.CostMultiplier)
	}
	if flash.ProviderOptions == nil || flash.ProviderOptions.OpenAI["temperature"] != 0.7 {
		t.Errorf("expected ProviderOptions preserved, got %+v", flash.ProviderOptions)
	}
	if flash.Capabilities == nil || !flash.Capabilities.Image || !flash.Capabilities.Tools {
		t.Errorf("expected capabilities updated, got %+v", flash.Capabilities)
	}
	if flash.Limits == nil || flash.Limits.MaxOutputTokens != 8192 {
		t.Errorf("expected limits updated, got %+v", flash.Limits)
	}

	// 2. Newly discovered model appended
	owl := synced[2]
	if owl.ID != "owl-alpha" {
		t.Errorf("expected owl-alpha appended, got %s", owl.ID)
	}
	if owl.DisplayName != "Owl Alpha" {
		t.Errorf("expected Owl Alpha display name, got %s", owl.DisplayName)
	}
	if owl.CostMultiplier == nil || *owl.CostMultiplier != 0 {
		t.Errorf("expected cost multiplier 0, got %v", owl.CostMultiplier)
	}

	// 3. deprecated-model preserved but marked Unavailable
	var deprecatedFound bool
	for _, m := range synced {
		if m.ID == "deprecated-model" {
			deprecatedFound = true
			if !m.Unavailable {
				t.Errorf("deprecated-model should be marked Unavailable")
			}
		}
	}
	if !deprecatedFound {
		t.Errorf("deprecated-model should be preserved with Unavailable status")
	}
}

func TestSyncOpenRouterModels(t *testing.T) {
	existing := []store.ProviderModel{
		{
			ID:          "openrouter/free",
			DisplayName: "Free Router",
		},
		{
			ID:             "z-ai/glm-5.3-flash",
			DisplayName:    "My Flash Alias",
			ContextWindow:  100000,
			CostMultiplier: float64Ptr(0.2),
		},
		{
			ID:          "custom/unlisted",
			DisplayName: "Unlisted Model",
		},
	}

	candidates := []provider.ModelCandidate{
		{
			ID:             "openrouter/free",
			DisplayName:    "Free Router Upstream",
			ContextWindow:  200000,
			CostMultiplier: float64Ptr(0),
			Capabilities:   map[string]bool{"tools": true, "image": true},
		},
		{
			ID:             "z-ai/glm-5.3-flash",
			DisplayName:    "GLM 5.3 Flash Upstream",
			ContextWindow:  1000000,
			CostMultiplier: float64Ptr(0.1),
			Capabilities:   map[string]bool{"tools": true},
		},
		{
			ID:             "other/should-not-append",
			DisplayName:    "Thousands of Other Models",
			CostMultiplier: float64Ptr(1.0),
		},
	}

	synced := syncOpenRouterModels(existing, candidates)
	if len(synced) != 3 {
		t.Fatalf("expected exactly 3 models, got %d: %+v", len(synced), synced)
	}

	// 1. openrouter/free updated with context, cost 0, caps, not unavailable
	if synced[0].ID != "openrouter/free" || synced[0].ContextWindow != 200000 || synced[0].Unavailable {
		t.Errorf("unexpected free model: %+v", synced[0])
	}
	if synced[0].CostMultiplier == nil || *synced[0].CostMultiplier != 0 {
		t.Errorf("expected cost multiplier 0, got %v", synced[0].CostMultiplier)
	}

	// 2. z-ai/glm-5.3-flash preserved display name alias, updated cost multiplier to 0.1, not unavailable
	if synced[1].ID != "z-ai/glm-5.3-flash" || synced[1].DisplayName != "My Flash Alias" || synced[1].ContextWindow != 1000000 || synced[1].Unavailable {
		t.Errorf("unexpected flash model: %+v", synced[1])
	}
	if synced[1].CostMultiplier == nil || *synced[1].CostMultiplier != 0.1 {
		t.Errorf("expected cost multiplier 0.1, got %v", synced[1].CostMultiplier)
	}

	// 3. custom/unlisted preserved safely and marked Unavailable: true
	if synced[2].ID != "custom/unlisted" || synced[2].DisplayName != "Unlisted Model" || !synced[2].Unavailable {
		t.Errorf("expected custom/unlisted preserved with Unavailable=true, got %+v", synced[2])
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}
