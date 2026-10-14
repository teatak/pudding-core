package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const OfficialPluginRegistry = "https://teatak.github.io/pudding-hub/plugins/registry.json"
const pluginSourcesFile = "plugin-sources.yaml"

type PluginSource struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Official bool   `json:"official"`
}

type pluginSourcesYAML struct {
	Version int      `yaml:"version"`
	URLs    []string `yaml:"urls"`
}

// URLs are configuration; removing one never changes installed plugins or connections.
func NormalizePluginRegistryURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return "", fmt.Errorf("registryURL must be an HTTPS URL without credentials or a fragment")
	}
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	return u.String(), nil
}

func pluginSource(raw string) PluginSource {
	if raw == OfficialPluginRegistry {
		return PluginSource{ID: "official", URL: raw, Official: true}
	}
	sum := sha256.Sum256([]byte(raw))
	return PluginSource{ID: fmt.Sprintf("source_%x", sum[:16]), URL: raw}
}

func (m *Manager) readPluginSources() (pluginSourcesYAML, error) {
	cfg := pluginSourcesYAML{Version: 1, URLs: []string{}}
	if err := readYAML(m.path(pluginSourcesFile), &cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if cfg.Version != 1 {
		return cfg, fmt.Errorf("unsupported plugin sources version")
	}
	seen := map[string]bool{OfficialPluginRegistry: true}
	if len(cfg.URLs) > 32 {
		return cfg, fmt.Errorf("at most 32 plugin sources are allowed")
	}
	for i, raw := range cfg.URLs {
		canonical, err := NormalizePluginRegistryURL(raw)
		if err != nil {
			return cfg, err
		}
		if seen[canonical] {
			return cfg, fmt.Errorf("duplicate plugin source")
		}
		seen[canonical] = true
		cfg.URLs[i] = canonical
	}
	return cfg, nil
}

func (m *Manager) PluginSources(_ context.Context) ([]PluginSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginSources()
	if err != nil {
		return nil, err
	}
	sources := []PluginSource{pluginSource(OfficialPluginRegistry)}
	for _, raw := range cfg.URLs {
		sources = append(sources, pluginSource(raw))
	}
	return sources, nil
}

func (m *Manager) AddPluginSource(_ context.Context, raw string) (PluginSource, error) {
	canonical, err := NormalizePluginRegistryURL(raw)
	if err != nil {
		return PluginSource{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginSources()
	if err != nil {
		return PluginSource{}, err
	}
	source := pluginSource(canonical)
	if source.Official {
		return source, nil
	}
	for _, existing := range cfg.URLs {
		if existing == canonical {
			return source, nil
		}
	}
	if len(cfg.URLs) >= 32 {
		return PluginSource{}, fmt.Errorf("at most 32 plugin sources are allowed")
	}
	cfg.URLs = append(cfg.URLs, canonical)
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return PluginSource{}, err
	}
	if err := writeYAML(m.path(pluginSourcesFile), cfg); err != nil {
		return PluginSource{}, err
	}
	return source, nil
}

func (m *Manager) RemovePluginSource(_ context.Context, id string) error {
	if id == "official" {
		return fmt.Errorf("the official plugin source cannot be removed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginSources()
	if err != nil {
		return err
	}
	for i, raw := range cfg.URLs {
		if pluginSource(raw).ID == id {
			cfg.URLs = append(cfg.URLs[:i], cfg.URLs[i+1:]...)
			return writeYAML(m.path(pluginSourcesFile), cfg)
		}
	}
	return nil
}
