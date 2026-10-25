package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

type pluginSessionLoginReq struct {
	EndpointInsecureTLS map[string]bool   `json:"endpointInsecureTLS"`
	PluginID            string            `json:"pluginID"`
	Name                string            `json:"name"`
	AuthMethodID        string            `json:"authMethodID"`
	EndpointURLs        map[string]string `json:"endpointURLs"`
	Inputs              map[string]string `json:"inputs"`
}

func (s *Server) loginPluginConnection(c *cart.Context) error {
	id, _ := c.Param("id")
	if id == "" || strings.ContainsAny(id, "/ ") {
		return badRequest(c, "invalid connection id")
	}
	cfg, ok := s.pluginConnectionConfig(c)
	if !ok {
		return nil
	}
	var req pluginSessionLoginReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	def, err := s.getPluginDefinition(c.Request.Context(), req.PluginID)
	if err != nil {
		if errors.Is(err, plugin.ErrNotFound) {
			return badRequest(c, "plugin not found")
		}
		return s.fail(c, err)
	}
	method, ok := plugin.FindAuthMethod(def, req.AuthMethodID, plugin.AuthTypeSession)
	if !ok || method.Type != plugin.AuthTypeSession {
		return badRequest(c, "session login is not supported")
	}
	existing, err := cfg.GetPluginConnection(c.Request.Context(), id)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return s.fail(c, err)
	}
	var expected time.Time
	if existing != nil {
		if existing.PluginID != def.ID {
			return badRequest(c, "connection belongs to another plugin")
		}
		expected = existing.UpdatedAt
	}
	endpointURLs, err := plugin.NormalizeConnectionEndpointURLs(def, req.EndpointURLs)
	if err != nil {
		return badRequest(c, err.Error())
	}
	endpointTLS, err := plugin.NormalizeConnectionEndpointInsecureTLS(def, endpointURLs, req.EndpointInsecureTLS, existing)
	if err != nil {
		return badRequest(c, err.Error())
	}
	baseURL := def.Endpoints[method.Session.Endpoint].URL
	if value := endpointURLs[method.Session.Endpoint]; value != "" {
		baseURL = value
	}
	result := plugin.AcquireSession(c.Request.Context(), http.DefaultClient, method.Session, baseURL, req.Inputs, endpointTLS[method.Session.Endpoint])
	if result.Status != "success" {
		c.JSON(http.StatusOK, result)
		return nil
	}
	// Do not merge previous credentials: an optional token omitted by this login
	// must not survive from a different account or server.
	fields, err := normalizePluginConnectionFields(def.Connection, result.Fields, nil)
	if err != nil {
		c.JSON(http.StatusOK, plugin.SessionLoginResult{Status: "error", Code: "invalid_credentials"})
		return nil
	}
	conn := &plugin.Connection{ID: id, PluginID: def.ID, Name: strings.TrimSpace(req.Name), Fields: fields, EndpointURLs: endpointURLs, EndpointInsecureTLS: endpointTLS, Account: result.Account, Auth: plugin.Auth{Type: plugin.AuthTypeSession, MethodID: method.ID, SessionEstablished: true, ExpiresAt: result.ExpiresAt}}
	if err := cfg.PutPluginConnectionIfUnchanged(c.Request.Context(), conn, expected); err != nil {
		if errors.Is(err, config.ErrInvalidSetting) {
			c.JSON(http.StatusConflict, map[string]string{"error": "connection_changed"})
			return nil
		}
		return s.fail(c, err)
	}
	saved, err := cfg.GetPluginConnection(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, struct {
		Status     string                `json:"status"`
		Connection plugin.ConnectionView `json:"connection"`
	}{"success", plugin.ViewConnection(saved)})
	return nil
}
