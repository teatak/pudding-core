package config

import (
	"context"
	"fmt"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"os"
	"sort"
	"strings"
	"time"
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

// WebTools and PatchWebTools are compatibility projections over standard connections.
func (m *Manager) WebTools(_ context.Context) (*WebToolsView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return nil, err
	}
	return webConnectionsView(cfg), nil
}

func webConnection(cfg pluginConnectionsYAML, provider, feature string) *plugin.Connection {
	bindings := cfg.Bindings[plugin.BuiltinWebSearchFetchID]
	for _, purpose := range []string{"active"} {
		if c := cfg.Connections[bindings[purpose]]; c != nil && store.CurrentPluginID(c.PluginID) == plugin.BuiltinWebSearchFetchID && c.Auth.MethodID == provider {
			return c
		}
	}
	ids := make([]string, 0, len(cfg.Connections))
	for id := range cfg.Connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := cfg.Connections[id]
		if c != nil && store.CurrentPluginID(c.PluginID) == plugin.BuiltinWebSearchFetchID && c.Auth.MethodID == provider {
			return c
		}
	}
	return nil
}

func addWebConnection(cfg *pluginConnectionsYAML, provider, key string) string {
	id := "web-search-fetch-" + provider
	for n := 2; cfg.Connections[id] != nil; n++ {
		id = fmt.Sprintf("web-search-fetch-%s-%d", provider, n)
	}
	cfg.Connections[id] = &plugin.Connection{Name: map[string]string{"tavily": "Tavily", "deepseek": "DeepSeek"}[provider], PluginID: plugin.BuiltinWebSearchFetchID, Auth: plugin.Auth{MethodID: provider, Type: plugin.AuthTypeBearer, Token: key}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return id
}

func webConnectionID(cfg pluginConnectionsYAML, target *plugin.Connection) string {
	for id, c := range cfg.Connections {
		if c == target {
			return id
		}
	}
	return ""
}

func webConnectionsView(cfg pluginConnectionsYAML) *WebToolsView {
	view := &WebToolsView{Providers: []WebToolProviderView{}}
	for _, name := range []string{"tavily", "deepseek"} {
		p := WebToolProviderView{Name: name}
		if c := webConnection(cfg, name, ""); c != nil {
			p.APIKey = c.Auth.Token
			p.APIKeySet = strings.TrimSpace(p.APIKey) != ""
		}
		view.Providers = append(view.Providers, p)
	}
	for purpose, id := range cfg.Bindings[plugin.BuiltinWebSearchFetchID] {
		c := cfg.Connections[id]
		if c == nil || store.CurrentPluginID(c.PluginID) != plugin.BuiltinWebSearchFetchID {
			continue
		}
		if purpose == "active" {
			view.SearchProvider = c.Auth.MethodID
		}
		if purpose == "active" {
			view.FetchProvider = c.Auth.MethodID
		}
	}
	return view
}

func (m *Manager) PatchWebTools(_ context.Context, upd WebToolsUpdate) (*WebToolsView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return nil, err
	}
	// Validate the whole legacy patch before changing the stored snapshot.
	for _, value := range []*string{upd.SearchProvider, upd.FetchProvider} {
		if value != nil {
			if _, err := normalizeWebProvider(*value); err != nil {
				return nil, err
			}
		}
	}
	if upd.SearchProvider != nil && upd.FetchProvider != nil {
		search, _ := normalizeWebProvider(*upd.SearchProvider)
		fetch, _ := normalizeWebProvider(*upd.FetchProvider)
		if search != fetch {
			return nil, fmt.Errorf("%w: search and fetch must share one active connection", ErrInvalidSetting)
		}
	}
	for name := range upd.Providers {
		if _, err := normalizeWebProvider(name); err != nil {
			return nil, err
		}
	}
	for name, p := range upd.Providers {
		name, _ = normalizeWebProvider(name)
		if p.APIKey == nil || name == "" {
			continue
		}
		key := strings.TrimSpace(*p.APIKey)
		c := webConnection(cfg, name, "")
		if c == nil {
			if key != "" {
				addWebConnection(&cfg, name, key)
			}
			continue
		}
		id := webConnectionID(cfg, c)
		if key == "" {
			delete(cfg.Connections, id)
			clearConnectionBindings(&cfg, id)
		} else {
			c.Auth.Token = key
			c.UpdatedAt = time.Now()
		}
	}
	if cfg.Bindings == nil {
		cfg.Bindings = map[string]map[string]string{}
	}
	bindings := cfg.Bindings[plugin.BuiltinWebSearchFetchID]
	if bindings == nil {
		bindings = map[string]string{}
	}
	value := upd.SearchProvider
	if value == nil {
		value = upd.FetchProvider
	}
	if value != nil {
		name, _ := normalizeWebProvider(*value)
		if name == "" {
			delete(bindings, "active")
		} else {
			c := webConnection(cfg, name, "")
			id := ""
			if c == nil {
				id = addWebConnection(&cfg, name, "")
			} else {
				id = webConnectionID(cfg, c)
			}
			bindings["active"] = id
		}
		cfg.LegacyWebBindings = nil
	}
	cfg.Bindings[plugin.BuiltinWebSearchFetchID] = bindings
	if err := m.writePluginConnections(cfg); err != nil {
		return nil, err
	}
	return webConnectionsView(cfg), nil
}

