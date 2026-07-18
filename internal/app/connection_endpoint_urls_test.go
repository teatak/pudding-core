package app

import "testing"

func TestNormalizeConnectionEndpointURLs(t *testing.T) {
	def := &Definition{Endpoints: map[string]Endpoint{
		"rest":    {Kind: EndpointKindREST, URL: "https://api.example.com", URLConfig: &EndpointURLConfig{Label: "REST address", Required: true}},
		"graphql": {Kind: EndpointKindGraphQL, URL: "https://api.example.com/graphql", URLConfig: &EndpointURLConfig{Label: "GraphQL address"}},
		"fixed":   {Kind: EndpointKindREST, URL: "https://fixed.example.com"},
		"mcp":     {Kind: EndpointKindMCP, Transport: EndpointTransportStreamableHTTP, URL: "https://api.example.com/mcp"},
	}}

	got, err := NormalizeConnectionEndpointURLs(def, map[string]string{
		"rest":    " https://self-hosted.example.com/api/ ",
		"graphql": "https://self-hosted.example.com/graphql",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["rest"] != "https://self-hosted.example.com/api" ||
		got["graphql"] != "https://self-hosted.example.com/graphql" {
		t.Fatalf("unexpected endpoint URLs: %+v", got)
	}

	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "unknown endpoint", values: map[string]string{"missing": "https://example.com"}},
		{name: "mcp endpoint", values: map[string]string{"mcp": "https://example.com/mcp"}},
		{name: "fixed endpoint", values: map[string]string{"fixed": "https://example.com"}},
		{name: "missing required endpoint", values: nil},
		{name: "unsupported scheme", values: map[string]string{"rest": "file:///tmp/data"}},
		{name: "userinfo", values: map[string]string{"rest": "https://user:secret@example.com"}},
		{name: "query", values: map[string]string{"rest": "https://example.com/api?token=secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeConnectionEndpointURLs(def, tt.values); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
