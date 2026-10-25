package tool

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
)

func TestAssistantRequestsUseConnectionEndpointTLS(t *testing.T) {
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			w.Write([]byte(`{"data":{"ok":true}}`))
		} else {
			w.Write([]byte(`{"ok":true}`))
		}
	}))
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.StartTLS()
	defer target.Close()
	for _, kind := range []string{plugin.EndpointKindREST, plugin.EndpointKindGraphQL} {
		binding := &plugin.EndpointBinding{PluginID: "private", ConnectionID: "nas", EndpointName: "api", Endpoint: plugin.Endpoint{Kind: kind, URL: target.URL}, Auth: plugin.Auth{Type: plugin.AuthTypeNone}}
		runner := NewBuiltinRunner(WithPluginEndpoints(fakeEndpointSource{binding: binding}))
		for _, enabled := range []bool{false, true, false} {
			binding.InsecureTLS = enabled
			call := Call{SessionID: "chat", Name: RESTRequest, Args: json.RawMessage(`{"endpoint":"api","path":"/files"}`)}
			if kind == plugin.EndpointKindGraphQL {
				call.Name = GraphQLRequest
				call.Args = json.RawMessage(`{"endpoint":"api","query":"{ ok }"}`)
			}
			result := runner.Call(context.Background(), call)
			if result.Ok != enabled {
				t.Fatalf("%s opt-in=%t result=%+v", kind, enabled, result)
			}
		}
	}
}

func TestGraphQLSchemaUsesEndpointTLSAndInvalidatesPolicyCache(t *testing.T) {
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"__schema":{"queryType":{"name":"Query"},"types":[]}}}`))
	}))
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.StartTLS()
	defer target.Close()
	binding := &plugin.EndpointBinding{PluginID: "private", ConnectionID: "nas", EndpointName: "api", Endpoint: plugin.Endpoint{Kind: plugin.EndpointKindGraphQL, URL: target.URL}, Auth: plugin.Auth{Type: plugin.AuthTypeNone}}
	runner := NewBuiltinRunner()
	originalKey := graphqlSchemaCacheKey(binding)
	for _, enabled := range []bool{false, true, false} {
		binding.InsecureTLS = enabled
		_, failure := runner.doGraphQLSchemaQuery(context.Background(), binding, "{ __schema { queryType { name } } }", nil)
		if (failure == nil) != enabled {
			t.Fatalf("schema opt-in=%t failure=%v", enabled, failure)
		}
		if enabled && graphqlSchemaCacheKey(binding) == originalKey {
			t.Fatal("schema cache ignored TLS policy")
		}
	}
}
