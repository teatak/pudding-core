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
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"golang.org/x/sync/singleflight"
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
	Brand    string `json:"brand"`
}

type providerWriter interface {
	ListProviderProfiles(ctx context.Context) ([]*store.ProviderProfile, error)
	GetProviderProfile(ctx context.Context, name string) (*store.ProviderProfile, error)
	PutProviderProfile(ctx context.Context, p *store.ProviderProfile) error
	UpdateProviderProfile(ctx context.Context, name string, update func(*store.ProviderProfile) error) (*store.ProviderProfile, error)
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
	if req.Protocol != nil && !registry.SupportedProtocol(*req.Protocol) {
		return badRequest(c, "unsupported protocol: "+*req.Protocol)
	}
	p, err := cfg.UpdateProviderProfile(ctx, name, func(p *store.ProviderProfile) error {
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
		return nil
	})
	if err != nil {
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
	if strings.EqualFold(strings.TrimSpace(req.Brand), "buzzhive") {
		req.Protocol = registry.TypeOpenAICompatible
		req.BaseURL = buzzHiveModelsBaseURL(req.BaseURL)
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	models, err := fetchProviderModels(ctx, req.Protocol, req.BaseURL, req.APIKey)
	if err != nil {
		c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		return nil
	}
	if models == nil {
		models = []provider.ModelCandidate{}
	}
	c.JSON(http.StatusOK, map[string]any{"models": models})
	return nil
}

// 模型目录代理:按 profile protocol 转发真实端点的模型列表,短缓存。
// 上游失败回 502,前端回落 presets 静态清单(docs/design.md 第 4 节)。
const modelsCacheTTL = 60 * time.Second

type modelsCacheEntry struct {
	at     time.Time
	models []provider.ModelCandidate
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
		models = []provider.ModelCandidate{}
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

func fetchProviderModels(ctx context.Context, protocol, baseURL, apiKey string) ([]provider.ModelCandidate, error) {
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

func (s *Server) syncProviderModels(c *cart.Context) error {
	name, _ := c.Param("name")
	cfg, ok := s.providerConfig(c)
	if !ok {
		return nil
	}
	ctx := c.Request.Context()
	p, err := cfg.GetProviderProfile(ctx, name)
	if err != nil {
		return s.fail(c, err)
	}
	result := s.syncProviderProfile(ctx, cfg, p)
	select {
	case <-ctx.Done():
		return nil
	case result := <-result:
		if result.Err != nil {
			return s.fail(c, result.Err)
		}
		response := result.Val.(providerSyncResponse)
		c.JSON(response.status, response.body)
		return nil
	}
}

func (s *Server) syncProviderProfile(ctx context.Context, cfg providerWriter, p *store.ProviderProfile) <-chan singleflight.Result {
	// Share the entire fetch/merge/cache operation, not just the upstream fetch:
	// a delayed merge must not overwrite a later sync's result.
	key := strings.Join([]string{p.ProfileID(), p.Brand, p.Protocol, p.BaseURL, config.EffectiveAPIKey(p)}, "\x00")
	return s.providerSyncs.DoChan(key, func() (any, error) {
		// One caller closing its request must not cancel the other waiters.
		// Shared work retains the existing bounded upstream timeout.
		syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return fetchAndMergeProviderModels(syncCtx, cfg, p)
	})
}

type providerSyncResponse struct {
	status int
	body   any
}

func fetchAndMergeProviderModels(ctx context.Context, cfg providerWriter, p *store.ProviderProfile) (providerSyncResponse, error) {
	isBuzzHive := strings.EqualFold(strings.TrimSpace(p.Brand), "buzzhive")
	isOpenRouter := strings.EqualFold(strings.TrimSpace(p.Brand), "openrouter")
	if !isBuzzHive && !isOpenRouter {
		return providerSyncResponse{http.StatusBadRequest, map[string]string{"error": "sync is only supported for buzzhive or openrouter providers"}}, nil
	}

	apiKey := config.EffectiveAPIKey(p)
	modelProtocol := p.Protocol
	if modelProtocol == "" {
		modelProtocol = registry.TypeOpenAICompatible
	}
	modelBaseURL := p.BaseURL
	if isBuzzHive {
		modelProtocol = registry.TypeOpenAICompatible
		modelBaseURL = buzzHiveModelsBaseURL(p.BaseURL)
	}

	candidates, err := fetchProviderModels(ctx, modelProtocol, modelBaseURL, apiKey)
	if err != nil {
		return providerSyncResponse{http.StatusBadGateway, map[string]string{"error": err.Error()}}, nil
	}

	// Merge into the latest profile, not the snapshot from before the network
	// request. An endpoint/key change makes that response stale.
	errStale := errors.New("provider_changed_during_sync")
	updated, err := cfg.UpdateProviderProfile(ctx, p.ProfileID(), func(current *store.ProviderProfile) error {
		if current.Brand != p.Brand || current.Protocol != p.Protocol || current.BaseURL != p.BaseURL || config.EffectiveAPIKey(current) != apiKey {
			return errStale
		}
		if isBuzzHive {
			current.Models = syncBuzzHiveModels(current.Models, candidates)
		} else {
			current.Models = syncOpenRouterModels(current.Models, candidates)
		}
		return nil
	})
	if errors.Is(err, errStale) {
		return providerSyncResponse{http.StatusConflict, map[string]string{"error": errStale.Error()}}, nil
	}
	if err != nil {
		return providerSyncResponse{}, err
	}

	cacheKey := p.ProfileID() + "\x00" + modelProtocol + "\x00" + modelBaseURL + "\x00" + apiKey
	modelsCacheMu.Lock()
	modelsCache[cacheKey] = modelsCacheEntry{at: time.Now(), models: candidates}
	modelsCacheMu.Unlock()
	return providerSyncResponse{http.StatusOK, viewProfile(updated)}, nil
}

func syncOpenRouterModels(existing []store.ProviderModel, candidates []provider.ModelCandidate) []store.ProviderModel {
	candidateMap := make(map[string]provider.ModelCandidate, len(candidates))
	for _, c := range candidates {
		id := strings.TrimSpace(c.ID)
		if id != "" {
			candidateMap[id] = c
		}
	}

	merged := make([]store.ProviderModel, 0, len(existing))
	for _, m := range existing {
		id := strings.TrimSpace(m.ID)
		cand, exists := candidateMap[id]
		if !exists {
			m.Unavailable = true
			merged = append(merged, m)
			continue
		}
		m.Unavailable = false
		if cand.ContextWindow > 0 {
			m.ContextWindow = cand.ContextWindow
		}
		m.CostMultiplier = cand.CostMultiplier
		m.Capabilities = mergeModelCapabilities(m.Capabilities, cand.Capabilities)
		merged = append(merged, m)
	}

	return cleanModels(merged)
}

func syncBuzzHiveModels(existing []store.ProviderModel, candidates []provider.ModelCandidate) []store.ProviderModel {
	candidateMap := make(map[string]provider.ModelCandidate, len(candidates))
	for _, c := range candidates {
		id := strings.TrimSpace(c.ID)
		if id != "" {
			candidateMap[id] = c
		}
	}

	merged := make([]store.ProviderModel, 0, len(candidates)+len(existing))
	seen := make(map[string]bool, len(candidates))

	for _, m := range existing {
		id := strings.TrimSpace(m.ID)
		cand, exists := candidateMap[id]
		if !exists {
			m.Unavailable = true
			merged = append(merged, m)
			continue
		}
		seen[id] = true
		m.Unavailable = false
		m.ContextWindow = cand.ContextWindow
		m.CostMultiplier = cand.CostMultiplier
		if cand.Limits != nil {
			limits := store.ModelLimits{}
			if m.Limits != nil {
				limits = *m.Limits
			}
			limits.MaxOutputTokens = cand.Limits.MaxOutputTokens
			m.Limits = &limits
		}
		m.Capabilities = mergeModelCapabilities(m.Capabilities, cand.Capabilities)
		merged = append(merged, m)
	}

	for _, cand := range candidates {
		id := strings.TrimSpace(cand.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		newModel := store.ProviderModel{
			ID:             id,
			DisplayName:    strings.TrimSpace(cand.DisplayName),
			ContextWindow:  cand.ContextWindow,
			CostMultiplier: cand.CostMultiplier,
		}
		if cand.Limits != nil {
			newModel.Limits = &store.ModelLimits{MaxOutputTokens: cand.Limits.MaxOutputTokens}
		}
		newModel.Capabilities = mergeModelCapabilities(nil, cand.Capabilities)
		merged = append(merged, newModel)
	}

	return cleanModels(merged)
}

func mergeModelCapabilities(existing *store.ModelCaps, reported map[string]bool) *store.ModelCaps {
	if len(reported) == 0 {
		return existing
	}
	// A nil capability config allows tools in the engine. Missing catalog fields
	// must retain that behavior; only an explicit false may disable a capability.
	caps := store.ModelCaps{Tools: true}
	if existing != nil {
		caps = *existing
	}
	if v, ok := reported["image"]; ok {
		caps.Image = v
	}
	if v, ok := reported["audio"]; ok {
		caps.Audio = v
	}
	if v, ok := reported["tools"]; ok {
		caps.Tools = v
	}
	return &caps
}
