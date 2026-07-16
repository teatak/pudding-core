package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/home"
)

var (
	ErrInvalidID          = errors.New("app: invalid id")
	ErrInvalidMCPOverride = errors.New("app: invalid mcp override")
	ErrNotFound           = errors.New("app: not found")
	ErrAlreadyExists      = errors.New("app: already exists")
	ErrBuiltinApp         = errors.New("app: builtin app cannot be uninstalled")
	ErrDisabled           = errors.New("app: disabled")
	ErrEnablementConfig   = errors.New("app: enablement config unavailable")
)

type ConnectionSource interface {
	ListAppConnections(ctx context.Context) ([]*Connection, error)
}

type EnablementSource interface {
	ListAppEnablement(ctx context.Context) (map[string]bool, error)
	SetAppEnabled(ctx context.Context, id string, enabled bool) error
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
	enablement  EnablementSource
	runtime     RuntimeSource
	packageMu   sync.RWMutex
}

func NewService(homeDir string, connections ConnectionSource) *Service {
	service := &Service{
		appsRoot:    home.AppsPath(homeDir),
		connections: connections,
	}
	service.enablement, _ = connections.(EnablementSource)
	return service
}

func (s *Service) WithRuntimeSource(source RuntimeSource) *Service {
	if s != nil {
		s.runtime = source
	}
	return s
}

func (s *Service) ListDefinitions(ctx context.Context) ([]*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	s.packageMu.RLock()
	defer s.packageMu.RUnlock()
	enabled := map[string]bool{}
	if s.enablement != nil {
		var err error
		enabled, err = s.enablement.ListAppEnablement(ctx)
		if err != nil {
			return nil, err
		}
	}
	defs, err := LoadUserDefinitions(s.appsRoot)
	if err != nil {
		return nil, err
	}
	builtins := BuiltinDefinitions()
	out := make([]*Definition, 0, len(builtins)+len(defs))
	seen := make(map[string]bool, len(builtins)+len(defs))
	for _, def := range builtins {
		applyEnabledOverride(def, enabled)
		out = append(out, def)
		seen[def.ID] = true
	}
	if s.runtime != nil {
		runtimeID := RuntimeIDFromContext(ctx)
		if runtimeID != "" {
			runtimeDefs, err := s.runtime.ListRuntimeDefinitions(ctx, runtimeID)
			if err != nil {
				return nil, err
			}
			for _, def := range runtimeDefs {
				resolved := decorateRuntimeDefinition(def)
				if resolved == nil || seen[resolved.ID] {
					continue
				}
				applyEnabledOverride(resolved, enabled)
				out = append(out, resolved)
				seen[resolved.ID] = true
			}
		}
	}
	for _, def := range defs {
		if IsReservedID(def.ID) || seen[def.ID] {
			continue
		}
		resolved := ResolveDefinitionPlatform(def)
		resolved, err = s.applyMCPOverrides(resolved)
		if err != nil {
			return nil, err
		}
		decorateInstalledDefinition(resolved)
		applyEnabledOverride(resolved, enabled)
		out = append(out, resolved)
		seen[resolved.ID] = true
	}
	return out, nil
}

func decorateRuntimeDefinition(def *Definition) *Definition {
	resolved := CloneDefinition(def)
	if resolved == nil {
		return nil
	}
	resolved.ID = strings.TrimSpace(resolved.ID)
	resolved.Name = strings.TrimSpace(resolved.Name)
	resolved.Kind = normalizedDefinitionKind(resolved.Kind)
	if !appIDPattern.MatchString(resolved.ID) || resolved.Name == "" {
		return nil
	}
	resolved.Source = SourceBuiltin
	resolved.Enabled = true
	resolved.CanUninstall = false
	resolved.Auth = nil
	resolved.Connection = nil
	resolved.Endpoints = nil
	resolved.Path = ""
	resolved.SourceURL = ""
	resolved.PackageSHA256 = ""
	if mode := strings.TrimSpace(resolved.RequiredMode); mode != "chat" && mode != "work" && mode != "code" {
		resolved.RequiredMode = "work"
	}
	return resolved
}

func decorateInstalledDefinition(def *Definition) {
	if def == nil {
		return
	}
	def.Source = SourceInstalled
	def.Kind = normalizedDefinitionKind(def.Kind)
	def.Enabled = true
	def.CanUninstall = true
	def.RequiredMode = "work"
	for _, endpoint := range def.Endpoints {
		if endpoint.Kind == EndpointKindMCP && endpoint.Transport == EndpointTransportStdio {
			def.RequiredMode = "code"
			break
		}
	}
	def.Tools = inferredEndpointTools(def.Endpoints)
	if len(def.Skills) > 0 {
		def.DefaultSkillID = def.Skills[0].ID
		if def.DefaultSkillID == "" {
			def.DefaultSkillID = def.Skills[0].Name
		}
		if def.DefaultSkillID == "" {
			def.DefaultSkillID = def.Skills[0].Path
		}
	}
}

