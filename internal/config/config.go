// Package config owns user-editable YAML configuration under the Pudding home.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"gopkg.in/yaml.v3"
)

const (
	settingsFile = "settings.yaml"
	profilesFile = "profiles.yaml"
)

type Manager struct {
	dir string
	mu  sync.Mutex
}

func NewManager(homeDir string) *Manager {
	return &Manager{dir: filepath.Join(homeDir, "config")}
}

func (m *Manager) Prepare() error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", m.dir, err)
	}
	if _, err := os.Stat(m.path(settingsFile)); errors.Is(err, os.ErrNotExist) {
		if err := m.writeSettings(settingsYAML{Version: 1}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(m.path(profilesFile)); errors.Is(err, os.ErrNotExist) {
		if err := m.writeProfiles(profilesYAML{Version: 1, Profiles: map[string]*store.ProviderProfile{}}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

func (m *Manager) Settings(_ context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readSettings()
	if err != nil {
		return nil, err
	}
	return cfg.asMap(), nil
}

func (m *Manager) SetSettings(_ context.Context, kv map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readSettings()
	if err != nil {
		return err
	}
	for k := range kv {
		if k != store.SettingSystemPrompt {
			return fmt.Errorf("config: unsupported setting %q", k)
		}
	}
	if v, ok := kv[store.SettingSystemPrompt]; ok {
		cfg.SystemPrompt = v
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return m.writeSettings(cfg)
}

func (m *Manager) ListProviderProfiles(_ context.Context) ([]*store.ProviderProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readProfiles()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cfg.Profiles))
	for id := range cfg.Profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*store.ProviderProfile, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneProfile(id, cfg.Profiles[id]))
	}
	return out, nil
}

func (m *Manager) GetProviderProfile(_ context.Context, id string) (*store.ProviderProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readProfiles()
	if err != nil {
		return nil, err
	}
	p, ok := cfg.Profiles[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneProfile(id, p), nil
}

func (m *Manager) PutProviderProfile(_ context.Context, p *store.ProviderProfile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := strings.TrimSpace(p.ProfileID())
	if id == "" {
		return store.ErrNotFound
	}
	cfg, err := m.readProfiles()
	if err != nil {
		return err
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*store.ProviderProfile{}
	}
	cp := cloneProfile(id, p)
	cp.ID = ""
	cp.CreatedAt = time.Time{}
	cp.UpdatedAt = time.Time{}
	cfg.Profiles[id] = cp
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return m.writeProfiles(cfg)
}

func (m *Manager) DeleteProviderProfile(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readProfiles()
	if err != nil {
		return err
	}
	if _, ok := cfg.Profiles[id]; !ok {
		return store.ErrNotFound
	}
	delete(cfg.Profiles, id)
	return m.writeProfiles(cfg)
}

func EffectiveAPIKey(p *store.ProviderProfile) string {
	if p == nil {
		return ""
	}
	if p.APIKey != "" {
		return p.APIKey
	}
	return ""
}

func (m *Manager) path(name string) string { return filepath.Join(m.dir, name) }

type settingsYAML struct {
	Version      int    `yaml:"version"`
	SystemPrompt string `yaml:"system_prompt,omitempty"`
}

func (s settingsYAML) asMap() map[string]string {
	out := map[string]string{}
	if s.SystemPrompt != "" {
		out[store.SettingSystemPrompt] = s.SystemPrompt
	}
	return out
}

type profilesYAML struct {
	Version  int                               `yaml:"version"`
	Profiles map[string]*store.ProviderProfile `yaml:"profiles"`
}

func (m *Manager) readSettings() (settingsYAML, error) {
	var cfg settingsYAML
	if err := readYAML(m.path(settingsFile), &cfg); err != nil {
		return cfg, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return cfg, nil
}

func (m *Manager) writeSettings(cfg settingsYAML) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return writeYAML(m.path(settingsFile), cfg)
}

func (m *Manager) readProfiles() (profilesYAML, error) {
	var cfg profilesYAML
	if err := readYAML(m.path(profilesFile), &cfg); err != nil {
		return cfg, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*store.ProviderProfile{}
	}
	return cfg, nil
}

func (m *Manager) writeProfiles(cfg profilesYAML) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*store.ProviderProfile{}
	}
	return writeYAML(m.path(profilesFile), cfg)
}

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	return nil
}

func writeYAML(path string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cloneProfile(id string, p *store.ProviderProfile) *store.ProviderProfile {
	if p == nil {
		return nil
	}
	cp := *p
	cp.ID = id
	cp.Models = append([]store.ProviderModel(nil), p.Models...)
	return &cp
}
