package oauthbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultBaseURL = "https://x-t.top"

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type StartRequest struct {
	Provider    string `json:"provider"`
	Client      string `json:"client"`
	ClientState string `json:"client_state"`
	Flow        string `json:"flow"`
	Challenge   string `json:"challenge"`
}

type StartResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	Provider         string `json:"provider"`
	AppID            string `json:"app_id"`
	ExpiresAt        string `json:"expires_at"`
}

type TokenResponse struct {
	AccessToken           string   `json:"access_token"`
	RefreshToken          string   `json:"refresh_token"`
	TokenType             string   `json:"token_type"`
	Scope                 string   `json:"scope"`
	ExpiresIn             int64    `json:"expires_in"`
	RefreshTokenExpiresIn int64    `json:"refresh_token_expires_in"`
	InstallationID        string   `json:"installation_id"`
	InstallationIDs       []string `json:"installation_ids"`
	InstallationURL       string   `json:"installation_url"`
}

type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func New(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, httpClient: httpClient}
}

func (c *Client) Start(ctx context.Context, in StartRequest) (*StartResponse, error) {
	var out StartResponse
	if err := c.postJSON(ctx, "/oauth/start", in, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AuthorizationURL) == "" {
		return nil, errors.New("oauth broker did not return an authorization URL")
	}
	return &out, nil
}

func (c *Client) Redeem(ctx context.Context, ticket, verifier string) (*TokenResponse, error) {
	var out TokenResponse
	if err := c.postJSON(ctx, "/oauth/redeem", map[string]string{
		"ticket":   ticket,
		"verifier": verifier,
	}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, errors.New("oauth broker did not return an access token")
	}
	return &out, nil
}

func (c *Client) Refresh(ctx context.Context, provider, refreshToken string) (*TokenResponse, error) {
	var out TokenResponse
	if err := c.postJSON(ctx, "/oauth/refresh", map[string]string{
		"provider":      provider,
		"refresh_token": refreshToken,
	}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, errors.New("oauth broker did not return a refreshed access token")
	}
	return &out, nil
}

func (c *Client) Revoke(ctx context.Context, provider, accessToken string) error {
	return c.postJSON(ctx, "/oauth/revoke", map[string]string{
		"provider":     provider,
		"access_token": accessToken,
	}, nil)
}

func (c *Client) postJSON(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure errorResponse
		_ = json.Unmarshal(data, &failure)
		if strings.TrimSpace(failure.ErrorDescription) != "" {
			return errors.New(failure.ErrorDescription)
		}
		if strings.TrimSpace(failure.Error) != "" {
			return errors.New(failure.Error)
		}
		return fmt.Errorf("oauth broker request failed: %s", resp.Status)
	}
	if output == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode oauth broker response: %w", err)
	}
	return nil
}
