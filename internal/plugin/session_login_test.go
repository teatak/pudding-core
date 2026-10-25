package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func loginDefinition() *Definition {
	return &Definition{ID: "fixture", Name: "Fixture", Endpoints: map[string]Endpoint{"api": {Kind: EndpointKindREST, URL: "https://fixture.test/api"}}, Connection: &ConnectionConfig{Fields: []ConnectionField{{ID: "sid", Secret: true, Required: true}}}, Auth: &AuthConfig{Methods: []AuthMethod{{ID: "login", Type: AuthTypeSession, Session: &SessionLoginSpec{Endpoint: "api", Path: "/login", Encoding: "json", Inputs: []LoginInputField{{ID: "password", Required: true, Secret: true}}, BodyFields: map[string]string{"password": "password"}, Outputs: map[string]string{"sid": "result.session"}}}}}}
}
func TestValidateSessionLogin(t *testing.T) {
	if err := ValidateDefinition(loginDefinition()); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Definition){
		"absolute URL":      func(d *Definition) { d.Auth.Methods[0].Session.Path = "https://evil.test/login" },
		"traversal":         func(d *Definition) { d.Auth.Methods[0].Session.Path = "/%2e%2e/login" },
		"unknown endpoint":  func(d *Definition) { d.Auth.Methods[0].Session.Endpoint = "other" },
		"unknown input":     func(d *Definition) { d.Auth.Methods[0].Session.BodyFields["password"] = "unknown" },
		"unknown output":    func(d *Definition) { d.Auth.Methods[0].Session.Outputs["unknown"] = "data.token" },
		"secret as account": func(d *Definition) { d.Auth.Methods[0].Session.AccountInput = "password" },
		"missing output":    func(d *Definition) { d.Auth.Methods[0].Session.Outputs = nil },
		"wrong encoding":    func(d *Definition) { d.Auth.Methods[0].Session.Encoding = "shell" },
	}
	for name, modify := range tests {
		t.Run(name, func(t *testing.T) {
			d := loginDefinition()
			modify(d)
			if ValidateDefinition(d) == nil {
				t.Fatal("invalid login accepted")
			}
		})
	}
	original := loginDefinition().Auth.Methods[0]
	clone := CloneAuthMethod(original)
	clone.Session.Outputs["sid"] = "new.path"
	if original.Session.Outputs["sid"] != "result.session" {
		t.Fatal("clone shared session output map")
	}
}
func TestAcquireSessionJSONAndFailureBoundaries(t *testing.T) {
	spec := loginDefinition().Auth.Methods[0].Session
	mode := "success"
	redirectCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++; fmt.Fprint(w, `{}`) }))
	defer target.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong content type")
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["password"] != " secret " {
			t.Error("password modified")
		}
		switch mode {
		case "redirect":
			http.Redirect(w, r, target.URL, 307)
		case "large":
			fmt.Fprint(w, strings.Repeat("x", 65537))
		case "malformed":
			fmt.Fprint(w, "secret provider diagnostic")
		default:
			fmt.Fprint(w, `{"result":{"session":"sid-value"}}`)
		}
	}))
	defer provider.Close()
	for _, scenario := range []string{"success", "redirect", "large", "malformed"} {
		mode = scenario
		result := AcquireSession(context.Background(), http.DefaultClient, spec, provider.URL, map[string]string{"password": " secret "}, false)
		if scenario == "success" {
			if result.Status != "success" || result.Fields["sid"] != "sid-value" {
				t.Fatal(result)
			}
		} else if result.Status != "error" {
			t.Fatal(scenario, result)
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "sid-value") {
			t.Fatal("leaked credentials")
		}
	}
	if redirectCalls != 0 {
		t.Fatal("forwarded credentials across redirect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := AcquireSession(ctx, http.DefaultClient, spec, provider.URL, map[string]string{"password": " secret "}, false); result.Status != "error" {
		t.Fatal("ignored cancellation")
	}
}

func TestSessionCredentialRotationKeepsAccountBinding(t *testing.T) {
	original := &Connection{ID: "nas", PluginID: "fixture", Auth: Auth{Type: AuthTypeSession, SessionEstablished: true}, Account: &ConnectionAccount{Login: "alice"}, Fields: map[string]string{"sid": "old"}, EndpointURLs: map[string]string{"api": "https://nas.test/api"}}
	rotated := CloneConnection(original)
	rotated.Fields["sid"] = "new"
	if connectionIdentity(original) != connectionIdentity(rotated) {
		t.Fatal("credential rotation invalidated account binding")
	}
	rotated.Account.Login = "bob"
	if connectionIdentity(original) == connectionIdentity(rotated) {
		t.Fatal("different account kept binding")
	}
	rotated = CloneConnection(original)
	rotated.EndpointURLs["api"] = "https://other.test/api"
	if connectionIdentity(original) == connectionIdentity(rotated) {
		t.Fatal("different server kept binding")
	}
}
