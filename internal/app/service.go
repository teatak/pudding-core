package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/home"
)

var (
	ErrInvalidID = errors.New("app: invalid id")
	ErrNotFound  = errors.New("app: not found")
)

type ConnectionSource interface {
	ListAppConnections(ctx context.Context) ([]*Connection, error)
}

type ConnectionChoice struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	AppID string `json:"appID"`
}

type EndpointResolveError struct {
	Reason      string             `json:"reason"`
	Endpoint    string             `json:"endpoint,omitempty"`
	Connection  string             `json:"connection,omitempty"`
	Connections []ConnectionChoice `json:"connections,omitempty"`
}

func (e *EndpointResolveError) Error() string {
	if e == nil {
		return ""
	}
	switch e.Reason {
	case "connection_required":
		if len(e.Connections) > 1 {
			return fmt.Sprintf("multiple connections are available for endpoint %q; choose one by connection name", e.Endpoint)
		}
		return fmt.Sprintf("no connection is available for endpoint %q", e.Endpoint)
	case "connection_not_found":
		return fmt.Sprintf("connection %q is not available for endpoint %q", e.Connection, e.Endpoint)
	case "endpoint_ambiguous":
		return fmt.Sprintf("endpoint %q matches multiple app connections", e.Endpoint)
	default:
		return fmt.Sprintf("endpoint %q is not available", e.Endpoint)
	}
}

type Service struct {
	appsRoot    string
	connections ConnectionSource
}

