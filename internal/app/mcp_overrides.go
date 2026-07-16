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
	exists, err := inspectMCPOverrideFile(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
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
	exists, err := inspectMCPOverrideFile(path)
	if err != nil {
		return err
	}
	if overrides == nil || len(overrides.MCP) == 0 {
		if !exists {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() {
		return errors.New("app: mcp override directory must be a directory, not a symlink")
	}
	data, err := yaml.Marshal(overrides)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func inspectMCPOverrideFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("app: mcp override must be a regular file, not a symlink")
	}
	return true, nil
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
	if err := validateEndpointStringMap("header", override.Headers, IsAllowedRequestHeaderName); err != nil {
		return err
	}
	return nil
}
