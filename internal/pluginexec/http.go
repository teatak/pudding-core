// Package pluginexec owns plugin HTTP requests and credential injection. Callers validate their own resource scope before supplying a binding.
package pluginexec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/teatak/pudding-core/internal/plugin"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Executor struct {
	webHTTPClient *http.Client
	pluginTokenMu sync.Mutex
	pluginTokens  map[string]endpointAuthTokenCacheEntry
}

func New(client *http.Client) *Executor {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Executor{webHTTPClient: client, pluginTokens: map[string]endpointAuthTokenCacheEntry{}}
}
func stringArg(args map[string]any, key string) string { value, _ := args[key].(string); return value }

const (
	EndpointRequestTimeout   = 15 * time.Second
	endpointMaxRequestBytes  = 1 * 1024 * 1024
	endpointMaxResponseBytes = 256 * 1024
)

var endpointAllowedMethods = map[string]struct{}{
	"GET":    {},
	"POST":   {},
	"PUT":    {},
	"PATCH":  {},
	"DELETE": {},
}

var endpointSensitiveResponseHeaders = map[string]struct{}{
	"set-cookie":           {},
	"set-cookie2":          {},
	"www-authenticate":     {},
	"proxy-authenticate":   {},
	"authentication-info":  {},
	"proxy-authentication": {},
}

type RequestPayload struct {
	EndpointName        string
	PluginID            string
	ConnectionID        string
	Method              string
	URL                 string
	Body                []byte
	ContentType         string
	Auth                plugin.Auth
	AuthMethod          plugin.AuthMethod
	ConnectionFields    map[string]string
	ConnectionFieldDefs []plugin.ConnectionField
	MaxResponseBytes    int
	GraphQL             bool
}

type endpointAuthTokenCacheEntry struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
}

func (r *Executor) REST(ctx context.Context, binding *plugin.EndpointBinding, args map[string]any, responseLimit ...int) map[string]any {
	target, err := BuildEndpointURL(binding.Endpoint.URL, stringArg(args, "path"))
	if err != nil {
		return (map[string]any{"ok": false, "reason": "invalid_path", "error": err.Error()})
	}
	method := strings.ToUpper(strings.TrimSpace(stringArg(args, "method")))
	if method == "" {
		method = http.MethodGet
	}
	if _, ok := endpointAllowedMethods[method]; !ok {
		return (map[string]any{"ok": false, "reason": "unsupported_method", "method": method})
	}
	if err := applyEndpointQuery(target, args["query"]); err != nil {
		return (map[string]any{"ok": false, "reason": "invalid_query", "error": err.Error()})
	}
	if err := ApplyEndpointConnectionQuery(target, method, binding.ConnectionFields, binding.ConnectionFieldDefs); err != nil {
		return (map[string]any{"ok": false, "reason": "connection_field_error", "error": err.Error()})
	}
	if err := applyEndpointConnectionBodyJSON(args, method, binding.ConnectionFields, binding.ConnectionFieldDefs); err != nil {
		return (map[string]any{"ok": false, "reason": "connection_field_error", "error": err.Error()})
	}
	body, contentType, err := buildEndpointRequestBody(args)
	if err != nil {
		return (map[string]any{"ok": false, "reason": "invalid_body", "error": err.Error()})
	}
	payload := RequestPayload{
		MaxResponseBytes:    responseBytes(responseLimit),
		EndpointName:        binding.EndpointName,
		PluginID:            binding.PluginID,
		ConnectionID:        binding.ConnectionID,
		Method:              method,
		URL:                 target.String(),
		Body:                body,
		ContentType:         contentType,
		Auth:                binding.Auth,
		AuthMethod:          binding.AuthMethod,
		ConnectionFields:    binding.ConnectionFields,
		ConnectionFieldDefs: binding.ConnectionFieldDefs,
	}
	return r.Do(ctx, payload)
}

