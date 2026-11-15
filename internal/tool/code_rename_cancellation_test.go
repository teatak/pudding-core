package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/lsp"
)

func TestCodeRenameCancellationDuringRestoreDoesNotWriteFiles(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), `{"include":["*.ts"]}`)
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "main.ts")
	before := "export const old = 1;\n"
	after := "export const renamed = 1;\n"
	writeCodeTestFile(t, path, before)
	start := strings.Index(before, "old")
	rangeValue := lsp.Range{Start: lsp.Position{Character: start}, End: lsp.Position{Character: start + len("old")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	previewed, restored := false, false
	currentOverlay := before
	version := 0
	service := &fakeCodeLanguageService{}
	service.sync = func(document lsp.Document) (lsp.DocumentState, error) {
		currentOverlay = document.Text
		if strings.Contains(document.Text, after) {
			previewed = true
		}
		if previewed && document.Text == before {
			// All validation requests succeeded. Cancellation arrives during
			// the independent restore, which must still finish successfully.
			restored = true
			cancel()
		}
		version++
		return lsp.DocumentState{URI: document.URI, Version: version, Changed: true, PositionEncoding: "utf-16"}, nil
	}
	service.request = func(method string, _ any, result any) error {
		switch method {
		case "textDocument/prepareRename":
			return assignCodeTestResult(result, map[string]any{"range": rangeValue, "placeholder": "old"})
		case "textDocument/rename":
			return assignCodeTestResult(result, map[string]any{"changes": map[string]any{codeFileURI(path): []any{
				map[string]any{"range": rangeValue, "newText": "renamed"},
			}}})
		case "workspace/executeCommand":
			return assignCodeTestResult(result, map[string]any{"success": true, "body": []any{}})
		default:
			t.Fatalf("unexpected language-service request %q", method)
			return nil
		}
	}
	runner := testCodeRunner(service)
	t.Cleanup(func() { _ = runner.Close() })
	args, err := json.Marshal(map[string]any{"scope": "project", "path": "main.ts", "line": 1, "column": start + 1, "new_name": "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	result := runner.Call(ctx, Call{SessionID: "cancel-rename", Name: CodeRename, Args: args, ProjectDirs: []string{root}})
	if !previewed || !restored || currentOverlay != before || ctx.Err() != context.Canceled {
		t.Fatalf("did not reach successful restore after cancellation: previewed=%v restored=%v overlay=%q context=%v", previewed, restored, currentOverlay, ctx.Err())
	}
	payload := decodeToolResult(t, result)
	if result.Ok || payload["ok"] != false || payload["reason"] != "cancelled" || payload["status"] == "applied" {
		t.Fatalf("canceled rename did not report structured cancellation: %s", result.Content)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != before {
		t.Fatalf("cancellation after validation changed disk: %q, %v", content, err)
	}
}
