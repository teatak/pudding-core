package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
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
		{"BuzzHive Responses", "openai-responses", "/models", `{"data":[{"id":"qwen3.6","name":"Qwen3.6","context_length":32768,"max_output_tokens":4096,"capabilities":{"vision":false,"audio_input":true,"tools":false}}]}`, []provider.ModelCandidate{
			{ID: "qwen3.6", DisplayName: "Qwen3.6", ContextWindow: 32768, Capabilities: map[string]bool{"image": false, "audio": true, "tools": false}, Limits: &provider.ModelLimits{MaxOutputTokens: 4096}},
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
