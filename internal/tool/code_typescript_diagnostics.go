package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teatak/pudding-core/internal/lsp"
)

// TypeScript publishes syntax and semantic diagnostics separately. An initial
// empty publish is not a completed type check; use its documented tsserver
// command bridge to obtain each completed diagnostic kind without a timing race.
func (r *BuiltinRunner) typeScriptDocumentDiagnostics(ctx context.Context, target resolvedCodeTarget, uri string) ([]lsp.Diagnostic, error) {
	var diagnostics []lsp.Diagnostic
	for _, command := range []string{"syntacticDiagnosticsSync", "semanticDiagnosticsSync", "suggestionDiagnosticsSync"} {
		var response struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Body    []struct {
				Start, End struct {
					Line, Offset int
				}
				Text, Category, Source string
				Code                   json.RawMessage
			} `json:"body"`
		}
		err := r.languageService.Request(ctx, target.spec, "workspace/executeCommand", map[string]any{
			"command": "typescript.tsserverRequest",
			"arguments": []any{command, map[string]any{"file": uri}, map[string]any{
				"executionTarget": 0, // Semantic server; await a synchronous result.
			}},
		}, &response)
		if err != nil {
			return nil, err
		}
		if !response.Success {
			return nil, fmt.Errorf("TypeScript %s failed: %s", command, response.Message)
		}
		for _, item := range response.Body {
			if item.Start.Line < 1 || item.Start.Offset < 1 || item.End.Line < 1 || item.End.Offset < 1 {
				return nil, fmt.Errorf("TypeScript %s returned an invalid diagnostic range", command)
			}
			severity := 1
			switch item.Category {
			case "warning":
				severity = 2
			case "suggestion":
				severity = 4
			}
			source := item.Source
			if source == "" {
				source = "typescript"
			}
			diagnostics = append(diagnostics, lsp.Diagnostic{
				Range: lsp.Range{
					Start: lsp.Position{Line: item.Start.Line - 1, Character: item.Start.Offset - 1},
					End:   lsp.Position{Line: item.End.Line - 1, Character: item.End.Offset - 1},
				},
				Severity: severity, Code: item.Code, Source: source, Message: item.Text,
			})
		}
	}
	return diagnostics, nil
}
