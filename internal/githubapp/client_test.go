package githubapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLoadsAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer github-token" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Fatalf("unexpected headers: %+v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":42,"login":"octocat","name":"The Octocat","avatar_url":"https://avatars.example/42"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := New(server.URL, server.Client())
	account, err := client.Account(context.Background(), "github-token")
	if err != nil || account.ID != "42" || account.Login != "octocat" {
		t.Fatalf("account=%+v err=%v", account, err)
	}
}

func TestClientSurfacesGitHubError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	t.Cleanup(server.Close)

	_, err := New(server.URL, server.Client()).Account(context.Background(), "expired")
	if err == nil || err.Error() != "Bad credentials" {
		t.Fatalf("err=%v", err)
	}
}
