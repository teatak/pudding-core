package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
}
