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
		{Name: "work", Type: TypeOpenAICompatible, BaseURL: "https://example.com/v1", APIKey: "k1"},
		{Name: "gem", Type: TypeGoogle, APIKey: "k2"},
		{Name: "bad", Type: "unknown"},
	} {
		if err := ms.PutProviderProfile(ctx, p); err != nil {
			t.Fatal(err)
		}
	}

	work1, err := r.Resolve(ctx, "work")
	if err != nil || work1.Name() != "openai-compatible" {
		t.Fatalf("work: %v %v", work1, err)
	}
	gem, err := r.Resolve(ctx, "gem")
	if err != nil || gem.Name() != "google" {
		t.Fatalf("gem: %v %v", gem, err)
	}
	if _, err := r.Resolve(ctx, "bad"); err == nil || !strings.Contains(err.Error(), "unsupported type") {
		t.Fatalf("want unsupported type error, got %v", err)
	}

	// 配置不变 → 复用实例;配置变化 → 重建
	work2, _ := r.Resolve(ctx, "work")
	if work1 != work2 {
		t.Fatal("unchanged profile must reuse cached client")
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		Name: "work", Type: TypeOpenAICompatible, BaseURL: "https://changed.example.com/v1", APIKey: "k1",
	}); err != nil {
		t.Fatal(err)
	}
	work3, _ := r.Resolve(ctx, "work")
	if work1 == work3 {
		t.Fatal("changed profile must rebuild client")
	}
}

func TestEmptyNameResolvesToDefaultProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	ms := memstore.New()
	r := New(ms)
	ctx := context.Background()

	// 没有 default profile → 失败且报 profile 名,不存在任何隐式回落
	if _, err := r.Resolve(ctx, ""); err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("want default-not-found error, got %v", err)
	}

	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		Name: store.DefaultProviderProfile, Type: TypeOpenAICompatible, BaseURL: srv.URL,
	}); err != nil {
		t.Fatal(err)
	}
	client, err := r.Resolve(ctx, "")
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
