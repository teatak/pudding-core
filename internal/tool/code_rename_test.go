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

func TestCodeRenameCreatesPatchProposalWithoutWriting(t *testing.T) {
	root, source := codeTestProject(t)
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	service := &fakeCodeLanguageService{request: func(method string, params, result any) error {
		switch method {
		case "textDocument/prepareRename":
			return assignCodeTestResult(result, map[string]any{
				"range": map[string]any{
					"start": map[string]int{"line": 4, "character": 13},
					"end":   map[string]int{"line": 4, "character": 19},
				},
				"placeholder": "Target",
			})
		case "textDocument/rename":
			renameParams := params.(map[string]any)
			if renameParams["newName"] != "RenamedTarget" {
				t.Fatalf("newName = %#v", renameParams["newName"])
			}
			return assignCodeTestResult(result, map[string]any{
				"changes": map[string]any{
					codeFileURI(source): []map[string]any{
						{
							"range":   map[string]any{"start": map[string]int{"line": 2, "character": 5}, "end": map[string]int{"line": 2, "character": 11}},
							"newText": "RenamedTarget",
						},
						{
							"range":   map[string]any{"start": map[string]int{"line": 4, "character": 13}, "end": map[string]int{"line": 4, "character": 19}},
							"newText": "RenamedTarget",
						},
					},
				},
			})
		default:
			return nil
		}
	}}
	runner := testCodeRunner(service)
	result := runner.Call(context.Background(), Call{
		SessionID:   "session_rename",
		TurnID:      "turn_rename",
		CallID:      "call_rename",
		Name:        CodeRename,
		Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":5,"column":14,"new_name":"RenamedTarget"}`),
		ProjectDirs: []string{root},
	})
	if !result.Ok {
		t.Fatalf("rename failed: %s", result.Content)
	}
	payload := decodeToolResult(t, result)
	if payload["status"] != "proposed" || payload["operation"] != "rename" || payload["oldName"] != "Target" || payload["newName"] != "RenamedTarget" {
		t.Fatalf("unexpected rename payload: %+v", payload)
	}
	if payload["editCount"] != float64(2) || payload["fileCount"] != float64(1) {
		t.Fatalf("unexpected rename counts: %+v", payload)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != string(original) {
		t.Fatalf("rename proposal changed source before apply: %q err=%v", got, err)
	}

	proposalID, _ := payload["proposalID"].(string)
	applyArgs, _ := json.Marshal(map[string]string{"proposal_id": proposalID})
	applied := runner.Call(context.Background(), Call{
		SessionID:   "session_rename",
		TurnID:      "turn_rename",
		CallID:      "call_apply",
		Name:        PatchApply,
		Args:        applyArgs,
		ProjectDirs: []string{root},
	})
	if !applied.Ok {
		t.Fatalf("apply failed: %s", applied.Content)
	}
	got, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(got), "RenamedTarget") != 2 || strings.Contains(string(got), "func Target") || strings.Contains(string(got), "{ Target") {
		t.Fatalf("rename was not applied atomically: %q", got)
	}
}

func TestCodeRenameAcceptsDocumentChanges(t *testing.T) {
	root, source := codeTestProject(t)
	service := &fakeCodeLanguageService{request: func(method string, _ any, result any) error {
		switch method {
		case "textDocument/prepareRename":
			return assignCodeTestResult(result, map[string]any{"defaultBehavior": true})
		case "textDocument/rename":
			return assignCodeTestResult(result, map[string]any{
				"documentChanges": []map[string]any{{
					"textDocument": map[string]any{"uri": codeFileURI(source), "version": 1},
					"edits": []map[string]any{{
						"range":        map[string]any{"start": map[string]int{"line": 2, "character": 5}, "end": map[string]int{"line": 2, "character": 11}},
						"newText":      "Renamed",
						"annotationId": "rename-symbol",
					}},
				}},
			})
		default:
			return nil
		}
	}}
	result := testCodeRunner(service).Call(context.Background(), Call{
		SessionID:   "session_rename",
		Name:        CodeRename,
		Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":3,"column":6,"new_name":"Renamed"}`),
		ProjectDirs: []string{root},
	})
	if !result.Ok || !strings.Contains(result.Content, `"proposalID"`) {
		t.Fatalf("documentChanges rename failed: %+v", result)
	}
}

