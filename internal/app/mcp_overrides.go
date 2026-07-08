package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const MCPOverrideFileName = ".pudding-mcp-overrides.yaml"

type rawMCPOverrideFile struct {
	MCP   map[string]MCPEndpointOverride `yaml:"mcp,omitempty"`
	Extra map[string]interface{}         `yaml:",inline"`
}

func LoadMCPOverrideFile(path string) (*MCPOverrideFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var raw rawMCPOverrideFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("app: parse mcp override %s: %w", path, err)
	}
	if len(raw.Extra) > 0 {
		return nil, fmt.Errorf("app: mcp override %s has unsupported root keys", path)
	}
	out := &MCPOverrideFile{MCP: make(map[string]MCPEndpointOverride, len(raw.MCP))}
	for name, override := range raw.MCP {
		name = strings.TrimSpace(name)
		if !endpointNamePattern.MatchString(name) {
			return nil, fmt.Errorf("app: mcp override %s has invalid endpoint %q", path, name)
		}
		override.Transport = strings.TrimSpace(override.Transport)
		override.URL = strings.TrimSpace(override.URL)
		override.Command = strings.TrimSpace(override.Command)
		if err := validateMCPEndpointOverride(override); err != nil {
			return nil, fmt.Errorf("app: mcp override %s endpoint %q: %w", path, name, err)
		}
		out.MCP[name] = CloneMCPEndpointOverride(override)
	}
	if len(out.MCP) == 0 {
		return nil, nil
	}
	return out, nil
}

func WriteMCPOverrideFile(path string, overrides *MCPOverrideFile) error {
	if overrides == nil || len(overrides.MCP) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(overrides)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ApplyMCPOverrides(def *Definition, overrides *MCPOverrideFile) (*Definition, error) {
	out := CloneDefinition(def)
	if out == nil || overrides == nil || len(overrides.MCP) == 0 {
		return out, nil
	}
	for name, override := range overrides.MCP {
		endpoint, ok := out.Endpoints[name]
		if !ok {
			return nil, fmt.Errorf("mcp override endpoint %q not found", name)
		}
		if endpoint.Kind != EndpointKindMCP {
			return nil, fmt.Errorf("mcp override endpoint %q is %q, not mcp", name, endpoint.Kind)
		}
		endpoint = applyMCPEndpointOverride(endpoint, override)
		if err := ValidateEndpoint(endpoint); err != nil {
			return nil, fmt.Errorf("mcp override endpoint %q: %w", name, err)
		}
		out.Endpoints[name] = endpoint
	}
	return out, nil
}

func applyMCPEndpointOverride(endpoint Endpoint, override MCPEndpointOverride) Endpoint {
	out := CloneEndpoint(endpoint)
	if override.Transport != "" {
		out.Transport = override.Transport
	}
	if override.URL != "" {
		out.URL = override.URL
	}
	if override.Command != "" {
		out.Command = override.Command
	}
	if override.Args != nil {
		out.Args = append([]string(nil), (*override.Args)...)
	}
	if override.Env != nil {
		out.Env = mergeStringMaps(out.Env, override.Env)
	}
	if override.Headers != nil {
		out.Headers = mergeStringMaps(out.Headers, override.Headers)
	}
	out.Platforms = nil
	return out
}

func validateMCPEndpointOverride(override MCPEndpointOverride) error {
	switch override.Transport {
	case "", EndpointTransportStdio, EndpointTransportStreamableHTTP:
	default:
		return fmt.Errorf("unsupported mcp transport %q", override.Transport)
	}
	if override.URL != "" {
		if err := validateEndpointURL(override.URL); err != nil {
			return err
		}
	}
	if err := validateEndpointStringMap("env", override.Env, validEndpointEnvName); err != nil {
		return err
	}
	if err := validateEndpointStringMap("header", override.Headers, validEndpointHeaderName); err != nil {
		return err
	}
	return nil
}
