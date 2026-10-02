package config

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

const pluginConnectionsFile = "plugin-connections.yaml"

type pluginConnectionsYAML struct {
	Version     int                           `yaml:"version"`
	Connections map[string]*plugin.Connection `yaml:"connections,omitempty"`
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
	conn.PluginID = strings.TrimSpace(conn.PluginID)
	if conn.PluginID == "" {
		return store.ErrNotFound
	}
	cfg, err := m.readPluginConnections()
	if err != nil {
		return err
	}
	now := time.Now()
	existing := cfg.Connections[id]
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
	return m.writePluginConnections(cfg)
}

func (m *Manager) readPluginConnections() (pluginConnectionsYAML, error) {
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
	return cp
}
