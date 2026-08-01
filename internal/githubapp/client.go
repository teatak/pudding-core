package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

type Installation struct {
	ID           string       `json:"id"`
	Account      Account      `json:"account"`
	HTMLURL      string       `json:"htmlURL"`
	Repositories []Repository `json:"repositories"`
}

type Repository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"htmlURL"`
	DefaultBranch string `json:"defaultBranch,omitempty"`
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

func (c *Client) Installations(ctx context.Context, accessToken string) ([]Installation, error) {
	installations := make([]Installation, 0)
	for page := 1; page <= 20; page++ {
		var payload struct {
			Installations []struct {
				ID      json.Number `json:"id"`
				HTMLURL string      `json:"html_url"`
				Account struct {
					ID        json.Number `json:"id"`
					Login     string      `json:"login"`
					AvatarURL string      `json:"avatar_url"`
					Type      string      `json:"type"`
				} `json:"account"`
			} `json:"installations"`
		}
		path := "/user/installations?per_page=100&page=" + strconv.Itoa(page)
		if err := c.get(ctx, path, accessToken, &payload); err != nil {
			return nil, err
		}
		for _, item := range payload.Installations {
			installation := Installation{
				ID:      item.ID.String(),
				HTMLURL: item.HTMLURL,
				Account: Account{ID: item.Account.ID.String(), Login: item.Account.Login, AvatarURL: item.Account.AvatarURL, Type: item.Account.Type},
			}
			repositories, err := c.repositories(ctx, accessToken, installation.ID)
			if err != nil {
				return nil, err
			}
			installation.Repositories = repositories
			installations = append(installations, installation)
		}
		if len(payload.Installations) < 100 {
			break
		}
	}
	return installations, nil
}

func (c *Client) repositories(ctx context.Context, accessToken, installationID string) ([]Repository, error) {
	repositories := make([]Repository, 0)
	for page := 1; page <= 20; page++ {
		var payload struct {
			Repositories []struct {
				ID            json.Number `json:"id"`
				Name          string      `json:"name"`
				FullName      string      `json:"full_name"`
				Private       bool        `json:"private"`
				HTMLURL       string      `json:"html_url"`
				DefaultBranch string      `json:"default_branch"`
			} `json:"repositories"`
		}
		path := "/user/installations/" + url.PathEscape(installationID) + "/repositories?per_page=100&page=" + strconv.Itoa(page)
		if err := c.get(ctx, path, accessToken, &payload); err != nil {
			return nil, err
		}
		for _, item := range payload.Repositories {
			repositories = append(repositories, Repository{
				ID: item.ID.String(), Name: item.Name, FullName: item.FullName, Private: item.Private, HTMLURL: item.HTMLURL, DefaultBranch: item.DefaultBranch,
			})
		}
		if len(payload.Repositories) < 100 {
			break
		}
	}
	return repositories, nil
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
