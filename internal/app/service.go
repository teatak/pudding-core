package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

type ConnectionSource interface {
	ListAppConnections(ctx context.Context) ([]*Connection, error)
	GetAppConnection(ctx context.Context, id string) (*Connection, error)
}

type GrantSource interface {
	ListSessionAppGrants(ctx context.Context, sessionID string) ([]*store.SessionAppGrant, error)
}

type Service struct {
	appsRoot    string
	connections ConnectionSource
	grants      GrantSource
}

func NewService(homeDir string, grants GrantSource, connections ConnectionSource) *Service {
	return &Service{
		appsRoot:    home.AppsPath(homeDir),
		connections: connections,
		grants:      grants,
	}
}

func (s *Service) ListDefinitions(ctx context.Context) ([]*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	defs, err := LoadUserDefinitions(s.appsRoot)
	if err != nil {
		return nil, err
	}
	out := make([]*Definition, 0, len(defs))
	for _, def := range defs {
		out = append(out, CloneDefinition(def))
	}
	return out, nil
}

func (s *Service) ResolveEndpoint(ctx context.Context, sessionID, endpointName string) (*EndpointBinding, error) {
	endpointName = strings.TrimSpace(endpointName)
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || s.grants == nil || s.connections == nil {
		return nil, errors.New("app service unavailable")
	}
	if sessionID == "" || endpointName == "" {
		return nil, errors.New("sessionID and endpoint are required")
	}
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	defByID := make(map[string]*Definition, len(defs))
	for _, def := range defs {
		defByID[def.ID] = def
	}
	grants, err := s.grants.ListSessionAppGrants(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var matches []*EndpointBinding
	for _, grant := range grants {
		if grant == nil || !grant.EndpointAllowed(endpointName) {
			continue
		}
		def := defByID[grant.AppID]
		if def == nil {
			continue
		}
		endpoint, ok := def.Endpoints[endpointName]
		if !ok {
			continue
		}
		conn, err := s.connections.GetAppConnection(ctx, grant.ConnectionID)
		if err != nil {
			return nil, err
		}
		if conn.AppID != grant.AppID {
			return nil, fmt.Errorf("connection %s belongs to app %s, not %s", conn.ID, conn.AppID, grant.AppID)
		}
		matches = append(matches, &EndpointBinding{
			AppID:        grant.AppID,
			ConnectionID: grant.ConnectionID,
			EndpointName: endpointName,
			Endpoint:     endpoint,
			Auth:         CloneAuth(conn.Auth),
		})
	}
	switch len(matches) {
	case 0:
		return nil, store.ErrNotFound
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("endpoint %q is ambiguous in session grants", endpointName)
	}
}
