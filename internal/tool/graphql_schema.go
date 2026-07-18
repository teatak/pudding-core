package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/app"
)

const (
	graphqlSchemaTTL              = 6 * time.Hour
	graphqlSearchDefaultMax       = 30
	graphqlSearchMax              = 50
	graphqlSchemaMaxResponseBytes = 4 * 1024 * 1024
)

const graphqlTypeRefFragment = `
fragment TR on __Type {
  kind name
  ofType {
    kind name
    ofType {
      kind name
      ofType { kind name }
    }
  }
}`

const graphqlTopLevelQuery = `
{
  __schema {
    queryType { name fields { name description args { name description type { ...TR } defaultValue } type { ...TR } } }
    mutationType { name fields { name description args { name description type { ...TR } defaultValue } type { ...TR } } }
    subscriptionType { name fields { name description args { name description type { ...TR } defaultValue } type { ...TR } } }
  }
}` + graphqlTypeRefFragment

const graphqlTypeQuery = `
query($n: String!) {
  __type(name: $n) {
    kind name description
    fields {
      name description
      args { name description type { ...TR } defaultValue }
      type { ...TR }
    }
    inputFields { name description type { ...TR } defaultValue }
    enumValues { name description }
    interfaces { ...TR }
    possibleTypes { ...TR }
  }
}` + graphqlTypeRefFragment

const graphqlFullQuery = `
{
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind name description
      fields {
        name description
        args { name description type { ...TR } defaultValue }
        type { ...TR }
      }
      inputFields { name description type { ...TR } defaultValue }
      enumValues { name description }
      interfaces { ...TR }
      possibleTypes { ...TR }
    }
  }
}` + graphqlTypeRefFragment

type graphqlTypeRef struct {
	Kind   string          `json:"kind"`
	Name   string          `json:"name,omitempty"`
	OfType *graphqlTypeRef `json:"ofType,omitempty"`
}

type graphqlInputValue struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Type         *graphqlTypeRef `json:"type"`
	DefaultValue string          `json:"defaultValue,omitempty"`
}

type graphqlField struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Args        []graphqlInputValue `json:"args,omitempty"`
	Type        *graphqlTypeRef     `json:"type"`
}

type graphqlEnumValue struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type graphqlSchemaType struct {
	Kind          string              `json:"kind"`
	Name          string              `json:"name"`
	Description   string              `json:"description,omitempty"`
	Fields        []graphqlField      `json:"fields,omitempty"`
	InputFields   []graphqlInputValue `json:"inputFields,omitempty"`
	EnumValues    []graphqlEnumValue  `json:"enumValues,omitempty"`
	Interfaces    []*graphqlTypeRef   `json:"interfaces,omitempty"`
	PossibleTypes []*graphqlTypeRef   `json:"possibleTypes,omitempty"`
}

type graphqlTopLevel struct {
	QueryName        string
	MutationName     string
	SubscriptionName string
	Query            []graphqlField
	Mutation         []graphqlField
	Subscription     []graphqlField
}

type graphqlSchemaCache struct {
	fetchedAt time.Time
	full      bool
	top       *graphqlTopLevel
	types     map[string]*graphqlSchemaType
}

func (c *graphqlSchemaCache) fresh() bool {
	return c != nil && !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < graphqlSchemaTTL
}

type graphqlSchemaResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors json.RawMessage `json:"errors"`
}

type graphqlSearchMatch struct {
	Kind    string `json:"kind"`
	Where   string `json:"where"`
	Snippet string `json:"snippet,omitempty"`
	score   int
}

func (r *BuiltinRunner) graphqlIntrospect(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	binding, err := r.resolveAppEndpoint(ctx, call.SessionID, stringArg(args, "endpoint"), stringArg(args, "connection"), app.EndpointKindGraphQL)
	if err != nil {
		return toolJSON(out, false, endpointResolveError("graphql_endpoint", err))
	}
	typeName := strings.TrimSpace(stringArg(args, "type_name"))
	force, _ := boolArg(args, "force_refresh")
	if typeName == "" {
		return r.graphqlIntrospectTopLevel(ctx, out, binding, force)
	}
	return r.graphqlIntrospectType(ctx, out, binding, typeName, force)
}

