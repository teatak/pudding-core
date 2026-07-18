package tool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
)

type fakeEndpointSource struct {
	binding *app.EndpointBinding
}

func (f fakeEndpointSource) ResolveEndpoint(_ context.Context, sessionID, endpointName, _ string) (*app.EndpointBinding, error) {
	if sessionID == "" || endpointName == "" {
		return nil, appErr("missing input")
	}
	return f.binding, nil
}

type appErr string

func (e appErr) Error() string { return string(e) }

func TestRESTRequestUsesGrantedEndpointAndInjectedAuth(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/repos/teatak/pudding-core/issues" || r.URL.Query().Get("state") != "open" {
			t.Fatalf("unexpected target: %s", r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer gh-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		return jsonResponse(200, `[{"number":1,"title":"hello"}]`), nil
	})}

	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: &app.EndpointBinding{
			AppID:        "github",
			ConnectionID: "github-main",
			EndpointName: "github_rest",
			Endpoint:     app.Endpoint{Kind: app.EndpointKindREST, URL: "https://api.example.test/api"},
			Auth:         app.Auth{Type: "bearer", Token: "gh-token"},
		}}),
	)
	res := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      RESTRequest,
		Args:      json.RawMessage(`{"endpoint":"github_rest","method":"GET","path":"/repos/teatak/pudding-core/issues","query":{"state":"open"}}`),
	})
	if !res.Ok {
		t.Fatalf("rest request should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["status"] != float64(200) || payload["endpoint"] != "github_rest" {
		t.Fatalf("unexpected result: %+v", payload)
	}
	if res.SummaryKind != SummaryReturnedItems || res.SummaryCount != 1 {
		t.Fatalf("unexpected summary: %+v", res)
	}
}

