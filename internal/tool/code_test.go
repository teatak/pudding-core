package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestBundledLanguageServersTakePriority(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable fixture uses a POSIX file mode")
	}
	appRoot := t.TempDir()
	daemon := filepath.Join(appRoot, "bin", "puddingd")
	writeCodeTestFile(t, daemon, "daemon")
	bundledGopls := filepath.Join(appRoot, bundledLanguageServersDir, "gopls")
	bundledTypeScript := filepath.Join(appRoot, bundledLanguageServersDir, typeScriptServerKind)
	for _, executable := range []string{bundledGopls, bundledTypeScript} {
		writeCodeTestFile(t, executable, "#!/bin/sh\nexit 0\n")
		if err := os.Chmod(executable, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bundledGopls, err := filepath.EvalSymlinks(bundledGopls)
	if err != nil {
		t.Fatal(err)
	}
	bundledTypeScript, err = filepath.EvalSymlinks(bundledTypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if got := bundledLanguageServerPathForExecutable(daemon, "gopls"); got != bundledGopls {
		t.Fatalf("bundled gopls = %q", got)
	}
	if got := bundledLanguageServerPathForExecutable(daemon, typeScriptServerKind); got != bundledTypeScript {
		t.Fatalf("bundled TypeScript server = %q", got)
	}

	languageRoot := t.TempDir()
	goSpec, err := resolveGoServer(languageRoot, bundledGopls)
	if err != nil {
		t.Fatal(err)
	}
	if goSpec.Command != bundledGopls {
		t.Fatalf("gopls command = %q", goSpec.Command)
	}

	projectRoot := t.TempDir()
	localTypeScript := filepath.Join(projectRoot, "node_modules", ".bin", typeScriptServerKind)
	writeCodeTestFile(t, localTypeScript, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(localTypeScript, 0o700); err != nil {
		t.Fatal(err)
	}
	typeScriptSpec, err := resolveTypeScriptServer(projectRoot, projectRoot, bundledTypeScript)
	if err != nil {
		t.Fatal(err)
	}
	if typeScriptSpec.Command != bundledTypeScript {
		t.Fatalf("TypeScript command = %q", typeScriptSpec.Command)
	}
}

func TestBundledLanguageServerNames(t *testing.T) {
	if got := bundledLanguageServerName("gopls", "windows"); got != "gopls.exe" {
		t.Fatalf("Windows gopls name = %q", got)
	}
	if got := bundledLanguageServerName(typeScriptServerKind, "windows"); got != typeScriptServerKind+".cmd" {
		t.Fatalf("Windows TypeScript server name = %q", got)
	}
	if got := bundledLanguageServerName("gopls", "linux"); got != "gopls" {
		t.Fatalf("Linux gopls name = %q", got)
	}
}

func TestResolveTypeScriptLanguageRootPrecedence(t *testing.T) {
	t.Run("tsconfig before closer jsconfig", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "packages", "app")
		writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), `{}`)
		writeCodeTestFile(t, filepath.Join(nested, "jsconfig.json"), `{}`)
		got, fallback := resolveTypeScriptLanguageRoot(nested, root)
		if got != root || fallback {
			t.Fatalf("root = %q, fallback = %v", got, fallback)
		}
	})
	t.Run("nearest jsconfig", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, "packages", "app")
		writeCodeTestFile(t, filepath.Join(root, "jsconfig.json"), `{}`)
		writeCodeTestFile(t, filepath.Join(nested, "jsconfig.json"), `{}`)
		got, fallback := resolveTypeScriptLanguageRoot(nested, root)
		if got != nested || fallback {
			t.Fatalf("root = %q, fallback = %v", got, fallback)
		}
	})
	t.Run("package and fallback", func(t *testing.T) {
		root := t.TempDir()
		packageDir := filepath.Join(root, "web")
		writeCodeTestFile(t, filepath.Join(packageDir, "package.json"), `{}`)
		got, fallback := resolveTypeScriptLanguageRoot(filepath.Join(packageDir, "src"), root)
		if got != packageDir || fallback {
			t.Fatalf("package root = %q, fallback = %v", got, fallback)
		}
		other := filepath.Join(root, "other")
		if err := os.MkdirAll(other, 0o700); err != nil {
			t.Fatal(err)
		}
		got, fallback = resolveTypeScriptLanguageRoot(other, root)
		if got != root || !fallback {
			t.Fatalf("fallback root = %q, fallback = %v", got, fallback)
		}
	})
}