func (r *Executor) GraphQL(ctx context.Context, binding *plugin.EndpointBinding, args map[string]any, responseLimit ...int) map[string]any {
	variables, err := parseEndpointGraphQLVariables(args["variables"])
	if err != nil {
		return (map[string]any{"ok": false, "reason": "invalid_variables", "error": err.Error()})
	}
	bodyFields := map[string]any{"query": stringArg(args, "query"), "variables": variables}
	if name := stringArg(args, "operationName"); name != "" {
		bodyFields["operationName"] = name
	}
	body, err := json.Marshal(bodyFields)
	if err != nil {
		return (map[string]any{"ok": false, "reason": "encode_error", "error": err.Error()})
	}
	payload := RequestPayload{
		MaxResponseBytes:    responseBytes(responseLimit),
		EndpointName:        binding.EndpointName,
		PluginID:            binding.PluginID,
		ConnectionID:        binding.ConnectionID,
		Method:              http.MethodPost,
		URL:                 binding.Endpoint.URL,
		Body:                body,
		ContentType:         "application/json",
		Auth:                binding.Auth,
		AuthMethod:          binding.AuthMethod,
		ConnectionFields:    binding.ConnectionFields,
		ConnectionFieldDefs: binding.ConnectionFieldDefs,
		GraphQL:             true,
	}
	return r.Do(ctx, payload)
}

func (r *Executor) Do(ctx context.Context, payload RequestPayload) map[string]any {
	if payload.MaxResponseBytes <= 0 {
		payload.MaxResponseBytes = endpointMaxResponseBytes
	}
	if len(payload.Body) > endpointMaxRequestBytes {
		return map[string]any{"ok": false, "reason": "request_too_large"}
	}
	reqCtx, cancel := context.WithTimeout(ctx, EndpointRequestTimeout)
	defer cancel()
	var reader io.Reader
	if len(payload.Body) > 0 {
		reader = bytes.NewReader(payload.Body)
	}
	req, err := http.NewRequestWithContext(reqCtx, payload.Method, payload.URL, reader)
	if err != nil {
		return (map[string]any{"ok": false, "reason": "request_error", "error": err.Error()})
	}
	resolvedAuth, err := r.ResolveEndpointAuth(reqCtx, payload.PluginID, payload.ConnectionID, payload.Auth, payload.AuthMethod, payload.ConnectionFields)
	if err != nil {
		return (map[string]any{"ok": false, "reason": "token_exchange_failed", "error": err.Error()})
	}
	if err := ApplyEndpointAuth(req.Header, resolvedAuth); err != nil {
		return (map[string]any{"ok": false, "reason": "auth_config_error", "error": err.Error()})
	}
	if err := ApplyEndpointConnectionHeaders(req.Header, payload.Method, payload.ConnectionFields, payload.ConnectionFieldDefs); err != nil {
		return (map[string]any{"ok": false, "reason": "connection_field_error", "error": err.Error()})
	}
	if payload.ContentType != "" {
		req.Header.Set("Content-Type", payload.ContentType)
	}
	req.Header.Set("Accept", "application/json")
	start := time.Now()
	client := *r.webHTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return (map[string]any{
			"ok":          false,
			"reason":      EndpointNetworkReason(err),
			"endpoint":    payload.EndpointName,
			"plugin":      payload.PluginID,
			"method":      payload.Method,
			"url":         payload.URL,
			"duration_ms": elapsed.Milliseconds(),
			"error":       err.Error(),
		})
	}
	defer resp.Body.Close()
	data, truncated, err := readEndpointBody(resp.Body, payload.MaxResponseBytes)
	if err != nil {
		return (map[string]any{"ok": false, "reason": "read_error", "error": err.Error(), "status": resp.StatusCode})
	}
	response := map[string]any{
		"ok":               true,
		"endpoint":         payload.EndpointName,
		"plugin":           payload.PluginID,
		"connection":       payload.ConnectionID,
		"auth_type":        payload.Auth.Type,
		"auth_method":      payload.Auth.MethodID,
		"auth_variant":     payload.Auth.Variant,
		"method":           payload.Method,
		"url":              payload.URL,
		"status":           resp.StatusCode,
		"duration_ms":      elapsed.Milliseconds(),
		"response_headers": flattenEndpointHeaders(resp.Header),
		"content_type":     resp.Header.Get("Content-Type"),
		"body_truncated":   truncated,
		"body_size":        len(data),
	}
	if payload.GraphQL {
		decodeGraphQLBody(response, data)
	} else {
		decodeEndpointBody(response, resp.Header.Get("Content-Type"), data)
	}
	return response
}

