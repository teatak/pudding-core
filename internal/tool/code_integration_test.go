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
	afterProposal, err := os.ReadFile(target)
	if err != nil || string(afterProposal) != string(before) {
		t.Fatalf("real rename changed source before apply: err=%v", err)
	}
	proposalID, _ := payload["proposalID"].(string)
	applyArgs, _ := json.Marshal(map[string]string{"proposal_id": proposalID})
	applied := runner.Call(ctx, Call{
		SessionID:   "session_integration",
		TurnID:      "turn_integration",
		CallID:      "call_apply",
		Name:        PatchApply,
		Args:        applyArgs,
		ProjectDirs: []string{root},
	})
	if !applied.Ok {
		t.Fatalf("real %s rename apply failed: %s", filepath.Ext(path), applied.Content)
	}
	afterApply, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.ReplaceAll(string(before), oldName, newName)
	if string(afterApply) != expected || strings.Count(string(afterApply), newName) != 2 {
		t.Fatalf("real rename did not update declaration and reference: %q", afterApply)
	}
}
