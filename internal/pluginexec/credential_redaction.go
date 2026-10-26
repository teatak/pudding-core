package pluginexec

import (
	"encoding/base64"
	"net/url"
	"sort"
	"strings"

	"github.com/teatak/pudding-core/internal/plugin"
)

func endpointCredentials(auth plugin.Auth, fields map[string]string, definitions []plugin.ConnectionField) []string {
	secrets := []string{auth.Token, auth.AccessToken, auth.RefreshToken, auth.Password}
	if auth.Type == plugin.AuthTypeBasic {
		secrets = append(secrets, base64.StdEncoding.EncodeToString([]byte(auth.Username+":"+auth.Password)))
	}
	for _, field := range definitions {
		if field.Secret {
			secrets = append(secrets, fields[field.ID])
		}
	}
	return secrets
}

// Redact known credentials after receiving a response, preserving the actual
// wire request. Providers may echo credentials in URLs, headers or JSON errors.
func redactEndpointResult(result map[string]any, credentials []string) {
	variants := map[string]struct{}{}
	for _, secret := range credentials {
		if secret == "" {
			continue
		}
		variants[secret] = struct{}{}
		variants[url.QueryEscape(secret)] = struct{}{}
	}
	if len(variants) == 0 {
		return
	}
	secrets := make([]string, 0, len(variants))
	for secret := range variants {
		secrets = append(secrets, secret)
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	pairs := make([]string, 0, len(secrets)*2)
	for _, secret := range secrets {
		pairs = append(pairs, secret, "[redacted]")
	}
	replacer := strings.NewReplacer(pairs...)
	var redact func(any) any
	redact = func(value any) any {
		switch value := value.(type) {
		case string:
			return replacer.Replace(value)
		case []string:
			out := make([]string, len(value))
			for i, item := range value {
				out[i] = replacer.Replace(item)
			}
			return out
		case []any:
			out := make([]any, len(value))
			for i, item := range value {
				out[i] = redact(item)
			}
			return out
		case map[string]any:
			out := make(map[string]any, len(value))
			for key, item := range value {
				out[replacer.Replace(key)] = redact(item)
			}
			return out
		default:
			return value
		}
	}
	// Authoritative connection and endpoint metadata is public configuration,
	// and is independent of provider-controlled response content.
	for _, key := range []string{"url", "error", "message", "response_headers", "body_json", "body_text", "data", "data_raw", "errors", "errors_raw"} {
		if value, ok := result[key]; ok {
			result[key] = redact(value)
		}
	}
}
