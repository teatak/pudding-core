// Package app loads local app definitions and resolves session-scoped app
// endpoints for generic REST / GraphQL tools.
package app

import (
	"encoding/json"
	"runtime"
	"strings"
	"time"
)

const (
	EndpointKindREST    = "rest"
	EndpointKindGraphQL = "graphql"
	EndpointKindMCP     = "mcp"
)

const (
	EndpointTransportStdio          = "stdio"
	EndpointTransportStreamableHTTP = "streamable_http"
)

const (
	AuthTypeNone          = "none"
	AuthTypeBearer        = "bearer"
	AuthTypeToken         = "token"
	AuthTypeBasic         = "basic"
	AuthTypeHeader        = "header"
	AuthTypeOAuth2        = "oauth2"
	AuthTypeTokenExchange = "token_exchange"

	GitHubAppAuthMethodID = "github-app"
	GitHubAppAuthVariant  = "github_app"
)

const (
	SourceBuiltin   = "builtin"
	SourceInstalled = "installed"
)

const (
	KindApp = "app"
	KindMCP = "mcp"
)

type Definition struct {
	Kind           string              `json:"kind" yaml:"kind,omitempty"`
	ID             string              `json:"id" yaml:"id"`
	Name           string              `json:"name" yaml:"name"`
	Version        string              `json:"version,omitempty" yaml:"version,omitempty"`
	Description    string              `json:"description,omitempty" yaml:"description,omitempty"`
	Icon           *IconSpec           `json:"icon,omitempty" yaml:"icon,omitempty"`
	Auth           *AuthConfig         `json:"auth,omitempty" yaml:"auth,omitempty"`
	Connection     *ConnectionConfig   `json:"connection,omitempty" yaml:"connection,omitempty"`
	Endpoints      map[string]Endpoint `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	Skills         []SkillRef          `json:"skills,omitempty" yaml:"skills,omitempty"`
	Tools          []ToolRef           `json:"tools,omitempty" yaml:"-"`
	Path           string              `json:"path,omitempty" yaml:"-"`
	SourceURL      string              `json:"sourceURL,omitempty" yaml:"-"`
	PackageSHA256  string              `json:"packageSHA256,omitempty" yaml:"-"`
	Source         string              `json:"source" yaml:"-"`
	Runtime        string              `json:"runtime,omitempty" yaml:"-"`
	Enabled        bool                `json:"enabled" yaml:"-"`
	CanUninstall   bool                `json:"canUninstall" yaml:"-"`
	RequiredMode   string              `json:"requiredMode" yaml:"-"`
	DefaultSkillID string              `json:"defaultSkillID,omitempty" yaml:"-"`
}

type IconSpec struct {
	SVG        string      `json:"svg,omitempty" yaml:"svg,omitempty"`
	Color      *ThemeColor `json:"color,omitempty" yaml:"color,omitempty"`
	Background *ThemeColor `json:"background,omitempty" yaml:"background,omitempty"`
}

type ThemeColor struct {
	Light string `json:"light,omitempty" yaml:"light,omitempty"`
	Dark  string `json:"dark,omitempty" yaml:"dark,omitempty"`
}

type AuthConfig struct {
	Required bool         `json:"required,omitempty" yaml:"required,omitempty"`
	Methods  []AuthMethod `json:"methods,omitempty" yaml:"methods,omitempty"`
}

type AuthMethod struct {
	ID            string             `json:"id,omitempty" yaml:"id,omitempty"`
	Type          string             `json:"type" yaml:"type"`
	Provider      string             `json:"provider,omitempty" yaml:"provider,omitempty"`
	Label         string             `json:"label,omitempty" yaml:"label,omitempty"`
	Default       bool               `json:"default,omitempty" yaml:"default,omitempty"`
	Prefix        string             `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Header        string             `json:"header,omitempty" yaml:"header,omitempty"`
	TokenExchange *TokenExchangeSpec `json:"tokenExchange,omitempty" yaml:"token_exchange,omitempty"`
}

