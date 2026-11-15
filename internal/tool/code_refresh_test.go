package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/lsp"
)

// This service models the relevant LSP behavior: an open document keeps its
// last client-supplied text even when the file changes on disk.
type overlayCodeLanguageService struct {
	fakeCodeLanguageService
	documents map[string]lsp.Document
	versions  map[string]int
}

func newOverlayCodeLanguageService() *overlayCodeLanguageService {
	return &overlayCodeLanguageService{documents: map[string]lsp.Document{}, versions: map[string]int{}}
}

func (f *overlayCodeLanguageService) SyncDocument(_ context.Context, _ lsp.ServerSpec, document lsp.Document) (lsp.DocumentState, error) {
	previous, exists := f.documents[document.URI]
	changed := !exists || previous.Text != document.Text || previous.LanguageID != document.LanguageID
	if changed {
		f.versions[document.URI]++
	}
	f.documents[document.URI] = document
	return lsp.DocumentState{URI: document.URI, Version: f.versions[document.URI], Changed: changed, PositionEncoding: "utf-16"}, nil
}

func (f *overlayCodeLanguageService) RefreshDocuments(ctx context.Context, spec lsp.ServerSpec, load lsp.DocumentLoader) (map[string]lsp.DocumentState, error) {
	URIs := make([]string, 0, len(f.documents))
	for uri := range f.documents {
		URIs = append(URIs, uri)
	}
	sort.Strings(URIs)
	states := map[string]lsp.DocumentState{}
	workspaceChanged := false
	for _, uri := range URIs {
		text, keep, err := load(uri)
		if err != nil {
			return nil, err
		}
		if !keep {
			delete(f.documents, uri)
			delete(f.versions, uri)
			workspaceChanged = true
			continue
		}
		document := f.documents[uri]
		document.Text = text
		state, err := f.SyncDocument(ctx, spec, document)
		if err != nil {
			return nil, err
		}
		workspaceChanged = workspaceChanged || state.Changed
		states[uri] = state
	}
	for uri, state := range states {
		state.Changed = workspaceChanged
		states[uri] = state
	}
	return states, nil
}

func TestCodeDiagnosticsRefreshesDependenciesAndSynchronizesWholeBatch(t *testing.T) {
	for _, cachedDependency := range []bool{true, false} {
		name := "new batch, dependent first"
		if cachedDependency {
			name = "dependency changed outside the query"
		}
		t.Run(name, func(t *testing.T) {
			root, _ := codeTestProject(t)
			dependency := filepath.Join(root, "a.go")
			dependent := filepath.Join(root, "b.go")
			oldText := "package demo\nfunc Value() int { return 1 }\n"
			writeCodeTestFile(t, dependency, oldText)
			writeCodeTestFile(t, dependent, "package demo\nvar X int = Value()\n")
			service := newOverlayCodeLanguageService()
			if cachedDependency {
				_, _ = service.SyncDocument(context.Background(), lsp.ServerSpec{}, lsp.Document{URI: codeFileURI(dependency), LanguageID: "go", Text: oldText})
			}
			writeCodeTestFile(t, dependency, "package demo\nfunc Value() string { return \"x\" }\n")
			service.request = func(method string, params, result any) error {
				if method != "textDocument/diagnostic" {
					t.Fatalf("unexpected request: %s", method)
				}
				uri := params.(map[string]any)["textDocument"].(map[string]string)["uri"]
				items := []lsp.Diagnostic{}
				if uri == codeFileURI(dependent) && strings.Contains(service.documents[codeFileURI(dependency)].Text, "Value() string") {
					items = append(items, lsp.Diagnostic{Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 12}, End: lsp.Position{Line: 1, Character: 17}}, Severity: 1, Message: "cannot use string as int"})
				}
				return assignCodeTestResult(result, map[string]any{"kind": "full", "items": items})
			}
			paths := []string{"b.go"}
			if !cachedDependency {
				paths = append(paths, "a.go")
			}
			args, _ := json.Marshal(map[string]any{"scope": "project", "paths": paths})
			result := testCodeRunner(service).Call(context.Background(), Call{Name: CodeDiagnostics, Args: args, ProjectDirs: []string{root}})
			payload := decodeToolResult(t, result)
			if !result.Ok || payload["fresh"] != true || payload["diagnosticCount"] != float64(1) {
				t.Fatalf("diagnostics used an old or incomplete workspace: %s", result.Content)
			}
		})
	}
}

