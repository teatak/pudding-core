package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/lsp"
)

func TestCodeToolsWithGopls(t *testing.T) {
	if os.Getenv("PUDDING_LSP_INTEGRATION") != "1" {
		t.Skip("set PUDDING_LSP_INTEGRATION=1 to test the installed gopls")
	}
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/integration\n\ngo 1.25\n")
	writeCodeTestFile(t, filepath.Join(root, "main.go"), "package demo\n\nfunc Target() {}\n\nfunc Use() { Target() }\n\nfunc Broken() { Missing() }\n")
	manager := lsp.NewManager(lsp.WithIdleTimeout(0), lsp.WithReapInterval(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Close(ctx)
	})
	runner := NewBuiltinRunner(WithLanguageService(manager))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, call := range []Call{
		{Name: CodeSymbols, Args: json.RawMessage(`{"scope":"project","path":".","query":"Target"}`), ProjectDirs: []string{root}},
		{Name: CodeDefinition, Args: json.RawMessage(`{"scope":"project","path":"main.go","line":5,"column":14}`), ProjectDirs: []string{root}},
		{Name: CodeReferences, Args: json.RawMessage(`{"scope":"project","path":"main.go","line":5,"column":14}`), ProjectDirs: []string{root}},
		{Name: CodeDiagnostics, Args: json.RawMessage(`{"scope":"project","paths":["main.go"],"severity":["error"]}`), ProjectDirs: []string{root}},
	} {
		result := runner.Call(ctx, call)
		if !result.Ok {
			t.Fatalf("%s failed: %s", call.Name, result.Content)
		}
		payload := decodeToolResult(t, result)
		count := payload["resultCount"]
		if call.Name != CodeSymbols {
			count = payload["locationCount"]
		}
		if call.Name == CodeDiagnostics {
			count = payload["diagnosticCount"]
		}
		if number, ok := count.(float64); !ok || number < 1 {
			t.Fatalf("%s returned no semantic results: %+v", call.Name, payload)
		}
	}
	assertRealCodeRename(t, ctx, runner, root, "main.go", 5, 14, "Target", "RenamedTarget")
}

func TestCodeSymbolsTypeScriptConfiguredProjects(t *testing.T) {
	if os.Getenv("PUDDING_LSP_TS_INTEGRATION") != "1" {
		t.Skip("set PUDDING_LSP_TS_INTEGRATION=1 to test the installed TypeScript language server")
	}
	for _, references := range []bool{false, true} {
		name := "include"
		if references {
			name = "references"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeCodeTestFile(t, filepath.Join(root, "eslint.config.js"), "export default []\n")
			writeCodeTestFile(t, filepath.Join(root, "src/main.ts"), "export function BusinessNeedle() { return 42 }\n")
			config := `{"compilerOptions":{"strict":true},"include":["src/**/*.ts"]}`
			if references {
				config = `{"files":[],"references":[{"path":"./src"}]}`
				writeCodeTestFile(t, filepath.Join(root, "src/tsconfig.json"), `{"compilerOptions":{"composite":true},"include":["*.ts"]}`)
			}
			writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), config)
			manager := lsp.NewManager(lsp.WithIdleTimeout(0), lsp.WithReapInterval(0))
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = manager.Close(ctx)
			})
			runner := NewBuiltinRunner(WithLanguageService(manager))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// Repeat to cover a warm server whose first open document is still the config script.
			for range 2 {
				result := runner.Call(ctx, Call{Name: CodeSymbols, Args: json.RawMessage(`{"scope":"project","path":".","language":"typescript","query":"BusinessNeedle"}`), ProjectDirs: []string{root}})
				payload := decodeToolResult(t, result)
				if !result.Ok || payload["resultCount"] != float64(1) {
					t.Fatalf("business symbol missing: %s", result.Content)
				}
				symbol := payload["symbols"].([]any)[0].(map[string]any)
				if symbol["relativePath"] != "src/main.ts" || symbol["kind"] != "function" || symbol["line"] != float64(1) {
					t.Fatalf("incorrect symbol: %+v", symbol)
				}
			}
		})
	}
}

