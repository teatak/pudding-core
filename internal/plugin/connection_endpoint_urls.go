package plugin

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeConnectionEndpointURLs validates per-connection base URL overrides.
// Only REST and GraphQL endpoints may be overridden this way; MCP endpoints use
// their private MCP override configuration.
func NormalizeConnectionEndpointURLs(def *Definition, values map[string]string) (map[string]string, error) {
	if def == nil {
		return nil, fmt.Errorf("plugin definition is required")
	}
	out := make(map[string]string, len(values))
	for rawName, rawValue := range values {
		name := strings.TrimSpace(rawName)
		value := strings.TrimSpace(rawValue)
		if name == "" {
			return nil, fmt.Errorf("endpoint name is required")
		}
		endpoint, ok := def.Endpoints[name]
		if !ok {
			return nil, fmt.Errorf("endpoint %s is not defined by plugin", name)
		}
		if endpoint.Kind != EndpointKindREST && endpoint.Kind != EndpointKindGraphQL {
			return nil, fmt.Errorf("endpoint %s does not support a connection URL override", name)
		}
		if endpoint.URLConfig == nil {
			return nil, fmt.Errorf("endpoint %s does not allow a connection URL override", name)
		}
		if value == "" {
			continue
		}
		if err := validateConnectionEndpointURL(value); err != nil {
			return nil, fmt.Errorf("endpoint %s URL: %w", name, err)
		}
		out[name] = strings.TrimRight(value, "/")
	}
	for name, endpoint := range def.Endpoints {
		if endpoint.URLConfig != nil && endpoint.URLConfig.Required && out[name] == "" {
			return nil, fmt.Errorf("endpoint %s URL is required", name)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validateConnectionEndpointURL(rawURL string) error {
	if err := validateEndpointURL(rawURL); err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	if u.User != nil {
		return fmt.Errorf("userinfo is not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("query and fragment are not allowed")
	}
	return nil
}

// NormalizeConnectionEndpointInsecureTLS accepts trust exceptions only for the
// connection's configurable HTTPS endpoints. Omitted options retain exceptions
// solely when the effective server address is unchanged.
func NormalizeConnectionEndpointInsecureTLS(def *Definition, endpointURLs map[string]string, values map[string]bool, existing *Connection) (map[string]bool, error) {
	effectiveURL := func(name string, overrides map[string]string) string {
		if value := overrides[name]; value != "" {
			return value
		}
		return strings.TrimRight(def.Endpoints[name].URL, "/")
	}
	if values == nil && existing != nil && existing.PluginID == def.ID {
		values = make(map[string]bool)
		for name, enabled := range existing.EndpointInsecureTLS {
			if enabled && effectiveURL(name, endpointURLs) == effectiveURL(name, existing.EndpointURLs) {
				values[name] = true
			}
		}
	}
	var out map[string]bool
	for name, enabled := range values {
		endpoint, ok := def.Endpoints[name]
		if !ok || endpoint.URLConfig == nil || (endpoint.Kind != EndpointKindREST && endpoint.Kind != EndpointKindGraphQL) {
			return nil, fmt.Errorf("endpoint %s does not allow connection TLS settings", name)
		}
		if !enabled {
			continue
		}
		target, err := url.Parse(effectiveURL(name, endpointURLs))
		if err != nil || target.Scheme != "https" || target.Host == "" {
			return nil, fmt.Errorf("endpoint %s must use HTTPS to ignore certificate errors", name)
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[name] = true
	}
	return out, nil
}
