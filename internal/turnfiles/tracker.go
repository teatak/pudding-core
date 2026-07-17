package turnfiles

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/teatak/pudding-core/internal/projectpath"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	maxSnapshotContentBytes      = 2 << 20
	maxSnapshotTotalContentBytes = 64 << 20
)

var ignoredDirectories = map[string]struct{}{
	".cache":        {},
	".git":          {},
	".gradle":       {},
	".mypy_cache":   {},
	".next":         {},
	".nuxt":         {},
	".pytest_cache": {},
	".ruff_cache":   {},
	".tox":          {},
	".turbo":        {},
	".venv":         {},
	"__pycache__":   {},
	"build":         {},
	"coverage":      {},
	"dist":          {},
	"node_modules":  {},
	"target":        {},
	"vendor":        {},
	"venv":          {},
}

type Tracker struct {
	mu    sync.Mutex
	turns map[string]*turnSnapshot
}

type turnSnapshot struct {
	roots    []string
	baseline map[fileKey]fileSnapshot
}

type fileKey struct {
	root string
	path string
}

type fileSnapshot struct {
	binary   bool
	content  string
	digest   string
	size     int64
	tooLarge bool
}

func New() *Tracker {
	return &Tracker{turns: make(map[string]*turnSnapshot)}
}

// EnsureBaseline captures each newly authorized root before a Code tool can
// mutate it. Repeated calls are cheap once the root belongs to this turn.
func (t *Tracker) EnsureBaseline(turnID string, roots []string) error {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return nil
	}
	roots = normalizeRoots(roots)
	if len(roots) == 0 {
		return nil
	}

	t.mu.Lock()
	tracked := t.turns[turnID]
	existing := make(map[string]struct{})
	if tracked != nil {
		for _, root := range tracked.roots {
			existing[root] = struct{}{}
		}
	}
	t.mu.Unlock()

	missing := make([]string, 0, len(roots))
	for _, root := range roots {
		if _, ok := existing[root]; !ok {
			missing = append(missing, root)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	baseline, err := snapshotRoots(missing)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	tracked = t.turns[turnID]
	if tracked == nil {
		tracked = &turnSnapshot{baseline: make(map[fileKey]fileSnapshot)}
		t.turns[turnID] = tracked
	}
	known := make(map[string]struct{}, len(tracked.roots))
	for _, root := range tracked.roots {
		known[root] = struct{}{}
	}
	for _, root := range missing {
		if _, ok := known[root]; ok {
			continue
		}
		tracked.roots = append(tracked.roots, root)
		known[root] = struct{}{}
	}
	for key, file := range baseline {
		if _, ok := tracked.baseline[key]; !ok {
			tracked.baseline[key] = file
		}
	}
	sort.Strings(tracked.roots)
	return nil
}

func (t *Tracker) Finish(turnID string) ([]store.TurnFileChangeInput, error) {
	turnID = strings.TrimSpace(turnID)
	t.mu.Lock()
	tracked := t.turns[turnID]
	delete(t.turns, turnID)
	t.mu.Unlock()
	if tracked == nil || len(tracked.roots) == 0 {
		return nil, nil
	}
	after, err := snapshotRoots(tracked.roots)
	if err != nil {
		return nil, err
	}
	return compareSnapshots(tracked.baseline, after), nil
}

func (t *Tracker) Discard(turnID string) {
	t.mu.Lock()
	delete(t.turns, strings.TrimSpace(turnID))
	t.mu.Unlock()
}

func normalizeRoots(roots []string) []string {
	normalized := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, raw := range roots {
		root := filepath.Clean(strings.TrimSpace(raw))
		if root == "." || root == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			continue
		}
		resolved = filepath.Clean(resolved)
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		normalized = append(normalized, resolved)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if len(normalized[i]) != len(normalized[j]) {
			return len(normalized[i]) < len(normalized[j])
		}
		return normalized[i] < normalized[j]
	})
	out := make([]string, 0, len(normalized))
	for _, root := range normalized {
		nested := false
		for _, parent := range out {
			if projectpath.Inside(root, parent) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, root)
		}
	}
	return out
}