func NewService(homeDir string, connections ConnectionSource) *Service {
	return &Service{
		appsRoot:    home.AppsPath(homeDir),
		connections: connections,
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

func (s *Service) InstallPackage(ctx context.Context, packageJSON []byte, expectedSHA256, sourceURL string) (*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	def, err := InstallPackage(s.appsRoot, packageJSON, expectedSHA256, sourceURL)
	if err != nil {
		return nil, err
	}
	return CloneDefinition(def), nil
}

func (s *Service) DeleteDefinition(ctx context.Context, id string) error {
	if s == nil {
		return errors.New("app service unavailable")
	}
	id = strings.TrimSpace(id)
	if !appIDPattern.MatchString(id) {
		return ErrInvalidID
	}
	target := filepath.Join(s.appsRoot, id)
	if _, err := os.Stat(filepath.Join(target, AppFileName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	return nil
}

func (s *Service) ReadAsset(ctx context.Context, rel string) ([]byte, string, error) {
	if s == nil {
		return nil, "", errors.New("app service unavailable")
	}
	return ReadAsset(s.appsRoot, rel)
}

func (s *Service) ReadSkill(ctx context.Context, appID, skillID string) (*SkillDetail, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	appID = strings.TrimSpace(appID)
	if !appIDPattern.MatchString(appID) {
		return nil, ErrInvalidID
	}
	def, err := LoadDefinitionDir(filepath.Join(s.appsRoot, appID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	selector := strings.TrimSpace(skillID)
	if selector == "" {
		return nil, ErrNotFound
	}
	cleanedPath := ""
	if strings.Contains(selector, "/") {
		cleaned, err := cleanRelativeSlashPath(selector)
		if err != nil {
			return nil, ErrNotFound
		}
		cleanedPath = cleaned
	}
	var ref *SkillRef
	for i := range def.Skills {
		item := &def.Skills[i]
		if selector == item.ID || selector == item.Name || (cleanedPath != "" && item.Path == cleanedPath) {
			ref = &def.Skills[i]
			break
		}
	}
	if ref == nil {
		return nil, ErrNotFound
	}
	data, err := os.ReadFile(filepath.Join(s.appsRoot, appID, filepath.FromSlash(ref.Path)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &SkillDetail{
		ID:          ref.ID,
		Name:        ref.Name,
		Description: ref.Description,
		Path:        ref.Path,
		Content:     string(data),
	}, nil
}

func (s *Service) ResolveEndpoint(ctx context.Context, sessionID, endpointName, connectionRef string) (*EndpointBinding, error) {
	endpointName = strings.TrimSpace(endpointName)
	connectionRef = strings.TrimSpace(connectionRef)
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || s.connections == nil {
		return nil, errors.New("app service unavailable")
	}
	if sessionID == "" || endpointName == "" {
		return nil, errors.New("sessionID and endpoint are required")
	}
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	var matches []*EndpointBinding
	connections, err := s.connections.ListAppConnections(ctx)
	if err != nil {
		return nil, err
	}
	allChoices := make([]ConnectionChoice, 0)
	endpointSeen := false
	for _, def := range defs {
		if def == nil {
			continue
		}
		endpoint, ok := def.Endpoints[endpointName]
		if !ok {
			continue
		}
		endpointSeen = true
		for _, conn := range connections {
			if conn == nil || conn.AppID != def.ID {
				continue
			}
			allChoices = append(allChoices, viewConnectionChoice(conn))
			if connectionRef != "" && !connectionMatches(conn, connectionRef) {
				continue
			}
			matches = append(matches, &EndpointBinding{
				AppID:               def.ID,
				ConnectionID:        conn.ID,
				EndpointName:        endpointName,
				Endpoint:            endpoint,
				Auth:                CloneAuth(conn.Auth),
				ConnectionFields:    cloneStringMap(conn.Fields),
				ConnectionFieldDefs: connectionFieldDefs(def.Connection),
			})
		}
	}
	switch len(matches) {
	case 0:
		if !endpointSeen {
			return nil, &EndpointResolveError{Reason: "endpoint_not_found", Endpoint: endpointName}
		}
		reason := "connection_required"
		if connectionRef != "" {
			reason = "connection_not_found"
		}
		if len(allChoices) == 0 {
			return nil, &EndpointResolveError{Reason: "connection_required", Endpoint: endpointName}
		}
		return nil, &EndpointResolveError{Reason: reason, Endpoint: endpointName, Connection: connectionRef, Connections: dedupeConnectionChoices(allChoices)}
	case 1:
		return matches[0], nil
	default:
		return nil, &EndpointResolveError{Reason: "connection_required", Endpoint: endpointName, Connection: connectionRef, Connections: dedupeConnectionChoices(allChoices)}
	}
}

func (s *Service) ListEndpointBindings(ctx context.Context, sessionID, kind string) ([]*EndpointBinding, error) {
	sessionID = strings.TrimSpace(sessionID)
	kind = strings.TrimSpace(kind)
	if s == nil || s.connections == nil {
		return nil, errors.New("app service unavailable")
	}
	if sessionID == "" {
		return nil, errors.New("sessionID is required")
	}
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	connections, err := s.connections.ListAppConnections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*EndpointBinding, 0)
	for _, def := range defs {
		if def == nil {
			continue
		}
		for endpointName, endpoint := range def.Endpoints {
			if kind != "" && endpoint.Kind != kind {
				continue
			}
			for _, conn := range connections {
				if conn == nil || conn.AppID != def.ID {
					continue
				}
				out = append(out, &EndpointBinding{
					AppID:               def.ID,
					ConnectionID:        conn.ID,
					EndpointName:        endpointName,
					Endpoint:            endpoint,
					Auth:                CloneAuth(conn.Auth),
					ConnectionFields:    cloneStringMap(conn.Fields),
					ConnectionFieldDefs: connectionFieldDefs(def.Connection),
				})
			}
		}
	}
	return out, nil
}

func connectionFieldDefs(config *ConnectionConfig) []ConnectionField {
	if config == nil || len(config.Fields) == 0 {
		return nil
	}
	return cloneConnectionFields(config.Fields)
}

func endpointExists(defs []*Definition, endpointName string) bool {
	for _, def := range defs {
		if def == nil {
			continue
		}
		if _, ok := def.Endpoints[endpointName]; ok {
			return true
		}
	}
	return false
}

func connectionMatches(conn *Connection, ref string) bool {
	if conn == nil {
		return false
	}
	ref = strings.TrimSpace(ref)
	return ref != "" && (strings.EqualFold(conn.ID, ref) || strings.EqualFold(conn.Name, ref))
}

func viewConnectionChoice(conn *Connection) ConnectionChoice {
	if conn == nil {
		return ConnectionChoice{}
	}
	return ConnectionChoice{ID: conn.ID, Name: strings.TrimSpace(conn.Name), AppID: conn.AppID}
}

func dedupeConnectionChoices(in []ConnectionChoice) []ConnectionChoice {
	seen := make(map[string]struct{}, len(in))
	out := make([]ConnectionChoice, 0, len(in))
	for _, item := range in {
		key := item.AppID + "/" + item.ID
		if key == "/" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