func (r *Executor) ResolveEndpointAuth(
	ctx context.Context,
	pluginID string,
	connectionID string,
	auth plugin.Auth,
	method plugin.AuthMethod,
	connectionFields map[string]string,
) (plugin.Auth, error) {
	if strings.TrimSpace(auth.Type) != plugin.AuthTypeTokenExchange {
		return auth, nil
	}
	exchange := method.TokenExchange
	if exchange == nil {
		return plugin.Auth{}, errors.New("token exchange configuration is missing")
	}
	body := make(map[string]string, len(exchange.BodyFields))
	for bodyName, fieldID := range exchange.BodyFields {
		value := strings.TrimSpace(connectionFields[fieldID])
		if value == "" {
			return plugin.Auth{}, fmt.Errorf("connection field %q is required for token exchange", fieldID)
		}
		body[bodyName] = value
	}
	cacheKey := endpointAuthTokenCacheKey(pluginID, connectionID, method, body)
	if cached, ok := r.cachedEndpointAuthToken(cacheKey); ok {
		return plugin.Auth{Type: plugin.AuthTypeOAuth2, AccessToken: cached.AccessToken, TokenType: cached.TokenType}, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return plugin.Auth{}, fmt.Errorf("encode token exchange request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchange.URL, bytes.NewReader(payload))
	if err != nil {
		return plugin.Auth{}, fmt.Errorf("create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := *r.webHTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return plugin.Auth{}, fmt.Errorf("request token: %w", err)
	}
	defer resp.Body.Close()
	data, _, err := readEndpointBody(resp.Body, endpointMaxResponseBytes)
	if err != nil {
		return plugin.Auth{}, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return plugin.Auth{}, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return plugin.Auth{}, errors.New("token endpoint returned invalid JSON")
	}
	accessToken, ok := tokenExchangeString(decoded, exchange.AccessTokenField)
	if !ok || strings.TrimSpace(accessToken) == "" {
		return plugin.Auth{}, fmt.Errorf("token response is missing %q", exchange.AccessTokenField)
	}
	if !plugin.IsAllowedRequestHeaderValue(accessToken) {
		return plugin.Auth{}, errors.New("token response contains an invalid access token")
	}
	tokenType := strings.TrimSpace(exchange.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	expiresIn := 3600 * time.Second
	if seconds, ok := tokenExchangeSeconds(decoded, exchange.ExpiresInField); ok && seconds > 0 {
		expiresIn = time.Duration(seconds) * time.Second
	}
	entry := endpointAuthTokenCacheEntry{
		AccessToken: accessToken,
		TokenType:   tokenType,
		ExpiresAt:   time.Now().Add(expiresIn),
	}
	r.pluginTokenMu.Lock()
	r.pluginTokens[cacheKey] = entry
	r.pluginTokenMu.Unlock()
	return plugin.Auth{Type: plugin.AuthTypeOAuth2, AccessToken: accessToken, TokenType: tokenType}, nil
}

func (r *Executor) cachedEndpointAuthToken(key string) (endpointAuthTokenCacheEntry, bool) {
	r.pluginTokenMu.Lock()
	defer r.pluginTokenMu.Unlock()
	entry, ok := r.pluginTokens[key]
	if !ok || !time.Now().Add(time.Minute).Before(entry.ExpiresAt) {
		delete(r.pluginTokens, key)
		return endpointAuthTokenCacheEntry{}, false
	}
	return entry, true
}

func endpointAuthTokenCacheKey(pluginID, connectionID string, method plugin.AuthMethod, body map[string]string) string {
	encoded, _ := json.Marshal(struct {
		PluginID     string                    `json:"plugin"`
		ConnectionID string                    `json:"connection"`
		MethodID     string                    `json:"method"`
		Exchange     *plugin.TokenExchangeSpec `json:"exchange"`
		Body         map[string]string         `json:"body"`
	}{pluginID, connectionID, method.ID, method.TokenExchange, body})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func tokenExchangeString(value map[string]any, field string) (string, bool) {
	raw, ok := tokenExchangeValue(value, field)
	if !ok {
		return "", false
	}
	text, ok := raw.(string)
	return text, ok
}

func tokenExchangeSeconds(value map[string]any, field string) (int64, bool) {
	if strings.TrimSpace(field) == "" {
		return 0, false
	}
	raw, ok := tokenExchangeValue(value, field)
	if !ok {
		return 0, false
	}
	switch item := raw.(type) {
	case float64:
		return int64(item), item > 0
	case json.Number:
		seconds, err := item.Int64()
		return seconds, err == nil && seconds > 0
	case string:
		seconds, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64)
		return seconds, err == nil && seconds > 0
	default:
		return 0, false
	}
}

func tokenExchangeValue(value map[string]any, field string) (any, bool) {
	var current any = value
	for _, part := range strings.Split(strings.TrimSpace(field), ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func BuildEndpointURL(rawBase, rawPath string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(rawBase))
	if err != nil {
		return nil, fmt.Errorf("parse endpoint url: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("endpoint url scheme %q not allowed", base.Scheme)
	}
	if base.Host == "" {
		return nil, errors.New("endpoint url missing host")
	}
	if base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("rest endpoint url must not contain query or fragment")
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return nil, errors.New("path is required")
	}
	if strings.HasPrefix(rawPath, "//") {
		return nil, errors.New("path must be relative to the endpoint base")
	}
	suffix, err := url.Parse(rawPath)
	if err != nil {
		return nil, fmt.Errorf("parse path: %w", err)
	}
	if suffix.Scheme != "" || suffix.Host != "" {
		return nil, errors.New("path must not be a full URL")
	}
	if suffix.RawQuery != "" || suffix.Fragment != "" {
		return nil, errors.New("path must not contain query or fragment; pass query separately")
	}
	basePath := cleanEndpointBasePath(base.Path)
	targetPath := path.Join(basePath, suffix.Path)
	if targetPath == "" {
		targetPath = "/"
	}
	if !pathWithinEndpointBase(basePath, targetPath) {
		return nil, fmt.Errorf("path %q escapes endpoint base %q", rawPath, basePath)
	}
	out := *base
	// Join escaped segments separately so an identifier containing '/' remains one segment.
	escaped := path.Join(cleanEndpointBasePath(base.EscapedPath()), suffix.EscapedPath())
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return nil, err
	}
	if !pathWithinEndpointBase(basePath, path.Clean(decoded)) {
		return nil, errors.New("encoded path escapes endpoint base")
	}
	out.Path = decoded
	out.RawPath = escaped
	out.RawQuery = ""
	out.Fragment = ""
	return &out, nil
}

func cleanEndpointBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return "/"
	}
	return path.Clean("/" + strings.Trim(p, "/"))
}

func pathWithinEndpointBase(basePath, targetPath string) bool {
	if basePath == "/" {
		return strings.HasPrefix(targetPath, "/")
	}
	return targetPath == basePath || strings.HasPrefix(targetPath, basePath+"/")
}

func applyEndpointQuery(target *url.URL, raw any) error {
	kv, err := coerceEndpointKV(raw, "query")
	if err != nil {
		return err
	}
	if len(kv) == 0 {
		return nil
	}
	values := target.Query()
	for k, v := range kv {
		values.Set(k, v)
	}
	target.RawQuery = values.Encode()
	return nil
}

func ApplyEndpointConnectionQuery(target *url.URL, method string, fields map[string]string, defs []plugin.ConnectionField) error {
	if len(fields) == 0 || len(defs) == 0 {
		return nil
	}
	values := target.Query()
	changed := false
	for _, field := range defs {
		id := strings.TrimSpace(field.ID)
		value := strings.TrimSpace(fields[id])
		if id == "" || value == "" {
			continue
		}
		for _, rule := range field.Inject {
			if !connectionFieldRuleMatches(rule, "query", method) {
				continue
			}
			name := connectionFieldInjectName(field, rule)
			if name == "" {
				return fmt.Errorf("connection field %q has empty query name", id)
			}
			if _, exists := values[name]; exists {
				continue
			}
			values.Set(name, value)
			changed = true
		}
	}
	if changed {
		target.RawQuery = values.Encode()
	}
	return nil
}

func applyEndpointConnectionBodyJSON(args map[string]any, method string, fields map[string]string, defs []plugin.ConnectionField) error {
	if len(fields) == 0 || len(defs) == 0 {
		return nil
	}
	type bodyInject struct {
		field plugin.ConnectionField
		rule  plugin.ConnectionFieldInject
	}
	bodyFields := make([]bodyInject, 0)
	for _, field := range defs {
		id := strings.TrimSpace(field.ID)
		if id == "" || strings.TrimSpace(fields[id]) == "" {
			continue
		}
		for _, rule := range field.Inject {
			if connectionFieldRuleMatches(rule, "body", method) {
				bodyFields = append(bodyFields, bodyInject{field: field, rule: rule})
			}
		}
	}
	if len(bodyFields) == 0 {
		return nil
	}
	if rawText, ok := args["body_text"]; ok && strings.TrimSpace(fmt.Sprint(rawText)) != "" {
		return errors.New("connection fields cannot be injected into body_text; use body_json")
	}
	body := map[string]any{}
	hadBody := false
	if raw, ok := args["body_json"]; ok && raw != nil {
		hadBody = true
		switch value := raw.(type) {
		case map[string]any:
			body = value
		case map[string]string:
			body = make(map[string]any, len(value))
			for k, v := range value {
				body[k] = v
			}
		default:
			return errors.New("connection fields require body_json to be an object")
		}
	}
	changed := false
	for _, item := range bodyFields {
		field := item.field
		id := strings.TrimSpace(field.ID)
		value := strings.TrimSpace(fields[id])
		if id == "" || value == "" {
			continue
		}
		name := connectionFieldInjectName(field, item.rule)
		if name == "" {
			return fmt.Errorf("connection field %q has empty body field name", id)
		}
		if _, exists := body[name]; exists {
			continue
		}
		body[name] = value
		changed = true
	}
	if changed || hadBody {
		args["body_json"] = body
	}
	return nil
}

func ApplyEndpointConnectionHeaders(headers http.Header, method string, fields map[string]string, defs []plugin.ConnectionField) error {
	if len(fields) == 0 || len(defs) == 0 {
		return nil
	}
	for _, field := range defs {
		id := strings.TrimSpace(field.ID)
		value := strings.TrimSpace(fields[id])
		if id == "" || value == "" {
			continue
		}
		for _, rule := range field.Inject {
			if !connectionFieldRuleMatches(rule, "header", method) {
				continue
			}
			name := connectionFieldInjectName(field, rule)
			if name == "" {
				return fmt.Errorf("connection field %q has empty header name", id)
			}
			if !plugin.IsAllowedRequestHeaderName(name) {
				return fmt.Errorf("connection field %q targets forbidden header %q", id, name)
			}
			if !plugin.IsAllowedRequestHeaderValue(value) {
				return fmt.Errorf("connection field %q contains an invalid header value", id)
			}
			if headers.Get(name) == "" {
				headers.Set(name, value)
			}
		}
	}
	return nil
}

func ApplyEndpointConnectionEnv(extra map[string]string, fields map[string]string, defs []plugin.ConnectionField) (map[string]string, error) {
	out := make(map[string]string, len(extra)+len(fields))
	for key, value := range extra {
		if !validConnectionEnvName(key) {
			return nil, fmt.Errorf("endpoint env name %q is invalid", key)
		}
		if strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("endpoint env %q contains a NUL byte", key)
		}
		out[key] = value
	}
	if len(fields) == 0 || len(defs) == 0 {
		return out, nil
	}
	for _, field := range defs {
		id := strings.TrimSpace(field.ID)
		value := strings.TrimSpace(fields[id])
		if id == "" || value == "" {
			continue
		}
		for _, rule := range field.Inject {
			if strings.TrimSpace(rule.Target) != "env" {
				continue
			}
			name := connectionFieldInjectName(field, rule)
			if !validConnectionEnvName(name) {
				return nil, fmt.Errorf("connection field %q has invalid env name %q", id, name)
			}
			if strings.ContainsRune(value, 0) {
				return nil, fmt.Errorf("connection field %q contains a NUL byte", id)
			}
			if _, exists := out[name]; !exists {
				out[name] = value
			}
		}
	}
	return out, nil
}

func connectionFieldRuleMatches(rule plugin.ConnectionFieldInject, target, method string) bool {
	if strings.TrimSpace(rule.Target) != target {
		return false
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" || len(rule.Methods) == 0 {
		return true
	}
	for _, allowed := range rule.Methods {
		if strings.ToUpper(strings.TrimSpace(allowed)) == method {
			return true
		}
	}
	return false
}

func connectionFieldInjectName(field plugin.ConnectionField, rule plugin.ConnectionFieldInject) string {
	name := strings.TrimSpace(rule.Name)
	if name == "" {
		name = strings.TrimSpace(field.ID)
	}
	return name
}

func validConnectionEnvName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for index, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func buildEndpointRequestBody(args map[string]any) ([]byte, string, error) {
	jsonBytes, err := encodeEndpointBodyJSON(args["body_json"])
	if err != nil {
		return nil, "", err
	}
	var text string
	if raw, ok := args["body_text"]; ok && raw != nil {
		value, ok := raw.(string)
		if !ok {
			return nil, "", errors.New("body_text must be a string")
		}
		text = value
	}
	hasJSON := len(jsonBytes) > 0
	hasText := strings.TrimSpace(text) != ""
	if hasJSON && hasText {
		return nil, "", errors.New("body_json and body_text are mutually exclusive")
	}
	if hasJSON {
		if len(jsonBytes) > endpointMaxRequestBytes {
			return nil, "", fmt.Errorf("body_json exceeds %d bytes", endpointMaxRequestBytes)
		}
		return jsonBytes, "application/json", nil
	}
	if hasText {
		if len(text) > endpointMaxRequestBytes {
			return nil, "", fmt.Errorf("body_text exceeds %d bytes", endpointMaxRequestBytes)
		}
		return []byte(text), "", nil
	}
	return nil, "", nil
}

func encodeEndpointBodyJSON(raw any) ([]byte, error) {
	if raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode body_json: %w", err)
	}
	return b, nil
}

