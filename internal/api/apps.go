package api

import (
	"context"
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
	AppID        string            `json:"appID"`
	Name         string            `json:"name"`
	AuthMethodID string            `json:"authMethodID"`
	AuthType     string            `json:"authType"`
	Token        string            `json:"token"`
	Prefix       string            `json:"prefix"`
	Header       string            `json:"header"`
	Username     string            `json:"username"`
	Password     string            `json:"password"`
	Fields       map[string]string `json:"fields"`
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
	id = strings.TrimSpace(id)
	var cfg appConnectionConfig
	var conns []*app.Connection
	if candidate, ok := s.config.(appConnectionConfig); ok {
		cfg = candidate
		var err error
		conns, err = cfg.ListAppConnections(c.Request.Context())
		if err != nil {
			return s.fail(c, err)
		}
	}
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
	if cfg != nil {
		for _, conn := range conns {
			if conn == nil || conn.AppID != id {
				continue
			}
			if err := cfg.DeleteAppConnection(c.Request.Context(), conn.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				return s.fail(c, err)
			}
		}
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
	appID := strings.TrimSpace(req.AppID)
	if appID == "" {
		return badRequest(c, "appID is required")
	}
	def, err := s.getAppDefinition(c.Request.Context(), appID)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "app_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	method, ok := app.FindAuthMethod(def, req.AuthMethodID, req.AuthType)
	if !ok {
		method, ok = appConnectionOnlyAuthMethod(def, req)
		if !ok {
			return badRequest(c, "auth method is not supported by app")
		}
	}
	var existing *app.Connection
	if found, err := cfg.GetAppConnection(c.Request.Context(), id); err == nil {
		existing = found
	}
	fields, err := normalizeAppConnectionFields(def.Connection, req.Fields, existing)
	if err != nil {
		return badRequest(c, err.Error())
	}
	prefix := strings.TrimSpace(req.Prefix)
	if prefix == "" {
		prefix = method.Prefix
	}
	header := strings.TrimSpace(req.Header)
	if header == "" {
		header = method.Header
	}
	conn := &app.Connection{
		ID:     id,
		Name:   strings.TrimSpace(req.Name),
		AppID:  appID,
		Fields: fields,
		Auth: app.Auth{
			MethodID: method.ID,
			Type:     method.Type,
			Token:    req.Token,
			Prefix:   prefix,
			Header:   header,
			Username: req.Username,
			Password: req.Password,
		},
	}
	if req.Token == "" && req.Password == "" {
		if existing != nil {
			if sameAppConnectionAuthMethod(existing.Auth, method) {
				conn.Auth.Token = existing.Auth.Token
				conn.Auth.AccessToken = existing.Auth.AccessToken
				conn.Auth.RefreshToken = existing.Auth.RefreshToken
				conn.Auth.TokenType = existing.Auth.TokenType
				conn.Auth.ExpiresAt = existing.Auth.ExpiresAt
				conn.Auth.Scopes = append([]string(nil), existing.Auth.Scopes...)
				conn.Auth.Password = existing.Auth.Password
				if conn.Auth.Username == "" {
					conn.Auth.Username = existing.Auth.Username
				}
			}
		}
	}
	if err := validateAppConnectionAuth(conn.Auth); err != nil {
		return badRequest(c, err.Error())
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

func appConnectionOnlyAuthMethod(def *app.Definition, req putAppConnectionReq) (app.AuthMethod, bool) {
	if def == nil || (def.Auth != nil && def.Auth.Required) {
		return app.AuthMethod{}, false
	}
	if strings.TrimSpace(req.AuthMethodID) != "" {
		return app.AuthMethod{}, false
	}
	authType := strings.TrimSpace(req.AuthType)
	if authType != "" && authType != app.AuthTypeNone {
		return app.AuthMethod{}, false
	}
	return app.AuthMethod{Type: app.AuthTypeNone}, true
}

func sameAppConnectionAuthMethod(auth app.Auth, method app.AuthMethod) bool {
	authMethodID := strings.TrimSpace(auth.MethodID)
	methodID := strings.TrimSpace(method.ID)
	if authMethodID != "" && methodID != "" {
		return authMethodID == methodID
	}
	return strings.TrimSpace(auth.Type) == strings.TrimSpace(method.Type)
}

func normalizeAppConnectionFields(config *app.ConnectionConfig, values map[string]string, existing *app.Connection) (map[string]string, error) {
	if config == nil || len(config.Fields) == 0 {
		if len(values) > 0 {
			return nil, errors.New("connection fields are not supported by app")
		}
		return nil, nil
	}
	out := make(map[string]string, len(config.Fields))
	seen := map[string]struct{}{}
	for _, field := range config.Fields {
		id := strings.TrimSpace(field.ID)
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
		value := strings.TrimSpace(values[id])
		if value == "" && field.Secret && existing != nil {
			value = strings.TrimSpace(existing.Fields[id])
		}
		if field.Required && value == "" {
			return nil, errors.New("connection field " + id + " is required")
		}
		if value != "" {
			out[id] = value
		}
	}
	for id := range values {
		if _, ok := seen[id]; !ok {
			return nil, errors.New("connection field " + id + " is not supported by app")
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validateAppConnectionAuth(auth app.Auth) error {
	switch strings.TrimSpace(auth.Type) {
	case app.AuthTypeNone:
		return nil
	case app.AuthTypeBearer:
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("bearer token is required")
		}
	case app.AuthTypeToken:
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("token is required")
		}
	case app.AuthTypeHeader:
		if strings.TrimSpace(auth.Header) == "" {
			return errors.New("header is required")
		}
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("header token is required")
		}
	case app.AuthTypeBasic:
		if strings.TrimSpace(auth.Username) == "" && strings.TrimSpace(auth.Password) == "" {
			return errors.New("username or password is required")
		}
	case app.AuthTypeOAuth2:
		if strings.TrimSpace(auth.AccessToken) == "" {
			return errors.New("oauth2 access token is required")
		}
	default:
		return errors.New("auth type is not supported")
	}
	return nil
}

func (s *Server) getAppDefinition(ctx context.Context, id string) (*app.Definition, error) {
	if s.apps == nil {
		return nil, errors.New("app service unavailable")
	}
	defs, err := s.apps.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		if def != nil && def.ID == id {
			return def, nil
		}
	}
	return nil, app.ErrNotFound
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

func (s *Server) appConnectionConfig(c *cart.Context) (appConnectionConfig, bool) {
	cfg, ok := s.config.(appConnectionConfig)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "app_connection_config_unavailable"})
		return nil, false
	}
	return cfg, true
}
