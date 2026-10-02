package tool

import (
	"context"
	"errors"
	"fmt"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"unicode/utf8"
)

func (r *BuiltinRunner) restRequest(ctx context.Context, call Call) Result {
	return r.pluginHTTPRequest(ctx, call, plugin.EndpointKindREST)
}
func (r *BuiltinRunner) graphqlRequest(ctx context.Context, call Call) Result {
	return r.pluginHTTPRequest(ctx, call, plugin.EndpointKindGraphQL)
}
func (r *BuiltinRunner) pluginHTTPRequest(ctx context.Context, call Call, kind string) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	binding, err := r.resolvePluginEndpoint(ctx, call.SessionID, stringArg(args, "endpoint"), stringArg(args, "connection"), kind)
	if err != nil {
		return toolJSON(out, false, endpointResolveError(kind+"_endpoint", err))
	}
	var response map[string]any
	if kind == plugin.EndpointKindREST {
		response = r.pluginHTTP.REST(ctx, binding, args)
	} else {
		if stringArg(args, "query") == "" {
			return toolJSON(out, false, map[string]any{"ok": false, "reason": "missing_query"})
		}
		response = r.pluginHTTP.GraphQL(ctx, binding, args)
	}
	ok, _ := response["ok"].(bool)
	summary, count := endpointSummary(response)
	return withResultSummary(toolJSON(out, ok, response), summary, count)
}
func (r *BuiltinRunner) resolvePluginEndpoint(ctx context.Context, sessionID, endpointName, connection, wantKind string) (*plugin.EndpointBinding, error) {
	if r.pluginEndpoints == nil {
		return nil, errors.New("app endpoints unavailable")
	}
	binding, err := r.pluginEndpoints.ResolveEndpoint(ctx, sessionID, endpointName, connection)
	if err != nil {
		return nil, err
	}
	if binding.Endpoint.Kind != wantKind {
		return nil, fmt.Errorf("endpoint kind is %s, want %s", binding.Endpoint.Kind, wantKind)
	}
	return binding, nil
}

func endpointResolveError(kind string, err error) map[string]any {
	reason := "endpoint_unavailable"
	var resolveErr *plugin.EndpointResolveError
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
