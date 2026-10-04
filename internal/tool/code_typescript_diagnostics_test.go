package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/lsp"
)

func TestTypeScriptDiagnosticsWaitsForCompleteSynchronousResults(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true}}`)
	writeCodeTestFile(t, filepath.Join(root, "中文.ts"), "export const broken: string = 1\n")
	var commands []string
	service := &fakeCodeLanguageService{}
	service.request = func(method string, params, result any) error {
		if method == "textDocument/diagnostic" {
			return &lsp.ResponseError{Code: -32601, Message: "unsupported"}
		}
		if method != "workspace/executeCommand" {
			t.Fatalf("unexpected method: %s", method)
		}
		request := params.(map[string]any)
		args := request["arguments"].([]any)
		if request["command"] != "typescript.tsserverRequest" || args[2].(map[string]any)["executionTarget"] != 0 {
			t.Fatalf("not a synchronous semantic server request: %+v", request)
		}
		if args[1].(map[string]any)["file"] != serviceDocumentURI(root, "中文.ts") {
			t.Fatalf("wrong document: %+v", args[1])
		}
		command := args[0].(string)
		commands = append(commands, command)
		diagnostics := []any{}
		if command == "semanticDiagnosticsSync" {
			diagnostics = append(diagnostics, map[string]any{
				"start": map[string]int{"line": 1, "offset": 14}, "end": map[string]int{"line": 1, "offset": 20},
				"text": "Type 'number' is not assignable to type 'string'.", "category": "error", "code": 2322,
			})
		}
		return assignCodeTestResult(result, map[string]any{"success": true, "body": diagnostics})
	}
	// The observed server first publishes empty syntax diagnostics, then type errors.
	service.published = func(uri string, _ uint64) (lsp.DiagnosticSnapshot, bool, error) {
		return lsp.DiagnosticSnapshot{URI: uri, Generation: 1}, true, nil
	}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{Name: CodeDiagnostics,
		Args: json.RawMessage(`{"scope":"project","paths":["中文.ts"],"severity":["error"]}`), ProjectDirs: []string{root}})
	payload := decodeToolResult(t, result)
	if !result.Ok || payload["diagnosticCount"] != float64(1) || payload["fresh"] != true {
		t.Fatalf("incomplete diagnostics: %s", result.Content)
	}
	if !reflect.DeepEqual(commands, []string{"syntacticDiagnosticsSync", "semanticDiagnosticsSync", "suggestionDiagnosticsSync"}) {
		t.Fatalf("diagnostic kinds: %v", commands)
	}
	diagnostic := payload["diagnostics"].([]any)[0].(map[string]any)
	if diagnostic["line"] != float64(1) || diagnostic["column"] != float64(14) || diagnostic["endColumn"] != float64(20) || diagnostic["source"] != "typescript" || diagnostic["code"] != "2322" {
		t.Fatalf("converted diagnostic: %+v", diagnostic)
	}
}

func serviceDocumentURI(root, name string) string {
	resolved, _ := filepath.EvalSymlinks(filepath.Join(root, name))
	return codeFileURI(resolved)
}

func TestTypeScriptDiagnosticsDoesNotFallBackOnFailedSynchronousRequest(t *testing.T) {
	service := &fakeCodeLanguageService{request: func(string, any, any) error {
		return &lsp.ResponseError{Code: -32601, Message: "unsupported command"}
	}}
	service.published = func(string, uint64) (lsp.DiagnosticSnapshot, bool, error) {
		t.Fatal("failed synchronous request must not become a clean published snapshot")
		return lsp.DiagnosticSnapshot{}, false, nil
	}
	runner := testCodeRunner(service)
	_, fresh, err := runner.codeDocumentDiagnostics(context.Background(), resolvedCodeTarget{language: "typescript"}, "file:///project/main.ts", lsp.DocumentState{})
	if err == nil || fresh {
		t.Fatalf("failure reported as fresh: %v, %v", fresh, err)
	}
}

func TestTypeScriptDiagnosticsCleanFileAndDiagnosticKinds(t *testing.T) {
	for _, clean := range []bool{true, false} {
		t.Run(fmt.Sprintf("clean=%v", clean), func(t *testing.T) {
			service := &fakeCodeLanguageService{}
			service.request = func(_ string, params, result any) error {
				command := params.(map[string]any)["arguments"].([]any)[0].(string)
				category := map[string]string{"syntacticDiagnosticsSync": "error", "semanticDiagnosticsSync": "warning", "suggestionDiagnosticsSync": "suggestion"}[command]
				items := []any{}
				if !clean {
					items = append(items, map[string]any{"start": map[string]int{"line": 2, "offset": 3}, "end": map[string]int{"line": 2, "offset": 4}, "text": command, "category": category, "source": "plugin"})
				}
				return assignCodeTestResult(result, map[string]any{"success": true, "body": items})
			}
			items, fresh, err := testCodeRunner(service).codeDocumentDiagnostics(context.Background(), resolvedCodeTarget{language: "typescript"}, "file:///project/main.ts", lsp.DocumentState{})
			if err != nil || !fresh || clean && len(items) != 0 || !clean && len(items) != 3 {
				t.Fatalf("diagnostics: %+v, %v, %v", items, fresh, err)
			}
			if !clean {
				for i, severity := range []int{1, 2, 4} {
					if items[i].Severity != severity || items[i].Source != "plugin" || items[i].Range.Start.Line != 1 || items[i].Range.Start.Character != 2 {
						t.Fatalf("diagnostic %d: %+v", i, items[i])
					}
				}
			}
		})
	}
}
