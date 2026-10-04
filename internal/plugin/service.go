package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/oauthbroker"
)

var (
	ErrInvalidID                         = errors.New("plugin: invalid id")
	ErrInvalidMCPOverride                = errors.New("plugin: invalid mcp override")
	ErrNotFound                          = errors.New("plugin: not found")
	ErrAlreadyExists                     = errors.New("plugin: already exists")
	ErrBuiltinPlugin                     = errors.New("plugin: builtin plugin cannot be uninstalled")
	ErrDisabled                          = errors.New("plugin: disabled")
	ErrEnablementConfig                  = errors.New("plugin: enablement config unavailable")
	ErrConnectionReauthorizationRequired = errors.New("plugin: connection reauthorization required")
)

type ConnectionSource interface {
	ListPluginConnections(ctx context.Context) ([]*Connection, error)
}

type ConnectionStore interface {
	ConnectionSource
	GetPluginConnection(ctx context.Context, id string) (*Connection, error)
	PutPluginConnection(ctx context.Context, connection *Connection) error
}

type EnablementSource interface {
	ListPluginEnablement(ctx context.Context) (map[string]bool, error)
	SetPluginEnabled(ctx context.Context, id string, enabled bool) error
}

type ConnectionChoice struct {
	ID           string             `json:"id"`
	Name         string             `json:"name,omitempty"`
	PluginID     string             `json:"pluginID"`
	AuthType     string             `json:"authType,omitempty"`
	AuthMethodID string             `json:"authMethodID,omitempty"`
	AuthVariant  string             `json:"authVariant,omitempty"`
	Account      *ConnectionAccount `json:"account,omitempty"`
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
			return fmt.Sprintf("multiple connections are available for endpoint %q; choose one by connection name and account identity, or ask the user when the intended connection is ambiguous", e.Endpoint)
		}
		return fmt.Sprintf("no connection is available for endpoint %q", e.Endpoint)
	case "connection_not_found":
		return fmt.Sprintf("connection %q is not available for endpoint %q", e.Connection, e.Endpoint)
	case "endpoint_ambiguous":
		return fmt.Sprintf("endpoint %q matches multiple plugin connections", e.Endpoint)
	default:
		return fmt.Sprintf("endpoint %q is not available", e.Endpoint)
	}
}

type Service struct {
	pluginsRoot          string
	connections          ConnectionSource
	enablement           EnablementSource
	runtime              RuntimeSource
	builtinToolAvailable func(string) bool
	packageMu            sync.RWMutex
	authMu               sync.Mutex
	connectionStore      ConnectionStore
	oauthBroker          *oauthbroker.Client
}

func NewService(homeDir string, connections ConnectionSource) *Service {
	service := &Service{
		pluginsRoot: home.PluginsPath(homeDir),
		connections: connections,
		oauthBroker: oauthbroker.New("", nil),
	}
	service.connectionStore, _ = connections.(ConnectionStore)
	service.enablement, _ = connections.(EnablementSource)
	return service
}

func (s *Service) WithOAuthBroker(client *oauthbroker.Client) *Service {
	if s != nil && client != nil {
		s.oauthBroker = client
	}
	return s
}

func (s *Service) WithRuntimeSource(source RuntimeSource) *Service {
	if s != nil {
		s.runtime = source
	}
	return s
}

// WithBuiltinToolAvailability connects the catalog to the actual runner. It does
// not persist capability flags or change the user's enablement preferences.
func (s *Service) WithBuiltinToolAvailability(available func(string) bool) *Service {
	s.builtinToolAvailable = available
	return s
}

