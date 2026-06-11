package main

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

func TestSettingsProviderResolution(t *testing.T) {
	ms := memstore.New()
	p := &settingsProvider{store: ms}
	ctx := context.Background()

	// 未配置:Stream 必须返回可读错误(engine 会落成 turn.failed)
	if _, err := p.Stream(ctx, provider.Request{Model: "m"}); err == nil || !strings.Contains(err.Error(), store.SettingOpenAIBaseURL) {
		t.Fatalf("want not-configured error, got %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	// 配置写进 settings 后,无需重启即生效
	if err := ms.SetSettings(ctx, map[string]string{store.SettingOpenAIBaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	ch, err := p.Stream(ctx, provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		text += chunk.Delta
	}
	if text != "ok" {
		t.Fatalf("want streamed ok, got %q", text)
	}
}
