package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
)

// providerProfileView 是 profile 的响应形状:api_key 存在本地配置中,
// 允许设置界面编辑时回显;apiKeySet 用于列表状态点。
type providerProfileView struct {
	ID          string                `json:"id"`
	DisplayName string                `json:"displayName"`
	Brand       string                `json:"brand,omitempty"`
	Group       string                `json:"group,omitempty"`
	Protocol    string                `json:"protocol"`
	BaseURL     string                `json:"baseURL"`
	APIKey      string                `json:"apiKey,omitempty"`
	APIKeySet   bool                  `json:"apiKeySet"`
	Models      []store.ProviderModel `json:"models"`
}

func viewProfile(p *store.ProviderProfile) providerProfileView {
	return providerProfileView{
		ID:          p.ProfileID(),
		DisplayName: p.DisplayLabel(),
		Brand:       strings.TrimSpace(p.Brand),
		Group:       strings.TrimSpace(p.Group),
		Protocol:    p.Protocol,
		BaseURL:     p.BaseURL,
		APIKey:      p.APIKey,
		APIKeySet:   config.EffectiveAPIKey(p) != "",
		Models:      append([]store.ProviderModel{}, p.Models...),
	}
}

type createProfileReq struct {
	ID          string                `json:"id"`
	DisplayName string                `json:"displayName"`
	Brand       string                `json:"brand"`
	Group       string                `json:"group"`
	Protocol    string                `json:"protocol"`
	BaseURL     string                `json:"baseURL"`
	APIKey      string                `json:"apiKey"`
	Models      []store.ProviderModel `json:"models"`
}

type patchProfileReq struct {
	DisplayName *string `json:"displayName"`
	Brand       *string `json:"brand"`
	Group       *string `json:"group"`
	Protocol    *string `json:"protocol"`
	BaseURL     *string `json:"baseURL"`
	// APIKey 传非空才覆盖;清除 key 走 DELETE 后重建。
	APIKey *string                `json:"apiKey"`
	Models *[]store.ProviderModel `json:"models"`
}

type probeProviderModelsReq struct {
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseURL"`
	APIKey   string `json:"apiKey"`
}

type providerWriter interface {
	ListProviderProfiles(ctx context.Context) ([]*store.ProviderProfile, error)
	GetProviderProfile(ctx context.Context, name string) (*store.ProviderProfile, error)
	PutProviderProfile(ctx context.Context, p *store.ProviderProfile) error
	DeleteProviderProfile(ctx context.Context, name string) error
}

func (s *Server) listProviders(c *cart.Context) error {
	cfg, ok := s.providerConfig(c)
	if !ok {
		return nil
	}
	profiles, err := cfg.ListProviderProfiles(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	views := make([]providerProfileView, 0, len(profiles))
	for _, p := range profiles {
		views = append(views, viewProfile(p))
	}
	c.JSON(http.StatusOK, map[string]any{"providers": views})
	return nil
}

func (s *Server) createProvider(c *cart.Context) error {
	var req createProfileReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" || strings.ContainsAny(req.ID, "/ ") {
		return badRequest(c, "id is required and must not contain '/' or spaces")
	}
	if !registry.SupportedProtocol(req.Protocol) {
		return badRequest(c, "unsupported protocol: "+req.Protocol)
	}
	ctx := c.Request.Context()
	cfg, ok := s.providerConfig(c)
	if !ok {
		return nil
	}
	if _, err := cfg.GetProviderProfile(ctx, req.ID); err == nil {
		c.JSON(http.StatusConflict, map[string]string{"error": "profile_exists"})
		return nil
	}
	p := &store.ProviderProfile{
		ID:          req.ID,
		DisplayName: strings.TrimSpace(req.DisplayName),
		Brand:       strings.TrimSpace(req.Brand),
		Group:       strings.TrimSpace(req.Group),
		Protocol:    req.Protocol,
		BaseURL:     strings.TrimRight(req.BaseURL, "/"),
		APIKey:      req.APIKey,
		Models:      cleanModels(req.Models),
	}
	if p.DisplayName == "" {
		p.DisplayName = p.ID
	}
	if err := cfg.PutProviderProfile(ctx, p); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusCreated, viewProfile(p))
	return nil
}

func (s *Server) getProvider(c *cart.Context) error {
	name, _ := c.Param("name")
	cfg, ok := s.providerConfig(c)
	if !ok {
		return nil
	}
	p, err := cfg.GetProviderProfile(c.Request.Context(), name)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, viewProfile(p))
	return nil
}