func (s *Service) ListDefinitions(ctx context.Context) ([]*Definition, error) {
	if s == nil {
		return nil, errors.New("plugin service unavailable")
	}
	s.packageMu.RLock()
	defer s.packageMu.RUnlock()
	enabled := map[string]bool{}
	if s.enablement != nil {
		var err error
		enabled, err = s.enablement.ListPluginEnablement(ctx)
		if err != nil {
			return nil, err
		}
	}
	defs, err := LoadUserDefinitions(s.pluginsRoot)
	if err != nil {
		return nil, err
	}
	builtins := BuiltinDefinitions()
	out := make([]*Definition, 0, len(builtins)+len(defs))
	seen := make(map[string]bool, len(builtins)+len(defs))
	for _, def := range builtins {
		if s.builtinToolAvailable != nil && len(def.Tools) > 0 {
			tools := def.Tools[:0]
			for _, item := range def.Tools {
				if s.builtinToolAvailable(item.Name) {
					tools = append(tools, item)
				}
			}
			def.Tools = tools
			if len(def.Tools) == 0 {
				continue
			}
		}
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
	if !pluginIDPattern.MatchString(resolved.ID) || resolved.Name == "" {
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
	if !pluginIDPattern.MatchString(id) {
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
		return nil, errors.New("plugin service unavailable")
	}
	def, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.enablement == nil {
		return nil, ErrEnablementConfig
	}
	if err := s.enablement.SetPluginEnabled(ctx, def.ID, enabled); err != nil {
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
		return nil, fmt.Errorf("plugin %s: %w", def.ID, err)
	}
	return out, nil
}

func (s *Service) InstallPackage(ctx context.Context, packageJSON []byte, expectedSHA256, sourceURL string) (*Definition, error) {
	if s == nil {
		return nil, errors.New("plugin service unavailable")
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	return s.installPackageLocked(ctx, packageJSON, expectedSHA256, sourceURL)
}

func (s *Service) SaveAuthoredPackage(ctx context.Context, packageJSON []byte, update bool) (*Definition, error) {
	if s == nil {
		return nil, errors.New("plugin service unavailable")
	}
	var pkg Package
	if err := json.Unmarshal(packageJSON, &pkg); err != nil {
		return nil, fmt.Errorf("plugin package: parse: %w", err)
	}
	pluginID := strings.TrimSpace(pkg.Plugin.ID)
	if !pluginIDPattern.MatchString(pluginID) {
		return nil, ErrInvalidID
	}
	if IsReservedID(pluginID) {
		return nil, ErrBuiltinPlugin
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	_, statErr := os.Lstat(filepath.Join(s.pluginsRoot, pluginID))
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
		enabled, err = s.enablement.ListPluginEnablement(ctx)
		if err != nil {
			return nil, err
		}
	}
	def, err := InstallPackage(s.pluginsRoot, packageJSON, expectedSHA256, sourceURL)
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
		return errors.New("plugin service unavailable")
	}
	id = strings.TrimSpace(id)
	if !pluginIDPattern.MatchString(id) {
		return ErrInvalidID
	}
	if IsReservedID(id) {
		return ErrBuiltinPlugin
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	root, err := resolvePluginRoot(s.pluginsRoot, false)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	target := filepath.Join(root, id)
	if _, err := os.Stat(filepath.Join(target, PluginFileName)); err != nil {
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

func (s *Service) GetMCPOverride(ctx context.Context, pluginID, endpointName string) (MCPEndpointOverride, bool, error) {
	_ = ctx
	if s == nil {
		return MCPEndpointOverride{}, false, errors.New("plugin service unavailable")
	}
	s.packageMu.RLock()
	defer s.packageMu.RUnlock()
	def, endpointName, err := s.resolveMCPOverrideTarget(pluginID, endpointName, nil)
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

func (s *Service) PutMCPOverride(ctx context.Context, pluginID, endpointName string, override MCPEndpointOverride) (MCPEndpointOverride, error) {
	_ = ctx
	if s == nil {
		return MCPEndpointOverride{}, errors.New("plugin service unavailable")
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	def, endpointName, err := s.resolveMCPOverrideTarget(pluginID, endpointName, &override)
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

func (s *Service) DeleteMCPOverride(ctx context.Context, pluginID, endpointName string) error {
	_ = ctx
	if s == nil {
		return errors.New("plugin service unavailable")
	}
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	def, endpointName, err := s.resolveMCPOverrideTarget(pluginID, endpointName, nil)
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
		return nil, "", errors.New("plugin service unavailable")
	}
	s.packageMu.RLock()
	defer s.packageMu.RUnlock()
	return ReadAsset(s.pluginsRoot, rel)
}

func (s *Service) ReadSkill(ctx context.Context, pluginID, skillID string) (*SkillDetail, error) {
	if s == nil {
		return nil, errors.New("plugin service unavailable")
	}
	pluginID = strings.TrimSpace(pluginID)
	if !pluginIDPattern.MatchString(pluginID) {
		return nil, ErrInvalidID
	}
	def, err := s.definition(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	if !def.Enabled {
		return nil, ErrDisabled
	}
	return s.readSkill(ctx, pluginID, skillID)
}

func (s *Service) ReadSkillDetail(ctx context.Context, pluginID, skillID string) (*SkillDetail, error) {
	if s == nil {
		return nil, errors.New("plugin service unavailable")
	}
	pluginID = strings.TrimSpace(pluginID)
	if !pluginIDPattern.MatchString(pluginID) {
		return nil, ErrInvalidID
	}
	if _, err := s.definition(ctx, pluginID); err != nil {
		return nil, err
	}
	return s.readSkill(ctx, pluginID, skillID)
}

func (s *Service) readSkill(ctx context.Context, pluginID, skillID string) (*SkillDetail, error) {
	if IsBuiltinID(pluginID) {
		detail, ok := ReadBuiltinSkill(pluginID, skillID)
		if !ok {
			return nil, ErrNotFound
		}
		return detail, nil
	}
	if s.runtime != nil {
		runtimeID := RuntimeIDFromContext(ctx)
		if runtimeID != "" {
			detail, err := s.runtime.ReadRuntimeSkill(ctx, runtimeID, pluginID, skillID)
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
	root, err := resolvePluginRoot(s.pluginsRoot, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	pluginDir := filepath.Join(root, pluginID)
	diskDef, err := LoadDefinitionDir(pluginDir)
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
	resolvedPath, err := resolvePluginRegularFile(pluginDir, ref.Path)
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

func (s *Service) resolveMCPOverrideTarget(pluginID, endpointName string, override *MCPEndpointOverride) (*Definition, string, error) {
	pluginID = strings.TrimSpace(pluginID)
	endpointName = strings.TrimSpace(endpointName)
	if !pluginIDPattern.MatchString(pluginID) {
		return nil, "", ErrInvalidID
	}
	if !endpointNamePattern.MatchString(endpointName) {
		return nil, "", ErrNotFound
	}
	root, err := resolvePluginRoot(s.pluginsRoot, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	def, err := LoadDefinitionDir(filepath.Join(root, pluginID))
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

func (s *Service) mcpOverrideFilePath(pluginID string) string {
	return filepath.Join(s.pluginsRoot, pluginID, MCPOverrideFileName)
}

func (s *Service) ResolveEndpoint(ctx context.Context, sessionID, endpointName, connectionRef string) (*EndpointBinding, error) {
	endpointName = strings.TrimSpace(endpointName)
	connectionRef = strings.TrimSpace(connectionRef)
	sessionID = strings.TrimSpace(sessionID)
	if s == nil {
		return nil, errors.New("plugin service unavailable")
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
		connections, err = s.connections.ListPluginConnections(ctx)
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
			if conn == nil || conn.PluginID != def.ID {
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
		return s.readyEndpointBinding(ctx, matches[0])
	default:
		if len(allChoices) == 0 {
			return nil, &EndpointResolveError{Reason: "endpoint_ambiguous", Endpoint: endpointName}
		}
		return nil, &EndpointResolveError{Reason: "connection_required", Endpoint: endpointName, Connection: connectionRef, Connections: dedupeConnectionChoices(allChoices)}
	}
}

func (s *Service) ReadyConnection(ctx context.Context, id string) (*Connection, error) {
	if s == nil || s.connectionStore == nil {
		return nil, errors.New("plugin connection store unavailable")
	}
	connection, err := s.connectionStore.GetPluginConnection(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return s.refreshConnectionIfNeeded(ctx, connection)
}

// ResolveBoundEndpoint resolves an exact resource binding, without borrowing a
// session's loaded plugins or choosing another account by display name.
func (s *Service) ResolveBoundEndpoint(ctx context.Context, pluginID, endpointName, connectionID string) (*EndpointBinding, string, error) {
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, def := range defs {
		if def == nil || def.ID != pluginID || !def.Enabled {
			continue
		}
		endpoint, ok := def.Endpoints[endpointName]
		if !ok || (endpoint.Kind != EndpointKindREST && endpoint.Kind != EndpointKindGraphQL) {
			return nil, "", ErrNotFound
		}
		if connectionID == "" {
			binding, ok := connectionlessEndpointBinding(def, endpointName, endpoint)
			if !ok {
				return nil, "", errors.New("connection required")
			}
			revision, _ := json.Marshal(struct {
				Definition *Definition
				Endpoint   Endpoint
			}{def, binding.Endpoint})
			return binding, string(revision), nil
		}
		connection, err := s.ReadyConnection(ctx, connectionID)
		if err != nil {
			return nil, "", err
		}
		if connection.PluginID != pluginID || connection.ID != connectionID {
			return nil, "", errors.New("connection does not belong to plugin")
		}
		binding := endpointBindingForConnection(def, endpointName, endpoint, connection)
		// Connection timestamps change on edits and authorization refresh.
		// The fingerprint invalidates prepared widget actions when that happens.
		revision, _ := json.Marshal(struct {
			Definition *Definition
			Endpoint   Endpoint
			Connection ConnectionView
		}{def, binding.Endpoint, ViewConnection(connection)})
		return binding, string(revision), nil
	}
	return nil, "", ErrNotFound
}

func (s *Service) readyEndpointBinding(ctx context.Context, binding *EndpointBinding) (*EndpointBinding, error) {
	if binding == nil || binding.ConnectionID == "" || s.connectionStore == nil {
		return binding, nil
	}
	connection, err := s.ReadyConnection(ctx, binding.ConnectionID)
	if err != nil {
		return nil, err
	}
	out := *binding
	out.Auth = CloneAuth(connection.Auth)
	out.ConnectionFields = cloneStringMap(connection.Fields)
	return &out, nil
}

func (s *Service) refreshConnectionIfNeeded(ctx context.Context, connection *Connection) (*Connection, error) {
	if connection == nil || connection.PluginID != "github" || connection.Auth.Type != AuthTypeOAuth2 {
		return CloneConnection(connection), nil
	}
	if githubAppReauthorizationRequired(connection) {
		return nil, ErrConnectionReauthorizationRequired
	}
	if connection.Auth.ExpiresAt.IsZero() || time.Now().Add(2*time.Minute).Before(connection.Auth.ExpiresAt) {
		return CloneConnection(connection), nil
	}
	if strings.TrimSpace(connection.Auth.RefreshToken) == "" {
		return nil, ErrConnectionReauthorizationRequired
	}

	s.authMu.Lock()
	defer s.authMu.Unlock()
	current, err := s.connectionStore.GetPluginConnection(ctx, connection.ID)
	if err != nil {
		return nil, err
	}
	if githubAppReauthorizationRequired(current) {
		return nil, ErrConnectionReauthorizationRequired
	}
	if current.Auth.ExpiresAt.IsZero() || time.Now().Add(2*time.Minute).Before(current.Auth.ExpiresAt) {
		return current, nil
	}
	token, err := s.oauthBroker.Refresh(ctx, "github", current.Auth.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh github connection %q: %w", current.ID, err)
	}
	current.Auth.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		current.Auth.RefreshToken = token.RefreshToken
	}
	current.Auth.TokenType = token.TokenType
	current.Auth.ExpiresAt = expiryFromDuration(token.ExpiresIn)
	current.Auth.RefreshExpiresAt = expiryFromDuration(token.RefreshTokenExpiresIn)
	if err := s.connectionStore.PutPluginConnection(ctx, current); err != nil {
		return nil, err
	}
	return s.connectionStore.GetPluginConnection(ctx, current.ID)
}

func expiryFromDuration(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

func (s *Service) ListEndpointBindings(ctx context.Context, kind string) ([]*EndpointBinding, error) {
	kind = strings.TrimSpace(kind)
	if s == nil {
		return nil, errors.New("plugin service unavailable")
	}
	defs, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	var connections []*Connection
	if s.connections != nil {
		connections, err = s.connections.ListPluginConnections(ctx)
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
			pluginConnections := connectionsForPlugin(connections, def.ID)
			if len(pluginConnections) == 0 {
				if binding, ok := connectionlessEndpointBinding(def, endpointName, endpoint); ok {
					out = append(out, binding)
				}
				continue
			}
			for _, conn := range pluginConnections {
				ready, err := s.refreshConnectionIfNeeded(ctx, conn)
				if err != nil {
					if errors.Is(err, ErrConnectionReauthorizationRequired) {
						continue
					}
					return nil, err
				}
				out = append(out, endpointBindingForConnection(def, endpointName, endpoint, ready))
			}
		}
	}
	return out, nil
}

func endpointBindingForConnection(def *Definition, endpointName string, endpoint Endpoint, conn *Connection) *EndpointBinding {
	if def == nil || conn == nil {
		return nil
	}
	resolvedEndpoint := ResolveEndpointPlatform(endpoint)
	if endpointURL := strings.TrimSpace(conn.EndpointURLs[endpointName]); endpointURL != "" &&
		(resolvedEndpoint.Kind == EndpointKindREST || resolvedEndpoint.Kind == EndpointKindGraphQL) {
		resolvedEndpoint.URL = endpointURL
	}
	authMethod, _ := FindAuthMethod(def, conn.Auth.MethodID, conn.Auth.Type)
	return &EndpointBinding{
		PluginID:            def.ID,
		ConnectionID:        conn.ID,
		EndpointName:        endpointName,
		Endpoint:            resolvedEndpoint,
		Auth:                CloneAuth(conn.Auth),
		AuthMethod:          CloneAuthMethod(authMethod),
		ConnectionFields:    cloneStringMap(conn.Fields),
		ConnectionFieldDefs: connectionFieldDefs(def.Connection),
	}
}

func connectionlessEndpointBinding(def *Definition, endpointName string, endpoint Endpoint) (*EndpointBinding, bool) {
	if !allowsConnectionlessEndpoint(def) {
		return nil, false
	}
	return &EndpointBinding{
		PluginID:     def.ID,
		EndpointName: endpointName,
		Endpoint:     ResolveEndpointPlatform(endpoint),
	}, true
}

func allowsConnectionlessEndpoint(def *Definition) bool {
	if def == nil || hasRequiredConnectionFields(def.Connection) {
		return false
	}
	// A simplified MCP plugin carries its complete server configuration in the
	// endpoint and never requires a separate plugin connection.
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

func connectionsForPlugin(connections []*Connection, pluginID string) []*Connection {
	out := make([]*Connection, 0)
	for _, conn := range connections {
		if conn == nil || conn.PluginID != pluginID {
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
	return ConnectionChoice{
		ID:           conn.ID,
		Name:         strings.TrimSpace(conn.Name),
		PluginID:     conn.PluginID,
		AuthType:     conn.Auth.Type,
		AuthMethodID: conn.Auth.MethodID,
		AuthVariant:  conn.Auth.Variant,
		Account:      cloneConnectionAccount(conn.Account),
	}
}

func dedupeConnectionChoices(in []ConnectionChoice) []ConnectionChoice {
	seen := make(map[string]struct{}, len(in))
	out := make([]ConnectionChoice, 0, len(in))
	for _, item := range in {
		key := item.PluginID + "/" + item.ID
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