func (r *BuiltinRunner) graphqlSearch(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "search_query_required"})
	}
	binding, err := r.resolveAppEndpoint(ctx, call.SessionID, stringArg(args, "endpoint"), stringArg(args, "connection"), app.EndpointKindGraphQL)
	if err != nil {
		return toolJSON(out, false, endpointResolveError("graphql_endpoint", err))
	}
	maxResults := intArg(args, "max_results")
	if maxResults <= 0 {
		maxResults = graphqlSearchDefaultMax
	}
	if maxResults > graphqlSearchMax {
		maxResults = graphqlSearchMax
	}
	force, _ := boolArg(args, "force_refresh")

	key := graphqlSchemaCacheKey(binding)
	r.graphqlSchemaMu.Lock()
	cache := r.graphqlSchemas[key]
	if cache == nil {
		cache = &graphqlSchemaCache{types: map[string]*graphqlSchemaType{}}
		r.graphqlSchemas[key] = cache
	}
	source := "cache"
	if force || !cache.full || !cache.fresh() {
		top, types, errPayload := r.fetchGraphQLFullSchema(ctx, binding)
		if errPayload != nil {
			r.graphqlSchemaMu.Unlock()
			return toolJSON(out, false, errPayload)
		}
		cache.top = top
		cache.types = types
		cache.full = true
		cache.fetchedAt = time.Now()
		source = "fresh"
	}
	keywords := splitGraphQLKeywords(query)
	matches := searchGraphQLSchema(cache, keywords)
	schemaFetched := cache.fetchedAt.UTC().Format(time.RFC3339)
	r.graphqlSchemaMu.Unlock()

	total := len(matches)
	truncated := false
	if len(matches) > maxResults {
		matches = matches[:maxResults]
		truncated = true
	}
	payload := map[string]any{
		"ok":             true,
		"endpoint":       binding.EndpointName,
		"app":            binding.AppID,
		"query":          query,
		"keywords":       keywords,
		"source":         source,
		"matches":        matches,
		"total_matches":  total,
		"truncated":      truncated,
		"schema_fetched": schemaFetched,
	}
	if total == 0 {
		payload["hint"] = "no matches; try shorter keywords or inspect a known type_name"
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(matches))
}

func (r *BuiltinRunner) graphqlIntrospectTopLevel(ctx context.Context, out Result, binding *app.EndpointBinding, force bool) Result {
	key := graphqlSchemaCacheKey(binding)
	r.graphqlSchemaMu.Lock()
	defer r.graphqlSchemaMu.Unlock()
	cache := r.graphqlSchemas[key]
	if cache == nil {
		cache = &graphqlSchemaCache{types: map[string]*graphqlSchemaType{}}
		r.graphqlSchemas[key] = cache
	}
	source := "cache"
	if force || cache.top == nil || !cache.fresh() {
		top, errPayload := r.fetchGraphQLTopLevel(ctx, binding)
		if errPayload != nil {
			return toolJSON(out, false, errPayload)
		}
		cache.top = top
		cache.fetchedAt = time.Now()
		source = "fresh"
	}
	payload := map[string]any{
		"ok":             true,
		"endpoint":       binding.EndpointName,
		"app":            binding.AppID,
		"source":         source,
		"schema_fetched": cache.fetchedAt.UTC().Format(time.RFC3339),
	}
	if cache.top != nil {
		if cache.top.QueryName != "" {
			payload["query_type"] = cache.top.QueryName
		}
		if cache.top.MutationName != "" {
			payload["mutation_type"] = cache.top.MutationName
		}
		if cache.top.SubscriptionName != "" {
			payload["subscription_type"] = cache.top.SubscriptionName
		}
		if len(cache.top.Query) > 0 {
			payload["query"] = renderGraphQLFields(cache.top.Query)
		}
		if len(cache.top.Mutation) > 0 {
			payload["mutation"] = renderGraphQLFields(cache.top.Mutation)
		}
		if len(cache.top.Subscription) > 0 {
			payload["subscription"] = renderGraphQLFields(cache.top.Subscription)
		}
	}
	payload["hint"] = "pass type_name to inspect a specific type; use builtin_graphql_search to find names"
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedFields, len(payload))
}

