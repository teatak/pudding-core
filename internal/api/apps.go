package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/store"
)

type appConnectionConfig interface {
	ListAppConnections(ctx context.Context) ([]*app.Connection, error)
	GetAppConnection(ctx context.Context, id string) (*app.Connection, error)
	PutAppConnection(ctx context.Context, conn *app.Connection) error
	DeleteAppConnection(ctx context.Context, id string) error
}

type putAppConnectionReq struct {
	AppID    string `json:"appID"`
	Name     string `json:"name"`
	AuthType string `json:"authType"`
	Token    string `json:"token"`
	Prefix   string `json:"prefix"`
	Header   string `json:"header"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type putSessionAppGrantReq struct {
	AppID            string          `json:"appID"`
	ConnectionID     string          `json:"connectionID"`
	AllowedEndpoints []string        `json:"allowedEndpoints"`
	Permissions      []string        `json:"permissions"`
	Constraints      json.RawMessage `json:"constraints"`
}

type installAppReq struct {
	PackageJSON   string `json:"packageJSON"`
	PackageSHA256 string `json:"packageSHA256"`
	SourceURL     string `json:"sourceURL"`
}

func (s *Server) listApps(c *cart.Context) error {
	if s.apps == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_service_unavailable"})
		return nil
	}
	apps, err := s.apps.ListDefinitions(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"apps": apps})
	return nil
}

func (s *Server) installApp(c *cart.Context) error {
	if s.apps == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_service_unavailable"})
		return nil
	}
	var req installAppReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	if strings.TrimSpace(req.PackageJSON) == "" {
		return badRequest(c, "packageJSON is required")
	}
	def, err := s.apps.InstallPackage(c.Request.Context(), []byte(req.PackageJSON), req.PackageSHA256, req.SourceURL)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, def)
	return nil
}

func (s *Server) deleteApp(c *cart.Context) error {
	if s.apps == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_service_unavailable"})
		return nil
	}
	id, _ := c.Param("id")
	if err := s.apps.DeleteDefinition(c.Request.Context(), id); err != nil {
		if errors.Is(err, app.ErrInvalidID) {
			return badRequest(c, "invalid app id")
		}
		if errors.Is(err, app.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "app_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) getAppAsset(c *cart.Context) error {
	if s.apps == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_service_unavailable"})
		return nil
	}
	rel, _ := c.Param("path")
	data, contentType, err := s.apps.ReadAsset(c.Request.Context(), rel)
	if err != nil {
		if errors.Is(err, app.ErrInvalidAsset) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "app_asset_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, contentType, data)
	return nil
}

func (s *Server) getAppSkill(c *cart.Context) error {
	if s.apps == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_service_unavailable"})
		return nil
	}
	rel, _ := c.Param("path")
	parts := strings.SplitN(strings.TrimPrefix(rel, "/"), "/", 2)
	if len(parts) != 2 {
		return badRequest(c, "invalid app skill path")
	}
	id := parts[0]
	skillPath := parts[1]
	detail, err := s.apps.ReadSkill(c.Request.Context(), id, skillPath)
	if err != nil {
		if errors.Is(err, app.ErrInvalidID) {
			return badRequest(c, "invalid app id")
		}
		if errors.Is(err, app.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "app_skill_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, detail)
	return nil
}

func (s *Server) listAppConnections(c *cart.Context) error {
	cfg, ok := s.appConnectionConfig(c)
	if !ok {
		return nil
	}
	conns, err := cfg.ListAppConnections(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	views := make([]app.ConnectionView, 0, len(conns))
	for _, conn := range conns {
		views = append(views, app.ViewConnection(conn))
	}
	c.JSON(http.StatusOK, app.AppConnectionsView{Connections: views})
	return nil
}

func (s *Server) getAppConnection(c *cart.Context) error {
	id, _ := c.Param("id")
	cfg, ok := s.appConnectionConfig(c)
	if !ok {
		return nil
	}
	conn, err := cfg.GetAppConnection(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "app_connection_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, app.ViewConnectionDetail(conn))
	return nil
}

func (s *Server) putAppConnection(c *cart.Context) error {
	id, _ := c.Param("id")
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/ ") {
		return badRequest(c, "connection id is required and must not contain '/' or spaces")
	}
	cfg, ok := s.appConnectionConfig(c)
	if !ok {
		return nil
	}
	var req putAppConnectionReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	authType := strings.TrimSpace(req.AuthType)
	if authType == "" {
		authType = "none"
	}
	conn := &app.Connection{
		ID:    id,
		Name:  strings.TrimSpace(req.Name),
		AppID: strings.TrimSpace(req.AppID),
		Auth: app.Auth{
			Type:     authType,
			Token:    req.Token,
			Prefix:   strings.TrimSpace(req.Prefix),
			Header:   strings.TrimSpace(req.Header),
			Username: req.Username,
			Password: req.Password,
		},
	}
	if conn.AppID == "" {
		return badRequest(c, "appID is required")
	}
	if req.Token == "" && req.Password == "" {
		if existing, err := cfg.GetAppConnection(c.Request.Context(), id); err == nil {
			conn.Auth.Token = existing.Auth.Token
			conn.Auth.Password = existing.Auth.Password
			if conn.Auth.Username == "" {
				conn.Auth.Username = existing.Auth.Username
			}
		}
	}
	if err := cfg.PutAppConnection(c.Request.Context(), conn); err != nil {
		return s.fail(c, err)
	}
	updated, err := cfg.GetAppConnection(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, app.ViewConnection(updated))
	return nil
}

func (s *Server) deleteAppConnection(c *cart.Context) error {
	id, _ := c.Param("id")
	cfg, ok := s.appConnectionConfig(c)
	if !ok {
		return nil
	}
	if err := cfg.DeleteAppConnection(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) listSessionAppGrants(c *cart.Context) error {
	id, _ := c.Param("id")
	grants, err := s.store.ListSessionAppGrants(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"grants": grants})
	return nil
}

func (s *Server) putSessionAppGrant(c *cart.Context) error {
	id, _ := c.Param("id")
	var req putSessionAppGrantReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	grant := &store.SessionAppGrant{
		SessionID:        id,
		AppID:            strings.TrimSpace(req.AppID),
		ConnectionID:     strings.TrimSpace(req.ConnectionID),
		AllowedEndpoints: req.AllowedEndpoints,
		Permissions:      req.Permissions,
		Constraints:      req.Constraints,
	}
	out, err := s.store.PutSessionAppGrant(c.Request.Context(), grant)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, out)
	return nil
}

func (s *Server) deleteSessionAppGrant(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	appID, _ := c.Param("appID")
	connectionID, _ := c.Param("connectionID")
	if err := s.store.DeleteSessionAppGrant(c.Request.Context(), sessionID, appID, connectionID); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) appConnectionConfig(c *cart.Context) (appConnectionConfig, bool) {
	cfg, ok := s.config.(appConnectionConfig)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_connection_config_unavailable"})
		return nil, false
	}
	return cfg, true
}
