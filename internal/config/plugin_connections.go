package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	return m.putPluginConnection(conn, nil)
}

// The revision check and write share the storage lock, so delayed logins cannot overwrite edits.
func (m *Manager) PutPluginConnectionIfUnchanged(_ context.Context, conn *plugin.Connection, expected time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.putPluginConnection(conn, &expected)
}

func (m *Manager) putPluginConnection(conn *plugin.Connection, expected *time.Time) error {
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
	if expected != nil && ((existing == nil && !expected.IsZero()) || (existing != nil && !existing.UpdatedAt.Equal(*expected))) {
		return fmt.Errorf("%w: connection changed during login", ErrInvalidSetting)
	}
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
	// References belong to the stored connection ID, never to a caller's clone.
	cp.CredentialRef = ""
	if existing != nil {
		cp.CredentialRef = existing.CredentialRef
	}
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
	if cfg.Version > 2 {
		return cfg, errors.New("unsupported plugin connection storage version")
	}
	legacy := false
	for id, conn := range cfg.Connections {
		if conn == nil {
			return cfg, errors.New("invalid plugin connection")
		}
		if conn.CredentialRef != "" {
			if hasPluginSecrets(conn) {
				return cfg, errors.New("mixed plaintext and encrypted plugin credentials")
			}
			data, err := m.credentials.Read(conn.CredentialRef, id)
			if err != nil {
				return cfg, err
			}
			var secrets pluginSecrets
			if json.Unmarshal(data, &secrets) != nil {
				return cfg, errors.New("invalid plugin credentials")
			}
			secrets.apply(conn)
		} else if hasPluginSecrets(conn) {
			legacy = true
		}
	}
	if legacy {
		if err := m.writePluginConnections(cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func (m *Manager) writePluginConnections(cfg pluginConnectionsYAML) error {
	disk := cfg
	disk.Version = 2
	disk.Connections = make(map[string]*plugin.Connection, len(cfg.Connections))
	live := map[string]bool{}
	for id, conn := range cfg.Connections {
		copy := plugin.CloneConnection(conn)
		if hasPluginSecrets(conn) {
			data, err := json.Marshal(secretsFor(conn))
			if err != nil {
				return err
			}
			ref := conn.CredentialRef
			same := false
			if ref != "" {
				old, err := m.credentials.Read(ref, id)
				if err != nil {
					return err
				}
				same = bytes.Equal(old, data)
			}
			if !same {
				ref, err = m.credentials.Write(id, data)
				if err != nil {
					return err
				}
			}
			copy.CredentialRef = ref
			live[ref] = true
		} else {
			copy.CredentialRef = ""
		}
		copy.Auth.Token, copy.Auth.AccessToken, copy.Auth.RefreshToken, copy.Auth.Password = "", "", "", ""
		copy.Fields = nil
		disk.Connections[id] = copy
	}
	if err := writeYAML(m.path(pluginConnectionsFile), disk, true); err != nil {
		return err
	}
	// Refresh references only after the atomic config commit.
	for id, conn := range cfg.Connections {
		conn.CredentialRef = disk.Connections[id].CredentialRef
	}
	if err := m.credentials.Prune(live); err != nil {
		slog.Warn("plugin encrypted credential cleanup incomplete")
	}
	return nil
}

type pluginSecrets struct {
	Token        string            `json:"token,omitempty"`
	AccessToken  string            `json:"accessToken,omitempty"`
	RefreshToken string            `json:"refreshToken,omitempty"`
	Password     string            `json:"password,omitempty"`
	Fields       map[string]string `json:"fields,omitempty"`
}

func secretsFor(c *plugin.Connection) pluginSecrets {
	return pluginSecrets{c.Auth.Token, c.Auth.AccessToken, c.Auth.RefreshToken, c.Auth.Password, c.Fields}
}
func hasPluginSecrets(c *plugin.Connection) bool {
	return c.Auth.Token != "" || c.Auth.AccessToken != "" || c.Auth.RefreshToken != "" || c.Auth.Password != "" || len(c.Fields) > 0
}
func (s pluginSecrets) apply(c *plugin.Connection) {
	c.Auth.Token = s.Token
	c.Auth.AccessToken = s.AccessToken
	c.Auth.RefreshToken = s.RefreshToken
	c.Auth.Password = s.Password
	c.Fields = s.Fields
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
