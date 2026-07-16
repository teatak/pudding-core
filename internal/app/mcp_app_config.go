package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const mcpAppEndpointName = "mcp"

var ErrInvalidMCPAppConfig = errors.New("app: invalid mcp app config")

type MCPServerConfig struct {
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Type        string            `json:"type,omitempty"`
	Transport   string            `json:"transport,omitempty"`
	Description string            `json:"description,omitempty"`
}

type mcpServersConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

type mcpConfigEntry struct {
	Name        string
	Description string
	Endpoint    Endpoint
}

// ImportMCPApps imports each mcpServers entry as a regular installed App. An
// existing MCP App with the same generated identity is updated in place.
func (s *Service) ImportMCPApps(ctx context.Context, configJSON []byte, displayName string) ([]*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	entries, err := parseMCPServersConfig(configJSON)
	if err != nil {
		return nil, err
	}
	if err := applyMCPAppDisplayName(entries, displayName); err != nil {
		return nil, err
	}
	definitions, err := s.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	occupied := make(map[string]*Definition, len(definitions)+len(entries))
	for _, def := range definitions {
		if def != nil {
			occupied[def.ID] = def
		}
	}

	type importTarget struct {
		entry  mcpConfigEntry
		id     string
		update bool
	}
	targets := make([]importTarget, 0, len(entries))
	for _, entry := range entries {
		id, update := availableMCPAppID(entry.Name, occupied)
		targets = append(targets, importTarget{entry: entry, id: id, update: update})
		occupied[id] = &Definition{Kind: KindMCP, ID: id, Name: entry.Name}
	}

	result := make([]*Definition, 0, len(targets))
	for _, target := range targets {
		def, err := s.saveMCPApp(ctx, target.id, target.entry, "1.0.0", target.update)
		if err != nil {
			return nil, err
		}
		result = append(result, def)
	}
	return result, nil
}

func (s *Service) GetMCPAppConfig(ctx context.Context, id string) ([]byte, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	def, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	entry, err := mcpConfigEntryFromDefinition(def)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(mcpServersConfig{MCPServers: map[string]MCPServerConfig{
		entry.Name: mcpServerConfigFromEndpoint(entry.Description, entry.Endpoint),
	}}, "", "  ")
}

func (s *Service) UpdateMCPApp(ctx context.Context, id string, configJSON []byte, displayName string) (*Definition, error) {
	if s == nil {
		return nil, errors.New("app service unavailable")
	}
	def, err := s.definition(ctx, id)
	if err != nil {
		return nil, err
	}
	if def.Source != SourceInstalled || def.Kind != KindMCP {
		return nil, ErrNotFound
	}
	entries, err := parseMCPServersConfig(configJSON)
	if err != nil {
		return nil, err
	}
	if len(entries) != 1 {
		return nil, fmt.Errorf("%w: editing an MCP App requires exactly one mcpServers entry", ErrInvalidMCPAppConfig)
	}
	if err := applyMCPAppDisplayName(entries, displayName); err != nil {
		return nil, err
	}
	version := strings.TrimSpace(def.Version)
	if version == "" {
		version = "1.0.0"
	}
	return s.saveMCPApp(ctx, def.ID, entries[0], version, true)
}

func applyMCPAppDisplayName(entries []mcpConfigEntry, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil
	}
	if len(entries) != 1 {
		return fmt.Errorf("%w: a custom name requires exactly one mcpServers entry", ErrInvalidMCPAppConfig)
	}
	if err := validateMCPAppName(displayName); err != nil {
		return err
	}
	entries[0].Name = displayName
	return nil
}

func (s *Service) saveMCPApp(ctx context.Context, id string, entry mcpConfigEntry, version string, update bool) (*Definition, error) {
	manifestEndpoint := CloneEndpoint(entry.Endpoint)
	manifestEndpoint.Env = nil
	manifestEndpoint.Headers = nil
	manifest := struct {
		Kind        string              `yaml:"kind"`
		ID          string              `yaml:"id"`
		Name        string              `yaml:"name"`
		Version     string              `yaml:"version"`
		Description string              `yaml:"description,omitempty"`
		Endpoints   map[string]Endpoint `yaml:"endpoints"`
	}{
		Kind:        KindMCP,
		ID:          id,
		Name:        entry.Name,
		Version:     version,
		Description: entry.Description,
		Endpoints:   map[string]Endpoint{mcpAppEndpointName: manifestEndpoint},
	}
	manifestYAML, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	pkgJSON, err := json.Marshal(Package{
		Kind:          AppPackageKind,
		SchemaVersion: AppPackageSchemaVersion,
		App: PackageApp{
			ID:          id,
			Name:        entry.Name,
			Version:     version,
			Description: entry.Description,
		},
		Files: []PackageFile{{Path: AppFileName, Content: string(manifestYAML)}},
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.SaveAuthoredPackage(ctx, pkgJSON, update); err != nil {
		return nil, err
	}
	override := MCPEndpointOverride{
		Env:     cloneStringMap(entry.Endpoint.Env),
		Headers: cloneStringMap(entry.Endpoint.Headers),
	}
	if len(override.Env) == 0 && len(override.Headers) == 0 {
		if err := s.DeleteMCPOverride(ctx, id, mcpAppEndpointName); err != nil {
			return nil, err
		}
	} else if _, err := s.PutMCPOverride(ctx, id, mcpAppEndpointName, override); err != nil {
		return nil, err
	}
	return s.definition(ctx, id)
}

func parseMCPServersConfig(data []byte) ([]mcpConfigEntry, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("%w: JSON is required", ErrInvalidMCPAppConfig)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMCPAppConfig, err)
	}
	rawServers, ok := root["mcpServers"]
	if !ok {
		return nil, fmt.Errorf("%w: mcpServers is required", ErrInvalidMCPAppConfig)
	}
	var servers map[string]MCPServerConfig
	if err := json.Unmarshal(rawServers, &servers); err != nil {
		return nil, fmt.Errorf("%w: mcpServers: %v", ErrInvalidMCPAppConfig, err)
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("%w: mcpServers cannot be empty", ErrInvalidMCPAppConfig)
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]mcpConfigEntry, 0, len(names))
	seenNames := make(map[string]struct{}, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if err := validateMCPAppName(name); err != nil {
			return nil, err
		}
		nameKey := strings.ToLower(name)
		if _, exists := seenNames[nameKey]; exists {
			return nil, fmt.Errorf("%w: duplicate server name %q", ErrInvalidMCPAppConfig, name)
		}
		seenNames[nameKey] = struct{}{}
		server := servers[rawName]
		endpoint, err := endpointFromMCPServerConfig(server)
		if err != nil {
			return nil, fmt.Errorf("%w: server %q: %v", ErrInvalidMCPAppConfig, name, err)
		}
		entries = append(entries, mcpConfigEntry{
			Name:        name,
			Description: strings.TrimSpace(server.Description),
			Endpoint:    endpoint,
		})
	}
	return entries, nil
}