func TestCodeNavigationRefreshesOtherOpenDocuments(t *testing.T) {
	for _, name := range []string{CodeDefinition, CodeReferences} {
		t.Run(name, func(t *testing.T) {
			root, source := codeTestProject(t)
			other := filepath.Join(root, "other.go")
			writeCodeTestFile(t, other, "package demo\n\nfunc Other() {}\n")
			service := newOverlayCodeLanguageService()
			cached, _ := os.ReadFile(other)
			queryPath, queryLine, queryColumn := "main.go", 3, 6
			cachedPath := other
			if name == CodeDefinition {
				cachedPath = source
				cached, _ = os.ReadFile(source)
				queryPath, queryLine, queryColumn = "other.go", 3, 16
			}
			_, _ = service.SyncDocument(context.Background(), lsp.ServerSpec{}, lsp.Document{URI: codeFileURI(cachedPath), LanguageID: "go", Text: string(cached)})
			writeCodeTestFile(t, other, "package demo\n\nfunc Other() { Target() }\n")
			if name == CodeDefinition {
				writeCodeTestFile(t, source, "\n"+string(cached))
			}
			service.request = func(method string, _ any, result any) error {
				locations := []any{}
				if name == CodeDefinition {
					if method != "textDocument/definition" {
						t.Fatalf("unexpected method: %s", method)
					}
					text := service.documents[codeFileURI(source)].Text
					line := strings.Count(text[:strings.Index(text, "func Target")], "\n")
					locations = append(locations, testLocation(source, line, 5, 11))
				} else if strings.Contains(service.documents[codeFileURI(other)].Text, "Target()") {
					locations = append(locations, testLocation(other, 2, 15, 21))
				}
				return assignCodeTestResult(result, locations)
			}
			args, _ := json.Marshal(map[string]any{"scope": "project", "path": queryPath, "line": queryLine, "column": queryColumn})
			result := testCodeRunner(service).Call(context.Background(), Call{Name: name, Args: args, ProjectDirs: []string{root}})
			payload := decodeToolResult(t, result)
			if !result.Ok || payload["locationCount"] != float64(1) {
				t.Fatalf("navigation missed a saved change: %s", result.Content)
			}
			if name == CodeDefinition && payload["locations"].([]any)[0].(map[string]any)["line"] != float64(4) {
				t.Fatalf("definition retained old coordinates: %s", result.Content)
			}
		})
	}
}

func TestCodeRenameAndSymbolsUseCurrentOverlay(t *testing.T) {
	root, source := codeTestProject(t)
	uri := codeFileURI(source)
	service := newOverlayCodeLanguageService()
	service.request = func(method string, params, result any) error {
		switch method {
		case "textDocument/prepareRename":
			return assignCodeTestResult(result, map[string]any{"defaultBehavior": true})
		case "textDocument/rename":
			return assignCodeTestResult(result, map[string]any{"changes": map[string]any{uri: []any{
				map[string]any{"range": lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 11}}, "newText": "RenamedTarget"},
				map[string]any{"range": lsp.Range{Start: lsp.Position{Line: 4, Character: 13}, End: lsp.Position{Line: 4, Character: 19}}, "newText": "RenamedTarget"},
			}}})
		case "workspace/symbol":
			query := params.(map[string]any)["query"].(string)
			symbols := []any{}
			if strings.Contains(service.documents[uri].Text, "func "+query+"()") {
				symbols = append(symbols, map[string]any{"name": query, "kind": 12, "location": testLocation(source, 2, 5, 5+len(query))})
			}
			return assignCodeTestResult(result, symbols)
		default:
			t.Fatalf("unexpected method: %s", method)
			return nil
		}
	}
	runner := testCodeRunner(service)
	rename := runner.Call(context.Background(), Call{SessionID: "refresh-rename", Name: CodeRename, Args: json.RawMessage(`{"scope":"project","path":"main.go","line":3,"column":6,"new_name":"RenamedTarget"}`), ProjectDirs: []string{root}})
	if !rename.Ok || decodeToolResult(t, rename)["languageServiceSynced"] != true || !strings.Contains(service.documents[uri].Text, "func RenamedTarget()") {
		t.Fatalf("rename left a stale overlay: %s", rename.Content)
	}
	for _, query := range []string{"RenamedTarget", "ExternalName"} {
		if query == "ExternalName" {
			content, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			writeCodeTestFile(t, source, strings.ReplaceAll(string(content), "RenamedTarget", query))
		}
		args, _ := json.Marshal(map[string]any{"scope": "project", "path": ".", "query": query})
		result := runner.Call(context.Background(), Call{Name: CodeSymbols, Args: args, ProjectDirs: []string{root}})
		if !result.Ok || decodeToolResult(t, result)["resultCount"] != float64(1) {
			t.Fatalf("symbols did not see %s: %s", query, result.Content)
		}
	}
}