func (m *Manager) TavilyAPIKey(ctx context.Context) (string, bool, error) {
	view, err := m.WebTools(ctx)
	if err != nil {
		return "", false, err
	}
	p := view.Providers[0]
	return p.APIKey, p.APIKeySet, nil
}

// ResolveWebTool resolves only the explicit binding, never another saved connection.
func (m *Manager) ResolveWebTool(_ context.Context, feature string) (string, string, error) {
	if feature != "search" && feature != "fetch" {
		return "", "", fmt.Errorf("config: unsupported web feature %q", feature)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return "", "", err
	}
	id := cfg.Bindings[plugin.BuiltinWebSearchFetchID]["active"]
	c := cfg.Connections[id]
	if c == nil {
		return "", "", nil
	}
	if store.CurrentPluginID(c.PluginID) != plugin.BuiltinWebSearchFetchID || c.Auth.Type != plugin.AuthTypeBearer {
		return "", "", fmt.Errorf("config: invalid web connection")
	}
	name, err := normalizeWebProvider(c.Auth.MethodID)
	return name, c.Auth.Token, err
}

func (m *Manager) migrateWebConnections(cfg *pluginConnectionsYAML) error {
	if cfg.WebMigrated {
		return nil
	}
	legacy, err := m.readWebTools()
	if err != nil {
		return err
	}
	if _, err := normalizeWebProvider(legacy.Search.Provider); err != nil {
		return err
	}
	if _, err := normalizeWebProvider(legacy.Fetch.Provider); err != nil {
		return err
	}
	if cfg.Bindings == nil {
		cfg.Bindings = map[string]map[string]string{}
	}
	bindings := cfg.Bindings[plugin.BuiltinWebSearchFetchID]
	if bindings == nil {
		bindings = map[string]string{}
	}
	for _, name := range []string{"tavily", "deepseek"} {
		key := strings.TrimSpace(legacy.Providers[name].APIKey)
		selected := legacy.Search.Provider == name || legacy.Fetch.Provider == name
		c := webConnection(*cfg, name, "")
		id := ""
		if key != "" && (c == nil || c.Auth.Token != key) {
			id = addWebConnection(cfg, name, key)
		} else if c != nil {
			id = webConnectionID(*cfg, c)
		} else if selected {
			id = addWebConnection(cfg, name, key)
		}
		if id != "" {
			if legacy.Search.Provider == name && bindings["search"] == "" {
				bindings["search"] = id
			}
			if legacy.Fetch.Provider == name && bindings["fetch"] == "" {
				bindings["fetch"] = id
			}
		}
	}
	cfg.Bindings[plugin.BuiltinWebSearchFetchID] = bindings
	cfg.WebMigrated = true
	// Credentials, explicit selection and the migration marker commit atomically.
	if err := m.writePluginConnections(*cfg); err != nil {
		return err
	}
	if err := os.Remove(m.path(webToolsFile)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
			webToolProviderView(cfg, "deepseek"),
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
	case "", "tavily", "deepseek":
		return name, nil
	default:
		return "", fmt.Errorf("config: unsupported web provider %q", name)
	}
}

// Convert old per-purpose selections once. Conflicting explicit choices are
// preserved for review rather than silently picking an account for the user.
func (m *Manager) migrateWebSingleActive(cfg *pluginConnectionsYAML) error {
	if cfg.WebSingleActiveMigrated {
		return nil
	}
	if cfg.Bindings == nil {
		cfg.Bindings = map[string]map[string]string{}
	}
	b := cfg.Bindings[plugin.BuiltinWebSearchFetchID]
	if b == nil {
		b = cfg.Bindings["web"]
	}
	active := b["active"]
	if active == "" {
		search, fetch := b["search"], b["fetch"]
		if search != "" && fetch != "" && search != fetch {
			cfg.LegacyWebBindings = map[string]string{"search": search, "fetch": fetch}
		} else if search != "" {
			active = search
		} else {
			active = fetch
		}
	}
	cfg.Bindings[plugin.BuiltinWebSearchFetchID] = map[string]string{}
	if active != "" {
		cfg.Bindings[plugin.BuiltinWebSearchFetchID]["active"] = active
	}
	delete(cfg.Bindings, "web")
	cfg.WebSingleActiveMigrated = true
	return m.writePluginConnections(*cfg)
}

func normalizeWebBindingUpdate(update map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for purpose, id := range update {
		if purpose != "active" && purpose != "search" && purpose != "fetch" {
			return nil, fmt.Errorf("%w: unknown connection binding", ErrInvalidSetting)
		}
		if prev, ok := out["active"]; ok && prev != id {
			return nil, fmt.Errorf("%w: search and fetch must share one active connection", ErrInvalidSetting)
		}
		out["active"] = id
	}
	return out, nil
}

func (m *Manager) LegacyWebConnectionBindings(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range cfg.LegacyWebBindings {
		out[k] = v
	}
	return out, nil
}
