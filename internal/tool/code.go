package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/lsp"
)

const (
	defaultCodeResults       = 100
	maxCodeSymbols           = 200
	maxCodeDefinitions       = 20
	maxCodeReferences        = 500
	maxCodeDiagnostics       = 500
	maxCodeDiagnosticTargets = 32
	codeDiagnosticWait       = 750 * time.Millisecond
)

type codeSymbolsArgs struct {
	Scope      string `json:"scope"`
	Path       string `json:"path,omitempty"`
	Language   string `json:"language,omitempty"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

type codePositionArgs struct {
	Scope    string `json:"scope"`
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type codeReferencesArgs struct {
	codePositionArgs
	IncludeDeclaration *bool `json:"include_declaration,omitempty"`
	MaxResults         int   `json:"max_results,omitempty"`
}

type codeDiagnosticsArgs struct {
	Scope    string   `json:"scope"`
	Paths    []string `json:"paths"`
	Language string   `json:"language,omitempty"`
	Severity []string `json:"severity,omitempty"`
}

type rawCodeSymbol struct {
	Name          string         `json:"name"`
	Kind          int            `json:"kind"`
	ContainerName string         `json:"containerName,omitempty"`
	Location      rawLSPLocation `json:"location"`
}

type codeSymbol struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	ContainerName string `json:"containerName,omitempty"`
	codeLocation
}

type codeDiagnostic struct {
	codeLocation
	Severity   string `json:"severity"`
	Code       string `json:"code,omitempty"`
	Source     string `json:"source,omitempty"`
	SourceKind string `json:"sourceKind"`
	Message    string `json:"message"`
}

func (r *BuiltinRunner) codeSymbols(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args codeSymbolsArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "code symbol arguments must be a JSON object")
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return toolJSONError(out, "invalid_scope", "code tools require project scope")
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return toolJSONError(out, "invalid_arguments", "query is required")
	}
	maxResults, err := boundedCodeResults(args.MaxResults, maxCodeSymbols)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	target, failed := r.codeTarget(out, call, args.Path, args.Language, true)
	if failed != nil {
		return *failed
	}
	if unavailable := r.codeServiceUnavailable(out); unavailable != nil {
		return *unavailable
	}
	var rawSymbols []rawCodeSymbol
	if err := r.languageService.Request(ctx, target.spec, "workspace/symbol", map[string]any{"query": args.Query}, &rawSymbols); err != nil {
		return codeServiceError(out, err)
	}
	converter := newCodeLocationConverter(call.ProjectDirs, "utf-16")
	processEncoding, err := r.languageService.PositionEncoding(ctx, target.spec)
	if err != nil {
		return codeServiceError(out, err)
	}
	converter.encoding = processEncoding
	symbols := make([]codeSymbol, 0, min(len(rawSymbols), maxResults))
	external := 0
	for _, raw := range rawSymbols {
		location, ok := converter.convert(raw.Location)
		if !ok {
			external++
			continue
		}
		symbols = append(symbols, codeSymbol{
			Name:          raw.Name,
			Kind:          codeSymbolKind(raw.Kind),
			ContainerName: raw.ContainerName,
			codeLocation:  location,
		})
	}
	sort.Slice(symbols, func(i, j int) bool {
		left, right := symbols[i], symbols[j]
		if left.RelativePath != right.RelativePath {
			return left.RelativePath < right.RelativePath
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		return left.Name < right.Name
	})
	truncated := len(symbols) > maxResults
	if truncated {
		symbols = symbols[:maxResults]
	}
	payload := codeBasePayload(target)
	payload["query"] = args.Query
	payload["symbols"] = symbols
	payload["resultCount"] = len(symbols)
	payload["externalResultCount"] = external
	payload["truncated"] = truncated
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(symbols))
}

func (r *BuiltinRunner) codeDefinition(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, result := decodeCodePositionArgs(out, call.Args)
	if result != nil {
		return *result
	}
	target, document, lines, state, failed := r.prepareCodePosition(ctx, out, call, args)
	if failed != nil {
		return *failed
	}
	position, err := codePosition(lines, args.Line, args.Column, state.PositionEncoding)
	if err != nil {
		return toolJSONError(out, "invalid_position", err.Error())
	}
	var raw json.RawMessage
	params := map[string]any{"textDocument": map[string]string{"uri": document.URI}, "position": position}
	if err := r.languageService.Request(ctx, target.spec, "textDocument/definition", params, &raw); err != nil {
		return codeServiceError(out, err)
	}
	rawLocations, err := parseLocationResponse(raw)
	if err != nil {
		return toolJSONError(out, "language_server_protocol_error", err.Error())
	}
	locations, external, truncated := convertCodeLocations(call.ProjectDirs, state.PositionEncoding, rawLocations, maxCodeDefinitions)
	payload := codeBasePayload(target)
	payload["locations"] = locations
	payload["locationCount"] = len(locations)
	payload["externalResultCount"] = external
	payload["truncated"] = truncated
	if len(locations) == 0 && external > 0 {
		payload["hint"] = "Definition is outside authorized project roots."
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(locations))
}

func (r *BuiltinRunner) codeReferences(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args codeReferencesArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "code reference arguments must be a JSON object")
	}
	if result := validateCodePositionArgs(out, args.codePositionArgs); result != nil {
		return *result
	}
	maxResults, err := boundedCodeResults(args.MaxResults, maxCodeReferences)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	target, document, lines, state, failed := r.prepareCodePosition(ctx, out, call, args.codePositionArgs)
	if failed != nil {
		return *failed
	}
	position, err := codePosition(lines, args.Line, args.Column, state.PositionEncoding)
	if err != nil {
		return toolJSONError(out, "invalid_position", err.Error())
	}
	includeDeclaration := true
	if args.IncludeDeclaration != nil {
		includeDeclaration = *args.IncludeDeclaration
	}
	var rawLocations []rawLSPLocation
	params := map[string]any{
		"textDocument": map[string]string{"uri": document.URI},
		"position":     position,
		"context":      map[string]bool{"includeDeclaration": includeDeclaration},
	}
	if err := r.languageService.Request(ctx, target.spec, "textDocument/references", params, &rawLocations); err != nil {
		return codeServiceError(out, err)
	}
	locations, external, truncated := convertCodeLocations(call.ProjectDirs, state.PositionEncoding, rawLocations, maxResults)
	payload := codeBasePayload(target)
	payload["locations"] = locations
	payload["locationCount"] = len(locations)
	payload["externalResultCount"] = external
	payload["truncated"] = truncated
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(locations))
}

func (r *BuiltinRunner) codeDiagnostics(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args codeDiagnosticsArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "code diagnostic arguments must be a JSON object")
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return toolJSONError(out, "invalid_scope", "code tools require project scope")
	}
	if len(args.Paths) == 0 || len(args.Paths) > maxCodeDiagnosticTargets {
		return toolJSONError(out, "invalid_arguments", "paths must contain between 1 and 32 files")
	}
	severity, err := codeSeverityFilter(args.Severity)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if unavailable := r.codeServiceUnavailable(out); unavailable != nil {
		return *unavailable
	}
	var sharedTarget *resolvedCodeTarget
	diagnostics := make([]codeDiagnostic, 0)
	fresh := true
	truncated := false
	for _, path := range args.Paths {
		target, failed := r.codeTarget(out, call, path, args.Language, false)
		if failed != nil {
			return *failed
		}
		if sharedTarget != nil && sharedTarget.spec.Key != target.spec.Key {
			return toolJSONError(out, "mixed_language_targets", "all diagnostic paths must use the same language root and server")
		}
		if sharedTarget == nil {
			copyTarget := target
			sharedTarget = &copyTarget
		}
		text, _, err := readCodeDocument(target.path)
		if err != nil {
			return codeDocumentError(out, err)
		}
		document := lsp.Document{URI: codeFileURI(target.path), LanguageID: target.documentLanguageID, Text: text}
		state, err := r.languageService.SyncDocument(ctx, target.spec, document)
		if err != nil {
			return codeServiceError(out, err)
		}
		items, itemFresh, err := r.codeDocumentDiagnostics(ctx, target, document.URI, state)
		if err != nil {
			return codeServiceError(out, err)
		}
		fresh = fresh && itemFresh
		converter := newCodeLocationConverter(call.ProjectDirs, state.PositionEncoding)
		for _, item := range items {
			level := codeDiagnosticSeverity(item.Severity)
			if len(severity) > 0 && !severity[level] {
				continue
			}
			location, ok := converter.convert(rawLSPLocation{URI: document.URI, Range: item.Range})
			if !ok {
				continue
			}
			if len(diagnostics) >= maxCodeDiagnostics {
				truncated = true
				continue
			}
			diagnostics = append(diagnostics, codeDiagnostic{
				codeLocation: location,
				Severity:     level,
				Code:         diagnosticCode(item.Code),
				Source:       item.Source,
				SourceKind:   "lsp",
				Message:      item.Message,
			})
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i], diagnostics[j]
		if left.RelativePath != right.RelativePath {
			return left.RelativePath < right.RelativePath
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Column < right.Column
	})
	payload := codeBasePayload(*sharedTarget)
	payload["diagnostics"] = diagnostics
	payload["diagnosticCount"] = len(diagnostics)
	payload["fresh"] = fresh
	payload["truncated"] = truncated
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(diagnostics))
}

func (r *BuiltinRunner) prepareCodePosition(ctx context.Context, out Result, call Call, args codePositionArgs) (resolvedCodeTarget, lsp.Document, []string, lsp.DocumentState, *Result) {
	if unavailable := r.codeServiceUnavailable(out); unavailable != nil {
		return resolvedCodeTarget{}, lsp.Document{}, nil, lsp.DocumentState{}, unavailable
	}
	target, failed := r.codeTarget(out, call, args.Path, args.Language, false)
	if failed != nil {
		return resolvedCodeTarget{}, lsp.Document{}, nil, lsp.DocumentState{}, failed
	}
	text, lines, err := readCodeDocument(target.path)
	if err != nil {
		result := codeDocumentError(out, err)
		return resolvedCodeTarget{}, lsp.Document{}, nil, lsp.DocumentState{}, &result
	}
	document := lsp.Document{URI: codeFileURI(target.path), LanguageID: target.documentLanguageID, Text: text}
	state, err := r.languageService.SyncDocument(ctx, target.spec, document)
	if err != nil {
		result := codeServiceError(out, err)
		return resolvedCodeTarget{}, lsp.Document{}, nil, lsp.DocumentState{}, &result
	}
	return target, document, lines, state, nil
}

func (r *BuiltinRunner) codeDocumentDiagnostics(ctx context.Context, target resolvedCodeTarget, uri string, state lsp.DocumentState) ([]lsp.Diagnostic, bool, error) {
	var report struct {
		Kind  string           `json:"kind"`
		Items []lsp.Diagnostic `json:"items"`
	}
	err := r.languageService.Request(ctx, target.spec, "textDocument/diagnostic", map[string]any{
		"textDocument": map[string]string{"uri": uri},
	}, &report)
	if err == nil && report.Kind != "unchanged" {
		return report.Items, true, nil
	}
	var responseErr *lsp.ResponseError
	if err != nil && (!errors.As(err, &responseErr) || responseErr.Code != -32601) {
		return nil, false, err
	}
	afterGeneration := uint64(0)
	if state.Changed {
		afterGeneration = state.PreviousDiagnosticGeneration
	}
	waitCtx, cancel := context.WithTimeout(ctx, codeDiagnosticWait)
	defer cancel()
	snapshot, ok, waitErr := r.languageService.PublishedDiagnostics(waitCtx, target.spec, uri, afterGeneration)
	if errors.Is(waitErr, context.DeadlineExceeded) {
		return nil, false, nil
	}
	if waitErr != nil {
		return nil, false, waitErr
	}
	return snapshot.Diagnostics, ok && (!state.Changed || snapshot.Generation > state.PreviousDiagnosticGeneration), nil
}

func decodeCodePositionArgs(out Result, raw json.RawMessage) (codePositionArgs, *Result) {
	var args codePositionArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		result := toolJSONError(out, "invalid_arguments", "code position arguments must be a JSON object")
		return args, &result
	}
	return args, validateCodePositionArgs(out, args)
}

func validateCodePositionArgs(out Result, args codePositionArgs) *Result {
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		result := toolJSONError(out, "invalid_scope", "code tools require project scope")
		return &result
	}
	if strings.TrimSpace(args.Path) == "" || args.Line < 1 || args.Column < 1 {
		result := toolJSONError(out, "invalid_arguments", "path, line, and column are required")
		return &result
	}
	return nil
}

func (r *BuiltinRunner) codeTarget(out Result, call Call, path, language string, allowDirectory bool) (resolvedCodeTarget, *Result) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	target, err := r.resolveCodeTarget(call, path, language, allowDirectory)
	if err == nil {
		return target, nil
	}
	var unavailable *languageServerUnavailableError
	if errors.As(err, &unavailable) {
		result := codeUnavailableError(out, unavailable)
		return resolvedCodeTarget{}, &result
	}
	if errors.Is(err, errProjectDirsRequired) || errors.Is(err, errProjectPathNotAllowed) || errors.Is(err, errProjectFilePathRequired) {
		result := filePathError(out, managedScopeProject, err)
		return resolvedCodeTarget{}, &result
	}
	reason := "language_root_not_found"
	if strings.Contains(err.Error(), "language_ambiguous") {
		reason = "language_ambiguous"
	} else if strings.Contains(err.Error(), "language_not_supported") {
		reason = "language_not_supported"
	}
	result := toolJSONError(out, reason, err.Error())
	return resolvedCodeTarget{}, &result
}

func (r *BuiltinRunner) codeServiceUnavailable(out Result) *Result {
	if r.languageService != nil {
		return nil
	}
	result := toolJSONError(out, "language_server_unavailable", "language service is not configured")
	return &result
}

func codeUnavailableError(out Result, unavailable *languageServerUnavailableError) Result {
	hint := unavailable.hint
	if hint == "" {
		hint = "Install or configure " + unavailable.server + ", then retry. Pudding did not install it automatically."
	}
	payload := map[string]any{
		"ok":       false,
		"reason":   "language_server_unavailable",
		"language": unavailable.language,
		"server":   unavailable.server,
		"checked":  unavailable.checked,
		"hint":     hint,
	}
	return withResultSummary(toolJSON(out, false, payload), SummaryReturnedFields, len(payload))
}

func codeServiceError(out Result, err error) Result {
	reason := "language_server_protocol_error"
	switch {
	case errors.Is(err, context.Canceled):
		reason = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "language_server_timeout"
	case errors.Is(err, lsp.ErrProcessClosed) || strings.Contains(err.Error(), "language server exited"):
		reason = "language_server_crashed"
	case strings.Contains(err.Error(), "initialize language server"):
		reason = "language_server_initialize_failed"
	case strings.Contains(err.Error(), "start language server"):
		reason = "language_server_start_failed"
	}
	return toolJSONError(out, reason, err.Error())
}

func codeDocumentError(out Result, err error) Result {
	reason := "language_server_protocol_error"
	if strings.Contains(err.Error(), "document_too_large") {
		reason = "document_too_large"
	}
	return toolJSONError(out, reason, err.Error())
}

func codeBasePayload(target resolvedCodeTarget) map[string]any {
	return map[string]any{
		"ok":           true,
		"server":       target.spec.Key.ServerKind,
		"language":     target.language,
		"languageRoot": target.languageRoot,
		"rootFallback": target.rootFallback,
	}
}

func boundedCodeResults(value, maximum int) (int, error) {
	if value == 0 {
		return defaultCodeResults, nil
	}
	if value < 1 || value > maximum {
		return 0, fmt.Errorf("max_results must be between 1 and %d", maximum)
	}
	return value, nil
}

func convertCodeLocations(projectDirs []string, encoding string, raw []rawLSPLocation, limit int) ([]codeLocation, int, bool) {
	converter := newCodeLocationConverter(projectDirs, encoding)
	locations := make([]codeLocation, 0, min(len(raw), limit))
	external := 0
	for _, item := range raw {
		location, ok := converter.convert(item)
		if !ok {
			external++
			continue
		}
		locations = append(locations, location)
	}
	locations = sortAndDedupeLocations(locations)
	truncated := len(locations) > limit
	if truncated {
		locations = locations[:limit]
	}
	return locations, external, truncated
}

func codeSeverityFilter(values []string) (map[string]bool, error) {
	filter := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "error", "warning", "information", "hint":
			filter[value] = true
		default:
			return nil, fmt.Errorf("unsupported severity %q", value)
		}
	}
	return filter, nil
}

func codeDiagnosticSeverity(value int) string {
	switch value {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "information"
	case 4:
		return "hint"
	default:
		return "information"
	}
}

func codeSymbolKind(value int) string {
	kinds := []string{"", "file", "module", "namespace", "package", "class", "method", "property", "field", "constructor", "enum", "interface", "function", "variable", "constant", "string", "number", "boolean", "array", "object", "key", "null", "enumMember", "struct", "event", "operator", "typeParameter"}
	if value > 0 && value < len(kinds) {
		return kinds[value]
	}
	return "unknown"
}
