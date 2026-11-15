package tool

import (
	"context"
	"fmt"

	"github.com/teatak/pudding-core/internal/lsp"
)

// TLS workspace/symbol supplies an arbitrary open file to navto, limiting the
// search to that file's project. A root config script may belong to an inferred
// project rather than the application's tsconfig. Omitting file lets tsserver
// load the ancestor project tree (including references) and search its projects.
func (r *BuiltinRunner) typeScriptWorkspaceSymbols(ctx context.Context, target resolvedCodeTarget, query string) ([]rawCodeSymbol, error) {
	var response struct {
		Success bool
		Message string
		Body    []struct {
			Name, Kind, ContainerName, File string
			Start, End                      struct{ Line, Offset int }
		}
	}
	err := r.codeLanguageService(ctx).Request(ctx, target.spec, "workspace/executeCommand", map[string]any{
		"command":   "typescript.tsserverRequest",
		"arguments": []any{"navto", map[string]any{"searchValue": query}, map[string]any{"executionTarget": 0}},
	}, &response)
	if err != nil {
		return nil, err
	}
	if !response.Success {
		return nil, fmt.Errorf("TypeScript navto failed: %s", response.Message)
	}
	symbols := make([]rawCodeSymbol, 0, len(response.Body))
	for _, item := range response.Body {
		if item.Start.Line < 1 || item.Start.Offset < 1 || item.End.Line < 1 || item.End.Offset < 1 {
			return nil, fmt.Errorf("TypeScript navto returned an invalid symbol range")
		}
		symbols = append(symbols, rawCodeSymbol{
			Name: item.Name, Kind: typeScriptSymbolKind(item.Kind), ContainerName: item.ContainerName,
			Location: rawLSPLocation{URI: codeFileURI(item.File), Range: lsp.Range{
				Start: lsp.Position{Line: item.Start.Line - 1, Character: item.Start.Offset - 1},
				End:   lsp.Position{Line: item.End.Line - 1, Character: item.End.Offset - 1},
			}},
		})
	}
	return symbols, nil
}

// Match the SymbolKind conversion used by typescript-language-server.
func typeScriptSymbolKind(kind string) int {
	switch kind {
	case "file":
		return 1
	case "module":
		return 2
	case "class", "local class":
		return 5
	case "method", "getter", "setter":
		return 6
	case "property", "JSX attribute":
		return 7
	case "field":
		return 8
	case "constructor":
		return 9
	case "enum":
		return 10
	case "interface":
		return 11
	case "function", "local function":
		return 12
	case "const", "enum member":
		return 14
	default:
		return 13
	}
}
