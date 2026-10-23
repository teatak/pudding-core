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

func renameConflictCall(root string) Call {
	return Call{SessionID: "rename-conflict", Name: CodeRename, Args: json.RawMessage(`{"scope":"project","path":"main.go","line":3,"column":6,"new_name":"Renamed"}`), ProjectDirs: []string{root}}
}

func renameConflictEdit(source, second string, versioned bool, version int) map[string]any {
	edits := map[string]any{codeFileURI(source): []any{map[string]any{"range": map[string]any{"start": map[string]int{"line": 2, "character": 5}, "end": map[string]int{"line": 2, "character": 11}}, "newText": "Renamed"}}}
	if second != "" {
		edits[codeFileURI(second)] = []any{map[string]any{"range": map[string]any{"start": map[string]int{"line": 2, "character": 13}, "end": map[string]int{"line": 2, "character": 19}}, "newText": "Renamed"}}
	}
	if !versioned {
		return map[string]any{"changes": edits}
	}
	changes := []any{}
	for uri, items := range edits {
		changes = append(changes, map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "edits": items})
	}
	return map[string]any{"documentChanges": changes}
}

func TestCodeRenamePreservesChangesWhileWaiting(t *testing.T) {
	for _, versioned := range []bool{false, true} {
		format := "changes"
		if versioned {
			format = "documentChanges"
		}
		for _, stage := range []string{"prepare", "discovery", "recompute-selected", "recompute-other", "before-patch", "deleted-other"} {
			t.Run(format+"/"+stage, func(t *testing.T) {
				root, source := codeTestProject(t)
				original := readPatchTestFile(t, source)
				second := filepath.Join(root, "other.go")
				other := "package demo\n\nfunc Use() { Target() }\n"
				writeCodeTestFile(t, second, other)
				changedPath, changedText := source, strings.ReplaceAll(original, "Target", "Manual")
				if stage == "recompute-other" || stage == "before-patch" || stage == "deleted-other" {
					changedPath, changedText = second, strings.ReplaceAll(other, "Target", "Manual")
				}
				change := func() {
					if stage == "deleted-other" {
						if err := os.Remove(second); err != nil {
							t.Fatal(err)
						}
					} else {
						writeCodeTestFile(t, changedPath, changedText)
					}
				}
				requests := 0
				service := &fakeCodeLanguageService{request: func(method string, _ any, result any) error {
					if method == "textDocument/prepareRename" {
						if stage == "prepare" {
							change()
						}
						return assignCodeTestResult(result, map[string]any{"defaultBehavior": true})
					}
					if method == "textDocument/rename" {
						requests++
						if (requests == 1 && stage == "discovery") || (requests == 2 && (strings.HasPrefix(stage, "recompute") || stage == "deleted-other")) {
							change()
						}
						return assignCodeTestResult(result, renameConflictEdit(source, second, versioned, 1))
					}
					return nil
				}}
				ctx := context.Background()
				if stage == "before-patch" {
					ctx = WithMutationTrackingSink(ctx, func([]string) { change() })
				}
				result := testCodeRunner(service).Call(ctx, renameConflictCall(root))
				if result.Ok || decodeToolResult(t, result)["reason"] != "rename_conflict" {
					t.Fatalf("expected conflict: %s", result.Content)
				}
				if stage == "deleted-other" {
					if _, err := os.Stat(second); !os.IsNotExist(err) {
						t.Fatalf("deleted file recreated: %v", err)
					}
				} else if got := readPatchTestFile(t, changedPath); got != changedText {
					t.Fatalf("saved change overwritten: %q", got)
				}
				if changedPath != source && readPatchTestFile(t, source) != original {
					t.Fatal("selected file partially renamed")
				}
				if changedPath != second && readPatchTestFile(t, second) != other {
					t.Fatal("other file partially renamed")
				}
			})
		}
	}
}

