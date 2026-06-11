package api

import (
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
)

// providerProfileView 是 profile 的脱敏响应形状:api_key 只进不出
// (docs/technology-decisions.md 第 5 节),读端点只回 apiKeySet。
type providerProfileView struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	BaseURL   string `json:"baseURL"`
	APIKeySet bool   `json:"apiKeySet"`
	Extra     string `json:"extra,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func viewProfile(p *store.ProviderProfile) providerProfileView {
	return providerProfileView{
		Name:      p.Name,
		Type:      p.Type,
		BaseURL:   p.BaseURL,
		APIKeySet: p.APIKey != "",
		Extra:     p.Extra,
		CreatedAt: p.CreatedAt.Format(timeRFC3339),
		UpdatedAt: p.UpdatedAt.Format(timeRFC3339),
	}
}

const timeRFC3339 = "2006-01-02T15:04:05.999999999Z07:00"

type createProfileReq struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
	Extra   string `json:"extra"`
}

type patchProfileReq struct {
	Type    *string `json:"type"`
	BaseURL *string `json:"baseURL"`
	// APIKey 传非空才覆盖;清除 key 走 DELETE 后重建。
	APIKey *string `json:"apiKey"`
	Extra  *string `json:"extra"`
}

func (s *Server) listProviders(c *cart.Context) error {
	profiles, err := s.store.ListProviderProfiles(c.Request.Context())
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
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || strings.ContainsAny(req.Name, "/ ") {
		return badRequest(c, "name is required and must not contain '/' or spaces")
	}
	if !registry.SupportedType(req.Type) {
		return badRequest(c, "unsupported type: "+req.Type)
	}
	ctx := c.Request.Context()
	if _, err := s.store.GetProviderProfile(ctx, req.Name); err == nil {
		c.JSON(http.StatusConflict, map[string]string{"error": "profile_exists"})
		return nil
	}
	p := &store.ProviderProfile{
		Name:    req.Name,
		Type:    req.Type,
		BaseURL: strings.TrimRight(req.BaseURL, "/"),
		APIKey:  req.APIKey,
		Extra:   defaultExtra(req.Extra),
	}
	if err := s.store.PutProviderProfile(ctx, p); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusCreated, viewProfile(p))
	return nil
}

func (s *Server) getProvider(c *cart.Context) error {
	name, _ := c.Param("name")
	p, err := s.store.GetProviderProfile(c.Request.Context(), name)
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
	p, err := s.store.GetProviderProfile(ctx, name)
	if err != nil {
		return s.fail(c, err)
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
	if req.Extra != nil {
		p.Extra = defaultExtra(*req.Extra)
	}
	if err := s.store.PutProviderProfile(ctx, p); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, viewProfile(p))
	return nil
}

func (s *Server) deleteProvider(c *cart.Context) error {
	name, _ := c.Param("name")
	if err := s.store.DeleteProviderProfile(c.Request.Context(), name); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func defaultExtra(extra string) string {
	if strings.TrimSpace(extra) == "" {
		return "{}"
	}
	return extra
}