func (r *BuiltinRunner) graphqlIntrospectType(ctx context.Context, out Result, binding *app.EndpointBinding, typeName string, force bool) Result {
	key := graphqlSchemaCacheKey(binding)
	r.graphqlSchemaMu.Lock()
	defer r.graphqlSchemaMu.Unlock()
	cache := r.graphqlSchemas[key]
	if cache == nil {
		cache = &graphqlSchemaCache{types: map[string]*graphqlSchemaType{}}
		r.graphqlSchemas[key] = cache
	}
	source := "cache"
	item := cache.types[typeName]
	if force || item == nil || !cache.fresh() {
		var errPayload map[string]any
		item, errPayload = r.fetchGraphQLType(ctx, binding, typeName)
		if errPayload != nil {
			if errPayload["reason"] == "type_not_found" && len(cache.types) > 0 {
				errPayload["available_types"] = graphQLTypeNameSample(cache.types, typeName)
			}
			return toolJSON(out, false, errPayload)
		}
		if cache.types == nil {
			cache.types = map[string]*graphqlSchemaType{}
		}
		cache.types[typeName] = item
		cache.fetchedAt = time.Now()
		source = "fresh"
	}
	payload := map[string]any{
		"ok":             true,
		"endpoint":       binding.EndpointName,
		"app":            binding.AppID,
		"source":         source,
		"schema_fetched": cache.fetchedAt.UTC().Format(time.RFC3339),
		"type":           renderGraphQLType(item),
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedFields, len(payload))
}

func (r *BuiltinRunner) fetchGraphQLTopLevel(ctx context.Context, binding *app.EndpointBinding) (*graphqlTopLevel, map[string]any) {
	res, errPayload := r.doGraphQLSchemaQuery(ctx, binding, graphqlTopLevelQuery, nil)
	if errPayload != nil {
		return nil, errPayload
	}
	if len(res.Errors) > 0 {
		return nil, graphQLSchemaError(binding, "introspection_disabled", graphQLErrorsPreview(res.Errors))
	}
	var envelope struct {
		Schema struct {
			QueryType *struct {
				Name   string         `json:"name"`
				Fields []graphqlField `json:"fields"`
			} `json:"queryType"`
			MutationType *struct {
				Name   string         `json:"name"`
				Fields []graphqlField `json:"fields"`
			} `json:"mutationType"`
			SubscriptionType *struct {
				Name   string         `json:"name"`
				Fields []graphqlField `json:"fields"`
			} `json:"subscriptionType"`
		} `json:"__schema"`
	}
	if err := json.Unmarshal(res.Data, &envelope); err != nil {
		return nil, graphQLSchemaError(binding, "introspection_parse_failed", err.Error())
	}
	top := &graphqlTopLevel{}
	if envelope.Schema.QueryType != nil {
		top.QueryName = envelope.Schema.QueryType.Name
		top.Query = envelope.Schema.QueryType.Fields
	}
	if envelope.Schema.MutationType != nil {
		top.MutationName = envelope.Schema.MutationType.Name
		top.Mutation = envelope.Schema.MutationType.Fields
	}
	if envelope.Schema.SubscriptionType != nil {
		top.SubscriptionName = envelope.Schema.SubscriptionType.Name
		top.Subscription = envelope.Schema.SubscriptionType.Fields
	}
	return top, nil
}

func (r *BuiltinRunner) fetchGraphQLType(ctx context.Context, binding *app.EndpointBinding, typeName string) (*graphqlSchemaType, map[string]any) {
	res, errPayload := r.doGraphQLSchemaQuery(ctx, binding, graphqlTypeQuery, map[string]any{"n": typeName})
	if errPayload != nil {
		return nil, errPayload
	}
	if len(res.Errors) > 0 {
		return nil, graphQLSchemaError(binding, "introspection_disabled", graphQLErrorsPreview(res.Errors))
	}
	var envelope struct {
		Type *graphqlSchemaType `json:"__type"`
	}
	if err := json.Unmarshal(res.Data, &envelope); err != nil {
		return nil, graphQLSchemaError(binding, "introspection_parse_failed", err.Error())
	}
	if envelope.Type == nil {
		return nil, graphQLSchemaError(binding, "type_not_found", typeName)
	}
	return envelope.Type, nil
}

