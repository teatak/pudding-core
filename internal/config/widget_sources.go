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

const OfficialWidgetRegistry = "https://teatak.github.io/pudding-hub/widgets/registry.json"
const widgetSourcesFile = "widget-sources.yaml"

type WidgetSource struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Official bool   `json:"official"`
}

type widgetSourcesYAML struct {
	Version int      `yaml:"version"`
	URLs    []string `yaml:"urls"`
}

// URLs are configuration; installed items and their provenance remain in Studio.
func NormalizeWidgetRegistryURL(raw string) (string, error) {
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

func widgetSource(raw string) WidgetSource {
	if raw == OfficialWidgetRegistry {
		return WidgetSource{ID: "official", URL: raw, Official: true}
	}
	sum := sha256.Sum256([]byte(raw))
	return WidgetSource{ID: fmt.Sprintf("source_%x", sum[:16]), URL: raw}
}

func (m *Manager) readWidgetSources() (widgetSourcesYAML, error) {
	cfg := widgetSourcesYAML{Version: 1, URLs: []string{}}
	if err := readYAML(m.path(widgetSourcesFile), &cfg); err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if cfg.Version != 1 {
		return cfg, fmt.Errorf("unsupported widget sources version")
	}
	seen := map[string]bool{OfficialWidgetRegistry: true}
	if len(cfg.URLs) > 32 {
		return cfg, fmt.Errorf("at most 32 widget sources are allowed")
	}
	for i, raw := range cfg.URLs {
		canonical, err := NormalizeWidgetRegistryURL(raw)
		if err != nil {
			return cfg, err
		}
		if seen[canonical] {
			return cfg, fmt.Errorf("duplicate widget source")
		}
		seen[canonical] = true
		cfg.URLs[i] = canonical
	}
	return cfg, nil
}

func (m *Manager) WidgetSources(_ context.Context) ([]WidgetSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readWidgetSources()
	if err != nil {
		return nil, err
	}
	sources := []WidgetSource{widgetSource(OfficialWidgetRegistry)}
	for _, raw := range cfg.URLs {
		sources = append(sources, widgetSource(raw))
	}
	return sources, nil
}

func (m *Manager) AddWidgetSource(_ context.Context, raw string) (WidgetSource, error) {
	canonical, err := NormalizeWidgetRegistryURL(raw)
	if err != nil {
		return WidgetSource{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readWidgetSources()
	if err != nil {
		return WidgetSource{}, err
	}
	source := widgetSource(canonical)
	if source.Official {
		return source, nil
	}
	for _, existing := range cfg.URLs {
		if existing == canonical {
			return source, nil
		}
	}
	if len(cfg.URLs) >= 32 {
		return WidgetSource{}, fmt.Errorf("at most 32 widget sources are allowed")
	}
	cfg.URLs = append(cfg.URLs, canonical)
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return WidgetSource{}, err
	}
	if err := writeYAML(m.path(widgetSourcesFile), cfg); err != nil {
		return WidgetSource{}, err
	}
	return source, nil
}

func (m *Manager) RemoveWidgetSource(_ context.Context, id string) error {
	if id == "official" {
		return fmt.Errorf("the official widget source cannot be removed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readWidgetSources()
	if err != nil {
		return err
	}
	for i, raw := range cfg.URLs {
		if widgetSource(raw).ID == id {
			cfg.URLs = append(cfg.URLs[:i], cfg.URLs[i+1:]...)
			return writeYAML(m.path(widgetSourcesFile), cfg)
		}
	}
	return nil
}
