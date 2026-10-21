package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"
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