func TestRESTRequestExchangesAndCachesAppCredentialToken(t *testing.T) {
	tokenCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			tokenCalls++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["app_id"] != "cli_test" || body["app_secret"] != "secret" {
				t.Fatalf("unexpected token body: %+v", body)
			}
			return jsonResponse(200, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`), nil
		case "/open-apis/drive/v1/files":
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("unexpected authorization header %q", got)
			}
			return jsonResponse(200, `{"code":0,"data":{"files":[]}}`), nil
		default:
			t.Fatalf("unexpected target: %s", r.URL.String())
			return nil, nil
		}
	})}
	binding := &app.EndpointBinding{
		AppID:        "feishu",
		ConnectionID: "feishu-main",
		EndpointName: "feishu_rest",
		Endpoint:     app.Endpoint{Kind: app.EndpointKindREST, URL: "https://open.feishu.cn/open-apis"},
		Auth:         app.Auth{MethodID: "feishu-app-credentials", Type: app.AuthTypeTokenExchange},
		AuthMethod: app.AuthMethod{
			ID:   "feishu-app-credentials",
			Type: app.AuthTypeTokenExchange,
			TokenExchange: &app.TokenExchangeSpec{
				URL:              "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal",
				BodyFields:       map[string]string{"app_id": "appId", "app_secret": "appSecret"},
				AccessTokenField: "tenant_access_token",
				ExpiresInField:   "expire",
				TokenType:        "Bearer",
			},
		},
		ConnectionFields: map[string]string{"appId": "cli_test", "appSecret": "secret"},
	}
	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: binding}),
	)
	for range 2 {
		res := runner.Call(context.Background(), Call{
			SessionID: "sess_1",
			Name:      RESTRequest,
			Args:      json.RawMessage(`{"endpoint":"feishu_rest","method":"GET","path":"/drive/v1/files"}`),
		})
		if !res.Ok {
			t.Fatalf("rest request should succeed: %+v", res)
		}
	}
	if tokenCalls != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", tokenCalls)
	}
}

func TestRESTRequestInjectsConnectionQueryFields(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query()
		if got := query.Get("hotelCode"); got != "H001" {
			t.Fatalf("hotelCode = %q, want H001 in %s", got, r.URL.String())
		}
		if got := query.Get("status"); got != "active" {
			t.Fatalf("status = %q, want active", got)
		}
		return jsonResponse(200, `{"ok":true}`), nil
	})}

	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: &app.EndpointBinding{
			AppID:            "unicorn",
			ConnectionID:     "unicorn-main",
			EndpointName:     "unicorn_rest",
			Endpoint:         app.Endpoint{Kind: app.EndpointKindREST, URL: "https://api.example.test/api"},
			Auth:             app.Auth{Type: "header", Header: "X-Token", Token: "secret"},
			ConnectionFields: map[string]string{"hotelCode": "H001"},
			ConnectionFieldDefs: []app.ConnectionField{
				{ID: "hotelCode", Inject: []app.ConnectionFieldInject{{Target: "query", Methods: []string{"GET"}}}},
			},
		}}),
	)
	res := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      RESTRequest,
		Args:      json.RawMessage(`{"endpoint":"unicorn_rest","path":"/rooms","query":{"status":"active"}}`),
	})
	if !res.Ok {
		t.Fatalf("rest request should succeed: %+v", res)
	}
}

func TestRESTRequestInjectsConnectionBodyFields(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("hotelCode"); got != "" {
			t.Fatalf("hotelCode should not be injected into POST query: %s", r.URL.String())
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["hotelCode"] != "H001" || body["roomNo"] != "1201" {
			t.Fatalf("unexpected body: %+v", body)
		}
		return jsonResponse(200, `{"ok":true}`), nil
	})}

	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: &app.EndpointBinding{
			AppID:            "unicorn",
			ConnectionID:     "unicorn-main",
			EndpointName:     "unicorn_rest",
			Endpoint:         app.Endpoint{Kind: app.EndpointKindREST, URL: "https://api.example.test/api"},
			Auth:             app.Auth{Type: "header", Header: "X-Token", Token: "secret"},
			ConnectionFields: map[string]string{"hotelCode": "H001"},
			ConnectionFieldDefs: []app.ConnectionField{
				{ID: "hotelCode", Inject: []app.ConnectionFieldInject{{Target: "body", Methods: []string{"POST"}}}},
			},
		}}),
	)
	res := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      RESTRequest,
		Args:      json.RawMessage(`{"endpoint":"unicorn_rest","method":"POST","path":"/rooms","body_json":{"roomNo":"1201"}}`),
	})
	if !res.Ok {
		t.Fatalf("rest request should succeed: %+v", res)
	}
}

func TestRESTRequestInjectsConnectionHeaderFields(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("X-Hotel-Code"); got != "H001" {
			t.Fatalf("X-Hotel-Code = %q, want H001", got)
		}
		return jsonResponse(200, `{"ok":true}`), nil
	})}

	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: &app.EndpointBinding{
			AppID:            "unicorn",
			ConnectionID:     "unicorn-main",
			EndpointName:     "unicorn_rest",
			Endpoint:         app.Endpoint{Kind: app.EndpointKindREST, URL: "https://api.example.test/api"},
			Auth:             app.Auth{Type: "header", Header: "X-Token", Token: "secret"},
			ConnectionFields: map[string]string{"hotelCode": "H001"},
			ConnectionFieldDefs: []app.ConnectionField{
				{ID: "hotelCode", Inject: []app.ConnectionFieldInject{{Target: "header", Name: "X-Hotel-Code"}}},
			},
		}}),
	)
	res := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      RESTRequest,
		Args:      json.RawMessage(`{"endpoint":"unicorn_rest","path":"/rooms"}`),
	})
	if !res.Ok {
		t.Fatalf("rest request should succeed: %+v", res)
	}
}

func TestGraphQLRequestExtractsDataAndErrors(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if got := r.Header.Get("X-Test-Token"); got != "secret" {
			t.Fatalf("unexpected token header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["query"] == "" {
			t.Fatalf("missing query in body: %+v", body)
		}
		return jsonResponse(200, `{"data":{"viewer":{"login":"octo"}}}`), nil
	})}

	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: &app.EndpointBinding{
			AppID:        "github",
			ConnectionID: "github-main",
			EndpointName: "github_graphql",
			Endpoint:     app.Endpoint{Kind: app.EndpointKindGraphQL, URL: "https://api.example.test/graphql"},
			Auth:         app.Auth{Type: "header", Header: "X-Test-Token", Token: "secret"},
		}}),
	)
	res := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      GraphQLRequest,
		Args:      json.RawMessage(`{"endpoint":"github_graphql","query":"query { viewer { login } }"}`),
	})
	if !res.Ok {
		t.Fatalf("graphql request should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	data := payload["data"].(map[string]any)
	viewer := data["viewer"].(map[string]any)
	if viewer["login"] != "octo" {
		t.Fatalf("unexpected data: %+v", payload)
	}
}

func TestGraphQLIntrospectUsesEndpointSchema(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("X-Test-Token"); got != "secret" {
			t.Fatalf("unexpected token header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		query, _ := body["query"].(string)
		if strings.Contains(query, "__type") {
			return jsonResponse(200, `{"data":{"__type":{"kind":"OBJECT","name":"User","fields":[{"name":"login","type":{"kind":"SCALAR","name":"String"}}]}}}`), nil
		}
		return jsonResponse(200, `{"data":{"__schema":{"queryType":{"name":"Query","fields":[{"name":"viewer","type":{"kind":"OBJECT","name":"User"}}]},"mutationType":null,"subscriptionType":null}}}`), nil
	})}
	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: &app.EndpointBinding{
			AppID:        "github",
			ConnectionID: "github-main",
			EndpointName: "github_graphql",
			Endpoint:     app.Endpoint{Kind: app.EndpointKindGraphQL, URL: "https://api.example.test/graphql"},
			Auth:         app.Auth{Type: "header", Header: "X-Test-Token", Token: "secret"},
		}}),
	)

	top := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      GraphQLIntrospect,
		Args:      json.RawMessage(`{"endpoint":"github_graphql"}`),
	})
	if !top.Ok {
		t.Fatalf("introspect top should succeed: %+v", top)
	}
	topPayload := decodeToolResult(t, top)
	queryFields := topPayload["query"].([]any)
	if queryFields[0].(map[string]any)["name"] != "viewer" {
		t.Fatalf("unexpected top payload: %+v", topPayload)
	}

	typ := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      GraphQLIntrospect,
		Args:      json.RawMessage(`{"endpoint":"github_graphql","type_name":"User"}`),
	})
	if !typ.Ok {
		t.Fatalf("introspect type should succeed: %+v", typ)
	}
	typePayload := decodeToolResult(t, typ)
	typeInfo := typePayload["type"].(map[string]any)
	if typeInfo["name"] != "User" {
		t.Fatalf("unexpected type payload: %+v", typePayload)
	}
}

func TestGraphQLSearchFindsSchemaFields(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"data":{"__schema":{"queryType":{"name":"Query"},"mutationType":null,"subscriptionType":null,"types":[{"kind":"OBJECT","name":"Query","fields":[{"name":"viewer","description":"Current user","type":{"kind":"OBJECT","name":"User"}}]},{"kind":"OBJECT","name":"User","fields":[{"name":"login","type":{"kind":"SCALAR","name":"String"}}]},{"kind":"INPUT_OBJECT","name":"UserInput","inputFields":[{"name":"login","type":{"kind":"SCALAR","name":"String"}}]}]}}}`), nil
	})}
	runner := NewBuiltinRunner(
		WithWebHTTPClient(client),
		WithAppEndpoints(fakeEndpointSource{binding: &app.EndpointBinding{
			AppID:        "github",
			ConnectionID: "github-main",
			EndpointName: "github_graphql",
			Endpoint:     app.Endpoint{Kind: app.EndpointKindGraphQL, URL: "https://api.example.test/graphql"},
			Auth:         app.Auth{Type: "none"},
		}}),
	)
	res := runner.Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      GraphQLSearch,
		Args:      json.RawMessage(`{"endpoint":"github_graphql","query":"viewer"}`),
	})
	if !res.Ok {
		t.Fatalf("graphql search should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	matches := payload["matches"].([]any)
	if len(matches) != 1 || matches[0].(map[string]any)["where"] != "Query.viewer" {
		t.Fatalf("unexpected matches: %+v", payload)
	}
}

func TestEndpointRequestMetadataRejectsInvalidValues(t *testing.T) {
	t.Run("auth", func(t *testing.T) {
		err := applyEndpointAuth(http.Header{}, app.Auth{
			Type:  app.AuthTypeBearer,
			Token: "secret\r\nX-Injected: true",
		})
		if err == nil {
			t.Fatal("invalid auth header value should be rejected")
		}
	})

	t.Run("connection header", func(t *testing.T) {
		err := applyEndpointConnectionHeaders(
			http.Header{},
			http.MethodGet,
			map[string]string{"credential": "secret\r\nX-Injected: true"},
			[]app.ConnectionField{{
				ID: "credential",
				Inject: []app.ConnectionFieldInject{{
					Target: "header",
					Name:   "X-Credential",
				}},
			}},
		)
		if err == nil {
			t.Fatal("invalid connection header value should be rejected")
		}
	})

	t.Run("endpoint env", func(t *testing.T) {
		_, err := applyEndpointConnectionEnv(map[string]string{"APP_TOKEN": "secret\x00suffix"}, nil, nil)
		if err == nil {
			t.Fatal("invalid endpoint env value should be rejected")
		}
	})

	t.Run("mcp header", func(t *testing.T) {
		err := applyAppMCPHeaders(http.Header{}, map[string]string{
			"X-Credential": "secret\r\nX-Injected: true",
		})
		if err == nil {
			t.Fatal("invalid MCP header value should be rejected")
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