func inferredEndpointTools(endpoints map[string]Endpoint) []ToolRef {
	hasREST := false
	hasGraphQL := false
	for _, endpoint := range endpoints {
		switch endpoint.Kind {
		case EndpointKindREST:
			hasREST = true
		case EndpointKindGraphQL:
			hasGraphQL = true
		}
	}
	tools := make([]ToolRef, 0, 4)
	if hasREST {
		tools = append(tools, ToolRef{Name: toolRESTRequest})
	}
	if hasGraphQL {
		tools = append(tools,
			ToolRef{Name: toolGraphQLRequest},
			ToolRef{Name: toolGraphQLIntrospect},
			ToolRef{Name: toolGraphQLSearch},
		)
	}
	return tools
}

func applyEnabledOverride(def *Definition, enabled map[string]bool) {
	if def == nil {
		return
	}
	if value, ok := enabled[def.ID]; ok {
		def.Enabled = value
	}
}

func (s *Service) definition(ctx context.Context, id string) (*Definition, error) {
	id = strings.TrimSpace(id)
	if !appIDPattern.MatchString(id) {
		return nil, ErrInvalidID
	}
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		if def != nil && def.ID == id {
			return def, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) (*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	def, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.enablement == nil {
		return nil, ErrEnablementConfig
	}
	if err := s.enablement.SetAppEnabled(ctx, def.ID, enabled); err != nil {
		return nil, err
	}
	def.Enabled = enabled
	return def, nil
}

func (s *Service) applyMCPOverrides(def *Definition) (*Definition, error) {
	if def == nil {
		return def, nil
	}
	overrides, err := LoadMCPOverrideFile(s.mcpOverrideFilePath(def.ID))
	if err != nil {
		return nil, err
	}
	if overrides == nil {
		return def, nil
	}
	out, err := ApplyMCPOverrides(def, overrides)
	if err != nil {
		return nil, fmt.Errorf("app %s: %w", def.ID, err)
	}
	return out, nil
}

func (s *Service) InstallPackage(ctx context.Context, packageJSON []byte, expectedSHA256, sourceURL string) (*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	return s.installPackageLocked(ctx, packageJSON, expectedSHA256, sourceURL)
}

func (s *Service) SaveAuthoredPackage(ctx context.Context, packageJSON []byte, update bool) (*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	var pkg Package
	if err := json.Unmarshal(packageJSON, &pkg); err != nil {
		return nil, fmt.Errorf("app package: parse: %w", err)
	}
	appID := strings.TrimSpace(pkg.App.ID)
	if !appIDPattern.MatchString(appID) {
		return nil, ErrInvalidID
	}
	if IsReservedID(appID) {
		return nil, ErrBuiltinApp
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	_, statErr := os.Lstat(filepath.Join(s.appsRoot, appID))
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if update && !exists {
		return nil, ErrNotFound
	}
	if !update && exists {
		return nil, ErrAlreadyExists
	}
	return s.installPackageLocked(ctx, packageJSON, "", "")
}

func (s *Service) installPackageLocked(ctx context.Context, packageJSON []byte, expectedSHA256, sourceURL string) (*Definition, error) {
	enabled := map[string]bool{}
	if s.enablement != nil {
		var err error
		enabled, err = s.enablement.ListAppEnablement(ctx)
		if err != nil {
			return nil, err
		}
	}
	def, err := InstallPackage(s.appsRoot, packageJSON, expectedSHA256, sourceURL)
	if err != nil {
		return nil, err
	}
	decorateInstalledDefinition(def)
	if s.enablement != nil {
		applyEnabledOverride(def, enabled)
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
	if IsReservedID(id) {
		return ErrBuiltinApp
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	root, err := resolveAppRoot(s.appsRoot, false)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	target := filepath.Join(root, id)
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

func (s *Service) GetMCPOverride(ctx context.Context, appID, endpointName string) (MCPEndpointOverride, bool, error) {
	_ = ctx
	if s == nil {
		return MCPEndpointOverride{}, false, errors.New("app service unavailable")
	}
	s.packageMu.RLock()
	defer s.packageMu.RUnlock()
	def, endpointName, err := s.resolveMCPOverrideTarget(appID, endpointName, nil)
	if err != nil {
		return MCPEndpointOverride{}, false, err
	}
	overrides, err := LoadMCPOverrideFile(s.mcpOverrideFilePath(def.ID))
	if err != nil {
		return MCPEndpointOverride{}, false, err
	}
	if overrides == nil {
		return MCPEndpointOverride{}, false, nil
	}
	override, ok := overrides.MCP[endpointName]
	return CloneMCPEndpointOverride(override), ok, nil
}

func (s *Service) PutMCPOverride(ctx context.Context, appID, endpointName string, override MCPEndpointOverride) (MCPEndpointOverride, error) {
	_ = ctx
	if s == nil {
		return MCPEndpointOverride{}, errors.New("app service unavailable")
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	def, endpointName, err := s.resolveMCPOverrideTarget(appID, endpointName, &override)
	if err != nil {
		return MCPEndpointOverride{}, err
	}
	path := s.mcpOverrideFilePath(def.ID)
	overrides, err := LoadMCPOverrideFile(path)
	if err != nil {
		return MCPEndpointOverride{}, err
	}
	if overrides == nil {
		overrides = &MCPOverrideFile{MCP: map[string]MCPEndpointOverride{}}
	}
	overrides.MCP[endpointName] = CloneMCPEndpointOverride(override)
	if err := WriteMCPOverrideFile(path, overrides); err != nil {
		return MCPEndpointOverride{}, err
	}
	return CloneMCPEndpointOverride(override), nil
}

func (s *Service) DeleteMCPOverride(ctx context.Context, appID, endpointName string) error {
	_ = ctx
	if s == nil {
		return errors.New("app service unavailable")
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	def, endpointName, err := s.resolveMCPOverrideTarget(appID, endpointName, nil)
	if err != nil {
		return err
	}
	path := s.mcpOverrideFilePath(def.ID)
	overrides, err := LoadMCPOverrideFile(path)
	if err != nil {
		return err
	}
	if overrides == nil {
		return nil
	}
	delete(overrides.MCP, endpointName)
	return WriteMCPOverrideFile(path, overrides)
}

func (s *Service) ReadAsset(ctx context.Context, rel string) ([]byte, string, error) {
	if s == nil {
		return nil, "", errors.New("app service unavailable")
	}
	s.packageMu.RLock()
	defer s.packageMu.RUnlock()
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
	def, err := s.definition(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !def.Enabled {
		return nil, ErrDisabled
	}
	return s.readSkill(ctx, appID, skillID)
}

func (s *Service) ReadSkillDetail(ctx context.Context, appID, skillID string) (*SkillDetail, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	appID = strings.TrimSpace(appID)
	if !appIDPattern.MatchString(appID) {
		return nil, ErrInvalidID
	}
	if _, err := s.definition(ctx, appID); err != nil {
		return nil, err
	}
	return s.readSkill(ctx, appID, skillID)
}

func (s *Service) readSkill(ctx context.Context, appID, skillID string) (*SkillDetail, error) {
	if IsBuiltinID(appID) {
		detail, ok := ReadBuiltinSkill(appID, skillID)
		if !ok {
			return nil, ErrNotFound
		}
		return detail, nil
	}
	if s.runtime != nil {
		runtimeID := RuntimeIDFromContext(ctx)
		if runtimeID != "" {
			detail, err := s.runtime.ReadRuntimeSkill(ctx, runtimeID, appID, skillID)
			if err == nil {
				return detail, nil
			}
			if !errors.Is(err, ErrNotFound) {
				return nil, err
			}
		}
	}
	s.packageMu.RLock()
	defer s.packageMu.RUnlock()
	root, err := resolveAppRoot(s.appsRoot, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	appDir := filepath.Join(root, appID)
	diskDef, err := LoadDefinitionDir(appDir)
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
	for i := range diskDef.Skills {
		item := &diskDef.Skills[i]
		if selector == item.ID || selector == item.Name || (cleanedPath != "" && item.Path == cleanedPath) {
			ref = &diskDef.Skills[i]
			break
		}
	}
	if ref == nil {
		return nil, ErrNotFound
	}
	resolvedPath, err := resolveAppRegularFile(appDir, ref.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	data, err := os.ReadFile(resolvedPath)
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

func (s *Service) resolveMCPOverrideTarget(appID, endpointName string, override *MCPEndpointOverride) (*Definition, string, error) {
	appID = strings.TrimSpace(appID)
	endpointName = strings.TrimSpace(endpointName)
	if !appIDPattern.MatchString(appID) {
		return nil, "", ErrInvalidID
	}
	if !endpointNamePattern.MatchString(endpointName) {
		return nil, "", ErrNotFound
	}
	root, err := resolveAppRoot(s.appsRoot, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	def, err := LoadDefinitionDir(filepath.Join(root, appID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	resolved := ResolveDefinitionPlatform(def)
	endpoint, ok := resolved.Endpoints[endpointName]
	if !ok || endpoint.Kind != EndpointKindMCP {
		return nil, "", ErrNotFound
	}
	if override != nil {
		override.Transport = strings.TrimSpace(override.Transport)
		override.URL = strings.TrimSpace(override.URL)
		override.Command = strings.TrimSpace(override.Command)
		if err := validateMCPEndpointOverride(*override); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidMCPOverride, err)
		}
		merged := applyMCPEndpointOverride(endpoint, *override)
		if err := ValidateEndpoint(merged); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidMCPOverride, err)
		}
	}
	return resolved, endpointName, nil
}

func (s *Service) mcpOverrideFilePath(appID string) string {
	return filepath.Join(s.appsRoot, appID, MCPOverrideFileName)
}

func (s *Service) ResolveEndpoint(ctx context.Context, sessionID, endpointName, connectionRef string) (*EndpointBinding, error) {
	endpointName = strings.TrimSpace(endpointName)
	connectionRef = strings.TrimSpace(connectionRef)
	sessionID = strings.TrimSpace(sessionID)
	if s == nil {
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
	var connections []*Connection
	if s.connections != nil {
		connections, err = s.connections.ListAppConnections(ctx)
		if err != nil {
			return nil, err
		}
	}
	allChoices := make([]ConnectionChoice, 0)
	endpointSeen := false
	for _, def := range defs {
		if def == nil || !def.Enabled {
			continue
		}
		endpoint, ok := def.Endpoints[endpointName]
		if !ok {
			continue
		}
		endpointSeen = true
		if connectionRef == "" {
			if binding, ok := connectionlessEndpointBinding(def, endpointName, endpoint); ok {
				matches = append(matches, binding)
				continue
			}
		}
		for _, conn := range connections {
			if conn == nil || conn.AppID != def.ID {
				continue
			}
			allChoices = append(allChoices, viewConnectionChoice(conn))
			if connectionRef != "" && !connectionMatches(conn, connectionRef) {
				continue
			}
			matches = append(matches, endpointBindingForConnection(def, endpointName, endpoint, conn))
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
		if len(allChoices) == 0 {
			return nil, &EndpointResolveError{Reason: "endpoint_ambiguous", Endpoint: endpointName}
		}
		return nil, &EndpointResolveError{Reason: "connection_required", Endpoint: endpointName, Connection: connectionRef, Connections: dedupeConnectionChoices(allChoices)}
	}
}

func (s *Service) ListEndpointBindings(ctx context.Context, kind string) ([]*EndpointBinding, error) {
	kind = strings.TrimSpace(kind)
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	var connections []*Connection
	if s.connections != nil {
		connections, err = s.connections.ListAppConnections(ctx)
		if err != nil {
			return nil, err
		}
	}
	out := make([]*EndpointBinding, 0)
	for _, def := range defs {
		if def == nil || !def.Enabled {
			continue
		}
		for endpointName, endpoint := range def.Endpoints {
			if kind != "" && endpoint.Kind != kind {
				continue
			}
			appConnections := connectionsForApp(connections, def.ID)
			if len(appConnections) == 0 {
				if binding, ok := connectionlessEndpointBinding(def, endpointName, endpoint); ok {
					out = append(out, binding)
				}
				continue
			}
			for _, conn := range appConnections {
				out = append(out, endpointBindingForConnection(def, endpointName, endpoint, conn))
			}
		}
	}
	return out, nil
}

func endpointBindingForConnection(def *Definition, endpointName string, endpoint Endpoint, conn *Connection) *EndpointBinding {
	if def == nil || conn == nil {
		return nil
	}
	return &EndpointBinding{
		AppID:               def.ID,
		ConnectionID:        conn.ID,
		EndpointName:        endpointName,
		Endpoint:            ResolveEndpointPlatform(endpoint),
		Auth:                CloneAuth(conn.Auth),
		ConnectionFields:    cloneStringMap(conn.Fields),
		ConnectionFieldDefs: connectionFieldDefs(def.Connection),
	}
}

func connectionlessEndpointBinding(def *Definition, endpointName string, endpoint Endpoint) (*EndpointBinding, bool) {
	if !allowsConnectionlessEndpoint(def) {
		return nil, false
	}
	return &EndpointBinding{
		AppID:        def.ID,
		EndpointName: endpointName,
		Endpoint:     ResolveEndpointPlatform(endpoint),
	}, true
}

func allowsConnectionlessEndpoint(def *Definition) bool {
	if def == nil || hasRequiredConnectionFields(def.Connection) {
		return false
	}
	// A simplified MCP App carries its complete server configuration in the
	// endpoint and never requires a separate App connection.
	if def.Kind == KindMCP {
		return true
	}
	return def.Auth != nil && !def.Auth.Required
}

func hasRequiredConnectionFields(config *ConnectionConfig) bool {
	if config == nil {
		return false
	}
	for _, field := range config.Fields {
		if field.Required {
			return true
		}
	}
	return false
}

func connectionsForApp(connections []*Connection, appID string) []*Connection {
	out := make([]*Connection, 0)
	for _, conn := range connections {
		if conn == nil || conn.AppID != appID {
			continue
		}
		out = append(out, conn)
	}
	return out
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