func parseEndpointGraphQLVariables(raw any) (map[string]any, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, err
		}
		return out, nil
	case map[string]any:
		return v, nil
	default:
		return nil, errors.New("variables must be an object or JSON object string")
	}
}

func coerceEndpointKV(raw any, field string) (map[string]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		out := make(map[string]string, len(v))
		for k, vv := range v {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			s, ok := coerceEndpointScalar(vv)
			if !ok {
				return nil, fmt.Errorf("%s[%q] must be a string/number/bool", field, k)
			}
			out[k] = s
		}
		return out, nil
	case map[string]string:
		return v, nil
	default:
		return nil, fmt.Errorf("%s must be an object", field)
	}
}

func coerceEndpointScalar(v any) (string, bool) {
	switch vv := v.(type) {
	case string:
		return vv, true
	case bool:
		return strconv.FormatBool(vv), true
	case float64:
		if vv == float64(int64(vv)) {
			return strconv.FormatInt(int64(vv), 10), true
		}
		return strconv.FormatFloat(vv, 'f', -1, 64), true
	case int:
		return strconv.Itoa(vv), true
	case int64:
		return strconv.FormatInt(vv, 10), true
	case json.Number:
		return vv.String(), true
	default:
		return "", false
	}
}

func ApplyEndpointAuth(headers http.Header, auth plugin.Auth) error {
	switch strings.TrimSpace(auth.Type) {
	case "", "none":
		return nil
	case "bearer":
		token := strings.TrimSpace(auth.Token)
		if token == "" {
			return errors.New("bearer token is empty")
		}
		if !plugin.IsAllowedRequestHeaderValue(token) {
			return errors.New("bearer token contains an invalid header value")
		}
		headers.Set("Authorization", "Bearer "+token)
	case "oauth2":
		token := strings.TrimSpace(auth.AccessToken)
		if token == "" {
			return errors.New("oauth2 access token is empty")
		}
		tokenType := strings.TrimSpace(auth.TokenType)
		if tokenType == "" || strings.EqualFold(tokenType, "bearer") {
			tokenType = "Bearer"
		}
		if strings.ContainsAny(tokenType, "\r\n \t") || !plugin.IsAllowedRequestHeaderValue(token) {
			return errors.New("oauth2 token type is invalid")
		}
		headers.Set("Authorization", tokenType+" "+token)
	case "token":
		token := strings.TrimSpace(auth.Token)
		if token == "" {
			return errors.New("token is empty")
		}
		prefix := strings.TrimSpace(auth.Prefix)
		if prefix == "" {
			prefix = "Token"
		}
		if !plugin.IsAllowedRequestHeaderValue(prefix) || !plugin.IsAllowedRequestHeaderValue(token) {
			return errors.New("token contains an invalid header value")
		}
		headers.Set("Authorization", prefix+" "+token)
	case "basic":
		if auth.Username == "" && auth.Password == "" {
			return errors.New("basic auth username/password are empty")
		}
		headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth.Username+":"+auth.Password)))
	case "header":
		name := strings.TrimSpace(auth.Header)
		if name == "" || auth.Token == "" {
			return errors.New("header auth name/token are required")
		}
		if !plugin.IsAllowedRequestHeaderName(name) {
			return fmt.Errorf("auth header %q is not allowed", name)
		}
		if !plugin.IsAllowedRequestHeaderValue(auth.Token) {
			return errors.New("header auth token contains an invalid header value")
		}
		headers.Set(name, auth.Token)
	default:
		return fmt.Errorf("unsupported auth type %q", auth.Type)
	}
	return nil
}