func TestCodeRefreshClosesDeletedOrUnauthorizedDocuments(t *testing.T) {
	for _, scenario := range []string{"deleted", "revoked root", "different language root", "retargeted symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root, _ := codeTestProject(t)
			outside := t.TempDir()
			outside, err := filepath.EvalSymlinks(outside)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "cached.go")
			projectDirs := []string{root}
			if scenario == "revoked root" || scenario == "different language root" {
				path = filepath.Join(outside, "cached.go")
			}
			if scenario == "different language root" {
				projectDirs = append(projectDirs, outside)
			}
			writeCodeTestFile(t, path, "package demo\nfunc Cached() {}\n")
			service := newOverlayCodeLanguageService()
			_, _ = service.SyncDocument(context.Background(), lsp.ServerSpec{}, lsp.Document{URI: codeFileURI(path), LanguageID: "go", Text: "package demo\nfunc Cached() {}\n"})
			if scenario == "deleted" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "retargeted symlink" {
				target := filepath.Join(outside, "private.go")
				writeCodeTestFile(t, target, string([]byte{0xff}))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			} else {
				// Reading this unauthorized path would fail the UTF-8 check.
				writeCodeTestFile(t, path, string([]byte{0xff}))
			}
			service.request = func(_ string, _ any, result any) error {
				if len(service.documents) != 0 {
					t.Fatalf("stale or unauthorized overlays survived refresh: %+v", service.documents)
				}
				return assignCodeTestResult(result, []any{})
			}
			args, _ := json.Marshal(map[string]any{"scope": "project", "path": root, "query": "Cached"})
			result := testCodeRunner(service).Call(context.Background(), Call{Name: CodeSymbols, Args: args, ProjectDirs: projectDirs})
			if !result.Ok || decodeToolResult(t, result)["resultCount"] != float64(0) {
				t.Fatalf("refresh failed to close %s: %s", scenario, result.Content)
			}
		})
	}
}

func TestCodeRefreshPreservesDiagnosticWatermark(t *testing.T) {
	root, source := codeTestProject(t)
	uri := codeFileURI(source)
	service := &fakeCodeLanguageService{
		refresh: func(lsp.DocumentLoader) (map[string]lsp.DocumentState, error) {
			return map[string]lsp.DocumentState{uri: {URI: uri, Version: 2, Changed: true, PreviousDiagnosticGeneration: 7}}, nil
		},
		sync: func(lsp.Document) (lsp.DocumentState, error) {
			return lsp.DocumentState{URI: uri, Version: 2, PositionEncoding: "utf-16"}, nil
		},
		request: func(string, any, any) error {
			return &lsp.ResponseError{Code: -32601, Message: "pull diagnostics unavailable"}
		},
		published: func(_ string, after uint64) (lsp.DiagnosticSnapshot, bool, error) {
			if after != 7 {
				t.Fatalf("refresh lost diagnostic watermark: %d", after)
			}
			return lsp.DiagnosticSnapshot{URI: uri, Generation: 8}, true, nil
		},
	}
	result := testCodeRunner(service).Call(context.Background(), Call{Name: CodeDiagnostics, Args: json.RawMessage(`{"scope":"project","paths":["main.go"]}`), ProjectDirs: []string{root}})
	if !result.Ok || decodeToolResult(t, result)["fresh"] != true {
		t.Fatalf("refreshed diagnostics failed: %s", result.Content)
	}
}

