package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/lsp"
)

type recordingCodeLanguageService struct {
	*fakeCodeLanguageService
	mu      sync.Mutex
	changes []lsp.FileChange
}

func (f *recordingCodeLanguageService) RecordFileChanges(changes []lsp.FileChange) {
	f.mu.Lock()
	f.changes = append(f.changes, changes...)
	f.mu.Unlock()
}

func (f *recordingCodeLanguageService) recordedChanges(clear bool) []lsp.FileChange {
	f.mu.Lock()
	defer f.mu.Unlock()
	changes := append([]lsp.FileChange(nil), f.changes...)
	if clear {
		f.changes = nil
	}
	return changes
}

func assertCodeFileChanges(t *testing.T, service *recordingCodeLanguageService, expected map[string]int) {
	t.Helper()
	changes := service.recordedChanges(true)
	if len(changes) != len(expected) {
		t.Fatalf("file changes = %+v, want %+v", changes, expected)
	}
	for _, change := range changes {
		if kind, ok := expected[change.Path]; !ok || kind != change.Type {
			t.Fatalf("unexpected file change %+v, want %+v", change, expected)
		}
	}
}

func TestCodeFileChangesFollowStructuredFileAndDirectoryOperations(t *testing.T) {
	root, _ := codeTestProject(t)
	service := &recordingCodeLanguageService{fakeCodeLanguageService: &fakeCodeLanguageService{}}
	runner := testCodeRunner(service)
	call := func(name string, args map[string]any) {
		t.Helper()
		args["scope"] = "project"
		raw, _ := json.Marshal(args)
		result := runner.Call(context.Background(), Call{Name: name, Args: raw, ProjectDirs: []string{root}})
		if !result.Ok {
			t.Fatalf("%s failed: %s", name, result.Content)
		}
	}
	call(FileWrite, map[string]any{"path": "source/added.go", "content": "package demo\n"})
	assertCodeFileChanges(t, service, map[string]int{filepath.Join(root, "source", "added.go"): lsp.FileCreated})
	call(FileMove, map[string]any{"from_path": "source", "to_path": "destination"})
	assertCodeFileChanges(t, service, map[string]int{
		filepath.Join(root, "source", "added.go"):      lsp.FileDeleted,
		filepath.Join(root, "destination", "added.go"): lsp.FileCreated,
	})
	call(FileDelete, map[string]any{"path": "destination", "recursive": true})
	assertCodeFileChanges(t, service, map[string]int{filepath.Join(root, "destination", "added.go"): lsp.FileDeleted})
}

func TestCodeFileChangesPreserveYieldedCommandCompletionChain(t *testing.T) {
	root, source := codeTestProject(t)
	created := filepath.Join(root, "created.go")
	service := &recordingCodeLanguageService{fakeCodeLanguageService: &fakeCodeLanguageService{}}
	runner := testCodeRunner(service)
	call := Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"opaque-tool","mutation_paths":["main.go","created.go"]}`), ProjectDirs: []string{root}}
	previousCalls := 0
	ctx := WithCommandCompletion(context.Background(), func() {
		previousCalls++
		if len(service.recordedChanges(false)) != 2 {
			t.Fatal("the original completion callback ran before language events were queued")
		}
	})
	ctx, returned := runner.trackCodeFileChanges(ctx, call)
	returned(Result{Ok: true, Content: `{"yielded":true}`})
	if len(service.recordedChanges(false)) != 0 || previousCalls != 0 {
		t.Fatal("a foreground yield was treated as command completion")
	}
	// The command changed disk and then failed. Its partial effects must still
	// reach gopls before the pre-existing completion callback releases locks.
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	writeCodeTestFile(t, created, "package demo\n")
	complete, ok := ctx.Value(commandCompletionKey{}).(func())
	if !ok {
		t.Fatal("command completion callback was lost")
	}
	complete()
	returned(Result{Ok: false, Content: `{"exitCode":7}`})
	if previousCalls != 1 {
		t.Fatalf("existing completion callback ran %d times", previousCalls)
	}
	assertCodeFileChanges(t, service, map[string]int{source: lsp.FileDeleted, created: lsp.FileCreated})
}

func TestCodeFileChangesRemainInsideAuthorizedRoots(t *testing.T) {
	root, _ := codeTestProject(t)
	outside := filepath.Join(t.TempDir(), "outside.go")
	service := &recordingCodeLanguageService{fakeCodeLanguageService: &fakeCodeLanguageService{}}
	runner := testCodeRunner(service)
	raw, _ := json.Marshal(map[string]any{"scope": "project", "path": outside, "content": "package outside\n"})
	result := runner.Call(context.Background(), Call{Name: FileWrite, Args: raw, ProjectDirs: []string{root}})
	if result.Ok {
		t.Fatal("out-of-scope write was allowed")
	}
	runner.recordCodeFileChanges([]string{root}, []lsp.FileChange{{Path: outside, Type: lsp.FileChanged}})
	assertCodeFileChanges(t, service, nil)
	link := filepath.Join(root, "linked")
	if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeCodeTestFile(t, outside, "package outside\n")
	files, uncertain := codeMutationFiles([]string{root}, []string{link})
	if len(files) != 0 || len(uncertain) != 0 {
		t.Fatalf("symlink mutation followed an external subtree: %+v, %+v", files, uncertain)
	}
}

func TestCodeMutationDoesNotStartUnusedLanguageServices(t *testing.T) {
	root, _ := codeTestProject(t)
	manager := lsp.NewManager(lsp.WithIdleTimeout(0), lsp.WithReapInterval(0))
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	runner := NewBuiltinRunner(WithLanguageService(manager))
	result := runner.Call(context.Background(), Call{Name: FileWrite, Args: json.RawMessage(`{"scope":"project","path":"new.go","content":"package demo\n"}`), ProjectDirs: []string{root}})
	if !result.Ok || manager.ProcessCount() != 0 {
		t.Fatalf("write failed or started an unused service: %s", result.Content)
	}
}

func TestCodeRenameRecordsChangesAfterApplyingFiles(t *testing.T) {
	root, source := codeTestProject(t)
	service := &recordingCodeLanguageService{fakeCodeLanguageService: renameWorkspaceEditService(map[string]any{"changes": map[string]any{codeFileURI(source): []any{
		map[string]any{"range": lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 11}}, "newText": "Renamed"},
	}}})}
	refreshes := 0
	service.refresh = func(lsp.DocumentLoader) (map[string]lsp.DocumentState, error) {
		refreshes++
		if refreshes > 1 && len(service.recordedChanges(false)) != 1 {
			t.Fatal("post-rename refresh ran before disk notifications were recorded")
		}
		return nil, nil
	}
	result := testCodeRunner(service).Call(context.Background(), Call{SessionID: "rename-events", Name: CodeRename, Args: json.RawMessage(`{"scope":"project","path":"main.go","line":3,"column":6,"new_name":"Renamed"}`), ProjectDirs: []string{root}})
	if !result.Ok || decodeToolResult(t, result)["status"] != "applied" {
		t.Fatalf("rename did not preserve the applied result: %s", result.Content)
	}
	assertCodeFileChanges(t, service, map[string]int{source: lsp.FileChanged})
}