func TestInferCodeDirectoryLanguageUsesNearestMarkers(t *testing.T) {
	root := t.TempDir()
	web := filepath.Join(root, "web")
	writeCodeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/mixed\n")
	writeCodeTestFile(t, filepath.Join(root, "package.json"), `{}`)
	writeCodeTestFile(t, filepath.Join(web, "package.json"), `{}`)
	language, err := inferCodeDirectoryLanguage(web, root)
	if err != nil || language != "typescript" {
		t.Fatalf("web language = %q, err = %v", language, err)
	}
	if _, err := inferCodeDirectoryLanguage(root, root); err == nil || !strings.Contains(err.Error(), "language_ambiguous") {
		t.Fatalf("mixed root should be ambiguous: %v", err)
	}
}

func TestTypeScriptServerResolverPrefersProjectLocalBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable fixture uses a POSIX file mode")
	}
	projectRoot := t.TempDir()
	languageRoot := filepath.Join(projectRoot, "packages", "app")
	localServer := filepath.Join(projectRoot, "node_modules", ".bin", typeScriptServerKind)
	writeCodeTestFile(t, localServer, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(localServer, 0o700); err != nil {
		t.Fatal(err)
	}
	pathServerDir := t.TempDir()
	writeCodeTestFile(t, filepath.Join(pathServerDir, typeScriptServerKind), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(pathServerDir, typeScriptServerKind), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathServerDir)
	spec, err := defaultTypeScriptServerResolver(languageRoot, projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != localServer || len(spec.Args) != 1 || spec.Args[0] != "--stdio" {
		t.Fatalf("unexpected local server spec: %+v", spec)
	}
	candidates := typeScriptServerCandidates(languageRoot, projectRoot)
	if len(candidates) != 3 || candidates[0] != filepath.Join(languageRoot, "node_modules", ".bin", typeScriptServerKind) || candidates[2] != localServer {
		t.Fatalf("candidate order: %+v", candidates)
	}
}

func TestCodeLanguageForTypeScriptAndJavaScriptPaths(t *testing.T) {
	tests := map[string]string{
		"app.ts":  "typescript",
		"app.tsx": "typescriptreact",
		"app.js":  "javascript",
		"app.jsx": "javascriptreact",
		"app.mts": "typescript",
		"app.cjs": "javascript",
	}
	for path, documentID := range tests {
		language, gotDocumentID := codeLanguageForPath(path)
		if language != "typescript" || gotDocumentID != documentID {
			t.Fatalf("%s => %s/%s", path, language, gotDocumentID)
		}
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

func TestCodeReferencesBoundsCanonicalResultSize(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/bounded\n")
	var sourceText strings.Builder
	for index := 0; index < maxCodeReferences+10; index++ {
		sourceText.WriteString(strings.Repeat("x", maxFileSearchExcerptLineChars))
		sourceText.WriteByte('\n')
	}
	source := filepath.Join(root, "main.go")
	writeCodeTestFile(t, source, sourceText.String())
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedSource := filepath.Join(resolvedRoot, "main.go")
	service := &fakeCodeLanguageService{encoding: "utf-16"}
	service.request = func(method string, _ any, result any) error {
		if method != "textDocument/references" {
			t.Fatalf("unexpected method %s", method)
		}
		locations := make([]map[string]any, 0, maxCodeReferences)
		for line := 0; line < maxCodeReferences; line++ {
			locations = append(locations, testLocation(resolvedSource, line, 0, 1))
		}
		return assignCodeTestResult(result, locations)
	}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{
		Name:        CodeReferences,
		Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":1,"column":1,"max_results":500}`),
		ProjectDirs: []string{resolvedRoot},
	})
	if !result.Ok {
		t.Fatalf("references failed: %s", result.Content)
	}
	if len(result.Content) > maxCodeResultBytes {
		t.Fatalf("result size = %d", len(result.Content))
	}
	payload := decodeToolResult(t, result)
	if payload["truncated"] != true || payload["locationCount"].(float64) >= maxCodeReferences {
		t.Fatalf("result was not size bounded: count=%v truncated=%v", payload["locationCount"], payload["truncated"])
	}
}

func TestCodeLocationOutsideFileRangeDoesNotPanic(t *testing.T) {
	root, source := codeTestProject(t)
	service := &fakeCodeLanguageService{encoding: "utf-16"}
	service.request = func(_ string, _ any, result any) error {
		return assignCodeTestResult(result, testLocation(source, 10_000, 0, 1))
	}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{
		Name:        CodeDefinition,
		Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":1,"column":1}`),
		ProjectDirs: []string{root},
	})
	if !result.Ok {
		t.Fatalf("definition failed: %s", result.Content)
	}
	payload := decodeToolResult(t, result)
	location := payload["locations"].([]any)[0].(map[string]any)
	if location["line"] != float64(10_001) || location["excerpt"] != nil {
		t.Fatalf("unexpected out-of-range location: %+v", location)
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

func TestTypeScriptDefinitionUsesUnifiedToolAndDocumentLanguage(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "package.json"), `{"private":true}`)
	source := filepath.Join(root, "app.tsx")
	line := "export const use = () => target()"
	writeCodeTestFile(t, source, "export function target() { return 1 }\n"+line+"\n")
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedSource := filepath.Join(resolvedRoot, "app.tsx")
	column := len([]rune(line[:strings.Index(line, "target")])) + 1
	service := &fakeCodeLanguageService{encoding: "utf-16"}
	service.sync = func(document lsp.Document) (lsp.DocumentState, error) {
		if document.LanguageID != "typescriptreact" {
			t.Fatalf("document language = %s", document.LanguageID)
		}
		return lsp.DocumentState{URI: document.URI, Version: 1, Changed: true, PositionEncoding: "utf-16"}, nil
	}
	service.request = func(method string, _ any, result any) error {
		if method != "textDocument/definition" {
			t.Fatalf("unexpected method %s", method)
		}
		return assignCodeTestResult(result, testLocation(resolvedSource, 0, 16, 22))
	}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{
		Name:        CodeDefinition,
		Args:        json.RawMessage(fmt.Sprintf(`{"scope":"project","path":"app.tsx","line":2,"column":%d}`, column)),
		ProjectDirs: []string{resolvedRoot},
	})
	if !result.Ok {
		t.Fatalf("TypeScript definition failed: %s", result.Content)
	}
	payload := decodeToolResult(t, result)
	if payload["language"] != "typescript" || payload["server"] != typeScriptServerKind || payload["locationCount"] != float64(1) {
		t.Fatalf("unexpected TypeScript result: %+v", payload)
	}
}