func TestCodeBatchInvalidatesUnchangedDependentPushDiagnostics(t *testing.T) {
	root, _ := codeTestProject(t)
	dependency := filepath.Join(root, "a.go")
	dependent := filepath.Join(root, "b.go")
	writeCodeTestFile(t, dependency, "package demo\nfunc Value() string { return \"x\" }\n")
	writeCodeTestFile(t, dependent, "package demo\nvar X int = Value()\n")
	dependentURI := codeFileURI(dependent)
	service := &fakeCodeLanguageService{
		refresh: func(lsp.DocumentLoader) (map[string]lsp.DocumentState, error) {
			return map[string]lsp.DocumentState{dependentURI: {URI: dependentURI, Version: 1, PreviousDiagnosticGeneration: 7}}, nil
		},
		sync: func(document lsp.Document) (lsp.DocumentState, error) {
			return lsp.DocumentState{URI: document.URI, Version: 1, Changed: document.URI != dependentURI, PositionEncoding: "utf-16", PreviousDiagnosticGeneration: 7}, nil
		},
		request: func(string, any, any) error {
			return &lsp.ResponseError{Code: -32601, Message: "pull diagnostics unavailable"}
		},
		published: func(uri string, after uint64) (lsp.DiagnosticSnapshot, bool, error) {
			if after != 7 {
				t.Fatalf("batch reused pre-change push diagnostics for %s: after=%d", uri, after)
			}
			items := []lsp.Diagnostic{}
			if uri == dependentURI {
				items = append(items, lsp.Diagnostic{Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 12}, End: lsp.Position{Line: 1, Character: 17}}, Severity: 1, Message: "cannot use string as int"})
			}
			return lsp.DiagnosticSnapshot{URI: uri, Generation: 8, Diagnostics: items}, true, nil
		},
	}
	result := testCodeRunner(service).Call(context.Background(), Call{Name: CodeDiagnostics, Args: json.RawMessage(`{"scope":"project","paths":["b.go","a.go"]}`), ProjectDirs: []string{root}})
	if !result.Ok || decodeToolResult(t, result)["diagnosticCount"] != float64(1) {
		t.Fatalf("batch lost dependency diagnostics: %s", result.Content)
	}
}

func TestCodeRefreshFailurePreventsSemanticRequest(t *testing.T) {
	root, source := codeTestProject(t)
	service := newOverlayCodeLanguageService()
	_, _ = service.SyncDocument(context.Background(), lsp.ServerSpec{}, lsp.Document{URI: codeFileURI(source), LanguageID: "go", Text: "package demo\n"})
	writeCodeTestFile(t, source, string([]byte{0xff}))
	service.request = func(string, any, any) error {
		t.Fatal("semantic request ran after an open document could not be refreshed")
		return nil
	}
	result := testCodeRunner(service).Call(context.Background(), Call{Name: CodeSymbols, Args: json.RawMessage(`{"scope":"project","path":".","query":"Target"}`), ProjectDirs: []string{root}})
	if result.Ok {
		t.Fatalf("unreadable overlay was accepted as current: %s", result.Content)
	}
}

func TestCodeRenamePreservesAppliedResultWhenRefreshFails(t *testing.T) {
	root, source := codeTestProject(t)
	service := renameWorkspaceEditService(map[string]any{"changes": map[string]any{codeFileURI(source): []any{
		map[string]any{"range": lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 11}}, "newText": "Renamed"},
	}}})
	refreshes := 0
	longError := "language server exited: " + strings.Repeat("failed \"refresh\"\\\n诊断\t", modelResultMaxChars)
	service.refresh = func(lsp.DocumentLoader) (map[string]lsp.DocumentState, error) {
		refreshes++
		if refreshes > 1 {
			return nil, errors.New(longError)
		}
		return nil, nil
	}
	result := testCodeRunner(service).Call(context.Background(), Call{SessionID: "refresh-failure", Name: CodeRename, Args: json.RawMessage(`{"scope":"project","path":"main.go","line":3,"column":6,"new_name":"Renamed"}`), ProjectDirs: []string{root}})
	payload := decodeToolResult(t, result)
	if !result.Ok || payload["status"] != "applied" || payload["languageServiceSynced"] != false || payload["languageServiceSyncError"] == nil {
		t.Fatalf("refresh failure changed the rename outcome: %s", result.Content)
	}
	if !strings.Contains(readPatchTestFile(t, source), "func Renamed()") {
		t.Fatal("rename was not applied")
	}
	if utf8.RuneCountInString(result.Content) <= modelResultMaxChars {
		t.Fatal("fixture did not produce a long canonical rename result")
	}
	canonical := result.Content
	view := ModelResultContent(CodeRename, result.Ok, result.Content, "turn", "rename", true)
	var preview map[string]any
	if err := json.Unmarshal([]byte(view), &preview); err != nil {
		t.Fatalf("invalid model preview: %v", err)
	}
	if utf8.RuneCountInString(view) > modelResultMaxChars || preview["preview_only"] != true || preview["ok"] != true || preview["status"] != "applied" {
		t.Fatalf("model preview lost the applied outcome or exceeded its bound: %s", view)
	}
	if preview["operation"] != "rename" || preview["newName"] != "Renamed" || preview["editCount"] != float64(1) || preview["languageServiceSynced"] != false {
		t.Fatalf("model preview lost rename or refresh metadata: %s", view)
	}
	failure, ok := preview["languageServiceSyncError"].(map[string]any)
	if !ok || failure["reason"] != "language_server_crashed" || failure["detailTruncated"] != true {
		t.Fatalf("model preview lost the bounded refresh failure: %s", view)
	}
	warning, _ := preview["languageServiceSyncWarning"].(string)
	if !strings.Contains(warning, "Do not repeat") {
		t.Fatalf("model preview could invite another rename: %s", view)
	}
	ref, ok := preview["result_ref"].(map[string]any)
	if !ok || ref["turn_id"] != "turn" || ref["call_id"] != "rename" || result.Content != canonical {
		t.Fatal("model preview changed the canonical result or lost its readback reference")
	}
}