type TokenExchangeSpec struct {
	URL              string            `json:"url" yaml:"url"`
	BodyFields       map[string]string `json:"bodyFields" yaml:"body_fields"`
	AccessTokenField string            `json:"accessTokenField" yaml:"access_token_field"`
	ExpiresInField   string            `json:"expiresInField,omitempty" yaml:"expires_in_field,omitempty"`
	TokenType        string            `json:"tokenType,omitempty" yaml:"token_type,omitempty"`
}

type ConnectionConfig struct {
	Fields []ConnectionField `json:"fields,omitempty" yaml:"fields,omitempty"`
}

type ConnectionField struct {
	ID          string                  `json:"id" yaml:"id"`
	Label       string                  `json:"label,omitempty" yaml:"label,omitempty"`
	Description string                  `json:"description,omitempty" yaml:"description,omitempty"`
	Placeholder string                  `json:"placeholder,omitempty" yaml:"placeholder,omitempty"`
	Required    bool                    `json:"required,omitempty" yaml:"required,omitempty"`
	Secret      bool                    `json:"secret,omitempty" yaml:"secret,omitempty"`
	Inject      []ConnectionFieldInject `json:"inject,omitempty" yaml:"inject,omitempty"`
}

type ConnectionFieldInject struct {
	Target  string   `json:"target" yaml:"target"` // query | body | header | env
	Name    string   `json:"name,omitempty" yaml:"name,omitempty"`
	Methods []string `json:"methods,omitempty" yaml:"methods,omitempty"`
}

type Endpoint struct {
	Kind        string                              `json:"kind" yaml:"kind"`
	Transport   string                              `json:"transport,omitempty" yaml:"transport,omitempty"`
	URL         string                              `json:"url,omitempty" yaml:"url,omitempty"`
	URLConfig   *EndpointURLConfig                  `json:"urlConfig,omitempty" yaml:"url_config,omitempty"`
	Command     string                              `json:"command,omitempty" yaml:"command,omitempty"`
	Args        []string                            `json:"args,omitempty" yaml:"args,omitempty"`
	Env         map[string]string                   `json:"env,omitempty" yaml:"env,omitempty"`
	Headers     map[string]string                   `json:"headers,omitempty" yaml:"headers,omitempty"`
	Platforms   map[string]EndpointPlatformOverride `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	Description string                              `json:"description,omitempty" yaml:"description,omitempty"`
}

type EndpointURLConfig struct {
	Label       string `json:"label" yaml:"label"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Placeholder string `json:"placeholder,omitempty" yaml:"placeholder,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
}

type EndpointPlatformOverride struct {
	URL     string            `json:"url,omitempty" yaml:"url,omitempty"`
	Command string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

type MCPOverrideFile struct {
	MCP map[string]MCPEndpointOverride `json:"mcp,omitempty" yaml:"mcp,omitempty"`
}

type MCPEndpointOverride struct {
	Transport string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	URL       string            `json:"url,omitempty" yaml:"url,omitempty"`
	Command   string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args      *[]string         `json:"args,omitempty" yaml:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

type SkillRef struct {
	ID          string `json:"id,omitempty" yaml:"-"`
	Name        string `json:"name,omitempty" yaml:"-"`
	Description string `json:"description,omitempty" yaml:"-"`
	Path        string `json:"path" yaml:"-"`
}

type ToolRef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SkillDetail struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Content     string `json:"content"`
}

type Connection struct {
	ID           string             `json:"id" yaml:"-"`
	Name         string             `json:"name,omitempty" yaml:"name,omitempty"`
	AppID        string             `json:"appID" yaml:"app"`
	Account      *ConnectionAccount `json:"account,omitempty" yaml:"account,omitempty"`
	Auth         Auth               `json:"-" yaml:"auth,omitempty"`
	Fields       map[string]string  `json:"-" yaml:"fields,omitempty"`
	EndpointURLs map[string]string  `json:"-" yaml:"endpoint_urls,omitempty"`
	CreatedAt    time.Time          `json:"createdAt,omitempty" yaml:"-"`
	UpdatedAt    time.Time          `json:"updatedAt,omitempty" yaml:"-"`
}