func TestTypeScriptSymbolsOpenSeedDocumentBeforeWorkspaceQuery(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "package.json"), `{"private":true}`)
	writeCodeTestFile(t, filepath.Join(root, "tsconfig.json"), `{"include":["*.ts"]}`)
	source := filepath.Join(root, "main.ts")
	writeCodeTestFile(t, source, "export function target() { return 1 }\n")
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedSource := filepath.Join(resolvedRoot, "main.ts")
	synced := false
	service := &fakeCodeLanguageService{encoding: "utf-16"}
	service.sync = func(document lsp.Document) (lsp.DocumentState, error) {
		if document.URI != codeFileURI(resolvedSource) || document.LanguageID != "typescript" {
			t.Fatalf("seed document = %+v", document)
		}
		synced = true
		return lsp.DocumentState{URI: document.URI, Version: 1, Changed: true, PositionEncoding: "utf-16"}, nil
	}
	service.request = func(method string, _ any, result any) error {
		if method != "workspace/symbol" || !synced {
			t.Fatalf("workspace symbols requested before seed sync: method=%s synced=%v", method, synced)
		}
		return assignCodeTestResult(result, []map[string]any{{
			"name": "target", "kind": 12, "location": testLocation(resolvedSource, 0, 16, 22),
		}})
	}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{
		Name:        CodeSymbols,
		Args:        json.RawMessage(`{"scope":"project","path":".","language":"typescript","query":"target"}`),
		ProjectDirs: []string{resolvedRoot},
	})
	if !result.Ok {
		t.Fatalf("TypeScript symbols failed: %s", result.Content)
	}
	payload := decodeToolResult(t, result)
	if payload["resultCount"] != float64(1) {
		t.Fatalf("unexpected symbols payload: %+v", payload)
	}
}

func TestTypeScriptToolReportsUnavailableServer(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, filepath.Join(root, "package.json"), `{"private":true}`)
	t.Setenv("PATH", t.TempDir())
	runner := NewBuiltinRunner(WithLanguageService(&fakeCodeLanguageService{}))
	result := runner.Call(context.Background(), Call{
		Name:        CodeSymbols,
		Args:        json.RawMessage(`{"scope":"project","path":".","language":"typescript","query":"target"}`),
		ProjectDirs: []string{root},
	})
	if result.Ok {
		t.Fatalf("unavailable TypeScript server should fail: %+v", result)
	}
	payload := decodeToolResult(t, result)
	if payload["reason"] != "language_server_unavailable" || payload["language"] != "typescript" || payload["server"] != typeScriptServerKind {
		t.Fatalf("unexpected unavailable payload: %+v", payload)
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
		WithTypeScriptServerResolver(func(root, _ string) (lsp.ServerSpec, error) {
			return lsp.ServerSpec{Key: lsp.ProcessKey{LanguageRoot: root, ServerKind: typeScriptServerKind}, Command: "fake", Dir: root}, nil
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
