package config

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/store"
)

const appConnectionsFile = "app-connections.yaml"

type appConnectionsYAML struct {
	Version     int                        `yaml:"version"`
	Connections map[string]*app.Connection `yaml:"connections,omitempty"`
}

func (m *Manager) ListAppConnections(_ context.Context) ([]*app.Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readAppConnections()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(cfg.Connections))
	for id := range cfg.Connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*app.Connection, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneAppConnection(id, cfg.Connections[id]))
	}
	return out, nil
}

func (m *Manager) GetAppConnection(_ context.Context, id string) (*app.Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readAppConnections()
	if err != nil {
		return nil, err
	}
	conn, ok := cfg.Connections[strings.TrimSpace(id)]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cloneAppConnection(id, conn), nil
}

func (m *Manager) PutAppConnection(_ context.Context, conn *app.Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := strings.TrimSpace(conn.ID)
	if id == "" || strings.ContainsAny(id, "/ ") {
		return store.ErrNotFound
	}
	conn.AppID = strings.TrimSpace(conn.AppID)
	if conn.AppID == "" {
		return store.ErrNotFound
	}
	cfg, err := m.readAppConnections()
	if err != nil {
		return err
	}
	now := time.Now()
	existing := cfg.Connections[id]
	cp := app.CloneConnection(conn)
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
	return m.writeAppConnections(cfg)
}

func (m *Manager) DeleteAppConnection(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, err := m.readAppConnections()
	if err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if _, ok := cfg.Connections[id]; !ok {
		return store.ErrNotFound
	}
	delete(cfg.Connections, id)
	return m.writeAppConnections(cfg)
}

func (m *Manager) readAppConnections() (appConnectionsYAML, error) {
	var cfg appConnectionsYAML
	if err := readYAML(m.path(appConnectionsFile), &cfg); err != nil {
		if os.IsNotExist(err) {
			return appConnectionsYAML{Version: 1, Connections: map[string]*app.Connection{}}, nil
		}
		return cfg, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Connections == nil {
		cfg.Connections = map[string]*app.Connection{}
	}
	return cfg, nil
}

func (m *Manager) writeAppConnections(cfg appConnectionsYAML) error {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Connections == nil {
		cfg.Connections = map[string]*app.Connection{}
	}
	return writeYAML(m.path(appConnectionsFile), cfg)
}

func cloneAppConnection(id string, conn *app.Connection) *app.Connection {
	cp := app.CloneConnection(conn)
	if cp == nil {
		return nil
	}
	cp.ID = id
	return cp
}
