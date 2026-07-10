package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/lsp"
)

type fakeCodeLanguageService struct {
	request   func(method string, params, result any) error
	sync      func(document lsp.Document) (lsp.DocumentState, error)
	published func(uri string, after uint64) (lsp.DiagnosticSnapshot, bool, error)
	encoding  string
}

func (f *fakeCodeLanguageService) Request(_ context.Context, _ lsp.ServerSpec, method string, params, result any) error {
	if f.request == nil {
		return nil
	}
	return f.request(method, params, result)
}

func (f *fakeCodeLanguageService) SyncDocument(_ context.Context, _ lsp.ServerSpec, document lsp.Document) (lsp.DocumentState, error) {
	if f.sync != nil {
		return f.sync(document)
	}
	encoding := f.encoding
	if encoding == "" {
		encoding = "utf-16"
	}
	return lsp.DocumentState{URI: document.URI, Version: 1, Changed: true, PositionEncoding: encoding}, nil
}

func (f *fakeCodeLanguageService) PublishedDiagnostics(_ context.Context, _ lsp.ServerSpec, uri string, after uint64) (lsp.DiagnosticSnapshot, bool, error) {
	if f.published != nil {
		return f.published(uri, after)
	}
	return lsp.DiagnosticSnapshot{}, false, context.DeadlineExceeded
}

func (f *fakeCodeLanguageService) PositionEncoding(context.Context, lsp.ServerSpec) (string, error) {
	if f.encoding == "" {
		return "utf-16", nil
	}
	return f.encoding, nil
}

func TestResolveGoLanguageRootPrefersGoWorkThenNearestModule(t *testing.T) {
	t.Run("go work", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "nested")
		writeCodeTestFile(t, filepath.Join(root, "go.work"), "go 1.25\n")
		writeCodeTestFile(t, filepath.Join(nested, "go.mod"), "module example.com/nested\n")
		writeCodeTestFile(t, filepath.Join(nested, "main.go"), "package nested\n")
		got, fallback := resolveGoLanguageRoot(nested, root)
		if got != root || fallback {
			t.Fatalf("root = %q, fallback = %v", got, fallback)
		}
	})
	t.Run("nearest module", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "nested")
		writeCodeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
		writeCodeTestFile(t, filepath.Join(nested, "go.mod"), "module example.com/nested\n")
		got, fallback := resolveGoLanguageRoot(nested, root)
		if got != nested || fallback {
			t.Fatalf("root = %q, fallback = %v", got, fallback)
		}
	})
	t.Run("fallback", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "nested")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		got, fallback := resolveGoLanguageRoot(nested, root)
		if got != root || !fallback {
			t.Fatalf("root = %q, fallback = %v", got, fallback)
		}
	})
}

func TestDefaultGoServerResolverUsesOfflineEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable fixture uses a POSIX file mode")
	}
	bin := t.TempDir()
	executable := filepath.Join(bin, "gopls")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("GOPROXY", "https://proxy.example")
	t.Setenv("GOTOOLCHAIN", "auto")
	root := t.TempDir()
	spec, err := defaultGoServerResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != resolvedExecutable || len(spec.Args) != 1 || spec.Args[0] != "serve" || spec.Dir != root {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	env := strings.Join(spec.Env, "\n")
	if !strings.Contains(env, "GOPROXY=off") || !strings.Contains(env, "GOTOOLCHAIN=local") {
		t.Fatalf("offline environment missing: %s", env)
	}
}

func TestCodePositionConvertsUTFEncodings(t *testing.T) {
	lines := []string{"a😀b"}
	utf16Position, err := codePosition(lines, 1, 3, "utf-16")
	if err != nil {
		t.Fatal(err)
	}
	if utf16Position.Character != 3 || lspCharacterToColumn(lines[0], 3, "utf-16") != 3 {
		t.Fatalf("unexpected UTF-16 conversion: %+v", utf16Position)
	}
	utf8Position, err := codePosition(lines, 1, 3, "utf-8")
	if err != nil {
		t.Fatal(err)
	}
	if utf8Position.Character != 5 || lspCharacterToColumn(lines[0], 5, "utf-8") != 3 {
		t.Fatalf("unexpected UTF-8 conversion: %+v", utf8Position)
	}
}