func validateMCPAppName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: server name is required", ErrInvalidMCPAppConfig)
	}
	if utf8.RuneCountInString(name) > 128 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: server name %q is invalid", ErrInvalidMCPAppConfig, name)
	}
	return nil
}

func endpointFromMCPServerConfig(server MCPServerConfig) (Endpoint, error) {
	command := strings.TrimSpace(server.Command)
	url := strings.TrimSpace(server.URL)
	if (command == "") == (url == "") {
		return Endpoint{}, errors.New("provide exactly one of command or url")
	}
	transportHint := strings.ToLower(strings.TrimSpace(server.Transport))
	if transportHint == "" {
		transportHint = strings.ToLower(strings.TrimSpace(server.Type))
	}
	endpoint := Endpoint{
		Kind:    EndpointKindMCP,
		Command: command,
		Args:    append([]string(nil), server.Args...),
		Env:     cloneStringMap(server.Env),
		URL:     url,
		Headers: cloneStringMap(server.Headers),
	}
	if command != "" {
		if transportHint != "" && transportHint != "stdio" {
			return Endpoint{}, fmt.Errorf("transport %q does not match command", transportHint)
		}
		if len(endpoint.Headers) > 0 {
			return Endpoint{}, errors.New("headers are only supported by URL servers")
		}
		endpoint.Transport = EndpointTransportStdio
	} else {
		switch transportHint {
		case "", "http", "streamable-http", EndpointTransportStreamableHTTP:
		default:
			return Endpoint{}, fmt.Errorf("unsupported URL transport %q", transportHint)
		}
		if len(endpoint.Args) > 0 || len(endpoint.Env) > 0 {
			return Endpoint{}, errors.New("args and env are only supported by command servers")
		}
		endpoint.Transport = EndpointTransportStreamableHTTP
	}
	for _, arg := range endpoint.Args {
		if strings.ContainsRune(arg, 0) {
			return Endpoint{}, errors.New("args cannot contain a NUL byte")
		}
	}
	if err := ValidateEndpoint(endpoint); err != nil {
		return Endpoint{}, err
	}
	return endpoint, nil
}

func mcpConfigEntryFromDefinition(def *Definition) (mcpConfigEntry, error) {
	if def == nil || def.Source != SourceInstalled || def.Kind != KindMCP || len(def.Endpoints) != 1 {
		return mcpConfigEntry{}, ErrNotFound
	}
	for _, endpoint := range def.Endpoints {
		if endpoint.Kind != EndpointKindMCP {
			return mcpConfigEntry{}, ErrNotFound
		}
		return mcpConfigEntry{
			Name:        def.Name,
			Description: def.Description,
			Endpoint:    CloneEndpoint(endpoint),
		}, nil
	}
	return mcpConfigEntry{}, ErrNotFound
}

func mcpServerConfigFromEndpoint(description string, endpoint Endpoint) MCPServerConfig {
	return MCPServerConfig{
		Command:     endpoint.Command,
		Args:        append([]string(nil), endpoint.Args...),
		Env:         cloneStringMap(endpoint.Env),
		URL:         endpoint.URL,
		Headers:     cloneStringMap(endpoint.Headers),
		Description: strings.TrimSpace(description),
	}
}

func availableMCPAppID(name string, occupied map[string]*Definition) (string, bool) {
	base := "mcp-" + mcpAppSlug(name)
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		existing := occupied[candidate]
		if existing == nil {
			return candidate, false
		}
		if existing.Kind == KindMCP && strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(name)) {
			return candidate, true
		}
	}
}

func mcpAppSlug(name string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if out.Len() >= 48 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r <= unicode.MaxASCII {
				out.WriteRune(r)
				lastDash = false
			}
			continue
		}
		if out.Len() > 0 && !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(out.String(), "-")
	if slug == "" {
		return "server"
	}
	return slug
}
