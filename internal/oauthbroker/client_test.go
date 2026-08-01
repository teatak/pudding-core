package oauthbroker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientOAuthLifecycle(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/oauth/start":
			if body["provider"] != "github" || body["client"] != "desktop" || body["flow"] != "install" || body["client_state"] != "state" || body["challenge"] != "challenge" {
				t.Fatalf("unexpected start body: %+v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"authorization_url":"https://github.com/login/oauth/authorize","provider":"github"}`))
		case "/oauth/redeem":
			if body["ticket"] != "transaction.secret" || body["verifier"] != "verifier" {
				t.Fatalf("unexpected redeem body: %+v", body)
			}
			_, _ = w.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","expires_in":28800,"refresh_token_expires_in":15811200}`))
		case "/oauth/refresh":
			if body["provider"] != "github" || body["refresh_token"] != "refresh-1" {
				t.Fatalf("unexpected refresh body: %+v", body)
			}
			_, _ = w.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2","expires_in":28800}`))
		case "/oauth/revoke":
			if body["provider"] != "github" || body["access_token"] != "access-2" {
				t.Fatalf("unexpected revoke body: %+v", body)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, server.Client())
	started, err := client.Start(context.Background(), StartRequest{
		Provider: "github", Client: "desktop", ClientState: "state", Flow: "install", Challenge: "challenge",
	})
	if err != nil || started.AuthorizationURL == "" {
		t.Fatalf("start: response=%+v err=%v", started, err)
	}
	token, err := client.Redeem(context.Background(), "transaction.secret", "verifier")
	if err != nil || token.AccessToken != "access-1" || token.RefreshToken != "refresh-1" {
		t.Fatalf("redeem: response=%+v err=%v", token, err)
	}
	token, err = client.Refresh(context.Background(), "github", token.RefreshToken)
	if err != nil || token.AccessToken != "access-2" || token.RefreshToken != "refresh-2" {
		t.Fatalf("refresh: response=%+v err=%v", token, err)
	}
	if err := client.Revoke(context.Background(), "github", token.AccessToken); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 4 {
		t.Fatalf("paths = %+v", paths)
	}
}

func TestClientReturnsBrokerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"invalid_redemption","error_description":"ticket expired"}`))
	}))
	t.Cleanup(server.Close)

	_, err := New(server.URL, server.Client()).Redeem(context.Background(), "bad.ticket", "verifier")
	if err == nil || err.Error() != "ticket expired" {
		t.Fatalf("err = %v", err)
	}
}
