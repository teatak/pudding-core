// Package app loads local app definitions and resolves session-scoped app
// endpoints for generic REST / GraphQL tools.
package app

import (
	"encoding/json"
	"time"
)

const (
	EndpointKindREST    = "rest"
	EndpointKindGraphQL = "graphql"
)

type Definition struct {
	ID          string              `json:"id" yaml:"id"`
	Name        string              `json:"name" yaml:"name"`
	Description string              `json:"description,omitempty" yaml:"description,omitempty"`
	Endpoints   map[string]Endpoint `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	Skills      []SkillRef          `json:"skills,omitempty" yaml:"skills,omitempty"`
	Path        string              `json:"path,omitempty" yaml:"-"`
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

type Connection struct {
	ID        string    `json:"id" yaml:"-"`
	AppID     string    `json:"appID" yaml:"app"`
	Auth      Auth      `json:"-" yaml:"auth,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty" yaml:"-"`
	UpdatedAt time.Time `json:"updatedAt,omitempty" yaml:"-"`
}

type ConnectionView struct {
	ID        string    `json:"id"`
	AppID     string    `json:"appID"`
	AuthType  string    `json:"authType,omitempty"`
	TokenSet  bool      `json:"tokenSet"`
	Header    string    `json:"header,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type Auth struct {
	Type     string `json:"type" yaml:"type"` // none | bearer | token | basic | header
	Token    string `json:"-" yaml:"token,omitempty"`
	Prefix   string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Header   string `json:"header,omitempty" yaml:"header,omitempty"`
	Username string `json:"-" yaml:"username,omitempty"`
	Password string `json:"-" yaml:"password,omitempty"`
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
		ID:        c.ID,
		AppID:     c.AppID,
		AuthType:  c.Auth.Type,
		TokenSet:  c.Auth.Token != "" || c.Auth.Password != "",
		Header:    c.Auth.Header,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func CloneAuth(in Auth) Auth {
	return Auth{
		Type:     in.Type,
		Token:    in.Token,
		Prefix:   in.Prefix,
		Header:   in.Header,
		Username: in.Username,
		Password: in.Password,
	}
}

func CloneDefinition(in *Definition) *Definition {
	if in == nil {
		return nil
	}
	out := *in
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
