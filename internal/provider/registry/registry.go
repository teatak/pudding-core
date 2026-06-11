// Package registry 按 provider profile 名解析并缓存 provider.Client
// (docs/technology-decisions.md 第 5 节)。profile 名的解析
// (session.provider > settings provider.default > 内置 default)由 engine 完成,
// 本包只负责 名字 → 客户端实例。
package registry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	TypeOpenAICompatible = "openai-compatible"
	TypeGoogle           = "google"
)

// SupportedType 报告 profile type 是否有对应的 Client 实现,API 校验用。
func SupportedType(t string) bool {
	switch t {
	case TypeOpenAICompatible, TypeGoogle:
		return true
	}
	return false
}

type Registry struct {
	store store.Store

	mu    sync.Mutex
	cache map[string]cached // profile 名 → 实例;配置指纹变化即重建
}

type cached struct {
	fingerprint string
	client      provider.Client
}

func New(s store.Store) *Registry {
	return &Registry{store: s, cache: make(map[string]cached)}
}

func (r *Registry) Resolve(ctx context.Context, name string) (provider.Client, error) {
	if name == "" {
		name = store.DefaultProviderProfile
	}
	p, err := r.store.GetProviderProfile(ctx, name)
	if err != nil {
		p = r.legacyDefault(ctx, name)
		if p == nil {
			return nil, fmt.Errorf("provider profile %q not found: configure it via POST /providers", name)
		}
	}

	fingerprint := p.Type + "\x00" + p.BaseURL + "\x00" + p.APIKey + "\x00" + p.Extra
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[name]; ok && c.fingerprint == fingerprint {
		return c.client, nil
	}
	client, err := build(p)
	if err != nil {
		return nil, err
	}
	r.cache[name] = cached{fingerprint: fingerprint, client: client}
	return client, nil
}

func build(p *store.ProviderProfile) (provider.Client, error) {
	switch p.Type {
	case TypeOpenAICompatible:
		if p.BaseURL == "" {
			return nil, fmt.Errorf("provider profile %q: base_url is required", p.Name)
		}
		return openai.New(openai.Config{BaseURL: p.BaseURL, APIKey: p.APIKey}), nil
	case TypeGoogle:
		if p.APIKey == "" {
			return nil, fmt.Errorf("provider profile %q: api_key is required", p.Name)
		}
		return google.New(google.Config{BaseURL: p.BaseURL, APIKey: p.APIKey}), nil
	default:
		return nil, fmt.Errorf("provider profile %q: unsupported type %q", p.Name, p.Type)
	}
}

// legacyDefault 是 default profile 的过渡回落:settings 的 provider.openai.* 键
// → 环境变量。web 的 provider 管理 UI(轨道 E3)落地后删除这两级,
// 届时 default 只认 provider_profiles 表。
func (r *Registry) legacyDefault(ctx context.Context, name string) *store.ProviderProfile {
	if name != store.DefaultProviderProfile {
		return nil
	}
	baseURL, apiKey := "", ""
	if kv, err := r.store.Settings(ctx); err == nil {
		baseURL = kv[store.SettingOpenAIBaseURL]
		apiKey = kv[store.SettingOpenAIAPIKey]
	}
	if baseURL == "" {
		baseURL = os.Getenv("PUDDING_OPENAI_BASE_URL")
	}
	if apiKey == "" {
		apiKey = os.Getenv("PUDDING_OPENAI_API_KEY")
	}
	if baseURL == "" {
		return nil
	}
	return &store.ProviderProfile{
		Name:    store.DefaultProviderProfile,
		Type:    TypeOpenAICompatible,
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
	}
}

// Static 返回固定 client 的 Resolver,服务 --mock 模式与测试。
func Static(client provider.Client) StaticResolver { return StaticResolver{client: client} }

type StaticResolver struct{ client provider.Client }

func (s StaticResolver) Resolve(context.Context, string) (provider.Client, error) {
	return s.client, nil
}
