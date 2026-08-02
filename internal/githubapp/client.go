package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultAPIBaseURL = "https://api.github.com"

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Account struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarURL,omitempty"`
	Type      string `json:"type,omitempty"`
}

func New(baseURL string, httpClient *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultAPIBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{baseURL: baseURL, httpClient: httpClient}
}

func (c *Client) Account(ctx context.Context, accessToken string) (*Account, error) {
	var payload struct {
		ID        json.Number `json:"id"`
		Login     string      `json:"login"`
		Name      string      `json:"name"`
		AvatarURL string      `json:"avatar_url"`
		Type      string      `json:"type"`
	}
	if err := c.get(ctx, "/user", accessToken, &payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.ID.String()) == "" || strings.TrimSpace(payload.Login) == "" {
		return nil, errors.New("github did not return an account identity")
	}
	return &Account{ID: payload.ID.String(), Login: payload.Login, Name: payload.Name, AvatarURL: payload.AvatarURL, Type: payload.Type}, nil
}

func (c *Client) get(ctx context.Context, path, accessToken string, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", "Pudding Desktop")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &failure)
		if strings.TrimSpace(failure.Message) != "" {
			return errors.New(failure.Message)
		}
		return fmt.Errorf("github request failed: %s", resp.Status)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode github response: %w", err)
	}
	return nil
}
