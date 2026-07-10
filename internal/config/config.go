// Package config owns user-editable YAML configuration under the Pudding home.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/store"
	"gopkg.in/yaml.v3"
)

const (
	settingsFile = "settings.yaml"
	profilesFile = "profiles.yaml"
)

const (
	SettingCompactTailInputTurns       = "compact_tail_input_turns"
	SettingCompactAutoThresholdPercent = "compact_auto_threshold_percent"
	SettingShowCompactSummary          = "show_compact_summary"
	SettingShowReasoning               = "show_reasoning"
	SettingShowRawToolInfo             = "show_raw_tool_info"
)

const (
	DefaultCompactTailInputTurns       = 2
	DefaultCompactAutoThresholdPercent = 80
)

var ErrInvalidSetting = errors.New("config: invalid setting")

type Manager struct {
	homeDir string
	dir     string
	mu      sync.Mutex
}

func NewManager(homeDir string) *Manager {
	return &Manager{homeDir: homeDir, dir: filepath.Join(homeDir, "config")}
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
	if _, err := os.Stat(m.path(audioFile)); errors.Is(err, os.ErrNotExist) {
		if err := m.writeAudio(DefaultAudioConfig()); err != nil {
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
	for k, v := range kv {
		if err := cfg.set(k, v); err != nil {
			return err
		}
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return m.writeSettings(cfg)
}

type UserPromptView struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

func (m *Manager) UserPrompt(_ context.Context) (*UserPromptView, error) {
	path := prompt.UserInstructionPath(m.homeDir)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &UserPromptView{Path: path}, nil
		}
		return nil, fmt.Errorf("config: read user prompt %s: %w", path, err)
	}
	return &UserPromptView{Path: path, Content: string(b), Exists: true}, nil
}

func (m *Manager) SetUserPrompt(_ context.Context, content string) (*UserPromptView, error) {
	if err := os.MkdirAll(m.homeDir, 0o700); err != nil {
		return nil, fmt.Errorf("config: mkdir %s: %w", m.homeDir, err)
	}
	path := prompt.UserInstructionPath(m.homeDir)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("config: write user prompt %s: %w", path, err)
	}
	return &UserPromptView{Path: path, Content: content, Exists: true}, nil
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
	Version int                 `yaml:"version"`
	Compact compactSettingsYAML `yaml:"compact,omitempty"`
	Display displaySettingsYAML `yaml:"display,omitempty"`
}

type compactSettingsYAML struct {
	TailInputTurns       int  `yaml:"tail_input_turns,omitempty"`
	AutoThresholdPercent *int `yaml:"auto_threshold_percent,omitempty"`
}

type displaySettingsYAML struct {
	CompactSummary *bool `yaml:"compact_summary,omitempty"`
	Reasoning      *bool `yaml:"reasoning,omitempty"`
	RawToolInfo    *bool `yaml:"raw_tool_info,omitempty"`
}

func (s settingsYAML) asMap() map[string]string {
	return map[string]string{
		SettingCompactTailInputTurns:       strconv.Itoa(s.compactTailInputTurns()),
		SettingCompactAutoThresholdPercent: strconv.Itoa(s.compactAutoThresholdPercent()),
		SettingShowCompactSummary:          formatBoolSetting(s.showCompactSummary()),
		SettingShowReasoning:               formatBoolSetting(s.showReasoning()),
		SettingShowRawToolInfo:             formatBoolSetting(s.showRawToolInfo()),
	}
}

func (s *settingsYAML) set(key, raw string) error {
	switch key {
	case SettingCompactTailInputTurns:
		n, err := parseIntSetting(raw, 1, 50)
		if err != nil {
			return err
		}
		s.Compact.TailInputTurns = n
	case SettingCompactAutoThresholdPercent:
		n, err := parseIntSetting(raw, 0, 100)
		if err != nil {
			return err
		}
		s.Compact.AutoThresholdPercent = &n
	case SettingShowCompactSummary:
		v, err := parseBoolSetting(raw)
		if err != nil {
			return err
		}
		s.Display.CompactSummary = &v
	case SettingShowReasoning:
		v, err := parseBoolSetting(raw)
		if err != nil {
			return err
		}
		s.Display.Reasoning = &v
	case SettingShowRawToolInfo:
		v, err := parseBoolSetting(raw)
		if err != nil {
			return err
		}
		s.Display.RawToolInfo = &v
	default:
		return fmt.Errorf("%w %q", ErrInvalidSetting, key)
	}
	return nil
}

func (s settingsYAML) compactTailInputTurns() int {
	if s.Compact.TailInputTurns > 0 {
		return s.Compact.TailInputTurns
	}
	return DefaultCompactTailInputTurns
}

func (s settingsYAML) compactAutoThresholdPercent() int {
	if s.Compact.AutoThresholdPercent != nil {
		return *s.Compact.AutoThresholdPercent
	}
	return DefaultCompactAutoThresholdPercent
}

func (s settingsYAML) showCompactSummary() bool {
	if s.Display.CompactSummary != nil {
		return *s.Display.CompactSummary
	}
	return true
}

func (s settingsYAML) showReasoning() bool {
	if s.Display.Reasoning != nil {
		return *s.Display.Reasoning
	}
	return true
}

func (s settingsYAML) showRawToolInfo() bool {
	if s.Display.RawToolInfo != nil {
		return *s.Display.RawToolInfo
	}
	return true
}

func parseIntSetting(raw string, min, max int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%w: expected integer %d..%d", ErrInvalidSetting, min, max)
	}
	return n, nil
}

func parseBoolSetting(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%w: expected boolean", ErrInvalidSetting)
	}
}

func formatBoolSetting(v bool) string {
	if v {
		return "true"
	}
	return "false"
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
