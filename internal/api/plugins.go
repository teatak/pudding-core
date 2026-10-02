package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type pluginConnectionConfig interface {
	ListPluginConnections(ctx context.Context) ([]*plugin.Connection, error)
	GetPluginConnection(ctx context.Context, id string) (*plugin.Connection, error)
	PutPluginConnection(ctx context.Context, conn *plugin.Connection) error
	DeletePluginConnection(ctx context.Context, id string) error
}

type pluginMCPOverrideConfig interface {
	GetMCPOverride(ctx context.Context, pluginID, endpointName string) (plugin.MCPEndpointOverride, bool, error)
	PutMCPOverride(ctx context.Context, pluginID, endpointName string, override plugin.MCPEndpointOverride) (plugin.MCPEndpointOverride, error)
	DeleteMCPOverride(ctx context.Context, pluginID, endpointName string) error
}

type pluginMCPConfigService interface {
	ImportMCPPlugins(ctx context.Context, configJSON []byte, displayName string) ([]*plugin.Definition, error)
	GetMCPPluginConfig(ctx context.Context, id string) ([]byte, error)
	UpdateMCPPlugin(ctx context.Context, id string, configJSON []byte, displayName string) (*plugin.Definition, error)
}

type pluginEnablementConfig interface {
	DeletePluginEnablement(ctx context.Context, id string) error
}

type putPluginConnectionReq struct {
	PluginID     string            `json:"pluginID"`
	Name         string            `json:"name"`
	AuthMethodID string            `json:"authMethodID"`
	AuthType     string            `json:"authType"`
	Token        string            `json:"token"`
	Prefix       string            `json:"prefix"`
	Header       string            `json:"header"`
	Username     string            `json:"username"`
	Password     string            `json:"password"`
	Fields       map[string]string `json:"fields"`
	EndpointURLs map[string]string `json:"endpointURLs"`
}

type installPluginReq struct {
	PackageJSON   string `json:"packageJSON"`
	PackageSHA256 string `json:"packageSHA256"`
	SourceURL     string `json:"sourceURL"`
}

type putPluginEnabledReq struct {
	Enabled *bool `json:"enabled"`
}

type pluginMCPConfigReq struct {
	ConfigJSON string `json:"configJSON"`
	Name       string `json:"name,omitempty"`
}

type pluginMCPOverrideView struct {
	Configured bool                       `json:"configured"`
	Override   plugin.MCPEndpointOverride `json:"override"`
}

const maxInstallPluginRequestBytes = 2*plugin.MaxPackageJSONBytes + 64<<10
const maxMCPPluginConfigRequestBytes = 2 << 20

func (s *Server) listPlugins(c *cart.Context) error {
	if s.plugins == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_service_unavailable"})
		return nil
	}
	plugins, err := s.plugins.ListDefinitions(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	enrichBuiltinPluginTools(plugins)
	c.JSON(http.StatusOK, map[string]any{"plugins": plugins})
	return nil
}

func enrichBuiltinPluginTools(definitions []*plugin.Definition) {
	descriptions := make(map[string]string)
	for _, definition := range tool.BuiltinDefinitions() {
		descriptions[definition.Name] = definition.Description
	}
	for _, definition := range definitions {
		if definition == nil || definition.Source != plugin.SourceBuiltin {
			continue
		}
		for index := range definition.Tools {
			if definition.Tools[index].Description != "" {
				continue
			}
			definition.Tools[index].Description = descriptions[definition.Tools[index].Name]
		}
	}
}

