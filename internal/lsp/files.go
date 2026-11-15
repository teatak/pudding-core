package lsp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const maxPendingFileChanges = 4096

// RecordFileChanges queues only for existing Go workspaces, including one
// currently initializing. A mutation never starts an otherwise unused server.
// The tool layer owns authorization; this layer additionally confines events
// to each process root and never reads the changed paths.
func (m *Manager) RecordFileChanges(changes []FileChange) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, entry := range m.entries {
		if key.ServerKind != "gopls" {
			continue
		}
		for _, change := range changes {
			if !filepath.IsAbs(change.Path) {
				continue
			}
			path := filepath.Clean(change.Path)
			inside := pathWithinRoot(path, key.LanguageRoot)
			if change.Type == FileChangeInvalidate {
				if inside || pathWithinRoot(key.LanguageRoot, path) {
					entry.invalid.Store(true)
					entry.files = nil
				}
				continue
			}
			if !inside || change.Type < FileCreated || change.Type > FileDeleted || entry.invalid.Load() {
				continue
			}
			if entry.files == nil {
				entry.files = make(map[string]int)
			}
			kind := change.Type
			if kind == FileChanged && (entry.files[path] == FileCreated || entry.files[path] == FileDeleted) {
				// Preserve discovery when a newly created/recreated file is
				// edited again before the next query flushes the batch.
				kind = FileCreated
			}
			entry.files[path] = kind
			if len(entry.files) > maxPendingFileChanges {
				// A new process reads the current disk state. Do not keep serving
				// an old snapshot after dropping an oversized mutation queue.
				entry.invalid.Store(true)
				entry.files = nil
			}
		}
	}
}

// flushFileChanges runs before semantic requests, after process acquisition.
// The per-entry lock preserves notification order across concurrent queries.
// gopls handles didChangeWatchedFiles before the following request, so known
// Pudding mutations do not wait for the server-side watcher's debounce timer.
func (m *Manager) flushFileChanges(ctx context.Context, process *Process) error {
	m.mu.Lock()
	entry := m.entries[process.spec.Key]
	m.mu.Unlock()
	if entry == nil || entry.process != process {
		return fmt.Errorf("%w: language workspace was replaced", ErrRequestNotSent)
	}
	// Never hold Manager.mu while waiting for a process's writer. A blocked
	// gopls stdin must not block acquisition or mutation recording for others.
	if err := lockContext(ctx, entry.flushMu); err != nil {
		return err
	}
	defer func() { <-entry.flushMu }()
	m.mu.Lock()
	if m.entries[process.spec.Key] != entry || entry.invalid.Load() {
		m.mu.Unlock()
		return fmt.Errorf("%w: language workspace requires a filesystem reload", ErrRequestNotSent)
	}
	pending := entry.files
	entry.files = nil
	m.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	paths := make([]string, 0, len(pending))
	for path := range pending {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	changes := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		changes = append(changes, map[string]any{"uri": fileURI(path), "type": pending[path]})
	}
	process.diagnostics.invalidate()
	if err := process.NotifyContext(ctx, "workspace/didChangeWatchedFiles", map[string]any{"changes": changes}); err != nil {
		entry.invalid.Store(true)
		return err
	}
	return nil
}

func pathWithinRoot(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
