package config

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

const pluginConnectionsFile = "plugin-connections.yaml"

type pluginConnectionsYAML struct {
	Version                 int                           `yaml:"version"`
	Connections             map[string]*plugin.Connection `yaml:"connections,omitempty"`
	Bindings                map[string]map[string]string  `yaml:"bindings,omitempty"`
	WebSingleActiveMigrated bool                          `yaml:"web_single_active_migrated,omitempty"`
	LegacyWebBindings       map[string]string             `yaml:"legacy_web_bindings,omitempty"`
	WebMigrated             bool                          `yaml:"web_connections_migrated,omitempty"`
}

func (m *Manager) ListPluginConnections(_ context.Context) ([]*plugin.Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cfg.Connections))
	for id := range cfg.Connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*plugin.Connection, 0, len(ids))
	for _, id := range ids {
		out = append(out, clonePluginConnection(id, cfg.Connections[id]))
	}
	return out, nil
}

func (m *Manager) GetPluginConnection(_ context.Context, id string) (*plugin.Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return nil, err
	}
	conn, ok := cfg.Connections[strings.TrimSpace(id)]
	if !ok {
		return nil, store.ErrNotFound
	}
	return clonePluginConnection(id, conn), nil
}

func (m *Manager) PutPluginConnection(_ context.Context, conn *plugin.Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := strings.TrimSpace(conn.ID)
	if id == "" || strings.ContainsAny(id, "/ ") {
		return store.ErrNotFound
	}
	conn.PluginID = store.CurrentPluginID(conn.PluginID)
	if conn.PluginID == "" {
		return store.ErrNotFound
	}
	cfg, err := m.readPluginConnections()
	if err != nil {
		return err
	}
	now := time.Now()
	existing := cfg.Connections[id]
	if existing != nil && (store.CurrentPluginID(existing.PluginID) != conn.PluginID || existing.Auth.MethodID != conn.Auth.MethodID || existing.Auth.Type != conn.Auth.Type) {
		for _, bindings := range cfg.Bindings {
			for _, selected := range bindings {
				if selected == id {
					return fmt.Errorf("%w: unbind a connection before changing its plugin or authentication method", ErrInvalidSetting)
				}
			}
		}
	}
	cp := plugin.CloneConnection(conn)
	cp.ID = ""
	if strings.TrimSpace(cp.Name) == "" {
		cp.Name = id
	}
	if existing != nil && !existing.CreatedAt.IsZero() {
		cp.CreatedAt = existing.CreatedAt
	} else {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	cfg.Connections[id] = cp
	return m.writePluginConnections(cfg)
}

func (m *Manager) DeletePluginConnection(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if _, ok := cfg.Connections[id]; !ok {
		return store.ErrNotFound
	}
	delete(cfg.Connections, id)
	clearConnectionBindings(&cfg, id)
	return m.writePluginConnections(cfg)
}

func (m *Manager) readPluginConnections() (pluginConnectionsYAML, error) {
	cfg, err := m.readPluginConnectionsRaw()
	if err != nil {
		return cfg, err
	}
	// Give legacy connections a durable instance identity before session selection.
	changed := false
	for _, c := range cfg.Connections {
		if c != nil && c.CreatedAt.IsZero() {
			c.CreatedAt = time.Now()
			changed = true
		}
	}
	if changed {
		if err := m.writePluginConnections(cfg); err != nil {
			return cfg, err
		}
	}
	if err := m.migrateWebConnections(&cfg); err != nil {
		return cfg, err
	}
	if err := m.migrateWebSingleActive(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (m *Manager) readPluginConnectionsRaw() (pluginConnectionsYAML, error) {
	var cfg pluginConnectionsYAML
	if err := readYAML(m.path(pluginConnectionsFile), &cfg); err != nil {
		if os.IsNotExist(err) {
			return pluginConnectionsYAML{Version: 1, Connections: map[string]*plugin.Connection{}}, nil
		}
		return cfg, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Connections == nil {
		cfg.Connections = map[string]*plugin.Connection{}
	}
	if cfg.Bindings == nil {
		cfg.Bindings = map[string]map[string]string{}
	}
	return cfg, nil
}

func (m *Manager) writePluginConnections(cfg pluginConnectionsYAML) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Connections == nil {
		cfg.Connections = map[string]*plugin.Connection{}
	}
	return writeYAML(m.path(pluginConnectionsFile), cfg)
}

func clonePluginConnection(id string, conn *plugin.Connection) *plugin.Connection {
	cp := plugin.CloneConnection(conn)
	if cp == nil {
		return nil
	}
	cp.ID = id
	cp.PluginID = store.CurrentPluginID(cp.PluginID)
	return cp
}

func clearConnectionBindings(cfg *pluginConnectionsYAML, id string) {
	for _, bindings := range cfg.Bindings {
		for purpose, selected := range bindings {
			if selected == id {
				delete(bindings, purpose)
			}
		}
	}
}

func (m *Manager) PluginConnectionBindings(_ context.Context, pluginID string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for purpose, id := range cfg.Bindings[store.CurrentPluginID(pluginID)] {
		out[purpose] = id
	}
	return out, nil
}

func (m *Manager) PatchPluginConnectionBindings(_ context.Context, pluginID string, update map[string]string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readPluginConnections()
	if err != nil {
		return nil, err
	}
	pluginID = store.CurrentPluginID(pluginID)
	if pluginID == plugin.BuiltinWebSearchFetchID {
		var err error
		update, err = normalizeWebBindingUpdate(update)
		if err != nil {
			return nil, err
		}
		cfg.LegacyWebBindings = nil
	}
	if cfg.Bindings == nil {
		cfg.Bindings = map[string]map[string]string{}
	}
	bindings := cfg.Bindings[pluginID]
	if bindings == nil {
		bindings = map[string]string{}
	}
	for purpose, id := range update {
		if id != "" {
			conn := cfg.Connections[id]
			if conn == nil || store.CurrentPluginID(conn.PluginID) != pluginID {
				return nil, fmt.Errorf("%w: connection does not belong to plugin", ErrInvalidSetting)
			}
		}
		if id == "" {
			delete(bindings, purpose)
		} else {
			bindings[purpose] = id
		}
	}
	cfg.Bindings[pluginID] = bindings
	if err := m.writePluginConnections(cfg); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for purpose, id := range bindings {
		out[purpose] = id
	}
	return out, nil
}
