package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/teatak/pudding-core/internal/plugin"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestParseGoogleIDToken(t *testing.T) {
	payload := map[string]any{
		"sub":     "google-user-12345",
		"email":   "test@gmail.com",
		"name":    "Test User",
		"picture": "https://lh3.googleusercontent.com/a/test-avatar",
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	fakeJWT := "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payloadBytes) + ".fake-sig"

	parsed, err := parseGoogleIDToken(fakeJWT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Sub != "google-user-12345" {
		t.Errorf("sub = %q, want google-user-12345", parsed.Sub)
	}
	if parsed.Email != "test@gmail.com" {
		t.Errorf("email = %q, want test@gmail.com", parsed.Email)
	}
	if parsed.Name != "Test User" {
		t.Errorf("name = %q, want Test User", parsed.Name)
	}
	if parsed.Picture != "https://lh3.googleusercontent.com/a/test-avatar" {
		t.Errorf("picture = %q, want https://lh3.googleusercontent.com/a/test-avatar", parsed.Picture)
	}
}

func TestFetchGoogleAccountInfoFromIDToken(t *testing.T) {
	payload := map[string]any{
		"sub":     "google-sub-99",
		"email":   "user@example.com",
		"name":    "Example User",
		"picture": "https://example.com/avatar.png",
	}
	payloadBytes, _ := json.Marshal(payload)
	fakeJWT := "header." + base64.RawURLEncoding.EncodeToString(payloadBytes) + ".signature"

	account, err := fetchGoogleAccountInfo(context.Background(), "", fakeJWT)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if account == nil {
		t.Fatal("expected account, got nil")
	}
	if account.ID != "google-sub-99" {
		t.Errorf("id = %q", account.ID)
	}
	if account.Login != "user@example.com" {
		t.Errorf("login = %q", account.Login)
	}
	if account.AvatarURL != "https://example.com/avatar.png" {
		t.Errorf("avatar = %q", account.AvatarURL)
	}
	if account.Name != "Example User" {
		t.Errorf("name = %q", account.Name)
	}
}

func TestFetchGoogleAccountInfoInvalid(t *testing.T) {
	_, err := fetchGoogleAccountInfo(context.Background(), "", "invalid.token")
	if err == nil {
		t.Fatal("expected error for invalid token and empty access token")
	}
}

func TestGoogleIDTokenInTokenResp(t *testing.T) {
	jsonBlob := []byte(`{"access_token":"at-123","id_token":"jwt-456","token_type":"Bearer","expires_in":3600}`)
	var resp pluginOAuthTokenResp
	if err := json.Unmarshal(jsonBlob, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken != "at-123" {
		t.Errorf("access_token = %q", resp.AccessToken)
	}
	if resp.IDToken != "jwt-456" {
		t.Errorf("id_token = %q", resp.IDToken)
	}
}

type googleOAuthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f googleOAuthRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestFetchGoogleAccountInfoFromMockServer(t *testing.T) {
	mockClient := &http.Client{
		Transport: googleOAuthRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer test-access-token" {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(bytes.NewBufferString(`{"error":"unauthorized"}`)),
				}, nil
			}
			data := `{"sub":"google-user-mock","email":"mock@gmail.com","name":"Mock User","picture":"https://lh3.googleusercontent.com/mock.jpg"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(data)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	account, err := fetchGoogleAccountInfoWithClient(context.Background(), mockClient, "test-access-token", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if account == nil {
		t.Fatal("expected account, got nil")
	}
	if account.ID != "google-user-mock" || account.Login != "mock@gmail.com" || account.AvatarURL != "https://lh3.googleusercontent.com/mock.jpg" {
		t.Fatalf("unexpected account: %+v", account)
	}
}

func TestGmailReauthorizationPreservesRefreshTokenOnlyForSameAccount(t *testing.T) {
	for _, sameAccount := range []bool{true, false} {
		t.Run(fmt.Sprint(sameAccount), func(t *testing.T) {
			srv, _, cfg := newConfigTestServer(t)
			original := &plugin.Connection{ID: "saved-gmail", PluginID: "gmail", Name: "Gmail", Account: &plugin.ConnectionAccount{ID: "old-account", Login: "old@example.com"}, Auth: plugin.Auth{Type: plugin.AuthTypeOAuth2, MethodID: "google-oauth", AccessToken: "old-access", RefreshToken: "saved-refresh", RefreshExpiresAt: time.Now().Add(24 * time.Hour)}}
			if err := cfg.PutPluginConnection(context.Background(), original); err != nil {
				t.Fatal(err)
			}
			accountID := "different-account"
			if sameAccount {
				accountID = "old-account"
			}
			payload, _ := json.Marshal(map[string]string{"sub": accountID, "email": "test@example.com", "picture": "https://example.com/avatar.png"})
			jwt := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
			previous := http.DefaultTransport
			http.DefaultTransport = googleOAuthRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://teatak.com/gmail/exchange" {
					return previous.RoundTrip(r)
				}
				data, _ := json.Marshal(map[string]any{"access_token": "new-access", "expires_in": 3600, "id_token": jwt})
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
			})
			defer func() { http.DefaultTransport = previous }()
			started := req(t, http.MethodPost, srv.URL+"/plugin-oauth/start", map[string]string{"pluginID": "gmail", "connectionID": "saved-gmail"})
			defer started.Body.Close()
			if started.StatusCode != 200 {
				t.Fatal(started.StatusCode)
			}
			var body struct {
				AuthorizationURL string `json:"authorizationURL"`
			}
			if err := json.NewDecoder(started.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(body.AuthorizationURL)
			if err != nil {
				t.Fatal(err)
			}
			response := req(t, http.MethodGet, srv.URL+"/oauth/callback/gmail?code=fixture&state="+url.QueryEscape(u.Query().Get("state")), nil)
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal(response.StatusCode)
			}
			saved, err := cfg.GetPluginConnection(context.Background(), "saved-gmail")
			if err != nil {
				t.Fatal(err)
			}
			if saved.Auth.AccessToken != "new-access" {
				t.Fatal("access token not updated")
			}
			if sameAccount {
				if saved.Auth.RefreshToken != "saved-refresh" || !saved.Auth.RefreshExpiresAt.Equal(original.Auth.RefreshExpiresAt) {
					t.Fatal("lost existing refresh token")
				}
			} else if saved.Auth.RefreshToken != "" {
				t.Fatal("copied refresh token across accounts")
			}
		})
	}
}