func TestCodeRenameModelPreviewKeepsOutcomeForManyChangedRanges(t *testing.T) {
	files := make([]patchFileView, 24)
	for i := range files {
		files[i] = patchFileView{Path: fmt.Sprintf("pkg/source_%02d.go", i), Operation: "update", Additions: 128, Deletions: 128, LineRanges: make([]patchLineRange, 128)}
		for j := range files[i].LineRanges {
			files[i].LineRanges[j] = patchLineRange{StartLine: 3*j + 1, LineCount: 1}
		}
	}
	for _, synced := range []bool{true, false} {
		t.Run(fmt.Sprintf("synced=%t", synced), func(t *testing.T) {
			payload := map[string]any{
				"ok": true, "status": "applied", "operation": "rename", "oldName": "Target", "newName": "Renamed",
				"editCount": 3072, "fileCount": len(files), "files": files, "language": "go", "server": "gopls",
				"languageRoot": "/project", "languageServiceSynced": synced,
			}
			if !synced {
				payload["languageServiceSyncError"] = map[string]any{"ok": false, "reason": "language_server_crashed", "detail": "language server exited"}
				// A large unrelated warning list must not hide the no-retry instruction.
				payload["warnings"] = []string{strings.Repeat("review the changed ranges; ", 200)}
			}
			canonical := jsonString(payload)
			if utf8.RuneCountInString(canonical) <= modelResultMaxChars {
				t.Fatal("fixture did not exceed the preview threshold")
			}
			view := ModelResultContent(CodeRename, true, canonical, "turn", "rename", true)
			var preview map[string]any
			if err := json.Unmarshal([]byte(view), &preview); err != nil {
				t.Fatalf("invalid model preview: %v", err)
			}
			if utf8.RuneCountInString(view) > modelResultMaxChars || preview["preview_only"] != true || preview["ok"] != true || preview["status"] != "applied" || preview["languageServiceSynced"] != synced {
				t.Fatalf("large rename preview lost its outcome or exceeded its bound: %s", view)
			}
			if preview["oldName"] != "Target" || preview["newName"] != "Renamed" || preview["editCount"] != float64(3072) || preview["fileCount"] != float64(len(files)) {
				t.Fatalf("large rename preview lost its metadata: %s", view)
			}
			shown, ok := preview["files"].([]any)
			if !ok || len(shown) == 0 || preview["shown_files"] != float64(len(shown)) || preview["head"] != nil || preview["tail"] != nil {
				t.Fatalf("large rename did not receive structured file previews: %s", view)
			}
			first, ok := shown[0].(map[string]any)
			if !ok || first["lineRangesTruncated"] != true {
				t.Fatalf("omitted ranges were not marked in the preview: %s", view)
			}
			if !synced {
				warning, _ := preview["languageServiceSyncWarning"].(string)
				failure, ok := preview["languageServiceSyncError"].(map[string]any)
				if !strings.Contains(warning, "Do not repeat") || !ok || failure["reason"] != "language_server_crashed" {
					t.Fatalf("large rename preview hid the refresh failure: %s", view)
				}
			} else if preview["languageServiceSyncError"] != nil || preview["languageServiceSyncWarning"] != nil {
				t.Fatalf("successful refresh received a false warning: %s", view)
			}
		})
	}
}