func (s *Server) patchProvider(c *cart.Context) error {
	name, _ := c.Param("name")
	var req patchProfileReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	ctx := c.Request.Context()
	cfg, ok := s.providerConfig(c)
	if !ok {
		return nil
	}
	p, err := cfg.GetProviderProfile(ctx, name)
	if err != nil {
		return s.fail(c, err)
	}
	if req.DisplayName != nil {
		p.DisplayName = strings.TrimSpace(*req.DisplayName)
		if p.DisplayName == "" {
			p.DisplayName = p.ProfileID()
		}
	}
	if req.Brand != nil {
		p.Brand = strings.TrimSpace(*req.Brand)
	}
	if req.Group != nil {
		p.Group = strings.TrimSpace(*req.Group)
	}
	if req.Protocol != nil {
		if !registry.SupportedProtocol(*req.Protocol) {
			return badRequest(c, "unsupported protocol: "+*req.Protocol)
		}
		p.Protocol = *req.Protocol
	}
	if req.BaseURL != nil {
		p.BaseURL = strings.TrimRight(*req.BaseURL, "/")
	}
	if req.APIKey != nil && *req.APIKey != "" {
		p.APIKey = *req.APIKey
	}
	if req.Models != nil {
		p.Models = cleanModels(*req.Models)
	}
	if err := cfg.PutProviderProfile(ctx, p); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, viewProfile(p))
	return nil
}

func (s *Server) deleteProvider(c *cart.Context) error {
	name, _ := c.Param("name")
	cfg, ok := s.providerConfig(c)
	if !ok {
		return nil
	}
	if err := cfg.DeleteProviderProfile(c.Request.Context(), name); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) providerConfig(c *cart.Context) (providerWriter, bool) {
	if s.providers == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "provider_config_unavailable"})
		return nil, false
	}
	return s.providers, true
}

func cleanModels(models []store.ProviderModel) []store.ProviderModel {
	out := make([]store.ProviderModel, 0, len(models))
	seen := map[string]bool{}
	for _, m := range models {
		m.ID = strings.TrimSpace(m.ID)
		m.DisplayName = strings.TrimSpace(m.DisplayName)
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}

func (s *Server) probeProviderModels(c *cart.Context) error {
	var req probeProviderModelsReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	req.Protocol = strings.TrimSpace(req.Protocol)
	req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if !registry.SupportedProtocol(req.Protocol) {
		return badRequest(c, "unsupported protocol: "+req.Protocol)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	models, err := fetchProviderModels(ctx, req.Protocol, req.BaseURL, req.APIKey)
	if err != nil {
		c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		return nil
	}
	if models == nil {
		models = []string{}
	}
	c.JSON(http.StatusOK, map[string]any{"models": models})
	return nil
}

// 模型目录代理:按 profile protocol 转发真实端点的模型列表,短缓存。
// 上游失败回 502,前端回落 presets 静态清单(docs/design.md 第 4 节)。
const modelsCacheTTL = 60 * time.Second

type modelsCacheEntry struct {
	at     time.Time
	models []string
}

var (
	modelsCacheMu sync.Mutex
	modelsCache   = map[string]modelsCacheEntry{}
)

func (s *Server) listProviderModels(c *cart.Context) error {
	name, _ := c.Param("name")
	cfg, ok := s.providerConfig(c)
	if !ok {
		return nil
	}
	p, err := cfg.GetProviderProfile(c.Request.Context(), name)
	if err != nil {
		return s.fail(c, err)
	}

	apiKey := config.EffectiveAPIKey(p)
	modelProtocol := p.Protocol
	modelBaseURL := p.BaseURL
	if strings.EqualFold(strings.TrimSpace(p.Brand), "buzzhive") {
		modelProtocol = registry.TypeOpenAICompatible
		modelBaseURL = buzzHiveModelsBaseURL(p.BaseURL)
	}
	cacheKey := p.ProfileID() + "\x00" + modelProtocol + "\x00" + modelBaseURL + "\x00" + apiKey
	modelsCacheMu.Lock()
	if entry, ok := modelsCache[cacheKey]; ok && time.Since(entry.at) < modelsCacheTTL {
		modelsCacheMu.Unlock()
		c.JSON(http.StatusOK, map[string]any{"models": entry.models})
		return nil
	}
	modelsCacheMu.Unlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	models, err := fetchProviderModels(ctx, modelProtocol, modelBaseURL, apiKey)
	if err != nil {
		c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		return nil
	}
	if models == nil {
		models = []string{}
	}

	modelsCacheMu.Lock()
	modelsCache[cacheKey] = modelsCacheEntry{at: time.Now(), models: models}
	modelsCacheMu.Unlock()
	c.JSON(http.StatusOK, map[string]any{"models": models})
	return nil
}

func buzzHiveModelsBaseURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	if strings.HasSuffix(base, "/v1beta") {
		return strings.TrimSuffix(base, "/v1beta") + "/v1"
	}
	return base + "/v1"
}

func fetchProviderModels(ctx context.Context, protocol, baseURL, apiKey string) ([]string, error) {
	switch protocol {
	case registry.TypeOpenAICompatible, registry.TypeOpenAIResponses:
		return openai.ListModels(ctx, openai.Config{BaseURL: baseURL, APIKey: apiKey})
	case registry.TypeGoogle:
		return google.ListModels(ctx, google.Config{BaseURL: baseURL, APIKey: apiKey})
	case registry.TypeAnthropic:
		return anthropic.ListModels(ctx, anthropic.Config{BaseURL: baseURL, APIKey: apiKey})
	default:
		return nil, errors.New("unsupported protocol: " + protocol)
	}
}
