package config

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const webToolsFile = "web.yaml"

type WebToolProviderView struct {
	Name      string `json:"name"`
	APIKey    string `json:"apiKey,omitempty"`
	APIKeySet bool   `json:"apiKeySet"`
}

type WebToolsView struct {
	SearchProvider string                `json:"searchProvider,omitempty"`
	FetchProvider  string                `json:"fetchProvider,omitempty"`
	Providers      []WebToolProviderView `json:"providers"`
}

type WebToolsUpdate struct {
	SearchProvider *string                          `json:"searchProvider,omitempty"`
	FetchProvider  *string                          `json:"fetchProvider,omitempty"`
	Providers      map[string]WebToolProviderUpdate `json:"providers,omitempty"`
}

type WebToolProviderUpdate struct {
	APIKey *string `json:"apiKey,omitempty"`
}

type webToolsYAML struct {
	Version   int                            `yaml:"version"`
	Search    webToolFeatureYAML             `yaml:"search,omitempty"`
	Fetch     webToolFeatureYAML             `yaml:"fetch,omitempty"`
	Providers map[string]webToolProviderYAML `yaml:"providers,omitempty"`
}

type webToolFeatureYAML struct {
	Provider string `yaml:"provider,omitempty"`
}

type webToolProviderYAML struct {
	APIKey string `yaml:"api_key,omitempty"`
}

func (m *Manager) WebTools(_ context.Context) (*WebToolsView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readWebTools()
	if err != nil {
		return nil, err
	}
	return webToolsView(cfg), nil
}

func (m *Manager) PatchWebTools(_ context.Context, upd WebToolsUpdate) (*WebToolsView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readWebTools()
	if err != nil {
		return nil, err
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]webToolProviderYAML{}
	}
	if upd.SearchProvider != nil {
		provider, err := normalizeWebProvider(*upd.SearchProvider)
		if err != nil {
			return nil, err
		}
		cfg.Search.Provider = provider
	}
	if upd.FetchProvider != nil {
		provider, err := normalizeWebProvider(*upd.FetchProvider)
		if err != nil {
			return nil, err
		}
		cfg.Fetch.Provider = provider
	}
	for name, provider := range upd.Providers {
		name, err := normalizeWebProvider(name)
		if err != nil {
			return nil, err
		}
		if name == "" {
			continue
		}
		if provider.APIKey == nil {
			continue
		}
		next := strings.TrimSpace(*provider.APIKey)
		if next == "" {
			delete(cfg.Providers, name)
			if cfg.Search.Provider == name {
				cfg.Search.Provider = ""
			}
			if cfg.Fetch.Provider == name {
				cfg.Fetch.Provider = ""
			}
			continue
		}
		cfg.Providers[name] = webToolProviderYAML{APIKey: next}
		if cfg.Search.Provider == "" {
			cfg.Search.Provider = name
		}
		if cfg.Fetch.Provider == "" {
			cfg.Fetch.Provider = name
		}
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if err := m.writeWebTools(cfg); err != nil {
		return nil, err
	}
	return webToolsView(cfg), nil
}

func (m *Manager) TavilyAPIKey(ctx context.Context) (string, bool, error) {
	view, err := m.WebTools(ctx)
	if err != nil {
		return "", false, err
	}
	for _, provider := range view.Providers {
		if provider.Name == "tavily" && strings.TrimSpace(provider.APIKey) != "" {
			return provider.APIKey, true, nil
		}
	}
	return "", false, nil
}

func (m *Manager) readWebTools() (webToolsYAML, error) {
	var cfg webToolsYAML
	if err := readYAML(m.path(webToolsFile), &cfg); err != nil {
		if os.IsNotExist(err) {
			return webToolsYAML{Version: 1, Providers: map[string]webToolProviderYAML{}}, nil
		}
		return cfg, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]webToolProviderYAML{}
	}
	return cfg, nil
}

func (m *Manager) writeWebTools(cfg webToolsYAML) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]webToolProviderYAML{}
	}
	return writeYAML(m.path(webToolsFile), cfg)
}

func webToolsView(cfg webToolsYAML) *WebToolsView {
	return &WebToolsView{
		SearchProvider: cfg.Search.Provider,
		FetchProvider:  cfg.Fetch.Provider,
		Providers: []WebToolProviderView{
			webToolProviderView(cfg, "tavily"),
		},
	}
}

func webToolProviderView(cfg webToolsYAML, name string) WebToolProviderView {
	provider := cfg.Providers[name]
	apiKey := strings.TrimSpace(provider.APIKey)
	return WebToolProviderView{Name: name, APIKey: apiKey, APIKeySet: apiKey != ""}
}

func normalizeWebProvider(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "", "tavily":
		return name, nil
	default:
		return "", fmt.Errorf("config: unsupported web provider %q", name)
	}
}
