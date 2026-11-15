package lsp

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerFileChangesDoNotStartUnusedServers(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	manager.RecordFileChanges([]FileChange{{Path: filepath.Join(t.TempDir(), "new.go"), Type: FileCreated}})
	if manager.ProcessCount() != 0 || len(manager.entries) != 0 {
		t.Fatal("a filesystem mutation started a language server")
	}
}

func TestManagerBlockedFileFlushDoesNotBlockOtherWorkspaces(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	ctx := context.Background()
	first, err := manager.Acquire(ctx, testServerSpec(t, "gopls"))
	if err != nil {
		t.Fatal(err)
	}
	secondSpec := testServerSpec(t, "gopls")
	second, err := manager.Acquire(ctx, secondSpec)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	entry := manager.entries[first.spec.Key]
	manager.mu.Unlock()
	entry.flushMu.Lock()
	defer entry.flushMu.Unlock()
	done := make(chan error, 1)
	go func() {
		manager.RecordFileChanges([]FileChange{
			{Path: filepath.Join(first.spec.Key.LanguageRoot, "a.go"), Type: FileChanged},
			{Path: filepath.Join(second.spec.Key.LanguageRoot, "b.go"), Type: FileCreated},
		})
		_, err := manager.Acquire(ctx, secondSpec)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("one process's file writer blocked other workspace operations")
	}
}

func TestManagerFlushesFileChangesBeforeSemanticRequests(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec, err := normalizeSpec(testServerSpec(t, "gopls"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	process, err := manager.Acquire(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(spec.Key.LanguageRoot, "new.go")
	deleted := filepath.Join(spec.Key.LanguageRoot, "old.go")
	manager.RecordFileChanges([]FileChange{
		{Path: created, Type: FileCreated},
		{Path: created, Type: FileChanged},
		{Path: deleted, Type: FileDeleted},
		{Path: filepath.Join(t.TempDir(), "outside.go"), Type: FileCreated},
		{Path: "relative.go", Type: FileCreated},
	})
	var changes []fakeLSPFileChange
	if err := process.Request(ctx, "test/fileChanges", nil, &changes); err != nil || len(changes) != 0 {
		t.Fatalf("recording sent events before a query: %+v, %v", changes, err)
	}
	if err := manager.Request(ctx, spec, "test/fileChanges", nil, &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].URI != fileURI(created) || changes[0].Type != FileCreated || changes[1].URI != fileURI(deleted) || changes[1].Type != FileDeleted {
		t.Fatalf("request did not see the ordered, confined file events: %+v", changes)
	}
	if err := manager.Request(ctx, spec, "test/fileChanges", nil, &changes); err != nil || len(changes) != 2 {
		t.Fatalf("a completed batch was replayed: %+v, %v", changes, err)
	}
}

func TestManagerRefreshFlushesChangesBeforeLoadingOverlays(t *testing.T) {
	manager := newTestManager()
	defer closeTestManager(t, manager)
	spec, err := normalizeSpec(testServerSpec(t, "gopls"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uri := fileURI(filepath.Join(spec.Key.LanguageRoot, "open.go"))
	if _, err := manager.SyncDocument(ctx, spec, Document{URI: uri, LanguageID: "go", Text: "package demo\n"}); err != nil {
		t.Fatal(err)
	}
	process, err := manager.Acquire(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	manager.RecordFileChanges([]FileChange{{Path: filepath.Join(spec.Key.LanguageRoot, "closed.go"), Type: FileChanged}})
	_, err = manager.RefreshDocuments(ctx, spec, func(string) (string, bool, error) {
		var changes []fakeLSPFileChange
		if err := process.Request(ctx, "test/fileChanges", nil, &changes); err != nil || len(changes) != 1 {
			t.Fatalf("refresh did not flush dependencies first: %+v, %v", changes, err)
		}
		return "package demo\n", true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagerRebuildsWorkspaceWhenFileChangesCannotBeQueued(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprintf("queue_overflow=%t", overflow), func(t *testing.T) {
			manager := newTestManager()
			defer closeTestManager(t, manager)
			spec, err := normalizeSpec(testServerSpec(t, "gopls"))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			before, err := manager.Acquire(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			changes := []FileChange{{Path: spec.Key.LanguageRoot, Type: FileChangeInvalidate}}
			if overflow {
				changes = make([]FileChange, maxPendingFileChanges+1)
				for index := range changes {
					changes[index] = FileChange{Path: filepath.Join(spec.Key.LanguageRoot, fmt.Sprintf("file%d.go", index)), Type: FileChanged}
				}
			}
			manager.RecordFileChanges(changes)
			after, err := manager.Acquire(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			if before == after || before.Alive() || !after.Alive() || manager.ProcessCount() != 1 {
				t.Fatal("an incomplete event queue left the old workspace available")
			}
			var received []fakeLSPFileChange
			if err := manager.Request(ctx, spec, "test/fileChanges", nil, &received); err != nil || len(received) != 0 {
				t.Fatalf("a fresh workspace received a partial old batch: %+v, %v", received, err)
			}
		})
	}
}

func TestManagerEnablesServerFileWatchingOnlyForGopls(t *testing.T) {
	for _, kind := range []string{"gopls", "typescript-language-server"} {
		t.Run(kind, func(t *testing.T) {
			manager := newTestManager()
			defer closeTestManager(t, manager)
			spec, err := normalizeSpec(testServerSpec(t, kind))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			var initial map[string]any
			if err := manager.Request(ctx, spec, "test/initialOptions", nil, &initial); err != nil {
				t.Fatal(err)
			}
			var configuration []map[string]any
			if err := manager.Request(ctx, spec, "test/goplsConfiguration", nil, &configuration); err != nil {
				t.Fatal(err)
			}
			if len(configuration) != 2 || configuration[1] != nil {
				t.Fatalf("unrelated server settings were changed: %+v", configuration)
			}
			if kind == "gopls" {
				if initial["fileWatcher"] != "fsnotify" || configuration[0]["fileWatcher"] != "fsnotify" {
					t.Fatalf("gopls lost watcher settings at startup or configuration: %+v, %+v", initial, configuration)
				}
				for _, settings := range []map[string]any{initial, configuration[0]} {
					patterns, ok := settings["workspaceFiles"].([]any)
					if !ok || len(patterns) != 1 || patterns[0] != "**/*.go" {
						t.Fatalf("gopls cannot discover unopened new packages after file events: %+v", settings)
					}
				}
			} else {
				if initial != nil || configuration[0] != nil {
					t.Fatalf("Go settings leaked to TypeScript: %+v, %+v", initial, configuration)
				}
				manager.RecordFileChanges([]FileChange{{Path: filepath.Join(spec.Key.LanguageRoot, "a.go"), Type: FileCreated}})
				var changes []fakeLSPFileChange
				if err := manager.Request(ctx, spec, "test/fileChanges", nil, &changes); err != nil || len(changes) != 0 {
					t.Fatalf("Go mutations leaked to TypeScript: %+v, %v", changes, err)
				}
			}
		})
	}
}