func TestCodeToolsWithTypeScriptLanguageServer(t *testing.T) {
	if os.Getenv("PUDDING_LSP_TS_INTEGRATION") != "1" {
		t.Skip("set PUDDING_LSP_TS_INTEGRATION=1 to test the installed TypeScript language server")
	}
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "package.json"), `{"private":true,"devDependencies":{"typescript":"*"}}`)
	writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true},"include":["*.ts"]}`)
	writeCodeTestFile(t, filepath.Join(root, "main.ts"), "export function target(): number { return 1 }\nexport const use = target()\nexport const broken: string = 1\n")
	manager := lsp.NewManager(lsp.WithIdleTimeout(0), lsp.WithReapInterval(0))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Close(ctx)
	})
	runner := NewBuiltinRunner(WithLanguageService(manager))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, call := range []Call{
		{Name: CodeSymbols, Args: json.RawMessage(`{"scope":"project","path":".","language":"typescript","query":"target"}`), ProjectDirs: []string{root}},
		{Name: CodeDefinition, Args: json.RawMessage(`{"scope":"project","path":"main.ts","line":2,"column":20}`), ProjectDirs: []string{root}},
		{Name: CodeReferences, Args: json.RawMessage(`{"scope":"project","path":"main.ts","line":2,"column":20}`), ProjectDirs: []string{root}},
		{Name: CodeDiagnostics, Args: json.RawMessage(`{"scope":"project","paths":["main.ts"],"severity":["error"]}`), ProjectDirs: []string{root}},
	} {
		result := runner.Call(ctx, call)
		if !result.Ok {
			t.Fatalf("%s failed: %s", call.Name, result.Content)
		}
		payload := decodeToolResult(t, result)
		count := payload["resultCount"]
		if call.Name != CodeSymbols {
			count = payload["locationCount"]
		}
		if call.Name == CodeDiagnostics {
			count = payload["diagnosticCount"]
		}
		if number, ok := count.(float64); !ok || number < 1 {
			t.Fatalf("%s returned no semantic results: %+v", call.Name, payload)
		}
	}
	assertRealCodeRename(t, ctx, runner, root, "main.ts", 2, 20, "target", "renamedTarget")
	// A completed clean check must replace the earlier error, not reuse a push
	// snapshot (which may still contain the previous document's diagnostics).
	writeCodeTestFile(t, filepath.Join(root, "main.ts"), "export const fixed: string = 'ok'\n")
	result := runner.Call(ctx, Call{Name: CodeDiagnostics, Args: json.RawMessage(`{"scope":"project","paths":["main.ts"]}`), ProjectDirs: []string{root}})
	payload := decodeToolResult(t, result)
	if !result.Ok || payload["diagnosticCount"] != float64(0) || payload["fresh"] != true {
		t.Fatalf("clean TypeScript check reused old diagnostics: %s", result.Content)
	}
}

func assertRealCodeRename(t *testing.T, ctx context.Context, runner *BuiltinRunner, root, path string, line, column int, oldName, newName string) {
	t.Helper()
	target := filepath.Join(root, path)
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	rawArgs, _ := json.Marshal(map[string]any{
		"scope":    "project",
		"path":     path,
		"line":     line,
		"column":   column,
		"new_name": newName,
	})
	rename := runner.Call(ctx, Call{
		SessionID:   "session_integration",
		TurnID:      "turn_integration",
		CallID:      "call_rename",
		Name:        CodeRename,
		Args:        rawArgs,
		ProjectDirs: []string{root},
	})
	if !rename.Ok {
		t.Fatalf("real %s rename failed: %s", filepath.Ext(path), rename.Content)
	}
	payload := decodeToolResult(t, rename)
	if payload["operation"] != "rename" || payload["newName"] != newName {
		t.Fatalf("unexpected real rename payload: %+v", payload)
	}
	afterApply, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.ReplaceAll(string(before), oldName, newName)
	if string(afterApply) != expected || strings.Count(string(afterApply), newName) != 2 {
		t.Fatalf("real rename did not update declaration and reference: %q", afterApply)
	}
	symbolArgs, _ := json.Marshal(map[string]any{"scope": "project", "path": path, "query": newName})
	symbols := runner.Call(ctx, Call{Name: CodeSymbols, Args: symbolArgs, ProjectDirs: []string{root}})
	symbolPayload := decodeToolResult(t, symbols)
	symbolCount, ok := symbolPayload["resultCount"].(float64)
	if !symbols.Ok || !ok || symbolCount < 1 {
		t.Fatalf("symbols retained the pre-rename overlay: %s", symbols.Content)
	}
}

func TestCodeCrossFileDiagnosticsWithLanguageServers(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		dependency  string
		dependent   string
		before      string
		after       string
		use         string
	}{
		{
			name: "Go", environment: "PUDDING_LSP_INTEGRATION", dependency: "a.go", dependent: "b.go",
			before: "package demo\nfunc Value() int { return 1 }\n",
			after:  "package demo\nfunc Value() string { return \"x\" }\n",
			use:    "package demo\nvar X int = Value()\n",
		},
		{
			name: "TypeScript", environment: "PUDDING_LSP_TS_INTEGRATION", dependency: "a.ts", dependent: "b.ts",
			before: "export function value(): number { return 1 }\n",
			after:  "export function value(): string { return 'x' }\n",
			use:    "import { value } from './a'\nexport const x: number = value()\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if os.Getenv(test.environment) != "1" {
				t.Skip("set " + test.environment + "=1 to test the installed language server")
			}
			root := t.TempDir()
			if test.name == "Go" {
				writeCodeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/crossfile\n\ngo 1.25\n")
			} else {
				writeCodeTestFile(t, filepath.Join(root, "package.json"), `{"private":true}`)
				writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions":{"strict":true},"include":["*.ts"]}`)
			}
			writeCodeTestFile(t, filepath.Join(root, test.dependency), test.before)
			writeCodeTestFile(t, filepath.Join(root, test.dependent), test.use)
			manager := lsp.NewManager(lsp.WithIdleTimeout(0), lsp.WithReapInterval(0))
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = manager.Close(ctx)
			})
			runner := NewBuiltinRunner(WithLanguageService(manager))
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			check := func(paths []string, wantError bool) {
				t.Helper()
				args, _ := json.Marshal(map[string]any{"scope": "project", "paths": paths, "severity": []string{"error"}})
				result := runner.Call(ctx, Call{Name: CodeDiagnostics, Args: args, ProjectDirs: []string{root}})
				payload := decodeToolResult(t, result)
				count, ok := payload["diagnosticCount"].(float64)
				if !result.Ok || !ok || payload["fresh"] != true || (count > 0) != wantError {
					t.Fatalf("cross-file diagnostics, want error=%v: %s", wantError, result.Content)
				}
			}
			check([]string{test.dependency, test.dependent}, false)
			writeCodeTestFile(t, filepath.Join(root, test.dependency), test.after)
			check([]string{test.dependent}, true)
			writeCodeTestFile(t, filepath.Join(root, test.dependency), test.before)
			check([]string{test.dependent, test.dependency}, false)
		})
	}
}
