package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/lsp"
)

const maxCodeMutationEntries = 4096

// trackCodeFileChanges observes only the mutation's already-authorized targets.
// It does not enumerate workspaces on reads and does not start language servers.
// Unknown command effects and external edits use gopls' asynchronous watcher.
func (r *BuiltinRunner) trackCodeFileChanges(ctx context.Context, call Call) (context.Context, func(Result)) {
	noop := func(Result) {}
	if _, ok := r.languageService.(lsp.FileChangeRecorder); !ok {
		return ctx, noop
	}
	mutation, ok := MutationTrackingForCall(call)
	if !ok || len(mutation.Targets)+len(mutation.ExactFiles) == 0 {
		return ctx, noop
	}
	roots := append([]string(nil), call.ProjectDirs...)
	targets := append(append([]string(nil), mutation.Targets...), mutation.ExactFiles...)
	before, uncertainBefore := codeMutationFiles(roots, targets)
	var once sync.Once
	complete := func() {
		once.Do(func() {
			after, uncertainAfter := codeMutationFiles(roots, targets)
			changes := make([]lsp.FileChange, 0, len(before)+len(after))
			for path := range before {
				kind := lsp.FileDeleted
				if after[path] {
					kind = lsp.FileChanged
				}
				changes = append(changes, lsp.FileChange{Path: path, Type: kind})
			}
			for path := range after {
				if !before[path] {
					changes = append(changes, lsp.FileChange{Path: path, Type: lsp.FileCreated})
				}
			}
			for _, path := range append(uncertainBefore, uncertainAfter...) {
				changes = append(changes, lsp.FileChange{Path: path, Type: lsp.FileChangeInvalidate})
			}
			// A failed tool may have made partial filesystem changes. Notification
			// follows the observed disk state, not the success bit in its result.
			r.recordCodeFileChanges(roots, changes)
		})
	}
	if call.Name == CommandRun {
		previous, _ := ctx.Value(commandCompletionKey{}).(func())
		ctx = WithCommandCompletion(ctx, func() {
			// Queue changes before an existing callback releases project locks or
			// exposes completion to a subsequent semantic query.
			complete()
			if previous != nil {
				previous()
			}
		})
	}
	return ctx, func(result Result) {
		if call.Name == CommandRun {
			var state struct {
				Yielded bool `json:"yielded"`
			}
			if json.Unmarshal([]byte(result.Content), &state) == nil && state.Yielded {
				return // The command's real completion callback owns this capture.
			}
		}
		complete()
	}
}

// codeMutationFiles lists relevant regular files inside the mutation scope,
// including a directory being copied, moved or deleted. Symlinks are never
// followed. An oversized or unreadable scope requests process invalidation
// instead of dropping changes and reusing an unverifiable workspace snapshot.
func codeMutationFiles(roots, targets []string) (map[string]bool, []string) {
	files := make(map[string]bool)
	var uncertain []string
	seen := make(map[string]bool)
	visited := 0
	for _, target := range targets {
		_, path, _, err := resolveProjectEntryPath(roots, target, true, true)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		if visited >= maxCodeMutationEntries {
			uncertain = append(uncertain, path)
			continue
		}
		err = filepath.WalkDir(path, func(name string, entry fs.DirEntry, walkErr error) error {
			visited++
			if visited > maxCodeMutationEntries {
				return errors.New("language mutation scope exceeds the file observation limit")
			}
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			_, resolved, _, err := resolveProjectEntryPath(roots, name, true, true)
			if err != nil || filepath.Clean(resolved) != filepath.Clean(name) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type().IsRegular() && codeGoWorkspaceFile(name) {
				files[resolved] = true
			}
			return nil
		})
		if err != nil {
			uncertain = append(uncertain, path)
		}
	}
	return files, uncertain
}

func codeGoWorkspaceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".mod", ".sum", ".work", ".s":
		return true
	default:
		return false
	}
}

func (r *BuiltinRunner) recordCodeFileChanges(roots []string, changes []lsp.FileChange) {
	recorder, ok := r.languageService.(lsp.FileChangeRecorder)
	if !ok {
		return
	}
	authorized := make([]lsp.FileChange, 0, len(changes))
	for _, change := range changes {
		_, path, _, err := resolveProjectEntryPath(roots, change.Path, true, true)
		if err != nil || filepath.Clean(path) != filepath.Clean(change.Path) {
			continue
		}
		change.Path = path
		authorized = append(authorized, change)
	}
	if len(authorized) > 0 {
		sort.Slice(authorized, func(i, j int) bool { return authorized[i].Path < authorized[j].Path })
		recorder.RecordFileChanges(authorized)
	}
}