func readEndpointBody(body io.Reader, limit int) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func flattenEndpointHeaders(h http.Header) map[string]any {
	out := make(map[string]any, len(h))
	for k, vs := range h {
		if _, bad := endpointSensitiveResponseHeaders[strings.ToLower(k)]; bad {
			continue
		}
		switch len(vs) {
		case 0:
			continue
		case 1:
			out[k] = vs[0]
		default:
			out[k] = append([]string(nil), vs...)
		}
	}
	return out
}

func decodeEndpointBody(out map[string]any, contentType string, data []byte) {
	if len(data) == 0 {
		return
	}
	if endpointIsJSONContentType(contentType) {
		var parsed any
		if err := json.Unmarshal(data, &parsed); err == nil {
			out["body_json"] = parsed
			return
		}
	}
	if utf8.Valid(data) {
		out["body_text"] = string(data)
		return
	}
	out["body_base64"] = base64.StdEncoding.EncodeToString(data)
}

func decodeGraphQLBody(out map[string]any, data []byte) {
	if len(data) == 0 {
		return
	}
	var parsed struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		decodeEndpointBody(out, "application/json", data)
		return
	}
	if len(parsed.Data) > 0 {
		var dataValue any
		if err := json.Unmarshal(parsed.Data, &dataValue); err == nil {
			out["data"] = dataValue
		} else {
			out["data_raw"] = string(parsed.Data)
		}
	}
	if len(parsed.Errors) > 0 {
		var errorsValue any
		if err := json.Unmarshal(parsed.Errors, &errorsValue); err == nil {
			out["errors"] = errorsValue
		} else {
			out["errors_raw"] = string(parsed.Errors)
		}
	}
}

func endpointIsJSONContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return false
	}
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return ct == "application/json" || strings.HasSuffix(ct, "+json")
}

func EndpointNetworkReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection_refused"
	case strings.Contains(msg, "no such host"):
		return "no_such_host"
	default:
		return "network_error"
	}
}

func responseBytes(limits []int) int {
	if len(limits) > 0 && limits[0] > 0 {
		return limits[0]
	}
	return endpointMaxResponseBytes
}

// ValidateBoundRequest prevents generated widgets from overriding fields
// owned by the selected connection. Chat tools retain their explicit-call rules.
func ValidateBoundRequest(binding *plugin.EndpointBinding, method string, query map[string]any, rawBody any) error {
	body, _ := rawBody.(map[string]any)
	for _, field := range binding.ConnectionFieldDefs {
		for _, rule := range field.Inject {
			var values map[string]any
			switch rule.Target {
			case "query":
				values = query
			case "body":
				values = body
			default:
				continue
			}
			if !connectionFieldRuleMatches(rule, rule.Target, method) {
				continue
			}
			if _, exists := values[connectionFieldInjectName(field, rule)]; exists {
				return fmt.Errorf("connection-owned field %q cannot be supplied by a widget", field.ID)
			}
		}
	}
	return nil
}
