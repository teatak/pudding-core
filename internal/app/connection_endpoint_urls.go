package app

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
		return nil, fmt.Errorf("app definition is required")
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
			return nil, fmt.Errorf("endpoint %s is not defined by app", name)
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