func TestCodeSymbolsFiltersExternalLocations(t *testing.T) {
	root, source := codeTestProject(t)
	outside := filepath.Join(t.TempDir(), "outside.go")
	writeCodeTestFile(t, outside, "package outside\n")
	service := &fakeCodeLanguageService{encoding: "utf-16"}
	service.request = func(method string, _ any, result any) error {
		if method != "workspace/symbol" {
			t.Fatalf("method = %s", method)
		}
		return assignCodeTestResult(result, []map[string]any{
			{"name": "Target", "kind": 12, "containerName": "demo", "location": testLocation(source, 2, 5, 11)},
			{"name": "Outside", "kind": 12, "location": testLocation(outside, 0, 0, 7)},
		})
	}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{
		Name:        CodeSymbols,
		Args:        json.RawMessage(`{"scope":"project","path":".","query":"Target"}`),
		ProjectDirs: []string{root},
	})
	if !result.Ok {
		t.Fatalf("symbols failed: %s", result.Content)
	}
	payload := decodeToolResult(t, result)
	if payload["resultCount"] != float64(1) || payload["externalResultCount"] != float64(1) {
		t.Fatalf("unexpected counts: %+v", payload)
	}
	symbol := payload["symbols"].([]any)[0].(map[string]any)
	if symbol["name"] != "Target" || symbol["kind"] != "function" || symbol["relativePath"] != "main.go" {
		t.Fatalf("unexpected symbol: %+v", symbol)
	}
}

func TestCodeDefinitionAndReferencesUseUnifiedLocations(t *testing.T) {
	root, source := codeTestProject(t)
	outside := filepath.Join(t.TempDir(), "outside.go")
	writeCodeTestFile(t, outside, "package outside\n")
	service := &fakeCodeLanguageService{encoding: "utf-16"}
	service.request = func(method string, params, result any) error {
		switch method {
		case "textDocument/definition":
			position := params.(map[string]any)["position"].(lsp.Position)
			if position.Line != 4 || position.Character != 13 {
				t.Fatalf("definition position = %+v", position)
			}
			return assignCodeTestResult(result, testLocation(source, 2, 5, 11))
		case "textDocument/references":
			return assignCodeTestResult(result, []map[string]any{
				testLocation(source, 2, 5, 11),
				testLocation(source, 4, 13, 19),
				testLocation(source, 4, 13, 19),
				testLocation(outside, 0, 0, 1),
			})
		default:
			t.Fatalf("unexpected method %s", method)
		}
		return nil
	}
	runner := testCodeRunner(service)
	definition := runner.Call(context.Background(), Call{
		Name:        CodeDefinition,
		Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":5,"column":14}`),
		ProjectDirs: []string{root},
	})
	if !definition.Ok {
		t.Fatalf("definition failed: %s", definition.Content)
	}
	definitionPayload := decodeToolResult(t, definition)
	if definitionPayload["locationCount"] != float64(1) {
		t.Fatalf("definition payload: %+v", definitionPayload)
	}
	references := runner.Call(context.Background(), Call{
		Name:        CodeReferences,
		Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":5,"column":14}`),
		ProjectDirs: []string{root},
	})
	if !references.Ok {
		t.Fatalf("references failed: %s", references.Content)
	}
	referencePayload := decodeToolResult(t, references)
	if referencePayload["locationCount"] != float64(2) || referencePayload["externalResultCount"] != float64(1) {
		t.Fatalf("reference payload: %+v", referencePayload)
	}
}