func (r *BuiltinRunner) fetchGraphQLFullSchema(ctx context.Context, binding *app.EndpointBinding) (*graphqlTopLevel, map[string]*graphqlSchemaType, map[string]any) {
	res, errPayload := r.doGraphQLSchemaQuery(ctx, binding, graphqlFullQuery, nil)
	if errPayload != nil {
		return nil, nil, errPayload
	}
	if len(res.Errors) > 0 {
		return nil, nil, graphQLSchemaError(binding, "introspection_disabled", graphQLErrorsPreview(res.Errors))
	}
	var envelope struct {
		Schema struct {
			QueryType        *struct{ Name string } `json:"queryType"`
			MutationType     *struct{ Name string } `json:"mutationType"`
			SubscriptionType *struct{ Name string } `json:"subscriptionType"`
			Types            []*graphqlSchemaType   `json:"types"`
		} `json:"__schema"`
	}
	if err := json.Unmarshal(res.Data, &envelope); err != nil {
		return nil, nil, graphQLSchemaError(binding, "introspection_parse_failed", err.Error())
	}
	types := make(map[string]*graphqlSchemaType, len(envelope.Schema.Types))
	for _, item := range envelope.Schema.Types {
		if item == nil || item.Name == "" || strings.HasPrefix(item.Name, "__") {
			continue
		}
		types[item.Name] = item
	}
	top := &graphqlTopLevel{}
	if envelope.Schema.QueryType != nil {
		top.QueryName = envelope.Schema.QueryType.Name
		if item := types[top.QueryName]; item != nil {
			top.Query = item.Fields
		}
	}
	if envelope.Schema.MutationType != nil {
		top.MutationName = envelope.Schema.MutationType.Name
		if item := types[top.MutationName]; item != nil {
			top.Mutation = item.Fields
		}
	}
	if envelope.Schema.SubscriptionType != nil {
		top.SubscriptionName = envelope.Schema.SubscriptionType.Name
		if item := types[top.SubscriptionName]; item != nil {
			top.Subscription = item.Fields
		}
	}
	return top, types, nil
}

func (r *BuiltinRunner) doGraphQLSchemaQuery(ctx context.Context, binding *app.EndpointBinding, query string, variables map[string]any) (*graphqlSchemaResponse, map[string]any) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, graphQLSchemaError(binding, "encode_error", err.Error())
	}
	reqCtx, cancel := context.WithTimeout(ctx, endpointRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, binding.Endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return nil, graphQLSchemaError(binding, "request_error", err.Error())
	}
	resolvedAuth, err := r.resolveEndpointAuth(reqCtx, binding.AppID, binding.ConnectionID, binding.Auth, binding.AuthMethod, binding.ConnectionFields)
	if err != nil {
		return nil, graphQLSchemaError(binding, "token_exchange_failed", err.Error())
	}
	if err := applyEndpointAuth(req.Header, resolvedAuth); err != nil {
		return nil, graphQLSchemaError(binding, "auth_config_error", err.Error())
	}
	if err := applyEndpointConnectionHeaders(req.Header, http.MethodPost, binding.ConnectionFields, binding.ConnectionFieldDefs); err != nil {
		return nil, graphQLSchemaError(binding, "connection_field_error", err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := r.webHTTPClient.Do(req)
	if err != nil {
		return nil, graphQLSchemaError(binding, endpointNetworkReason(err), err.Error())
	}
	defer resp.Body.Close()
	data, truncated, err := readGraphQLSchemaBody(resp.Body)
	if err != nil {
		return nil, graphQLSchemaError(binding, "read_error", err.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload := graphQLSchemaError(binding, "http_error", fmt.Sprintf("status %d", resp.StatusCode))
		payload["status"] = resp.StatusCode
		payload["body"] = truncateString(string(data), 512)
		payload["body_truncated"] = truncated
		return nil, payload
	}
	var parsed graphqlSchemaResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, graphQLSchemaError(binding, "decode_error", err.Error())
	}
	if len(parsed.Data) == 0 {
		return nil, graphQLSchemaError(binding, "introspection_empty_response", "")
	}
	return &parsed, nil
}

func readGraphQLSchemaBody(body io.Reader) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, graphqlSchemaMaxResponseBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > graphqlSchemaMaxResponseBytes {
		return data[:graphqlSchemaMaxResponseBytes], true, nil
	}
	return data, false, nil
}

func graphqlSchemaCacheKey(binding *app.EndpointBinding) string {
	return strings.Join([]string{binding.AppID, binding.ConnectionID, binding.EndpointName, binding.Endpoint.URL}, "|")
}

