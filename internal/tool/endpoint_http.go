package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	endpointRequestTimeout   = 15 * time.Second
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

var endpointForbiddenRequestHeaders = map[string]struct{}{
	"host":                {},
	"content-length":      {},
	"connection":          {},
	"transfer-encoding":   {},
	"expect":              {},
	"upgrade":             {},
	"proxy-connection":    {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
}

var endpointSensitiveResponseHeaders = map[string]struct{}{
	"set-cookie":           {},
	"set-cookie2":          {},
	"www-authenticate":     {},
	"proxy-authenticate":   {},
	"authentication-info":  {},
	"proxy-authentication": {},
}

func (r *BuiltinRunner) restRequest(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	binding, err := r.resolveAppEndpoint(ctx, call.SessionID, stringArg(args, "endpoint"), stringArg(args, "connection"), app.EndpointKindREST)
	if err != nil {
		return toolJSON(out, false, endpointResolveError("rest_endpoint", err))
	}
	target, err := buildEndpointURL(binding.Endpoint.URL, stringArg(args, "path"))
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_path", "error": err.Error()})
	}
	method := strings.ToUpper(strings.TrimSpace(stringArg(args, "method")))
	if method == "" {
		method = http.MethodGet
	}
	if _, ok := endpointAllowedMethods[method]; !ok {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "unsupported_method", "method": method})
	}
	if err := applyEndpointQuery(target, args["query"]); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_query", "error": err.Error()})
	}
	if err := applyEndpointConnectionQuery(target, method, binding.ConnectionFields, binding.ConnectionFieldDefs); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "connection_field_error", "error": err.Error()})
	}
	if err := applyEndpointConnectionBodyJSON(args, method, binding.ConnectionFields, binding.ConnectionFieldDefs); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "connection_field_error", "error": err.Error()})
	}
	body, contentType, err := buildEndpointRequestBody(args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_body", "error": err.Error()})
	}
	payload := endpointRequestPayload{
		EndpointName:        binding.EndpointName,
		AppID:               binding.AppID,
		Method:              method,
		URL:                 target.String(),
		Body:                body,
		ContentType:         contentType,
		Auth:                binding.Auth,
		ConnectionFields:    binding.ConnectionFields,
		ConnectionFieldDefs: binding.ConnectionFieldDefs,
	}
	return r.doEndpointRequest(ctx, out, payload)
}

func (r *BuiltinRunner) graphqlRequest(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "missing_query"})
	}
	binding, err := r.resolveAppEndpoint(ctx, call.SessionID, stringArg(args, "endpoint"), stringArg(args, "connection"), app.EndpointKindGraphQL)
	if err != nil {
		return toolJSON(out, false, endpointResolveError("graphql_endpoint", err))
	}
	variables, err := parseEndpointGraphQLVariables(args["variables"])
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_variables", "error": err.Error()})
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "encode_error", "error": err.Error()})
	}
	payload := endpointRequestPayload{
		EndpointName:        binding.EndpointName,
		AppID:               binding.AppID,
		Method:              http.MethodPost,
		URL:                 binding.Endpoint.URL,
		Body:                body,
		ContentType:         "application/json",
		Auth:                binding.Auth,
		ConnectionFields:    binding.ConnectionFields,
		ConnectionFieldDefs: binding.ConnectionFieldDefs,
		GraphQL:             true,
	}
	return r.doEndpointRequest(ctx, out, payload)
}

func (r *BuiltinRunner) resolveAppEndpoint(ctx context.Context, sessionID, endpointName, connection, wantKind string) (*app.EndpointBinding, error) {
	if r.appEndpoints == nil {
		return nil, errors.New("app endpoints unavailable")
	}
	binding, err := r.appEndpoints.ResolveEndpoint(ctx, sessionID, endpointName, connection)
	if err != nil {
		return nil, err
	}
	if binding.Endpoint.Kind != wantKind {
		return nil, fmt.Errorf("endpoint kind is %s, want %s", binding.Endpoint.Kind, wantKind)
	}
	return binding, nil
}

type endpointRequestPayload struct {
	EndpointName        string
	AppID               string
	Method              string
	URL                 string
	Body                []byte
	ContentType         string
	Auth                app.Auth
	ConnectionFields    map[string]string
	ConnectionFieldDefs []app.ConnectionField
	GraphQL             bool
}

func (r *BuiltinRunner) doEndpointRequest(ctx context.Context, out Result, payload endpointRequestPayload) Result {
	reqCtx, cancel := context.WithTimeout(ctx, endpointRequestTimeout)
	defer cancel()
	var reader io.Reader
	if len(payload.Body) > 0 {
		reader = bytes.NewReader(payload.Body)
	}
	req, err := http.NewRequestWithContext(reqCtx, payload.Method, payload.URL, reader)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "request_error", "error": err.Error()})
	}
	if err := applyEndpointAuth(req.Header, payload.Auth); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "auth_config_error", "error": err.Error()})
	}
	if err := applyEndpointConnectionHeaders(req.Header, payload.Method, payload.ConnectionFields, payload.ConnectionFieldDefs); err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "connection_field_error", "error": err.Error()})
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
		return toolJSON(out, false, map[string]any{
			"ok":          false,
			"reason":      endpointNetworkReason(err),
			"endpoint":    payload.EndpointName,
			"app":         payload.AppID,
			"method":      payload.Method,
			"url":         payload.URL,
			"duration_ms": elapsed.Milliseconds(),
			"error":       err.Error(),
		})
	}
	defer resp.Body.Close()
	data, truncated, err := readEndpointBody(resp.Body)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "read_error", "error": err.Error(), "status": resp.StatusCode})
	}
	response := map[string]any{
		"ok":               true,
		"endpoint":         payload.EndpointName,
		"app":              payload.AppID,
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
	summaryKind, summaryCount := endpointSummary(response)
	return withResultSummary(toolJSON(out, true, response), summaryKind, summaryCount)
}

