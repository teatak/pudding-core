// Package app loads local app definitions and resolves session-scoped app
// endpoints for generic REST / GraphQL tools.
package app

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	EndpointKindREST    = "rest"
	EndpointKindGraphQL = "graphql"
)

const (
	AuthTypeNone   = "none"
	AuthTypeBearer = "bearer"
	AuthTypeToken  = "token"
	AuthTypeBasic  = "basic"
	AuthTypeHeader = "header"
	AuthTypeOAuth2 = "oauth2"
)

type Definition struct {
	ID            string              `json:"id" yaml:"id"`
	Name          string              `json:"name" yaml:"name"`
	Version       string              `json:"version,omitempty" yaml:"version,omitempty"`
	Description   string              `json:"description,omitempty" yaml:"description,omitempty"`
	Icon          *IconSpec           `json:"icon,omitempty" yaml:"icon,omitempty"`
	Auth          *AuthConfig         `json:"auth,omitempty" yaml:"auth,omitempty"`
	Endpoints     map[string]Endpoint `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	Skills        []SkillRef          `json:"skills,omitempty" yaml:"skills,omitempty"`
	Path          string              `json:"path,omitempty" yaml:"-"`
	SourceURL     string              `json:"sourceURL,omitempty" yaml:"-"`
	PackageSHA256 string              `json:"packageSHA256,omitempty" yaml:"-"`
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
	ID       string `json:"id,omitempty" yaml:"id,omitempty"`
	Type     string `json:"type" yaml:"type"`
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Label    string `json:"label,omitempty" yaml:"label,omitempty"`
	Default  bool   `json:"default,omitempty" yaml:"default,omitempty"`
	Prefix   string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Header   string `json:"header,omitempty" yaml:"header,omitempty"`
}

type Endpoint struct {
	Kind        string `json:"kind" yaml:"kind"`
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type SkillRef struct {
	ID          string `json:"id,omitempty" yaml:"-"`
	Name        string `json:"name,omitempty" yaml:"-"`
	Description string `json:"description,omitempty" yaml:"-"`
	Path        string `json:"path" yaml:"-"`
}

type SkillDetail struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Content     string `json:"content"`
}

type Connection struct {
	ID        string    `json:"id" yaml:"-"`
	Name      string    `json:"name,omitempty" yaml:"name,omitempty"`
	AppID     string    `json:"appID" yaml:"app"`
	Auth      Auth      `json:"-" yaml:"auth,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty" yaml:"-"`
	UpdatedAt time.Time `json:"updatedAt,omitempty" yaml:"-"`
}

type ConnectionView struct {
	ID           string    `json:"id"`
	Name         string    `json:"name,omitempty"`
	AppID        string    `json:"appID"`
	AuthType     string    `json:"authType,omitempty"`
	AuthMethodID string    `json:"authMethodID,omitempty"`
	TokenSet     bool      `json:"tokenSet"`
	Header       string    `json:"header,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

type ConnectionDetailView struct {
	ConnectionView
	Token    string `json:"token,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type Auth struct {
	MethodID     string    `json:"methodID,omitempty" yaml:"method_id,omitempty"`
	Type         string    `json:"type" yaml:"type"` // none | bearer | token | basic | header | oauth2
	Token        string    `json:"-" yaml:"token,omitempty"`
	AccessToken  string    `json:"-" yaml:"access_token,omitempty"`
	RefreshToken string    `json:"-" yaml:"refresh_token,omitempty"`
	TokenType    string    `json:"tokenType,omitempty" yaml:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty" yaml:"expires_at,omitempty"`
	Scopes       []string  `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Prefix       string    `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Header       string    `json:"header,omitempty" yaml:"header,omitempty"`
	Username     string    `json:"-" yaml:"username,omitempty"`
	Password     string    `json:"-" yaml:"password,omitempty"`
}

type EndpointBinding struct {
	AppID        string
	ConnectionID string
	EndpointName string
	Endpoint     Endpoint
	Auth         Auth
}

type AppConnectionsView struct {
	Connections []ConnectionView `json:"connections"`
}

func ViewConnection(c *Connection) ConnectionView {
	if c == nil {
		return ConnectionView{}
	}
	return ConnectionView{
		ID:           c.ID,
		Name:         c.Name,
		AppID:        c.AppID,
		AuthType:     c.Auth.Type,
		AuthMethodID: c.Auth.MethodID,
		TokenSet:     c.Auth.Token != "" || c.Auth.AccessToken != "" || c.Auth.Password != "",
		Header:       c.Auth.Header,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

func ViewConnectionDetail(c *Connection) ConnectionDetailView {
	if c == nil {
		return ConnectionDetailView{}
	}
	return ConnectionDetailView{
		ConnectionView: ViewConnection(c),
		Token:          c.Auth.Token,
		Prefix:         c.Auth.Prefix,
		Username:       c.Auth.Username,
		Password:       c.Auth.Password,
	}
}

func CloneAuth(in Auth) Auth {
	return Auth{
		MethodID:     in.MethodID,
		Type:         in.Type,
		Token:        in.Token,
		AccessToken:  in.AccessToken,
		RefreshToken: in.RefreshToken,
		TokenType:    in.TokenType,
		ExpiresAt:    in.ExpiresAt,
		Scopes:       append([]string(nil), in.Scopes...),
		Prefix:       in.Prefix,
		Header:       in.Header,
		Username:     in.Username,
		Password:     in.Password,
	}
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
		auth.Methods = append([]AuthMethod(nil), in.Auth.Methods...)
		out.Auth = &auth
	}
	if in.Endpoints != nil {
		out.Endpoints = make(map[string]Endpoint, len(in.Endpoints))
		for k, v := range in.Endpoints {
			out.Endpoints[k] = v
		}
	}
	if in.Skills != nil {
		out.Skills = append([]SkillRef(nil), in.Skills...)
	}
	return &out
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
	out.Auth = CloneAuth(in.Auth)
	return &out
}

func CloneJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