func TestCodeDiagnosticsFallsBackToPublishedDiagnostics(t *testing.T) {
	root, source := codeTestProject(t)
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeCodeLanguageService{encoding: "utf-16"}
	service.request = func(method string, _ any, _ any) error {
		if method != "textDocument/diagnostic" {
			t.Fatalf("unexpected method %s", method)
		}
		return &lsp.ResponseError{Code: -32601, Message: "unsupported"}
	}
	service.published = func(uri string, after uint64) (lsp.DiagnosticSnapshot, bool, error) {
		if uri != codeFileURI(resolvedSource) || after != 0 {
			t.Fatalf("published request uri=%s after=%d", uri, after)
		}
		return lsp.DiagnosticSnapshot{
			URI:        uri,
			Generation: 1,
			UpdatedAt:  time.Now(),
			Diagnostics: []lsp.Diagnostic{{
				Range:    lsp.Range{Start: lsp.Position{Line: 4, Character: 13}, End: lsp.Position{Line: 4, Character: 19}},
				Severity: 2,
				Code:     json.RawMessage(`"unused"`),
				Source:   "gopls",
				Message:  "fake warning",
			}},
		}, true, nil
	}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{
		Name:        CodeDiagnostics,
		Args:        json.RawMessage(`{"scope":"project","paths":["main.go"],"severity":["warning"]}`),
		ProjectDirs: []string{root},
	})
	if !result.Ok {
		t.Fatalf("diagnostics failed: %s", result.Content)
	}
	payload := decodeToolResult(t, result)
	if payload["diagnosticCount"] != float64(1) || payload["fresh"] != true {
		t.Fatalf("diagnostic payload: %+v", payload)
	}
	diagnostic := payload["diagnostics"].([]any)[0].(map[string]any)
	if diagnostic["severity"] != "warning" || diagnostic["sourceKind"] != "lsp" || diagnostic["relativePath"] != "main.go" {
		t.Fatalf("diagnostic: %+v", diagnostic)
	}
}

func TestCodeToolReportsUnavailableServer(t *testing.T) {
	root, _ := codeTestProject(t)
	runner := NewBuiltinRunner(
		WithLanguageService(&fakeCodeLanguageService{}),
		WithGoServerResolver(func(string) (lsp.ServerSpec, error) {
			return lsp.ServerSpec{}, &languageServerUnavailableError{language: "go", server: "gopls", checked: []string{"PATH:gopls"}}
		}),
	)
	result := runner.Call(context.Background(), Call{
		Name:        CodeSymbols,
		Args:        json.RawMessage(`{"scope":"project","query":"Target"}`),
		ProjectDirs: []string{root},
	})
	if result.Ok {
		t.Fatalf("unavailable server should fail: %+v", result)
	}
	payload := decodeToolResult(t, result)
	if payload["reason"] != "language_server_unavailable" || payload["server"] != "gopls" {
		t.Fatalf("unexpected unavailable payload: %+v", payload)
	}
}

func testCodeRunner(service lsp.Service) *BuiltinRunner {
	return NewBuiltinRunner(
		WithLanguageService(service),
		WithGoServerResolver(func(root string) (lsp.ServerSpec, error) {
			return lsp.ServerSpec{Key: lsp.ProcessKey{LanguageRoot: root, ServerKind: "gopls"}, Command: "fake", Dir: root}, nil
		}),
	)
}

func codeTestProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.25\n")
	source := filepath.Join(root, "main.go")
	writeCodeTestFile(t, source, "package demo\n\nfunc Target() {}\n\nfunc Use() { Target() }\n")
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolvedRoot, filepath.Join(resolvedRoot, "main.go")
}

func writeCodeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testLocation(path string, line, start, end int) map[string]any {
	return map[string]any{
		"uri": codeFileURI(path),
		"range": map[string]any{
			"start": map[string]int{"line": line, "character": start},
			"end":   map[string]int{"line": line, "character": end},
		},
	}
}

func assignCodeTestResult(target, value any) error {
	if target == nil {
		return errors.New("result target is nil")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
