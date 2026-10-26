package plugin

import "testing"

func TestConnectionEndpointTLSNormalization(t *testing.T) {
	def := &Definition{ID: "nas", Endpoints: map[string]Endpoint{
		"api":   {Kind: EndpointKindREST, URL: "https://nas.test/api", URLConfig: &EndpointURLConfig{Label: "Server"}},
		"other": {Kind: EndpointKindREST, URL: "https://other.test"},
	}}
	existing := &Connection{PluginID: "nas", EndpointURLs: map[string]string{"api": "https://private.test/api"}, EndpointInsecureTLS: map[string]bool{"api": true}}
	tests := []struct {
		name     string
		urls     map[string]string
		settings map[string]bool
		old      *Connection
		want     bool
		invalid  bool
	}{
		{name: "strict default"},
		{name: "explicit opt-in", settings: map[string]bool{"api": true}, want: true},
		{name: "preserve unchanged server", urls: existing.EndpointURLs, old: existing, want: true},
		{name: "changed server clears", urls: map[string]string{"api": "https://new.test/api"}, old: existing},
		{name: "reset default URL clears", old: existing},
		{name: "explicit opt-out", urls: existing.EndpointURLs, settings: map[string]bool{}, old: existing},
		{name: "HTTP rejects opt-in", urls: map[string]string{"api": "http://private.test/api"}, settings: map[string]bool{"api": true}, invalid: true},
		{name: "HTTP accepts false", urls: map[string]string{"api": "http://private.test/api"}, settings: map[string]bool{"api": false}},
		{name: "unknown endpoint", settings: map[string]bool{"missing": true}, invalid: true},
		{name: "fixed endpoint rejects", settings: map[string]bool{"other": true}, invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeConnectionEndpointInsecureTLS(def, tt.urls, tt.settings, tt.old)
			if (err != nil) != tt.invalid || got["api"] != tt.want {
				t.Fatal(got, err)
			}
		})
	}
	clone := CloneConnection(existing)
	clone.EndpointInsecureTLS["api"] = false
	if !existing.EndpointInsecureTLS["api"] {
		t.Fatal("cloned TLS map shares storage")
	}
	changed := CloneConnection(existing)
	changed.EndpointInsecureTLS = nil
	if connectionIdentity(existing) == connectionIdentity(changed) {
		t.Fatal("trust policy change did not invalidate saved grant")
	}
}

func TestTLSExceptionPinsDefaultAddressAcrossPluginUpdates(t *testing.T) {
	def := &Definition{ID: "nas", Endpoints: map[string]Endpoint{"api": {Kind: EndpointKindREST, URL: "https://old-default.test/api", URLConfig: &EndpointURLConfig{Label: "Server"}}}}
	urls, settings, err := NormalizeConnectionEndpoints(def, nil, map[string]bool{"api": true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	connection := &Connection{PluginID: "nas", EndpointURLs: urls, EndpointInsecureTLS: settings}
	endpoint := def.Endpoints["api"]
	endpoint.URL = "https://new-default.test/api"
	def.Endpoints["api"] = endpoint
	binding := endpointBindingForConnection(def, "api", endpoint, connection)
	if !binding.InsecureTLS || binding.Endpoint.URL != "https://old-default.test/api" {
		t.Fatal("certificate exception followed plugin's changed default address", binding)
	}
	if len(urls) != 1 || len(settings) != 1 {
		t.Fatal("missing explicit URL for TLS exception")
	}
}