func snapshotRoots(roots []string) (map[fileKey]fileSnapshot, error) {
	out := make(map[fileKey]fileSnapshot)
	remainingContentBytes := int64(maxSnapshotTotalContentBytes)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				// A tool may intentionally remove the project root. Treat a missing
				// root as an empty after-snapshot so baseline files become deletions.
				if path == root && errors.Is(walkErr, os.ErrNotExist) {
					return nil
				}
				if path == root {
					return walkErr
				}
				return nil
			}
			if entry.IsDir() {
				if path != root {
					if _, ignored := ignoredDirectories[entry.Name()]; ignored {
						return filepath.SkipDir
					}
				}
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil
			}
			file, ok := readSnapshotFile(path, &remainingContentBytes)
			if !ok {
				return nil
			}
			out[fileKey{root: root, path: filepath.ToSlash(relative)}] = file
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func readSnapshotFile(path string, remainingContentBytes *int64) (fileSnapshot, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return fileSnapshot{}, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return fileSnapshot{}, false
		}
		data := []byte(target)
		return snapshotFromBytes(data, remainingContentBytes), true
	}
	if !info.Mode().IsRegular() {
		return fileSnapshot{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return fileSnapshot{}, false
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() {
		return fileSnapshot{}, false
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(currentInfo, openedInfo) {
		return fileSnapshot{}, false
	}
	if openedInfo.Size() > maxSnapshotContentBytes {
		identity := fmt.Sprintf("large:%d:%d", openedInfo.Size(), openedInfo.ModTime().UnixNano())
		return fileSnapshot{digest: identity, size: openedInfo.Size(), tooLarge: true}, true
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSnapshotContentBytes+1))
	if err != nil {
		return fileSnapshot{}, false
	}
	if len(data) > maxSnapshotContentBytes {
		latestInfo, statErr := file.Stat()
		if statErr != nil {
			latestInfo = openedInfo
		}
		identity := fmt.Sprintf("large:%d:%d", latestInfo.Size(), latestInfo.ModTime().UnixNano())
		return fileSnapshot{digest: identity, size: latestInfo.Size(), tooLarge: true}, true
	}
	if int64(len(data)) > *remainingContentBytes {
		snapshot := snapshotFromBytes(data, nil)
		snapshot.content = ""
		snapshot.tooLarge = true
		return snapshot, true
	}
	return snapshotFromBytes(data, remainingContentBytes), true
}

func snapshotFromBytes(data []byte, remainingContentBytes *int64) fileSnapshot {
	sum := sha256.Sum256(data)
	binary := !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0
	file := fileSnapshot{binary: binary, digest: fmt.Sprintf("%x", sum), size: int64(len(data))}
	if !binary {
		if remainingContentBytes == nil || int64(len(data)) <= *remainingContentBytes {
			file.content = string(data)
			if remainingContentBytes != nil {
				*remainingContentBytes -= int64(len(data))
			}
		} else {
			file.tooLarge = true
		}
	}
	return file
}

func compareSnapshots(before, after map[fileKey]fileSnapshot) []store.TurnFileChangeInput {
	changes := make([]store.TurnFileChangeInput, 0)
	deleted := make(map[fileKey]fileSnapshot)
	added := make(map[fileKey]fileSnapshot)

	for key, oldFile := range before {
		newFile, exists := after[key]
		if !exists {
			deleted[key] = oldFile
			continue
		}
		if oldFile.digest == newFile.digest {
			continue
		}
		changes = append(changes, buildChange(store.FileChangeModified, key, "", oldFile, newFile))
	}
	for key, newFile := range after {
		if _, exists := before[key]; !exists {
			added[key] = newFile
		}
	}

	usedAdded := make(map[fileKey]struct{})
	deletedKeys := sortedFileKeys(deleted)
	addedKeys := sortedFileKeys(added)
	for _, oldKey := range deletedKeys {
		oldFile := deleted[oldKey]
		var renamedKey *fileKey
		for _, newKey := range addedKeys {
			newFile := added[newKey]
			if oldKey.root != newKey.root || oldFile.digest == "" || oldFile.digest != newFile.digest {
				continue
			}
			if _, used := usedAdded[newKey]; used {
				continue
			}
			candidate := newKey
			renamedKey = &candidate
			break
		}
		if renamedKey != nil {
			usedAdded[*renamedKey] = struct{}{}
			changes = append(changes, buildChange(store.FileChangeRenamed, *renamedKey, oldKey.path, oldFile, added[*renamedKey]))
			continue
		}
		changes = append(changes, buildChange(store.FileChangeDeleted, oldKey, "", oldFile, fileSnapshot{}))
	}
	for _, key := range addedKeys {
		newFile := added[key]
		if _, used := usedAdded[key]; used {
			continue
		}
		changes = append(changes, buildChange(store.FileChangeAdded, key, "", fileSnapshot{}, newFile))
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].RootPath != changes[j].RootPath {
			return changes[i].RootPath < changes[j].RootPath
		}
		return changes[i].Path < changes[j].Path
	})
	return changes
}

func sortedFileKeys(files map[fileKey]fileSnapshot) []fileKey {
	keys := make([]fileKey, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].root != keys[j].root {
			return keys[i].root < keys[j].root
		}
		return keys[i].path < keys[j].path
	})
	return keys
}

func buildChange(kind store.FileChangeKind, key fileKey, originalPath string, oldFile, newFile fileSnapshot) store.TurnFileChangeInput {
	change := store.TurnFileChangeInput{
		RootPath:     key.root,
		Path:         key.path,
		OriginalPath: originalPath,
		Kind:         kind,
		Binary:       oldFile.binary || newFile.binary,
		TooLarge:     oldFile.tooLarge || newFile.tooLarge,
		OldSize:      oldFile.size,
		NewSize:      newFile.size,
	}
	if !change.Binary && !change.TooLarge {
		change.OldContent = oldFile.content
		change.NewContent = newFile.content
		change.Additions, change.Deletions = lineStats(change.OldContent, change.NewContent)
	}
	return change
}

func lineStats(oldContent, newContent string) (int, int) {
	if oldContent == newContent {
		return 0, 0
	}
	dmp := diffmatchpatch.New()
	oldChars, newChars, lines := dmp.DiffLinesToChars(oldContent, newContent)
	diffs := dmp.DiffMain(oldChars, newChars, false)
	diffs = dmp.DiffCharsToLines(diffs, lines)
	additions := 0
	deletions := 0
	for _, diff := range diffs {
		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			additions += logicalLineCount(diff.Text)
		case diffmatchpatch.DiffDelete:
			deletions += logicalLineCount(diff.Text)
		}
	}
	return additions, deletions
}

func logicalLineCount(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}
	return count
}
