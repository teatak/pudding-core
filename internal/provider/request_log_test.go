package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
)

func TestRequestLogsOutputLimitFromWireBody(t *testing.T) {
	for _, protocol := range []string{"openai-compatible", "openai-responses", "anthropic", "google"} {
		for _, configKind := range []string{"default", "provider_option", "resolved_limit"} {
			t.Run(protocol+"/"+configKind, func(t *testing.T) {
				var logs bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&logs, nil)).With(
					"sessionID", "session-log-test", "turnID", "turn-log-test", "providerCallIndex", 2)
				ctx := provider.WithRequestLogger(context.Background(), logger)
				bodies := make(chan map[string]any, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					bodies <- body
					w.Header().Set("Content-Type", "text/event-stream")
					switch protocol {
					case "openai-compatible":
						io.WriteString(w, "data: [DONE]\n\n")
					case "openai-responses":
						io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{}}\n\n")
					case "anthropic":
						io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
					case "google":
						io.WriteString(w, "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n")
					}
				}))
				defer server.Close()
				var client provider.Client
				field := ""
				switch protocol {
				case "openai-compatible":
					client = openai.New(openai.Config{BaseURL: server.URL, APIKey: "private-api-key"})
					field = "max_completion_tokens"
				case "openai-responses":
					client = openai.NewResponses(openai.Config{BaseURL: server.URL, APIKey: "private-api-key"})
					field = "max_output_tokens"
				case "anthropic":
					client = anthropic.New(anthropic.Config{BaseURL: server.URL, APIKey: "private-api-key"})
					field = "max_tokens"
				case "google":
					client = google.New(google.Config{BaseURL: server.URL, APIKey: "private-api-key"})
					field = "generationConfig.maxOutputTokens"
				}
				cfg := provider.ModelConfig{}
				if configKind != "default" {
					cfg.ProviderOptions = &provider.ModelProviderOptions{
						OpenAI: map[string]any{"max_tokens": 456}, Google: map[string]any{"max_tokens": 456},
						Anthropic: map[string]any{"max_tokens": 456},
					}
				}
				if configKind == "resolved_limit" {
					cfg.Limits = &provider.ModelLimits{MaxOutputTokens: 1234}
				}
				stream, err := client.Stream(ctx, provider.Request{Model: "test-model", System: "private-prompt", Config: cfg})
				if err != nil {
					t.Fatal(err)
				}
				for chunk := range stream {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
				}
				body := <-bodies
				var wireLimit any = body
				for _, segment := range strings.Split(field, ".") {
					object, _ := wireLimit.(map[string]any)
					wireLimit = object[segment]
				}
				var logged map[string]any
				if err := json.Unmarshal(logs.Bytes(), &logged); err != nil {
					t.Fatalf("expected one diagnostic record: %v; %s", err, logs.String())
				}
				if logged["maxOutputTokens"] != wireLimit || logged["outputLimitSet"] != (wireLimit != nil) {
					t.Fatalf("diagnostic limit %v does not match wire limit %v", logged, wireLimit)
				}
				if logged["protocol"] != protocol || logged["outputLimitField"] != field || logged["outputLimitSource"] != "request_body" {
					t.Fatalf("missing wire attribution: %v", logged)
				}
				if logged["sessionID"] != "session-log-test" || logged["turnID"] != "turn-log-test" || logged["providerCallIndex"] != float64(2) {
					t.Fatalf("missing engine correlation: %v", logged)
				}
				if strings.Contains(logs.String(), "private-") {
					t.Fatalf("request content or credentials leaked: %s", logs.String())
				}
			})
		}
	}
}
