package registry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestModelRequestsCarryPuddingAttribution(t *testing.T) {
	for _, tc := range []struct {
		protocol   string
		path       string
		authHeader string
		authValue  string
		response   string
	}{
		{
			protocol: TypeOpenAICompatible, path: "/chat/completions",
			authHeader: "Authorization", authValue: "Bearer fixture-key",
			response: "data: [DONE]\n\n",
		},
		{
			protocol: TypeOpenAIResponses, path: "/responses",
			authHeader: "Authorization", authValue: "Bearer fixture-key",
			response: "data: {\"type\":\"response.completed\"}\n\n",
		},
		{
			protocol: TypeAnthropic, path: "/v1/messages",
			authHeader: "x-api-key", authValue: "fixture-key",
			response: "data: {\"type\":\"message_stop\"}\n\n",
		},
		{
			protocol: TypeGoogle, path: "/v1beta/models/model-a:streamGenerateContent",
			authHeader: "x-goog-api-key", authValue: "fixture-key",
			response: "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n",
		},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer gateway.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			profiles := memstore.New()
			// A custom gateway URL must carry attribution just like a direct
			// OpenRouter endpoint; model names and profile brands are irrelevant.
			if err := profiles.PutProviderProfile(ctx, &store.ProviderProfile{
				DisplayName: "gateway", Protocol: tc.protocol, BaseURL: gateway.URL, APIKey: "fixture-key",
			}); err != nil {
				t.Fatal(err)
			}
			client, err := New(profiles).Resolve(ctx, "gateway")
			if err != nil {
				t.Fatal(err)
			}
			chunks, err := client.Stream(ctx, provider.Request{
				Model: "model-a", Messages: []provider.Message{{Role: provider.RoleUser, Text: "hi"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			done := false
			for chunk := range chunks {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
				done = done || chunk.Done
			}
			if !done {
				t.Fatal("stream did not finish")
			}
			select {
			case request := <-requests:
				if request.Method != http.MethodPost || request.URL.Path != tc.path {
					t.Fatalf("request = %s %s, want POST %s", request.Method, request.URL.Path, tc.path)
				}
				for name, want := range map[string]string{
					"HTTP-Referer":       "https://x-t.top",
					"X-OpenRouter-Title": "Pudding",
					tc.authHeader:        tc.authValue,
				} {
					if got := request.Header.Get(name); got != want {
						t.Errorf("%s = %q, want %q", name, got, want)
					}
				}
				switch categories := request.Header.Get("X-OpenRouter-Categories"); categories {
				case "programming-app,personal-agent", "general-chat,creative-writing":
				default:
					t.Errorf("unexpected category group: %q", categories)
				}
			default:
				t.Fatal("gateway did not receive a request")
			}
		})
	}
}