func graphQLSchemaError(binding *app.EndpointBinding, reason, message string) map[string]any {
	out := map[string]any{
		"ok":       false,
		"reason":   reason,
		"endpoint": binding.EndpointName,
		"app":      binding.AppID,
	}
	if message != "" {
		out["message"] = message
	}
	switch reason {
	case "introspection_disabled":
		out["hint"] = "endpoint disabled GraphQL introspection; use app docs or ask the user for schema details"
	case "type_not_found":
		out["hint"] = "type_name was not found; use builtin_graphql_search with a shorter keyword"
	}
	return out
}

func graphQLErrorsPreview(raw json.RawMessage) string {
	var arr []struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return string(raw)
	}
	parts := make([]string, 0, len(arr))
	for _, item := range arr {
		if msg := strings.TrimSpace(item.Message); msg != "" {
			parts = append(parts, msg)
		}
	}
	if len(parts) == 0 {
		return string(raw)
	}
	return strings.Join(parts, "; ")
}

func renderGraphQLFields(fields []graphqlField) []map[string]any {
	out := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		item := map[string]any{"name": field.Name, "return": renderGraphQLTypeRef(field.Type)}
		if field.Description != "" {
			item["description"] = field.Description
		}
		if len(field.Args) > 0 {
			item["args"] = renderGraphQLInputValues(field.Args)
		}
		out = append(out, item)
	}
	return out
}

func renderGraphQLInputValues(values []graphqlInputValue) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item := map[string]any{"name": value.Name, "type": renderGraphQLTypeRef(value.Type)}
		if value.Type != nil && value.Type.Kind == "NON_NULL" {
			item["required"] = true
		}
		if value.Description != "" {
			item["description"] = value.Description
		}
		if value.DefaultValue != "" {
			item["default"] = value.DefaultValue
		}
		out = append(out, item)
	}
	return out
}

func renderGraphQLType(t *graphqlSchemaType) map[string]any {
	if t == nil {
		return nil
	}
	out := map[string]any{"kind": t.Kind, "name": t.Name}
	if t.Description != "" {
		out["description"] = t.Description
	}
	switch t.Kind {
	case "OBJECT", "INTERFACE":
		if len(t.Fields) > 0 {
			out["fields"] = renderGraphQLFields(t.Fields)
		}
		if len(t.Interfaces) > 0 {
			out["interfaces"] = renderGraphQLTypeRefList(t.Interfaces)
		}
	case "INPUT_OBJECT":
		if len(t.InputFields) > 0 {
			out["input_fields"] = renderGraphQLInputValues(t.InputFields)
		}
	case "ENUM":
		values := make([]map[string]any, 0, len(t.EnumValues))
		for _, value := range t.EnumValues {
			item := map[string]any{"name": value.Name}
			if value.Description != "" {
				item["description"] = value.Description
			}
			values = append(values, item)
		}
		if len(values) > 0 {
			out["enum_values"] = values
		}
	case "UNION":
		if len(t.PossibleTypes) > 0 {
			out["possible_types"] = renderGraphQLTypeRefList(t.PossibleTypes)
		}
	}
	return out
}

func renderGraphQLTypeRefList(refs []*graphqlTypeRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, renderGraphQLTypeRef(ref))
	}
	return out
}

func renderGraphQLTypeRef(ref *graphqlTypeRef) string {
	if ref == nil {
		return "Unknown"
	}
	switch ref.Kind {
	case "NON_NULL":
		return renderGraphQLTypeRef(ref.OfType) + "!"
	case "LIST":
		return "[" + renderGraphQLTypeRef(ref.OfType) + "]"
	default:
		if ref.Name != "" {
			return ref.Name
		}
		return "Unknown"
	}
}