func TestCodeRenameChecksVersionAndTargetSet(t *testing.T) {
	for _, scenario := range []string{"matching", "stale-version", "new-target", "duplicate-document"} {
		t.Run(scenario, func(t *testing.T) {
			root, source := codeTestProject(t)
			original := readPatchTestFile(t, source)
			other := filepath.Join(root, "other.go")
			writeCodeTestFile(t, other, "package demo\n\nfunc Use() { Target() }\n")
			requests := 0
			service := &fakeCodeLanguageService{sync: func(d lsp.Document) (lsp.DocumentState, error) {
				return lsp.DocumentState{URI: d.URI, Version: 7, PositionEncoding: "utf-16"}, nil
			}, request: func(method string, _ any, result any) error {
				if method == "textDocument/prepareRename" {
					return assignCodeTestResult(result, map[string]any{"defaultBehavior": true})
				}
				if method == "textDocument/rename" {
					requests++
					version := 7
					second := ""
					if requests == 2 && scenario == "stale-version" {
						version = 6
					}
					if requests == 2 && scenario == "new-target" {
						second = other
					}
					edit := renameConflictEdit(source, second, true, version)
					if scenario == "duplicate-document" {
						items := edit["documentChanges"].([]any)
						edit["documentChanges"] = append(items, items[0])
					}
					return assignCodeTestResult(result, edit)
				}
				return nil
			}}
			result := testCodeRunner(service).Call(context.Background(), renameConflictCall(root))
			if scenario == "matching" {
				if !result.Ok || !strings.Contains(readPatchTestFile(t, source), "func Renamed") {
					t.Fatalf("matching version failed: %s", result.Content)
				}
				return
			}
			reason := "rename_conflict"
			if scenario == "duplicate-document" {
				reason = "unsafe_workspace_edit"
			}
			if result.Ok || decodeToolResult(t, result)["reason"] != reason {
				t.Fatalf("expected %s: %s", reason, result.Content)
			}
			if readPatchTestFile(t, source) != original {
				t.Fatal("failed rename wrote the source")
			}
		})
	}
}

func TestCodeRenameRecomputesOtherFileFromSavedContent(t *testing.T) {
	root, source := codeTestProject(t)
	other := filepath.Join(root, "other.go")
	original := "package demo\n\nfunc Use() { Target() }\n"
	saved := "// saved while discovering references\n" + original
	writeCodeTestFile(t, other, original)
	requests := 0
	var synced string
	service := &fakeCodeLanguageService{sync: func(d lsp.Document) (lsp.DocumentState, error) {
		if d.URI == codeFileURI(other) {
			synced = d.Text
		}
		return lsp.DocumentState{URI: d.URI, Version: 1, PositionEncoding: "utf-16"}, nil
	}, request: func(method string, _ any, result any) error {
		if method == "textDocument/prepareRename" {
			return assignCodeTestResult(result, map[string]any{"defaultBehavior": true})
		}
		if method == "textDocument/rename" {
			requests++
			edit := renameConflictEdit(source, other, false, 1)
			if requests == 1 {
				writeCodeTestFile(t, other, saved)
			} else {
				if synced != saved {
					t.Fatal("recomputation did not receive saved content")
				}
				item := edit["changes"].(map[string]any)[codeFileURI(other)].([]any)[0].(map[string]any)
				r := item["range"].(map[string]any)
				r["start"].(map[string]int)["line"] = 3
				r["end"].(map[string]int)["line"] = 3
			}
			return assignCodeTestResult(result, edit)
		}
		return nil
	}}
	result := testCodeRunner(service).Call(context.Background(), renameConflictCall(root))
	if !result.Ok {
		t.Fatal(result.Content)
	}
	if requests != 2 || readPatchTestFile(t, other) != strings.ReplaceAll(saved, "Target", "Renamed") {
		t.Fatal("discovery coordinates were applied instead of recomputed coordinates")
	}
}

func TestCodeRenamePreparedPatchRejectsLaterSave(t *testing.T) {
	root, source := codeTestProject(t)
	original := readPatchTestFile(t, source)
	target := resolvedCodeTarget{path: source, language: "go", languageRoot: root}
	edits, err := parseCodeRenameWorkspaceEdit(json.RawMessage(jsonString(renameConflictEdit(source, "", false, 1))))
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := readCodeRenameSnapshots([]string{root}, target, lsp.Document{URI: codeFileURI(source), Text: original}, edits)
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := buildCodeRenamePatchFiles("utf-16", edits, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := preparePatch(renameConflictCall(root), filePatchArgs{Scope: "project", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	saved := original + "// saved after patch preparation\n"
	writeCodeTestFile(t, source, saved)
	if _, err = applyPreparedPatch([]string{root}, patch); err == nil || !strings.Contains(err.Error(), "changed after patch validation") {
		t.Fatalf("stale patch accepted: %v", err)
	}
	if readPatchTestFile(t, source) != saved {
		t.Fatal("later save overwritten")
	}
}