type ConnectionAccount struct {
	ID        string `json:"id" yaml:"id"`
	Login     string `json:"login" yaml:"login"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	AvatarURL string `json:"avatarURL,omitempty" yaml:"avatar_url,omitempty"`
}

type ConnectionView struct {
	ID                      string             `json:"id"`
	Name                    string             `json:"name,omitempty"`
	AppID                   string             `json:"appID"`
	AuthType                string             `json:"authType,omitempty"`
	AuthMethodID            string             `json:"authMethodID,omitempty"`
	AuthVariant             string             `json:"authVariant,omitempty"`
	TokenSet                bool               `json:"tokenSet"`
	Account                 *ConnectionAccount `json:"account,omitempty"`
	ReauthorizationRequired bool               `json:"reauthorizationRequired,omitempty"`
	Header                  string             `json:"header,omitempty"`
	CreatedAt               time.Time          `json:"createdAt,omitempty"`
	UpdatedAt               time.Time          `json:"updatedAt,omitempty"`
}

type ConnectionDetailView struct {
	ConnectionView
	Fields       map[string]string `json:"fields,omitempty"`
	EndpointURLs map[string]string `json:"endpointURLs,omitempty"`
	Token        string            `json:"token,omitempty"`
	Prefix       string            `json:"prefix,omitempty"`
	Username     string            `json:"username,omitempty"`
	Password     string            `json:"password,omitempty"`
}

type Auth struct {
	MethodID         string    `json:"methodID,omitempty" yaml:"method_id,omitempty"`
	Type             string    `json:"type" yaml:"type"` // none | bearer | token | basic | header | oauth2
	Variant          string    `json:"variant,omitempty" yaml:"variant,omitempty"`
	Token            string    `json:"-" yaml:"token,omitempty"`
	AccessToken      string    `json:"-" yaml:"access_token,omitempty"`
	RefreshToken     string    `json:"-" yaml:"refresh_token,omitempty"`
	TokenType        string    `json:"tokenType,omitempty" yaml:"token_type,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt,omitempty" yaml:"expires_at,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitempty" yaml:"refresh_expires_at,omitempty"`
	Scopes           []string  `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Prefix           string    `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Header           string    `json:"header,omitempty" yaml:"header,omitempty"`
	Username         string    `json:"-" yaml:"username,omitempty"`
	Password         string    `json:"-" yaml:"password,omitempty"`
}

type EndpointBinding struct {
	AppID               string
	ConnectionID        string
	EndpointName        string
	Endpoint            Endpoint
	Auth                Auth
	AuthMethod          AuthMethod
	ConnectionFields    map[string]string
	ConnectionFieldDefs []ConnectionField
}

type AppConnectionsView struct {
	Connections []ConnectionView `json:"connections"`
}

func ViewConnection(c *Connection) ConnectionView {
	if c == nil {
		return ConnectionView{}
	}
	reauthorizationRequired := githubAppReauthorizationRequired(c)
	return ConnectionView{
		ID:                      c.ID,
		Name:                    c.Name,
		AppID:                   c.AppID,
		AuthType:                c.Auth.Type,
		AuthMethodID:            c.Auth.MethodID,
		AuthVariant:             c.Auth.Variant,
		TokenSet:                !reauthorizationRequired && (c.Auth.Token != "" || c.Auth.AccessToken != "" || c.Auth.Password != ""),
		Account:                 cloneConnectionAccount(c.Account),
		ReauthorizationRequired: reauthorizationRequired,
		Header:                  c.Auth.Header,
		CreatedAt:               c.CreatedAt,
		UpdatedAt:               c.UpdatedAt,
	}
}

func ViewConnectionDetail(c *Connection) ConnectionDetailView {
	if c == nil {
		return ConnectionDetailView{}
	}
	return ConnectionDetailView{
		ConnectionView: ViewConnection(c),
		Fields:         cloneStringMap(c.Fields),
		EndpointURLs:   cloneStringMap(c.EndpointURLs),
		Token:          c.Auth.Token,
		Prefix:         c.Auth.Prefix,
		Username:       c.Auth.Username,
		Password:       c.Auth.Password,
	}
}

func CloneAuth(in Auth) Auth {
	return Auth{
		MethodID:         in.MethodID,
		Type:             in.Type,
		Variant:          in.Variant,
		Token:            in.Token,
		AccessToken:      in.AccessToken,
		RefreshToken:     in.RefreshToken,
		TokenType:        in.TokenType,
		ExpiresAt:        in.ExpiresAt,
		RefreshExpiresAt: in.RefreshExpiresAt,
		Scopes:           append([]string(nil), in.Scopes...),
		Prefix:           in.Prefix,
		Header:           in.Header,
		Username:         in.Username,
		Password:         in.Password,
	}
}

func cloneConnectionAccount(in *ConnectionAccount) *ConnectionAccount {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func githubAppReauthorizationRequired(c *Connection) bool {
	return c != nil && c.AppID == "github" && c.Auth.Type == AuthTypeOAuth2 &&
		(c.Auth.MethodID != GitHubAppAuthMethodID || c.Auth.Variant != GitHubAppAuthVariant)
}

func CloneDefinition(in *Definition) *Definition {
	if in == nil {
		return nil
	}
	out := *in
	if in.Icon != nil {
		icon := *in.Icon
		if in.Icon.Color != nil {
			color := *in.Icon.Color
			icon.Color = &color
		}
		if in.Icon.Background != nil {
			background := *in.Icon.Background
			icon.Background = &background
		}
		out.Icon = &icon
	}
	if in.Auth != nil {
		auth := *in.Auth
		auth.Methods = make([]AuthMethod, len(in.Auth.Methods))
		for i, method := range in.Auth.Methods {
			auth.Methods[i] = CloneAuthMethod(method)
		}
		out.Auth = &auth
	}
	if in.Connection != nil {
		connection := *in.Connection
		connection.Fields = cloneConnectionFields(in.Connection.Fields)
		out.Connection = &connection
	}
	if in.Endpoints != nil {
		out.Endpoints = make(map[string]Endpoint, len(in.Endpoints))
		for k, v := range in.Endpoints {
			out.Endpoints[k] = CloneEndpoint(v)
		}
	}
	if in.Skills != nil {
		out.Skills = append([]SkillRef(nil), in.Skills...)
	}
	if in.Tools != nil {
		out.Tools = append([]ToolRef(nil), in.Tools...)
	}
	return &out
}

func ResolveDefinitionPlatform(in *Definition) *Definition {
	out := CloneDefinition(in)
	if out == nil {
		return nil
	}
	for name, endpoint := range out.Endpoints {
		out.Endpoints[name] = ResolveEndpointPlatform(endpoint)
	}
	return out
}

func CloneEndpoint(in Endpoint) Endpoint {
	out := in
	if in.URLConfig != nil {
		urlConfig := *in.URLConfig
		out.URLConfig = &urlConfig
	}
	out.Args = append([]string(nil), in.Args...)
	out.Env = cloneStringMap(in.Env)
	out.Headers = cloneStringMap(in.Headers)
	if in.Platforms != nil {
		out.Platforms = make(map[string]EndpointPlatformOverride, len(in.Platforms))
		for key, platform := range in.Platforms {
			out.Platforms[key] = CloneEndpointPlatformOverride(platform)
		}
	}
	return out
}

func CloneAuthMethod(in AuthMethod) AuthMethod {
	out := in
	if in.TokenExchange != nil {
		exchange := *in.TokenExchange
		exchange.BodyFields = cloneStringMap(in.TokenExchange.BodyFields)
		out.TokenExchange = &exchange
	}
	return out
}

func CloneEndpointPlatformOverride(in EndpointPlatformOverride) EndpointPlatformOverride {
	out := in
	out.Args = append([]string(nil), in.Args...)
	out.Env = cloneStringMap(in.Env)
	out.Headers = cloneStringMap(in.Headers)
	return out
}

func CloneMCPEndpointOverride(in MCPEndpointOverride) MCPEndpointOverride {
	out := in
	if in.Args != nil {
		args := append([]string(nil), (*in.Args)...)
		out.Args = &args
	}
	out.Env = cloneStringMap(in.Env)
	out.Headers = cloneStringMap(in.Headers)
	return out
}

func ResolveEndpointPlatform(endpoint Endpoint) Endpoint {
	return ResolveEndpointPlatformForGOOS(endpoint, runtime.GOOS)
}

func ResolveEndpointPlatformForGOOS(endpoint Endpoint, goos string) Endpoint {
	out := CloneEndpoint(endpoint)
	override, ok := endpoint.Platforms[strings.TrimSpace(goos)]
	if !ok {
		out.Platforms = nil
		return out
	}
	if value := strings.TrimSpace(override.URL); value != "" {
		out.URL = value
	}
	if value := strings.TrimSpace(override.Command); value != "" {
		out.Command = value
	}
	if override.Args != nil {
		out.Args = append([]string(nil), override.Args...)
	}
	if override.Env != nil {
		out.Env = mergeStringMaps(out.Env, override.Env)
	}
	if override.Headers != nil {
		out.Headers = mergeStringMaps(out.Headers, override.Headers)
	}
	out.Platforms = nil
	return out
}

func DefaultAuthMethod(def *Definition) (AuthMethod, bool) {
	if def == nil || def.Auth == nil || len(def.Auth.Methods) == 0 {
		return AuthMethod{}, false
	}
	for _, method := range def.Auth.Methods {
		if method.Default {
			return method, true
		}
	}
	return def.Auth.Methods[0], true
}

func FindAuthMethod(def *Definition, methodID, authType string) (AuthMethod, bool) {
	if def == nil || def.Auth == nil {
		return AuthMethod{}, false
	}
	methodID = strings.TrimSpace(methodID)
	authType = strings.TrimSpace(authType)
	if methodID != "" {
		for _, method := range def.Auth.Methods {
			if method.ID == methodID {
				return method, true
			}
		}
		return AuthMethod{}, false
	}
	if authType == "" {
		return DefaultAuthMethod(def)
	}
	var found AuthMethod
	count := 0
	for _, method := range def.Auth.Methods {
		if method.Type == authType {
			found = method
			count++
		}
	}
	if count == 1 {
		return found, true
	}
	return AuthMethod{}, false
}

func CloneConnection(in *Connection) *Connection {
	if in == nil {
		return nil
	}
	out := *in
	out.Account = cloneConnectionAccount(in.Account)
	out.Auth = CloneAuth(in.Auth)
	out.Fields = cloneStringMap(in.Fields)
	out.EndpointURLs = cloneStringMap(in.EndpointURLs)
	return &out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeStringMaps(base, override map[string]string) map[string]string {
	out := cloneStringMap(base)
	if len(override) == 0 {
		return out
	}
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func cloneConnectionFields(in []ConnectionField) []ConnectionField {
	if len(in) == 0 {
		return nil
	}
	out := append([]ConnectionField(nil), in...)
	for i := range out {
		out[i].Inject = append([]ConnectionFieldInject(nil), in[i].Inject...)
		for j := range out[i].Inject {
			out[i].Inject[j].Methods = append([]string(nil), in[i].Inject[j].Methods...)
		}
	}
	return out
}

func CloneJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
