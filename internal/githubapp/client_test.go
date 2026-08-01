package githubapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLoadsAccountInstallationsAndRepositories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer github-token" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Fatalf("unexpected headers: %+v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":42,"login":"octocat","name":"The Octocat","avatar_url":"https://avatars.example/42"}`))
		case "/user/installations":
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
				t.Fatalf("unexpected installation query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"installations":[{"id":11,"html_url":"https://github.com/settings/installations/11","account":{"id":7,"login":"teatak","type":"Organization"}}]}`))
		case "/user/installations/11/repositories":
			_, _ = w.Write([]byte(`{"repositories":[{"id":99,"name":"pudding-core","full_name":"teatak/pudding-core","private":true,"html_url":"https://github.com/teatak/pudding-core","default_branch":"main"}]}`))
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
	installations, err := client.Installations(context.Background(), "github-token")
	if err != nil {
		t.Fatal(err)
	}
	if len(installations) != 1 || installations[0].ID != "11" || installations[0].Account.Login != "teatak" || len(installations[0].Repositories) != 1 || installations[0].Repositories[0].FullName != "teatak/pudding-core" {
		t.Fatalf("installations=%+v", installations)
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
