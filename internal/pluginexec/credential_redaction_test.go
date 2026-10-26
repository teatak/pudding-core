package pluginexec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
)

type redactionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f redactionRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRESTResultsDoNotExposeInjectedCredentials(t *testing.T) {
	const sid = "private/sid+with space"
	const token = "private-bearer-token"
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("_sid") != sid || r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("credentials were modified before sending")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", "/files?_sid="+url.QueryEscape(sid))
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "hello.txt", "size": 42, "echo": r.RequestURI, "nested": []string{sid, token}})
	}))
	defer target.Close()
	binding := &plugin.EndpointBinding{Endpoint: plugin.Endpoint{URL: target.URL, Kind: plugin.EndpointKindREST}, Auth: plugin.Auth{Type: plugin.AuthTypeBearer, Token: token}, ConnectionFields: map[string]string{"sid": sid}, ConnectionFieldDefs: []plugin.ConnectionField{{ID: "sid", Secret: true, Inject: []plugin.ConnectionFieldInject{{Target: "query", Name: "_sid"}}}}}
	result := New(nil).REST(context.Background(), binding, map[string]any{"path": "/files", "query": map[string]any{"page": "1"}})
	encoded, _ := json.Marshal(result)
	for _, secret := range []string{sid, url.QueryEscape(sid), token} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("result exposed a connection credential")
		}
	}
	body := result["body_json"].(map[string]any)
	if result["ok"] != true || body["name"] != "hello.txt" || body["size"] != float64(42) || !strings.Contains(result["url"].(string), "page=1") {
		t.Fatal("normal result changed", result)
	}
}

func TestRESTNetworkErrorsDoNotExposeInjectedCredentials(t *testing.T) {
	const sid = "network/session+secret"
	client := &http.Client{Transport: redactionRoundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, errors.New("connection failed for " + sid) })}
	binding := &plugin.EndpointBinding{Endpoint: plugin.Endpoint{URL: "https://private.test", Kind: plugin.EndpointKindREST}, Auth: plugin.Auth{Type: plugin.AuthTypeNone}, ConnectionFields: map[string]string{"sid": sid}, ConnectionFieldDefs: []plugin.ConnectionField{{ID: "sid", Secret: true, Inject: []plugin.ConnectionFieldInject{{Target: "query", Name: "_sid"}}}}}
	result := New(client).REST(context.Background(), binding, map[string]any{"path": "/files"})
	encoded, _ := json.Marshal(result)
	if result["ok"] != false || strings.Contains(string(encoded), sid) || strings.Contains(string(encoded), url.QueryEscape(sid)) {
		t.Fatal("network error exposed a credential")
	}
}

func TestBasicAuthEchoIsRedactedWithoutChangingMetadata(t *testing.T) {
	encodedCredential := base64.StdEncoding.EncodeToString([]byte("alice:secret-password"))
	result := map[string]any{"connection": "public-account", "response_headers": map[string]any{"X-Echo": []string{"Basic " + encodedCredential, "secret-password"}}, "body_json": map[string]any{"account": "alice"}}
	redactEndpointResult(result, endpointCredentials(plugin.Auth{Type: plugin.AuthTypeBasic, Username: "alice", Password: "secret-password"}, nil, nil))
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), encodedCredential) || strings.Contains(string(encoded), "secret-password") {
		t.Fatal("Basic authorization was exposed")
	}
	if result["connection"] != "public-account" || result["body_json"].(map[string]any)["account"] != "alice" {
		t.Fatal("public metadata changed")
	}
}

func TestExchangedAccessTokenIsRedactedFromProviderEcho(t *testing.T) {
	const issued = "issued-private-access-token"
	client := &http.Client{Transport: redactionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"` + issued + `","expires_in":3600}`
		if r.URL.Host != "auth.test" {
			if r.Header.Get("Authorization") != "Bearer "+issued {
				t.Error("exchanged token was not sent")
			}
			body = `{"echo":"` + issued + `","name":"normal-result"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	binding := &plugin.EndpointBinding{Endpoint: plugin.Endpoint{URL: "https://api.test", Kind: plugin.EndpointKindREST}, Auth: plugin.Auth{Type: plugin.AuthTypeTokenExchange}, AuthMethod: plugin.AuthMethod{ID: "app", Type: plugin.AuthTypeTokenExchange, TokenExchange: &plugin.TokenExchangeSpec{URL: "https://auth.test/token", BodyFields: map[string]string{"client_secret": "secret"}, AccessTokenField: "access_token", ExpiresInField: "expires_in"}}, ConnectionFields: map[string]string{"secret": "private-client-secret"}, ConnectionFieldDefs: []plugin.ConnectionField{{ID: "secret", Secret: true}}}
	result := New(client).REST(context.Background(), binding, map[string]any{"path": "/files"})
	encoded, _ := json.Marshal(result)
	if result["ok"] != true || strings.Contains(string(encoded), issued) || !strings.Contains(string(encoded), "normal-result") {
		t.Fatal("exchanged credential exposed or response changed")
	}
}
