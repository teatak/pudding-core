package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

type ConnectionBindingsStore interface {
	PluginConnectionBindings(context.Context, string) (map[string]string, error)
	PatchPluginConnectionBindings(context.Context, string, map[string]string) (map[string]string, error)
}

func (s *Service) ConnectionBindings(ctx context.Context, id string) (*ConnectionBindingsView, error) {
	def, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	bindings := map[string]string{}
	if cfg, ok := s.connections.(ConnectionBindingsStore); ok {
		bindings, err = cfg.PluginConnectionBindings(ctx, def.ID)
		if err != nil {
			return nil, err
		}
	}
	view := &ConnectionBindingsView{Bindings: bindings}
	if def.ID == BuiltinWebSearchFetchID {
		if source, ok := s.connections.(interface {
			LegacyWebConnectionBindings(context.Context) (map[string]string, error)
		}); ok {
			view.LegacyBindings, err = source.LegacyWebConnectionBindings(ctx)
			if err != nil {
				return nil, err
			}
		}
	}
	return view, nil
}

func declaredConnectionBinding(def *Definition, id string) bool {
	return ConnectionSelection(def) == ConnectionSelectionSingleActive && id == "active"
}

func (s *Service) PatchConnectionBindings(ctx context.Context, id string, update map[string]string) (*ConnectionBindingsView, error) {
	def, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	if def.ID == BuiltinWebSearchFetchID {
		canonical := map[string]string{}
		for k, v := range update {
			if k != "active" && k != "search" && k != "fetch" {
				return nil, fmt.Errorf("%w: unknown connection binding", ErrInvalidID)
			}
			if prev, ok := canonical["active"]; ok && prev != v {
				return nil, fmt.Errorf("%w: search and fetch must share one connection", ErrInvalidID)
			}
			canonical["active"] = v
		}
		update = canonical
	}
	cfg, ok := s.connections.(ConnectionBindingsStore)
	if !ok {
		return nil, fmt.Errorf("plugin: connection bindings unavailable")
	}
	for purpose, connectionID := range update {
		if !declaredConnectionBinding(def, purpose) {
			return nil, fmt.Errorf("%w: unknown connection binding %q", ErrInvalidID, purpose)
		}
		if connectionID == "" {
			continue
		}
		conn, err := s.ReadyConnection(ctx, connectionID)
		if err != nil {
			return nil, fmt.Errorf("%w: connection unavailable", ErrInvalidID)
		}
		if store.CurrentPluginID(conn.PluginID) != def.ID {
			return nil, fmt.Errorf("%w: connection belongs to another plugin", ErrInvalidID)
		}
		method, ok := FindAuthMethod(def, conn.Auth.MethodID, conn.Auth.Type)
		if !ok || (method.Type != AuthTypeNone && !ViewConnection(conn).TokenSet) {
			return nil, fmt.Errorf("%w: connection is not configured", ErrInvalidID)
		}
	}
	bindings, err := cfg.PatchPluginConnectionBindings(ctx, def.ID, update)
	if err != nil {
		return nil, err
	}
	return &ConnectionBindingsView{Bindings: bindings}, nil
}

// ResolveConnectionBinding never borrows an unselected connection.
func (s *Service) ResolveConnectionBinding(ctx context.Context, id, purpose string) (*Connection, error) {
	def, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	if !def.Enabled {
		return nil, ErrDisabled
	}
	if !declaredConnectionBinding(def, purpose) {
		return nil, ErrNotFound
	}
	view, err := s.ConnectionBindings(ctx, def.ID)
	if err != nil {
		return nil, err
	}
	selected := strings.TrimSpace(view.Bindings[purpose])
	if selected == "" {
		return nil, &EndpointResolveError{Reason: "connection_required", Endpoint: purpose}
	}
	conn, err := s.ReadyConnection(ctx, selected)
	if err != nil {
		return nil, err
	}
	if store.CurrentPluginID(conn.PluginID) != def.ID {
		return nil, ErrNotFound
	}
	if _, ok := FindAuthMethod(def, conn.Auth.MethodID, conn.Auth.Type); !ok {
		return nil, ErrNotFound
	}
	return conn, nil
}
