package api

import (
	"context"
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

// providerProfileView 是 profile 的脱敏响应形状:api_key 只进不出
// (docs/technology-decisions.md 第 5 节),读端点只回 apiKeySet。
type providerProfileView struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Brand     string                `json:"brand,omitempty"`
	Type      string                `json:"type"`
	BaseURL   string                `json:"baseURL"`
	APIKeySet bool                  `json:"apiKeySet"`
	APIKeyEnv string                `json:"apiKeyEnv,omitempty"`
	Models    []store.ProviderModel `json:"models"`
}

func viewProfile(p *store.ProviderProfile) providerProfileView {
	return providerProfileView{
		ID:        p.ProfileID(),
		Name:      p.DisplayName(),
		Brand:     strings.TrimSpace(p.Brand),
		Type:      p.Type,
		BaseURL:   p.BaseURL,
		APIKeySet: config.EffectiveAPIKey(p) != "",
		APIKeyEnv: p.APIKeyEnv,
		Models:    append([]store.ProviderModel{}, p.Models...),
	}
}

type createProfileReq struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Brand     string                `json:"brand"`
	Type      string                `json:"type"`
	BaseURL   string                `json:"baseURL"`
	APIKey    string                `json:"apiKey"`
	APIKeyEnv string                `json:"apiKeyEnv"`
	Models    []store.ProviderModel `json:"models"`
}

type patchProfileReq struct {
	Name    *string `json:"name"`
	Brand   *string `json:"brand"`
	Type    *string `json:"type"`
	BaseURL *string `json:"baseURL"`
	// APIKey 传非空才覆盖;清除 key 走 DELETE 后重建。
	APIKey    *string                `json:"apiKey"`
	APIKeyEnv *string                `json:"apiKeyEnv"`
	Models    *[]store.ProviderModel `json:"models"`
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
	if !registry.SupportedType(req.Type) {
		return badRequest(c, "unsupported type: "+req.Type)
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
		ID:        req.ID,
		Name:      strings.TrimSpace(req.Name),
		Brand:     strings.TrimSpace(req.Brand),
		Type:      req.Type,
		BaseURL:   strings.TrimRight(req.BaseURL, "/"),
		APIKey:    req.APIKey,
		APIKeyEnv: strings.TrimSpace(req.APIKeyEnv),
		Models:    cleanModels(req.Models),
	}
	if p.Name == "" {
		p.Name = p.ID
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
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
		if p.Name == "" {
			p.Name = p.ProfileID()
		}
	}
	if req.Brand != nil {
		p.Brand = strings.TrimSpace(*req.Brand)
	}
	if req.Type != nil {
		if !registry.SupportedType(*req.Type) {
			return badRequest(c, "unsupported type: "+*req.Type)
		}
		p.Type = *req.Type
	}
	if req.BaseURL != nil {
		p.BaseURL = strings.TrimRight(*req.BaseURL, "/")
	}
	if req.APIKey != nil && *req.APIKey != "" {
		p.APIKey = *req.APIKey
	}
	if req.APIKeyEnv != nil {
		p.APIKeyEnv = strings.TrimSpace(*req.APIKeyEnv)
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
		m.Name = strings.TrimSpace(m.Name)
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	return out
}

// 模型目录代理:按 profile type 转发真实端点的模型列表,短缓存。
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
	cacheKey := p.ProfileID() + "\x00" + p.Type + "\x00" + p.BaseURL + "\x00" + apiKey
	modelsCacheMu.Lock()
	if entry, ok := modelsCache[cacheKey]; ok && time.Since(entry.at) < modelsCacheTTL {
		modelsCacheMu.Unlock()
		c.JSON(http.StatusOK, map[string]any{"models": entry.models})
		return nil
	}
	modelsCacheMu.Unlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	var models []string
	switch p.Type {
	case registry.TypeOpenAICompatible, registry.TypeOpenAIResponses:
		models, err = openai.ListModels(ctx, openai.Config{BaseURL: p.BaseURL, APIKey: apiKey})
	case registry.TypeGoogle:
		models, err = google.ListModels(ctx, google.Config{BaseURL: p.BaseURL, APIKey: apiKey})
	case registry.TypeAnthropic:
		models, err = anthropic.ListModels(ctx, anthropic.Config{BaseURL: p.BaseURL, APIKey: apiKey})
	default:
		return badRequest(c, "unsupported type: "+p.Type)
	}
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