func endpointResolveError(kind string, err error) map[string]any {
	reason := "endpoint_unavailable"
	var resolveErr *app.EndpointResolveError
	if errors.As(err, &resolveErr) {
		out := map[string]any{"ok": false, "reason": resolveErr.Reason, "error": resolveErr.Error()}
		if resolveErr.Endpoint != "" {
			out["endpoint"] = resolveErr.Endpoint
		}
		if resolveErr.Connection != "" {
			out["connection"] = resolveErr.Connection
		}
		if len(resolveErr.Connections) > 0 {
			out["connections"] = resolveErr.Connections
		}
		return out
	}
	if errors.Is(err, context.Canceled) {
		reason = "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "timeout"
	}
	if errors.Is(err, store.ErrNotFound) {
		reason = kind + "_not_granted"
	}
	return map[string]any{"ok": false, "reason": reason, "error": err.Error()}
}

func buildEndpointURL(rawBase, rawPath string) (*url.URL, error) {
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
	out.Path = targetPath
	out.RawPath = ""
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

func applyEndpointConnectionQuery(target *url.URL, method string, fields map[string]string, defs []app.ConnectionField) error {
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

func applyEndpointConnectionBodyJSON(args map[string]any, method string, fields map[string]string, defs []app.ConnectionField) error {
	if len(fields) == 0 || len(defs) == 0 {
		return nil
	}
	type bodyInject struct {
		field app.ConnectionField
		rule  app.ConnectionFieldInject
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

func applyEndpointConnectionHeaders(headers http.Header, method string, fields map[string]string, defs []app.ConnectionField) error {
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
			if _, forbidden := endpointForbiddenRequestHeaders[strings.ToLower(name)]; forbidden {
				return fmt.Errorf("connection field %q targets forbidden header %q", id, name)
			}
			if headers.Get(name) == "" {
				headers.Set(name, value)
			}
		}
	}
	return nil
}

func applyEndpointConnectionEnv(extra map[string]string, fields map[string]string, defs []app.ConnectionField) (map[string]string, error) {
	out := make(map[string]string, len(extra)+len(fields))
	for key, value := range extra {
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
			out[name] = value
		}
	}
	return out, nil
}

func connectionFieldRuleMatches(rule app.ConnectionFieldInject, target, method string) bool {
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

func connectionFieldInjectName(field app.ConnectionField, rule app.ConnectionFieldInject) string {
	name := strings.TrimSpace(rule.Name)
	if name == "" {
		name = strings.TrimSpace(field.ID)
	}
	return name
}

func validConnectionEnvName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !strings.ContainsAny(name, "=\x00")
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

func applyEndpointAuth(headers http.Header, auth app.Auth) error {
	switch strings.TrimSpace(auth.Type) {
	case "", "none":
		return nil
	case "bearer":
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("bearer token is empty")
		}
		headers.Set("Authorization", "Bearer "+strings.TrimSpace(auth.Token))
	case "oauth2":
		token := strings.TrimSpace(auth.AccessToken)
		if token == "" {
			return errors.New("oauth2 access token is empty")
		}
		tokenType := strings.TrimSpace(auth.TokenType)
		if tokenType == "" || strings.EqualFold(tokenType, "bearer") {
			tokenType = "Bearer"
		}
		if strings.ContainsAny(tokenType, "\r\n ") {
			return errors.New("oauth2 token type is invalid")
		}
		headers.Set("Authorization", tokenType+" "+token)
	case "token":
		if strings.TrimSpace(auth.Token) == "" {
			return errors.New("token is empty")
		}
		prefix := strings.TrimSpace(auth.Prefix)
		if prefix == "" {
			prefix = "Token"
		}
		headers.Set("Authorization", prefix+" "+strings.TrimSpace(auth.Token))
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
		if _, bad := endpointForbiddenRequestHeaders[strings.ToLower(name)]; bad {
			return fmt.Errorf("auth header %q is not allowed", name)
		}
		headers.Set(name, auth.Token)
	default:
		return fmt.Errorf("unsupported auth type %q", auth.Type)
	}
	return nil
}

func readEndpointBody(body io.Reader) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, endpointMaxResponseBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > endpointMaxResponseBytes {
		return data[:endpointMaxResponseBytes], true, nil
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

func endpointNetworkReason(err error) string {
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

func endpointSummary(response map[string]any) (string, int) {
	if v, ok := response["body_json"]; ok {
		switch vv := v.(type) {
		case []any:
			return SummaryReturnedItems, len(vv)
		case map[string]any:
			return SummaryReturnedFields, len(vv)
		}
	}
	if v, ok := response["data"].(map[string]any); ok {
		return SummaryReturnedFields, len(v)
	}
	if v, ok := response["body_text"].(string); ok {
		return SummaryReadChars, utf8.RuneCountInString(v)
	}
	return SummaryReturnedFields, len(response)
}