func splitGraphQLKeywords(query string) []string {
	parts := strings.Fields(strings.ToLower(query))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func graphQLMatchAll(haystack string, keywords []string) bool {
	haystack = strings.ToLower(haystack)
	for _, keyword := range keywords {
		if !strings.Contains(haystack, keyword) {
			return false
		}
	}
	return true
}

func searchGraphQLSchema(cache *graphqlSchemaCache, keywords []string) []graphqlSearchMatch {
	if cache == nil || len(keywords) == 0 || len(cache.types) == 0 {
		return nil
	}
	names := make([]string, 0, len(cache.types))
	for name := range cache.types {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]graphqlSearchMatch, 0, 32)
	for _, name := range names {
		item := cache.types[name]
		if item == nil {
			continue
		}
		if graphQLMatchAll(item.Name, keywords) {
			out = append(out, graphqlSearchMatch{Kind: graphqlKindLabel(item.Kind), Where: item.Name, Snippet: graphqlTypeSnippet(item), score: 0})
		} else if item.Description != "" && graphQLMatchAll(item.Description, keywords) {
			out = append(out, graphqlSearchMatch{Kind: graphqlKindLabel(item.Kind), Where: item.Name, Snippet: item.Description, score: 100})
		}
		for _, field := range item.Fields {
			if graphQLMatchAll(field.Name, keywords) {
				out = append(out, graphqlSearchMatch{Kind: "FIELD", Where: item.Name + "." + field.Name, Snippet: graphqlFieldSnippet(item.Name, field), score: 10})
			} else if field.Description != "" && graphQLMatchAll(field.Description, keywords) {
				out = append(out, graphqlSearchMatch{Kind: "FIELD", Where: item.Name + "." + field.Name, Snippet: field.Description, score: 110})
			}
			for _, arg := range field.Args {
				if graphQLMatchAll(arg.Name, keywords) {
					out = append(out, graphqlSearchMatch{Kind: "ARG", Where: item.Name + "." + field.Name + "." + arg.Name, Snippet: arg.Name + ": " + renderGraphQLTypeRef(arg.Type), score: 20})
				}
			}
		}
		for _, input := range item.InputFields {
			if graphQLMatchAll(input.Name, keywords) {
				out = append(out, graphqlSearchMatch{Kind: "INPUT_FIELD", Where: item.Name + "." + input.Name, Snippet: input.Name + ": " + renderGraphQLTypeRef(input.Type), score: 25})
			} else if input.Description != "" && graphQLMatchAll(input.Description, keywords) {
				out = append(out, graphqlSearchMatch{Kind: "INPUT_FIELD", Where: item.Name + "." + input.Name, Snippet: input.Description, score: 125})
			}
		}
		for _, value := range item.EnumValues {
			if graphQLMatchAll(value.Name, keywords) {
				out = append(out, graphqlSearchMatch{Kind: "ENUM_VALUE", Where: item.Name + "." + value.Name, score: 30})
			} else if value.Description != "" && graphQLMatchAll(value.Description, keywords) {
				out = append(out, graphqlSearchMatch{Kind: "ENUM_VALUE", Where: item.Name + "." + value.Name, Snippet: value.Description, score: 130})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score < out[j].score
		}
		return out[i].Where < out[j].Where
	})
	return out
}

func graphqlKindLabel(kind string) string {
	if kind == "" {
		return "TYPE"
	}
	return kind
}

func graphqlTypeSnippet(item *graphqlSchemaType) string {
	if item == nil {
		return ""
	}
	names := make([]string, 0, 5)
	switch item.Kind {
	case "OBJECT", "INTERFACE":
		for _, field := range item.Fields {
			names = append(names, field.Name)
		}
	case "INPUT_OBJECT":
		for _, field := range item.InputFields {
			names = append(names, field.Name)
		}
	case "ENUM":
		for _, value := range item.EnumValues {
			names = append(names, value.Name)
		}
	}
	if len(names) > 5 {
		return strings.Join(names[:5], ", ") + ", ..."
	}
	return strings.Join(names, ", ")
}

func graphqlFieldSnippet(parent string, field graphqlField) string {
	var b strings.Builder
	b.WriteString(parent)
	b.WriteString(".")
	b.WriteString(field.Name)
	if len(field.Args) > 0 {
		b.WriteString("(")
		limit := len(field.Args)
		more := false
		if limit > 3 {
			limit = 3
			more = true
		}
		for i := 0; i < limit; i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			arg := field.Args[i]
			b.WriteString(arg.Name)
			b.WriteString(": ")
			b.WriteString(renderGraphQLTypeRef(arg.Type))
		}
		if more {
			b.WriteString(", ...")
		}
		b.WriteString(")")
	}
	b.WriteString(": ")
	b.WriteString(renderGraphQLTypeRef(field.Type))
	return b.String()
}

func graphQLTypeNameSample(types map[string]*graphqlSchemaType, prefix string) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	names := make([]string, 0, len(types))
	for name := range types {
		if prefix == "" || strings.Contains(strings.ToLower(name), prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 20 {
		return names[:20]
	}
	return names
}
