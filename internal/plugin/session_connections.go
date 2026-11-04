package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

type SessionConnectionStore interface {
	GetSessionPluginConnection(context.Context, string, string) (*store.SessionPluginConnection, error)
	SetSessionPluginConnection(context.Context, string, string, string, string) error
}

type SessionConnectionView struct {
	PluginID     string           `json:"pluginID"`
	Selection    string           `json:"selection"`
	ConnectionID string           `json:"connectionID,omitempty"`
	Required     bool             `json:"required"`
	Connections  []ConnectionView `json:"connections"`
}

func (s *Service) WithSessionConnections(source SessionConnectionStore) *Service {
	s.sessionConnections = source
	return s
}

func ConnectionSelection(def *Definition) string {
	if def.Connection != nil && def.Connection.Selection != "" {
		return def.Connection.Selection
	}
	return ConnectionSelectionUser
}

func connectionIdentity(c *Connection) string {
	identity := struct {
		PluginID, ID, Method, Type, CreatedAt string
		AccountID, AccountType                string
		Fields, EndpointURLs                  map[string]string
		EndpointInsecureTLS                   map[string]bool `json:",omitempty"`
		Credentials                           []string
	}{PluginID: store.CurrentPluginID(c.PluginID), ID: c.ID, Method: c.Auth.MethodID, Type: c.Auth.Type,
		CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z"), Fields: c.Fields, EndpointURLs: c.EndpointURLs, EndpointInsecureTLS: c.EndpointInsecureTLS}
	if c.Account != nil {
		identity.AccountID = c.Account.ID
		identity.AccountType = c.Account.Type
		if identity.AccountID == "" {
			identity.AccountID = c.Account.Login
		}
	}
	if (c.Auth.Type == AuthTypeSession || (c.Account != nil && c.Account.Type == "email")) && identity.AccountID != "" {
		// Session credential rotation does not change the account grant.
		identity.Fields = nil
	}
	if identity.AccountID == "" {
		// Without a verified account identity, replacement credentials may be a
		// different account. Re-select instead of silently carrying its chat grant.
		identity.Credentials = []string{c.Auth.Token, c.Auth.AccessToken, c.Auth.Username, c.Auth.Password}
	}
	raw, _ := json.Marshal(identity)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Service) SessionConnection(ctx context.Context, sessionID, pluginID string) (*SessionConnectionView, error) {
	def, err := s.definition(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := &SessionConnectionView{PluginID: def.ID, Selection: ConnectionSelection(def), Connections: []ConnectionView{}}
	var connections []*Connection
	if s.connections != nil {
		connections, err = s.connections.ListPluginConnections(ctx)
		if err != nil {
			return nil, err
		}
	}
	for _, c := range connectionsForPlugin(connections, def.ID) {
		out.Connections = append(out.Connections, ViewConnection(c))
	}
	out.Required = len(out.Connections) > 0 || (def.Auth != nil && def.Auth.Required)
	if def.Connection != nil {
		for _, field := range def.Connection.Fields {
			out.Required = out.Required || field.Required
		}
	}
	if !out.Required {
		return out, nil
	}
	c, err := s.selectedSessionConnection(ctx, sessionID, def)
	if err != nil {
		var choice *EndpointResolveError
		if errors.As(err, &choice) || errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrConnectionReauthorizationRequired) {
			return out, nil
		}
		return nil, err
	}
	out.ConnectionID, out.Required = c.ID, false
	return out, nil
}

// Used by the chat UI and plugin loading tool to persist a connection choice.
func (s *Service) SetSessionConnection(ctx context.Context, sessionID, pluginID, connectionID string) (*SessionConnectionView, error) {
	def, err := s.definition(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	if ConnectionSelection(def) != ConnectionSelectionUser {
		return nil, fmt.Errorf("%w: this plugin uses a global active connection", ErrInvalidID)
	}
	if s.sessionConnections == nil {
		return nil, fmt.Errorf("session connection storage unavailable")
	}
	connectionID = strings.TrimSpace(connectionID)
	identity := ""
	if connectionID != "" {
		c, err := s.ReadyConnection(ctx, connectionID)
		if err != nil {
			return nil, fmt.Errorf("%w: connection unavailable", ErrInvalidID)
		}
		if !connectionConfiguredFor(def, c) {
			return nil, fmt.Errorf("%w: connection does not belong to this plugin or is not configured", ErrInvalidID)
		}
		identity = connectionIdentity(c)
	}
	if err := s.sessionConnections.SetSessionPluginConnection(ctx, sessionID, def.ID, connectionID, identity); err != nil {
		return nil, err
	}
	return s.SessionConnection(ctx, sessionID, def.ID)
}

func connectionConfiguredFor(def *Definition, c *Connection) bool {
	if c == nil || store.CurrentPluginID(c.PluginID) != def.ID {
		return false
	}
	// The standard connection API also accepts configuration-only connections
	// without a declared auth method when authentication is optional.
	if c.Auth.MethodID == "" && (c.Auth.Type == AuthTypeNone || c.Auth.Type == "") && (def.Auth == nil || !def.Auth.Required) {
		return !ViewConnection(c).ReauthorizationRequired
	}
	method, ok := FindAuthMethod(def, c.Auth.MethodID, c.Auth.Type)
	return ok && method.Type == c.Auth.Type && (method.Type == AuthTypeNone || ViewConnection(c).TokenSet) && !ViewConnection(c).ReauthorizationRequired
}

func sessionConnectionChoices(def *Definition, connections []*Connection) []ConnectionChoice {
	choices := make([]ConnectionChoice, 0, len(connections))
	for _, c := range connectionsForPlugin(connections, def.ID) {
		choice := viewConnectionChoice(c)
		switch {
		case ViewConnection(c).ReauthorizationRequired:
			choice.Status = "reauthorization_required"
		case !connectionConfiguredFor(def, c):
			choice.Status = "not_configured"
		default:
			choice.Status = "available"
		}
		choices = append(choices, choice)
	}
	return choices
}

func markConnectionReauthorization(choices []ConnectionChoice, id string) {
	for i := range choices {
		if choices[i].ID == id {
			choices[i].Status = "reauthorization_required"
		}
	}
}

func (s *Service) selectedSessionConnection(ctx context.Context, sessionID string, def *Definition) (*Connection, error) {
	required := &EndpointResolveError{Reason: "connection_required", PluginID: def.ID}
	var connections []*Connection
	var err error
	if s.connections != nil {
		connections, err = s.connections.ListPluginConnections(ctx)
		if err != nil {
			return nil, err
		}
		required.Connections = sessionConnectionChoices(def, connections)
	}
	switch ConnectionSelection(def) {
	case ConnectionSelectionSingleActive:
		c, err := s.ResolveConnectionBinding(ctx, def.ID, "active")
		if err != nil {
			return nil, err
		}
		if !connectionConfiguredFor(def, c) {
			return nil, required
		}
		return c, nil
	case ConnectionSelectionUser:
		if s.sessionConnections == nil || sessionID == "" {
			return nil, required
		}
		saved, err := s.sessionConnections.GetSessionPluginConnection(ctx, sessionID, def.ID)
		if err != nil {
			return nil, err
		}
		if saved == nil {
			var selected *Connection
			reauthorizationRequired := false
			for _, candidate := range connectionsForPlugin(connections, def.ID) {
				if !connectionConfiguredFor(def, candidate) {
					continue
				}
				ready, err := s.ReadyConnection(ctx, candidate.ID)
				if errors.Is(err, ErrConnectionReauthorizationRequired) {
					reauthorizationRequired = true
					markConnectionReauthorization(required.Connections, candidate.ID)
					continue
				}
				if err != nil {
					return nil, err
				}
				if selected != nil {
					return nil, required
				}
				selected = ready
			}
			if selected == nil {
				if reauthorizationRequired {
					required.Reason = "connection_reauthorization_required"
				}
				return nil, required
			}
			return selected, nil
		}
		required.CurrentConnectionID = saved.ConnectionID
		c, err := s.ReadyConnection(ctx, saved.ConnectionID)
		if errors.Is(err, ErrConnectionReauthorizationRequired) {
			required.Reason = "connection_reauthorization_required"
			markConnectionReauthorization(required.Connections, saved.ConnectionID)
			return nil, required
		}
		if errors.Is(err, store.ErrNotFound) {
			return nil, required
		}
		if err != nil {
			return nil, err
		}
		if !connectionConfiguredFor(def, c) || saved.Identity != connectionIdentity(c) {
			return nil, required
		}
		return c, nil
	default:
		return nil, fmt.Errorf("unsupported connection selection policy")
	}
}

func (s *Service) ResolveSessionEndpoint(ctx context.Context, sessionID, pluginID, endpointName, connectionID string) (*EndpointBinding, error) {
	def, err := s.definition(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	if !def.Enabled {
		return nil, ErrDisabled
	}
	endpoint, ok := def.Endpoints[endpointName]
	if !ok {
		return nil, ErrNotFound
	}
	var connections []*Connection
	if s.connections != nil {
		connections, err = s.connections.ListPluginConnections(ctx)
		if err != nil {
			return nil, err
		}
	}
	if len(connectionsForPlugin(connections, def.ID)) == 0 && connectionID == "" {
		if b, ok := connectionlessEndpointBinding(def, endpointName, endpoint); ok {
			return b, nil
		}
	}
	c, err := s.selectedSessionConnection(ctx, sessionID, def)
	if err != nil {
		var choice *EndpointResolveError
		if errors.As(err, &choice) {
			choice.Endpoint, choice.PluginID = endpointName, def.ID
			return nil, requestedSessionConnectionError(def, connections, connectionID, choice)
		}
		return nil, err
	}
	if connectionID != "" && !connectionMatches(c, connectionID) {
		return nil, requestedSessionConnectionError(def, connections, connectionID, &EndpointResolveError{
			Reason: "connection_required", PluginID: def.ID, Endpoint: endpointName, CurrentConnectionID: c.ID,
		})
	}
	return endpointBindingForConnection(def, endpointName, endpoint, c), nil
}

// Describe the requested account without selecting it or refreshing its credentials.
// Only the explicit selection path may change the chat's connection grant.
func requestedSessionConnectionError(def *Definition, connections []*Connection, ref string, failure *EndpointResolveError) *EndpointResolveError {
	if failure.Connections == nil {
		failure.Connections = sessionConnectionChoices(def, connections)
	}
	failure.Connection = strings.TrimSpace(ref)
	if failure.Connection == "" {
		return failure
	}
	var requested *Connection
	candidates := connectionsForPlugin(connections, def.ID)
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.ID, failure.Connection) {
			requested = candidate
			break
		}
	}
	if requested == nil {
		for _, candidate := range candidates {
			if connectionMatches(candidate, failure.Connection) {
				if requested != nil {
					failure.Reason = "connection_ambiguous"
					return failure
				}
				requested = candidate
			}
		}
	}
	if requested == nil {
		failure.Reason = "connection_not_found"
		return failure
	}
	for _, choice := range failure.Connections {
		if choice.ID == requested.ID && choice.Status == "reauthorization_required" {
			failure.Reason = "connection_reauthorization_required"
			return failure
		}
	}
	if !connectionConfiguredFor(def, requested) {
		failure.Reason = "connection_not_configured"
		return failure
	}
	if ConnectionSelection(def) == ConnectionSelectionUser {
		failure.Reason = "connection_not_selected"
		failure.Recovery = &ConnectionRecovery{Tool: "builtin_plugin_load", Arguments: map[string]string{
			"plugin_id": def.ID, "connection_id": requested.ID,
		}}
	}
	return failure
}

// Discovery uses the chat binding or its sole usable connection, just like calls.
func (s *Service) ListSessionEndpointBindings(ctx context.Context, sessionID, kind string) ([]*EndpointBinding, error) {
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	out := []*EndpointBinding{}
	for _, def := range defs {
		if !def.Enabled {
			continue
		}
		for name, endpoint := range def.Endpoints {
			if endpoint.Kind != kind {
				continue
			}
			b, err := s.ResolveSessionEndpoint(ctx, sessionID, def.ID, name, "")
			if err != nil {
				var required *EndpointResolveError
				if errors.As(err, &required) || errors.Is(err, store.ErrNotFound) || errors.Is(err, ErrConnectionReauthorizationRequired) {
					continue
				}
				return nil, err
			}
			out = append(out, b)
		}
	}
	return out, nil
}
