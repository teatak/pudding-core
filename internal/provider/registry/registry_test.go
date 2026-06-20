package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestResolveProfileTypesAndCache(t *testing.T) {
	ms := memstore.New()
	r := New(ms)
	ctx := context.Background()

	if _, err := r.Resolve(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("want not-found error naming profile, got %v", err)
	}

	for _, p := range []*store.ProviderProfile{
		{DisplayName: "work", Protocol: TypeOpenAICompatible, BaseURL: "https://example.com/v1", APIKey: "k1"},
		{DisplayName: "responses", Protocol: TypeOpenAIResponses, BaseURL: "https://api.openai.com/v1", APIKey: "k1"},
		{DisplayName: "gem", Protocol: TypeGoogle, APIKey: "k2"},
		{DisplayName: "bad", Protocol: "unknown"},
	} {
		if err := ms.PutProviderProfile(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	work1, err := r.Resolve(ctx, "work")
	if err != nil || work1.Name() != "openai-compatible" {
		t.Fatalf("work: %v %v", work1, err)
	}
	responses, err := r.Resolve(ctx, "responses")
	if err != nil || responses.Name() != "openai-responses" {
		t.Fatalf("responses: %v %v", responses, err)
	}
	gem, err := r.Resolve(ctx, "gem")
	if err != nil || gem.Name() != "google" {
		t.Fatalf("gem: %v %v", gem, err)
	}
	if _, err := r.Resolve(ctx, "bad"); err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Fatalf("want unsupported protocol error, got %v", err)
	}

	// 配置不变 → 复用实例;配置变化 → 重建
	work2, _ := r.Resolve(ctx, "work")
	if work1 != work2 {
		t.Fatal("unchanged profile must reuse cached client")
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "work", Protocol: TypeOpenAICompatible, BaseURL: "https://changed.example.com/v1", APIKey: "k1",
	}); err != nil {
		t.Fatal(err)
	}
	work3, _ := r.Resolve(ctx, "work")
	if work1 == work3 {
		t.Fatal("changed profile must rebuild client")
	}
}

func TestEmptyNameIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	ms := memstore.New()
	r := New(ms)
	ctx := context.Background()

	if _, err := r.Resolve(ctx, ""); err == nil || !strings.Contains(err.Error(), "profile name is required") {
		t.Fatalf("want required profile name error, got %v", err)
	}

	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "local", Protocol: TypeOpenAICompatible, BaseURL: srv.URL,
	}); err != nil {
		t.Fatal(err)
	}
	client, err := r.Resolve(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := client.Stream(ctx, provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		text += chunk.Delta
	}
	if text != "ok" {
		t.Fatalf("want ok, got %q", text)
	}
}

func TestAPIKeyRequirementAllowsOnlyLocalOpenAICompatible(t *testing.T) {
	ms := memstore.New()
	r := New(ms)
	ctx := context.Background()

	cases := []struct {
		name    string
		profile *store.ProviderProfile
		wantErr bool
	}{
		{
			name:    "remote openai-compatible without key",
			profile: &store.ProviderProfile{DisplayName: "remote-compatible", Protocol: TypeOpenAICompatible, BaseURL: "https://example.com/v1"},
			wantErr: true,
		},
		{
			name:    "local openai-compatible without key",
			profile: &store.ProviderProfile{DisplayName: "local-compatible", Protocol: TypeOpenAICompatible, BaseURL: "http://127.0.0.1:11434/v1"},
		},
		{
			name:    "localhost openai-compatible without key",
			profile: &store.ProviderProfile{DisplayName: "localhost-compatible", Protocol: TypeOpenAICompatible, BaseURL: "http://localhost:11434/v1"},
		},
		{
			name:    "openai responses without key",
			profile: &store.ProviderProfile{DisplayName: "responses-no-key", Protocol: TypeOpenAIResponses, BaseURL: "https://api.openai.com/v1"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ms.PutProviderProfile(ctx, tc.profile); err != nil {
				t.Fatal(err)
			}
			_, err := r.Resolve(ctx, tc.profile.ProfileID())
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "api_key is required") {
					t.Fatalf("want api_key error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want success, got %v", err)
			}
		})
	}
}
