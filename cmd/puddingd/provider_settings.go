package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
)

// settingsProvider 每个 turn 从 settings 解析 OpenAI-compatible 配置,
// settings 缺失时回落环境变量;改 settings 即时生效,不需要重启 daemon。
// 未配置时 Stream 返回错误,由 engine 落成 turn.failed 呈现给 UI。
type settingsProvider struct {
	store store.Store

	mu        sync.Mutex
	cachedKey string
	cached    *openai.Client // 配置不变时复用,保住连接池
}

var _ provider.Client = (*settingsProvider)(nil)

func (p *settingsProvider) Name() string { return "openai-compatible" }

func (p *settingsProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	kv, err := p.store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(firstNonEmpty(kv[store.SettingOpenAIBaseURL], os.Getenv("PUDDING_OPENAI_BASE_URL")), "/")
	apiKey := firstNonEmpty(kv[store.SettingOpenAIAPIKey], os.Getenv("PUDDING_OPENAI_API_KEY"))
	if baseURL == "" {
		return nil, errors.New("provider not configured: set " + store.SettingOpenAIBaseURL + " in settings")
	}

	key := baseURL + "\x00" + apiKey
	p.mu.Lock()
	if p.cachedKey != key {
		p.cached = openai.New(openai.Config{BaseURL: baseURL, APIKey: apiKey})
		p.cachedKey = key
	}
	client := p.cached
	p.mu.Unlock()
	return client.Stream(ctx, req)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