func (s *Server) installPlugin(c *cart.Context) error {
	if s.plugins == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_service_unavailable"})
		return nil
	}
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxInstallPluginRequestBytes)
	var req installPluginReq
	if err := decode(c, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "plugin_package_too_large"})
			return nil
		}
		return badRequest(c, "invalid json body")
	}
	if strings.TrimSpace(req.PackageJSON) == "" {
		return badRequest(c, "packageJSON is required")
	}
	def, err := s.plugins.InstallPackage(c.Request.Context(), []byte(req.PackageJSON), req.PackageSHA256, req.SourceURL)
	if err != nil {
		if errors.Is(err, plugin.ErrPackageTooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "plugin_package_too_large"})
			return nil
		}
		if errors.Is(err, plugin.ErrBuiltinPlugin) {
			c.JSON(http.StatusConflict, map[string]string{"error": "builtin_plugin_id_reserved"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, def)
	return nil
}

func (s *Server) importMCPPlugins(c *cart.Context) error {
	plugins, ok := s.plugins.(pluginMCPConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_mcp_config_unavailable"})
		return nil
	}
	req, ok := decodePluginMCPConfigRequest(c)
	if !ok {
		return nil
	}
	definitions, err := plugins.ImportMCPPlugins(c.Request.Context(), []byte(req.ConfigJSON), req.Name)
	if err != nil {
		return s.handlePluginMCPConfigError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"plugins": definitions})
	return nil
}

func (s *Server) getMCPPluginConfig(c *cart.Context) error {
	plugins, ok := s.plugins.(pluginMCPConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_mcp_config_unavailable"})
		return nil
	}
	id, _ := c.Param("id")
	configJSON, err := plugins.GetMCPPluginConfig(c.Request.Context(), id)
	if err != nil {
		return s.handlePluginMCPConfigError(c, err)
	}
	c.JSON(http.StatusOK, map[string]string{"configJSON": string(configJSON)})
	return nil
}

func (s *Server) putMCPPluginConfig(c *cart.Context) error {
	plugins, ok := s.plugins.(pluginMCPConfigService)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_mcp_config_unavailable"})
		return nil
	}
	req, ok := decodePluginMCPConfigRequest(c)
	if !ok {
		return nil
	}
	id, _ := c.Param("id")
	definition, err := plugins.UpdateMCPPlugin(c.Request.Context(), id, []byte(req.ConfigJSON), req.Name)
	if err != nil {
		return s.handlePluginMCPConfigError(c, err)
	}
	c.JSON(http.StatusOK, definition)
	return nil
}

func decodePluginMCPConfigRequest(c *cart.Context) (pluginMCPConfigReq, bool) {
	c.Request.Body = http.MaxBytesReader(c.Response, c.Request.Body, maxMCPPluginConfigRequestBytes)
	var req pluginMCPConfigReq
	if err := decode(c, &req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": "plugin_mcp_config_too_large"})
			return pluginMCPConfigReq{}, false
		}
		_ = badRequest(c, "invalid json body")
		return pluginMCPConfigReq{}, false
	}
	if strings.TrimSpace(req.ConfigJSON) == "" {
		_ = badRequest(c, "configJSON is required")
		return pluginMCPConfigReq{}, false
	}
	return req, true
}

func (s *Server) handlePluginMCPConfigError(c *cart.Context, err error) error {
	if errors.Is(err, plugin.ErrInvalidID) || errors.Is(err, plugin.ErrInvalidMCPPluginConfig) {
		return badRequest(c, err.Error())
	}
	if errors.Is(err, plugin.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "mcp_plugin_not_found"})
		return nil
	}
	if errors.Is(err, plugin.ErrAlreadyExists) || errors.Is(err, plugin.ErrBuiltinPlugin) {
		c.JSON(http.StatusConflict, map[string]string{"error": "mcp_plugin_conflict"})
		return nil
	}
	return s.fail(c, err)
}

func (s *Server) putPluginEnabled(c *cart.Context) error {
	plugins, ok := s.plugins.(pluginEnableService)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_enablement_unavailable"})
		return nil
	}
	var req putPluginEnabledReq
	if err := decode(c, &req); err != nil || req.Enabled == nil {
		return badRequest(c, "enabled is required")
	}
	id, _ := c.Param("id")
	def, err := plugins.SetEnabled(c.Request.Context(), id, *req.Enabled)
	if errors.Is(err, plugin.ErrInvalidID) {
		return badRequest(c, "invalid plugin id")
	}
	if errors.Is(err, plugin.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_not_found"})
		return nil
	}
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, def)
	return nil
}