func TestCodeRenameRejectsUnsafeWorkspaceEdits(t *testing.T) {
	t.Run("outside project", func(t *testing.T) {
		root, _ := codeTestProject(t)
		externalRoot := t.TempDir()
		external := filepath.Join(externalRoot, "external.go")
		writeCodeTestFile(t, external, "package external\n")
		service := renameWorkspaceEditService(map[string]any{
			"changes": map[string]any{
				codeFileURI(external): []map[string]any{{
					"range":   map[string]any{"start": map[string]int{"line": 0, "character": 0}, "end": map[string]int{"line": 0, "character": 7}},
					"newText": "outside",
				}},
			},
		})
		result := testCodeRunner(service).Call(context.Background(), Call{
			SessionID:   "session_rename",
			Name:        CodeRename,
			Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":3,"column":6,"new_name":"Renamed"}`),
			ProjectDirs: []string{root},
		})
		if result.Ok || !strings.Contains(result.Content, `"reason":"rename_outside_project"`) {
			t.Fatalf("outside edit should fail closed: %+v", result)
		}
	})

	t.Run("resource operation", func(t *testing.T) {
		root, source := codeTestProject(t)
		service := renameWorkspaceEditService(map[string]any{
			"documentChanges": []map[string]any{{
				"kind":   "rename",
				"oldUri": codeFileURI(source),
				"newUri": codeFileURI(filepath.Join(root, "renamed.go")),
			}},
		})
		result := testCodeRunner(service).Call(context.Background(), Call{
			SessionID:   "session_rename",
			Name:        CodeRename,
			Args:        json.RawMessage(`{"scope":"project","path":"main.go","line":3,"column":6,"new_name":"Renamed"}`),
			ProjectDirs: []string{root},
		})
		if result.Ok || !strings.Contains(result.Content, `"reason":"unsafe_workspace_edit"`) {
			t.Fatalf("resource operation should fail closed: %+v", result)
		}
	})
}

func TestApplyCodeRenameTextEditsHandlesEncodingsAndCRLF(t *testing.T) {
	for _, test := range []struct {
		name     string
		encoding string
		start    int
		end      int
	}{
		{name: "utf8", encoding: "utf-8", start: 4, end: 8},
		{name: "utf16", encoding: "utf-16", start: 2, end: 6},
		{name: "utf32", encoding: "utf-32", start: 1, end: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := applyCodeRenameTextEdits("😀name\r\n", []codeRenameTextEdit{{
				Range: lsp.Range{
					Start: lsp.Position{Line: 0, Character: test.start},
					End:   lsp.Position{Line: 0, Character: test.end},
				},
				NewText: "value",
			}}, test.encoding)
			if err != nil || got != "😀value\r\n" {
				t.Fatalf("got %q, err=%v", got, err)
			}
		})
	}

	_, err := applyCodeRenameTextEdits("😀name", []codeRenameTextEdit{{
		Range:   lsp.Range{Start: lsp.Position{Character: 1}, End: lsp.Position{Character: 2}},
		NewText: "x",
	}}, "utf-16")
	if err == nil || !strings.Contains(err.Error(), "surrogate") {
		t.Fatalf("surrogate split should fail: %v", err)
	}

	_, err = applyCodeRenameTextEdits("abcdef", []codeRenameTextEdit{
		{Range: lsp.Range{Start: lsp.Position{Character: 0}, End: lsp.Position{Character: 3}}, NewText: "x"},
		{Range: lsp.Range{Start: lsp.Position{Character: 2}, End: lsp.Position{Character: 4}}, NewText: "y"},
	}, "utf-8")
	if err == nil || !strings.Contains(err.Error(), "overlapping") {
		t.Fatalf("overlapping edits should fail: %v", err)
	}
}

func renameWorkspaceEditService(edit map[string]any) *fakeCodeLanguageService {
	return &fakeCodeLanguageService{request: func(method string, _ any, result any) error {
		switch method {
		case "textDocument/prepareRename":
			return assignCodeTestResult(result, map[string]any{"defaultBehavior": true})
		case "textDocument/rename":
			return assignCodeTestResult(result, edit)
		default:
			return nil
		}
	}}
}
