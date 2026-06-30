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

func (f fakeEndpointSource) ResolveEndpoint(_ context.Context, sessionID, endpointName string) (*app.EndpointBinding, error) {
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