func (s *Server) deletePlugin(c *cart.Context) error {
	if s.plugins == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_service_unavailable"})
		return nil
	}
	id, _ := c.Param("id")
	id = strings.TrimSpace(id)
	var cfg pluginConnectionConfig
	var conns []*plugin.Connection
	if candidate, ok := s.config.(pluginConnectionConfig); ok {
		cfg = candidate
		var err error
		conns, err = cfg.ListPluginConnections(c.Request.Context())
		if err != nil {
			return s.fail(c, err)
		}
	}
	sessions, err := s.store.ListSessions(c.Request.Context(), store.SessionListOptions{Scope: store.SessionListAll})
	if err != nil {
		return s.fail(c, err)
	}
	deleteErr := s.plugins.DeleteDefinition(c.Request.Context(), id)
	if deleteErr != nil && !errors.Is(deleteErr, plugin.ErrNotFound) {
		if errors.Is(deleteErr, plugin.ErrInvalidID) {
			return badRequest(c, "invalid plugin id")
		}
		if errors.Is(deleteErr, plugin.ErrBuiltinPlugin) {
			c.JSON(http.StatusConflict, map[string]string{"error": "builtin_plugin_cannot_be_uninstalled"})
			return nil
		}
		return s.fail(c, deleteErr)
	}
	var cleanupErr error
	if cfg != nil {
		for _, conn := range conns {
			if conn == nil || conn.PluginID != id {
				continue
			}
			if err := cfg.DeletePluginConnection(c.Request.Context(), conn.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
	}
	for _, session := range sessions {
		if session == nil {
			continue
		}
		loaded := make([]string, 0, len(session.LoadedPluginIDs))
		for _, loadedID := range session.LoadedPluginIDs {
			if loadedID != id {
				loaded = append(loaded, loadedID)
			}
		}
		if len(loaded) == len(session.LoadedPluginIDs) {
			continue
		}
		if _, err := s.store.UpdateSession(c.Request.Context(), session.ID, store.SessionUpdate{LoadedPluginIDs: &loaded}); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if cfg, ok := s.config.(pluginEnablementConfig); ok {
		if err := cfg.DeletePluginEnablement(c.Request.Context(), id); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if cleanupErr != nil {
		return s.fail(c, cleanupErr)
	}
	if errors.Is(deleteErr, plugin.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_not_found"})
		return nil
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) getPluginMCPOverride(c *cart.Context) error {
	cfg, ok := s.pluginMCPOverrideConfig(c)
	if !ok {
		return nil
	}
	pluginID, _ := c.Param("id")
	endpointName, _ := c.Param("endpoint")
	override, configured, err := cfg.GetMCPOverride(c.Request.Context(), pluginID, endpointName)
	if err != nil {
		return s.handlePluginMCPOverrideError(c, err)
	}
	c.JSON(http.StatusOK, pluginMCPOverrideView{Configured: configured, Override: override})
	return nil
}

func (s *Server) putPluginMCPOverride(c *cart.Context) error {
	cfg, ok := s.pluginMCPOverrideConfig(c)
	if !ok {
		return nil
	}
	pluginID, _ := c.Param("id")
	endpointName, _ := c.Param("endpoint")
	var req plugin.MCPEndpointOverride
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	override, err := cfg.PutMCPOverride(c.Request.Context(), pluginID, endpointName, req)
	if err != nil {
		return s.handlePluginMCPOverrideError(c, err)
	}
	c.JSON(http.StatusOK, pluginMCPOverrideView{Configured: true, Override: override})
	return nil
}

func (s *Server) deletePluginMCPOverride(c *cart.Context) error {
	cfg, ok := s.pluginMCPOverrideConfig(c)
	if !ok {
		return nil
	}
	pluginID, _ := c.Param("id")
	endpointName, _ := c.Param("endpoint")
	if err := cfg.DeleteMCPOverride(c.Request.Context(), pluginID, endpointName); err != nil {
		return s.handlePluginMCPOverrideError(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) handlePluginMCPOverrideError(c *cart.Context, err error) error {
	if errors.Is(err, plugin.ErrInvalidID) {
		return badRequest(c, "invalid plugin id")
	}
	if errors.Is(err, plugin.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_mcp_endpoint_not_found"})
		return nil
	}
	if errors.Is(err, plugin.ErrInvalidMCPOverride) {
		return badRequest(c, err.Error())
	}
	return s.fail(c, err)
}

func (s *Server) getPluginAsset(c *cart.Context) error {
	if s.plugins == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_service_unavailable"})
		return nil
	}
	rel, _ := c.Param("path")
	data, contentType, err := s.plugins.ReadAsset(c.Request.Context(), rel)
	if err != nil {
		if errors.Is(err, plugin.ErrInvalidAsset) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_asset_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, contentType, data)
	return nil
}

func (s *Server) getPluginSkill(c *cart.Context) error {
	if s.plugins == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_service_unavailable"})
		return nil
	}
	rel, _ := c.Param("path")
	parts := strings.SplitN(strings.TrimPrefix(rel, "/"), "/", 2)
	if len(parts) != 2 {
		return badRequest(c, "invalid plugin skill path")
	}
	id := parts[0]
	skillPath := parts[1]
	detail, err := s.plugins.ReadSkillDetail(c.Request.Context(), id, skillPath)
	if err != nil {
		if errors.Is(err, plugin.ErrInvalidID) {
			return badRequest(c, "invalid plugin id")
		}
		if errors.Is(err, plugin.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_skill_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, detail)
	return nil
}

func (s *Server) listPluginConnections(c *cart.Context) error {
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	conns, err := cfg.ListPluginConnections(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	views := make([]plugin.ConnectionView, 0, len(conns))
	for _, conn := range conns {
		views = append(views, plugin.ViewConnection(conn))
	}
	c.JSON(http.StatusOK, plugin.PluginConnectionsView{Connections: views})
	return nil
}

func (s *Server) getPluginConnection(c *cart.Context) error {
	id, _ := c.Param("id")
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	conn, err := cfg.GetPluginConnection(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_connection_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, plugin.ViewConnectionDetail(conn))
	return nil
}

func (s *Server) putPluginConnection(c *cart.Context) error {
	id, _ := c.Param("id")
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/ ") {
		return badRequest(c, "connection id is required and must not contain '/' or spaces")
	}
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	var req putPluginConnectionReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	pluginID := strings.TrimSpace(req.PluginID)
	if pluginID == "" {
		return badRequest(c, "pluginID is required")
	}
	def, err := s.getPluginDefinition(c.Request.Context(), pluginID)
	if err != nil {
		if errors.Is(err, plugin.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "plugin_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	method, ok := plugin.FindAuthMethod(def, req.AuthMethodID, req.AuthType)
	if !ok {
		method, ok = pluginConnectionOnlyAuthMethod(def, req)
		if !ok {
			return badRequest(c, "auth method is not supported by plugin")
		}
	}
	var existing *plugin.Connection
	if found, err := cfg.GetPluginConnection(c.Request.Context(), id); err == nil {
		existing = found
	}
	fields, err := normalizePluginConnectionFields(def.Connection, req.Fields, existing)
	if err != nil {
		return badRequest(c, err.Error())
	}
	endpointURLsInput := req.EndpointURLs
	if endpointURLsInput == nil && existing != nil && existing.PluginID == pluginID {
		endpointURLsInput = existing.EndpointURLs
	}
	endpointURLs, err := plugin.NormalizeConnectionEndpointURLs(def, endpointURLsInput)
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
	conn := &plugin.Connection{
		ID:           id,
		Name:         strings.TrimSpace(req.Name),
		PluginID:     pluginID,
		Fields:       fields,
		EndpointURLs: endpointURLs,
		Auth: plugin.Auth{
			MethodID: method.ID,
			Type:     method.Type,
			Token:    req.Token,
			Prefix:   prefix,
			Header:   header,
			Username: req.Username,
			Password: req.Password,
		},
	}
	sameExistingIdentity := existing != nil && existing.PluginID == pluginID && samePluginConnectionAuthMethod(existing.Auth, method)
	if sameExistingIdentity {
		conn.Account = plugin.CloneConnection(existing).Account
	}
	if req.Token == "" && req.Password == "" {
		if sameExistingIdentity {
			conn.Auth.Token = existing.Auth.Token
			conn.Auth.AccessToken = existing.Auth.AccessToken
			conn.Auth.RefreshToken = existing.Auth.RefreshToken
			conn.Auth.TokenType = existing.Auth.TokenType
			conn.Auth.Variant = existing.Auth.Variant
			conn.Auth.ExpiresAt = existing.Auth.ExpiresAt
			conn.Auth.RefreshExpiresAt = existing.Auth.RefreshExpiresAt
			conn.Auth.Scopes = append([]string(nil), existing.Auth.Scopes...)
			conn.Auth.Password = existing.Auth.Password
			if conn.Auth.Username == "" {
				conn.Auth.Username = existing.Auth.Username
			}
		}
	}
	if err := validatePluginConnectionAuth(conn.Auth); err != nil {
		return badRequest(c, err.Error())
	}
	patTokenChanged := strings.TrimSpace(req.Token) != "" && (existing == nil || req.Token != existing.Auth.Token)
	if pluginID == "github" && method.ID == plugin.GitHubPATAuthMethodID && (patTokenChanged || conn.Account == nil) {
		account, err := s.github.Account(c.Request.Context(), conn.Auth.Token)
		if err != nil {
			return badRequest(c, "github account could not be verified: "+err.Error())
		}
		conn.Account = &plugin.ConnectionAccount{
			ID: account.ID, Login: account.Login, Name: account.Name, AvatarURL: account.AvatarURL, Type: account.Type,
		}
	}
	if err := cfg.PutPluginConnection(c.Request.Context(), conn); err != nil {
		return s.fail(c, err)
	}
	updated, err := cfg.GetPluginConnection(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, plugin.ViewConnection(updated))
	return nil
}

func pluginConnectionOnlyAuthMethod(def *plugin.Definition, req putPluginConnectionReq) (plugin.AuthMethod, bool) {
	if def == nil || (def.Auth != nil && def.Auth.Required) {
		return plugin.AuthMethod{}, false
	}
	if strings.TrimSpace(req.AuthMethodID) != "" {
		return plugin.AuthMethod{}, false
	}
	authType := strings.TrimSpace(req.AuthType)
	if authType != "" && authType != plugin.AuthTypeNone {
		return plugin.AuthMethod{}, false
	}
	return plugin.AuthMethod{Type: plugin.AuthTypeNone}, true
}

func samePluginConnectionAuthMethod(auth plugin.Auth, method plugin.AuthMethod) bool {
	authMethodID := strings.TrimSpace(auth.MethodID)
	methodID := strings.TrimSpace(method.ID)
	if authMethodID != "" && methodID != "" {
		return authMethodID == methodID
	}
	return strings.TrimSpace(auth.Type) == strings.TrimSpace(method.Type)
}

func normalizePluginConnectionFields(config *plugin.ConnectionConfig, values map[string]string, existing *plugin.Connection) (map[string]string, error) {
	if config == nil || len(config.Fields) == 0 {
		if len(values) > 0 {
			return nil, errors.New("connection fields are not supported by plugin")
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
			for _, rule := range field.Inject {
				switch strings.TrimSpace(rule.Target) {
				case "header":
					if !plugin.IsAllowedRequestHeaderValue(value) {
						return nil, errors.New("connection field " + id + " contains an invalid header value")
					}
				case "env":
					if strings.ContainsRune(value, 0) {
						return nil, errors.New("connection field " + id + " contains a NUL byte")
					}
				}
			}
			out[id] = value
		}
	}
	for id := range values {
		if _, ok := seen[id]; !ok {
			return nil, errors.New("connection field " + id + " is not supported by plugin")
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validatePluginConnectionAuth(auth plugin.Auth) error {
	switch strings.TrimSpace(auth.Type) {
	case plugin.AuthTypeNone:
		return nil
	case plugin.AuthTypeBearer:
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("bearer token is required")
		}
		if !plugin.IsAllowedRequestHeaderValue(auth.Token) {
			return errors.New("bearer token contains an invalid header value")
		}
	case plugin.AuthTypeToken:
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("token is required")
		}
		if !plugin.IsAllowedRequestHeaderValue(auth.Token) || !plugin.IsAllowedRequestHeaderValue(auth.Prefix) {
			return errors.New("token contains an invalid header value")
		}
	case plugin.AuthTypeHeader:
		if !plugin.IsAllowedRequestHeaderName(auth.Header) {
			return errors.New("header is invalid or not allowed")
		}
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("header token is required")
		}
		if !plugin.IsAllowedRequestHeaderValue(auth.Token) {
			return errors.New("header token contains an invalid header value")
		}
	case plugin.AuthTypeBasic:
		if strings.TrimSpace(auth.Username) == "" && strings.TrimSpace(auth.Password) == "" {
			return errors.New("username or password is required")
		}
	case plugin.AuthTypeOAuth2:
		if strings.TrimSpace(auth.AccessToken) == "" {
			return errors.New("oauth2 access token is required")
		}
		if !plugin.IsAllowedRequestHeaderValue(auth.AccessToken) || strings.ContainsAny(strings.TrimSpace(auth.TokenType), "\r\n \t") {
			return errors.New("oauth2 token contains an invalid header value")
		}
	case plugin.AuthTypeTokenExchange:
		return nil
	default:
		return errors.New("auth type is not supported")
	}
	return nil
}

func (s *Server) getPluginDefinition(ctx context.Context, id string) (*plugin.Definition, error) {
	if s.plugins == nil {
		return nil, errors.New("plugin service unavailable")
	}
	defs, err := s.plugins.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		if def != nil && def.ID == id {
			return def, nil
		}
	}
	return nil, plugin.ErrNotFound
}

func (s *Server) deletePluginConnection(c *cart.Context) error {
	id, _ := c.Param("id")
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	connection, err := cfg.GetPluginConnection(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	if connection.PluginID == "github" && connection.Auth.Type == plugin.AuthTypeOAuth2 && connection.Auth.Variant == plugin.GitHubAppAuthVariant && strings.TrimSpace(connection.Auth.AccessToken) != "" {
		if err := s.oauthBroker.Revoke(c.Request.Context(), "github", connection.Auth.AccessToken); err != nil {
			return s.fail(c, err)
		}
	}
	if err := cfg.DeletePluginConnection(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) pluginConnectionConfig(c *cart.Context) (pluginConnectionConfig, bool) {
	cfg, ok := s.config.(pluginConnectionConfig)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_connection_config_unavailable"})
		return nil, false
	}
	return cfg, true
}

func (s *Server) pluginMCPOverrideConfig(c *cart.Context) (pluginMCPOverrideConfig, bool) {
	cfg, ok := s.plugins.(pluginMCPOverrideConfig)
	if !ok {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "plugin_mcp_override_config_unavailable"})
		return nil, false
	}
	return cfg, true
}
