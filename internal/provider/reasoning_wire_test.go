package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
)

// Check serialized requests, not just the mapping helper: defaults and session
// overrides both reach these adapters, and neither may be mutated during sends.
func TestFiveLevelReasoningWireRequests(t *testing.T) {
	levels := []string{"low", "medium", "high", "xhigh", "max"}
	tests := []struct {
		models    []string
		want      []string
		anthropic bool
	}{
		{[]string{"mimo-v2.5", "mimo-v2.5-pro"}, []string{"low", "medium", "high", "high", "high"}, false},
		{[]string{"gemini-3.8-flash", "gemini-3.5-flash-lite", "gemini-3.1-pro-preview", "gemini-3-flash-preview", "gemini-3.1-flash-lite-preview", "gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite"}, []string{"low", "medium", "high", "high", "high"}, false},
		{[]string{"kimi-k3", "glm-5.3", "glm-5.3-flash"}, []string{"low", "high", "high", "max", "max"}, false},
		{[]string{"glm-5.2"}, []string{"high", "high", "high", "max", "max"}, false},
		{[]string{"gpt-5.5", "gpt-5.5-2026-04-23", "gpt-5.4", "gpt-5.4-2026-03-05", "gpt-5.4-mini", "gpt-5.4-mini-2026-03-17", "gpt-5.4-nano", "gpt-5.4-nano-2026-03-17"}, []string{"low", "medium", "high", "xhigh", "xhigh"}, false},
		{[]string{"claude-opus-4-5", "claude-opus-4-5-20251101"}, []string{"low", "medium", "high", "high", "high"}, true},
		{[]string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-mythos-preview"}, []string{"low", "medium", "high", "max", "max"}, true},
		{[]string{"deepseek-flash", "deepseek-v4-pro", "claude-opus-5"}, levels, true},
		{[]string{"gpt-6-astra", "gpt-5.6-terra", "custom-model", "mimo-custom", "gpt-5.5-custom", "gemini-custom"}, levels, false},
	}
	for _, tt := range tests {
		protocols := []string{"chat", "responses"}
		if tt.anthropic {
			protocols = append(protocols, "anthropic")
		}
		for _, protocol := range protocols {
			for _, model := range tt.models {
				for index, effort := range levels {
					t.Run(protocol+"/"+model+"/"+effort, func(t *testing.T) {
						options := &provider.ModelProviderOptions{OpenAI: map[string]any{"reasoning_effort": effort}}
						if protocol == "anthropic" {
							options = &provider.ModelProviderOptions{Anthropic: map[string]any{"output_config": map[string]any{"effort": effort, "format": map[string]any{"type": "json_schema"}}}}
						}
						body := captureReasoningWire(t, protocol, model, options)
						var got any
						switch protocol {
						case "chat":
							got = body["reasoning_effort"]
						case "responses":
							got = body["reasoning"].(map[string]any)["effort"]
						case "anthropic":
							config := body["output_config"].(map[string]any)
							got = config["effort"]
							if config["format"].(map[string]any)["type"] != "json_schema" {
								t.Fatal("effort mapping lost other output_config fields")
							}
						}
						if got != tt.want[index] {
							t.Fatalf("wire effort = %v, want %s", got, tt.want[index])
						}
					})
				}
			}
		}
	}
}

func TestGoogleFiveLevelReasoningWireRequests(t *testing.T) {
	for _, model := range []string{"gemini-3.1-pro-preview", "gemini-2.5-pro"} {
		for index, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				options := &provider.ModelProviderOptions{Google: map[string]any{"thinking": map[string]any{"level": effort, "include_thoughts": true}}}
				body := captureReasoningWire(t, "google", model, options)
				thinking := body["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
				if thinking["includeThoughts"] != true {
					t.Fatal("effort mapping lost includeThoughts")
				}
				if model == "gemini-3.1-pro-preview" {
					want := []string{"LOW", "MEDIUM", "HIGH", "HIGH", "HIGH"}[index]
					if thinking["thinkingLevel"] != want {
						t.Fatalf("thinkingLevel = %v, want %s", thinking["thinkingLevel"], want)
					}
				} else {
					want := []float64{1024, 8192, 24576, 24576, 24576}[index]
					if thinking["thinkingBudget"] != want {
						t.Fatalf("thinkingBudget = %v, want %v", thinking["thinkingBudget"], want)
					}
				}
			})
		}
	}
	options := &provider.ModelProviderOptions{Google: map[string]any{"thinking": map[string]any{"level": "max", "budget": 1234}}}
	body := captureReasoningWire(t, "google", "gemini-2.5-pro", options)
	if body["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)["thinkingBudget"] != float64(1234) {
		t.Fatal("explicit thinking budget must retain priority")
	}
}

func TestAbsentReasoningIsNotSynthesized(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "anthropic", "google"} {
		t.Run(protocol, func(t *testing.T) {
			body := captureReasoningWire(t, protocol, "mimo-v2.5", nil)
			for _, field := range []string{"reasoning_effort", "reasoning", "output_config"} {
				if _, ok := body[field]; ok {
					t.Fatalf("unexpected %s in request without effort", field)
				}
			}
		})
	}
}

func captureReasoningWire(t *testing.T, protocol, model string, options *provider.ModelProviderOptions) map[string]any {
	t.Helper()
	before, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	bodies := make(chan map[string]any, 1)
	response := map[string]string{
		"chat":      "data: [DONE]\n\n",
		"responses": "data: {\"type\":\"response.completed\"}\n\n",
		"anthropic": "data: {\"type\":\"message_stop\"}\n\n",
		"google":    "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n",
	}[protocol]
	h := &http.Client{Transport: attachmentRoundTrip(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		bodies <- body
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
	var client provider.Client
	switch protocol {
	case "chat":
		client = openai.New(openai.Config{BaseURL: "https://test.invalid", HTTPClient: h})
	case "responses":
		client = openai.NewResponses(openai.Config{BaseURL: "https://test.invalid", HTTPClient: h})
	case "anthropic":
		client = anthropic.New(anthropic.Config{BaseURL: "https://test.invalid", HTTPClient: h})
	case "google":
		client = google.New(google.Config{BaseURL: "https://test.invalid", HTTPClient: h})
	default:
		t.Fatalf("unknown protocol %s", protocol)
	}
	chunks, err := client.Stream(context.Background(), provider.Request{Model: model, Config: provider.ModelConfig{ProviderOptions: options}, Messages: []provider.Message{{Role: provider.RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	for chunk := range chunks {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
	}
	after, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("request mutated saved options: %s -> %s", before, after)
	}
	select {
	case body := <-bodies:
		return body
	default:
		t.Fatal("no provider request captured")
		return nil
	}
}
