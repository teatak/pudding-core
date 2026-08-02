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

type Tracker struct {
	mu    sync.Mutex
	turns map[string]*turnState
}

type turnState struct {
	calls   map[string]*callSnapshot
	initial map[fileKey]fileSnapshot
	final   map[fileKey]fileSnapshot
	touched map[fileKey]struct{}
	origins map[fileKey]store.FileChangeOrigin
}

type callSnapshot struct {
	roots   []string
	targets []string
	before  map[fileKey]fileSnapshot
	origin  store.FileChangeOrigin
	ready   bool
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
	return &Tracker{turns: make(map[string]*turnState)}
}

// BeginCall captures only the explicit paths owned by one structured tool call.
// Empty, invalid, or project-root targets are ignored and never expand into a
// whole-project scan.
func (t *Tracker) BeginCall(turnID, callID string, roots, targets []string) error {
	return t.BeginCallWithOrigin(turnID, callID, roots, targets, store.FileChangeOriginStructured)
}

// BeginCallWithOrigin captures explicit targets and records how the mutation
// was attributed. It never expands an empty or project-root target.
func (t *Tracker) BeginCallWithOrigin(turnID, callID string, roots, targets []string, origin store.FileChangeOrigin) error {
	turnID = strings.TrimSpace(turnID)
	callID = strings.TrimSpace(callID)
	if turnID == "" || callID == "" {
		return nil
	}
	roots = normalizeRoots(roots)
	if len(roots) == 0 {
		return nil
	}
	targets = normalizeTargets(roots, targets)
	if len(targets) == 0 {
		return nil
	}

	t.mu.Lock()
	tracked := t.turns[turnID]
	if tracked == nil {
		tracked = newTurnState()
		t.turns[turnID] = tracked
	}
	call := &callSnapshot{
		roots:   roots,
		targets: targets,
		origin:  store.NormalizeFileChangeOrigin(origin),
	}
	tracked.calls[callID] = call
	t.mu.Unlock()

	before, err := snapshotScope(roots, targets)
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.turns[turnID]
	if current != tracked || current.calls[callID] != call {
		return err
	}
	if err != nil {
		delete(current.calls, callID)
		if len(current.calls) == 0 && len(current.touched) == 0 {
			delete(t.turns, turnID)
		}
		return err
	}
	call.before = before
	call.ready = true
	return nil
}

// EndCall captures the same scope after the tool returns and folds its net
// mutations into the turn. It does not attribute changes outside this call.
func (t *Tracker) EndCall(turnID, callID string) error {
	turnID = strings.TrimSpace(turnID)
	callID = strings.TrimSpace(callID)
	t.mu.Lock()
	tracked := t.turns[turnID]
	if tracked == nil {
		t.mu.Unlock()
		return nil
	}
	call, ok := tracked.calls[callID]
	t.mu.Unlock()
	if !ok || !call.ready {
		return nil
	}
	after, err := snapshotScope(call.roots, call.targets)
	if err != nil {
		t.mu.Lock()
		if current := t.turns[turnID]; current == tracked && current.calls[callID] == call {
			delete(current.calls, callID)
		}
		t.mu.Unlock()
		return err
	}
	changed := changedFileKeys(call.before, after)

	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.turns[turnID]
	if current != tracked || current.calls[callID] != call {
		return nil
	}
	delete(current.calls, callID)
	for _, key := range changed {
		if _, seen := current.touched[key]; !seen {
			current.touched[key] = struct{}{}
			if file, exists := call.before[key]; exists {
				current.initial[key] = file
			}
		}
		if file, exists := after[key]; exists {
			current.final[key] = file
		} else {
			delete(current.final, key)
		}
		current.origins[key] = mergeFileChangeOrigin(current.origins[key], call.origin)
	}
	return nil
}

func (t *Tracker) Finish(turnID string) ([]store.TurnFileChangeInput, error) {
	turnID = strings.TrimSpace(turnID)
	t.mu.Lock()
	tracked := t.turns[turnID]
	delete(t.turns, turnID)
	t.mu.Unlock()
	if tracked == nil || len(tracked.touched) == 0 {
		return nil, nil
	}
	changes := compareSnapshots(tracked.initial, tracked.final)
	for i := range changes {
		key := fileKey{root: changes[i].RootPath, path: changes[i].Path}
		origin := tracked.origins[key]
		if changes[i].Kind == store.FileChangeRenamed && changes[i].OriginalPath != "" {
			original := fileKey{root: changes[i].RootPath, path: changes[i].OriginalPath}
			origin = mergeFileChangeOrigin(origin, tracked.origins[original])
		}
		changes[i].Origin = store.NormalizeFileChangeOrigin(origin)
	}
	return changes, nil
}

func (t *Tracker) Discard(turnID string) {
	t.mu.Lock()
	delete(t.turns, strings.TrimSpace(turnID))
	t.mu.Unlock()
}

func newTurnState() *turnState {
	return &turnState{
		calls:   make(map[string]*callSnapshot),
		initial: make(map[fileKey]fileSnapshot),
		final:   make(map[fileKey]fileSnapshot),
		touched: make(map[fileKey]struct{}),
		origins: make(map[fileKey]store.FileChangeOrigin),
	}
}

func mergeFileChangeOrigin(current, next store.FileChangeOrigin) store.FileChangeOrigin {
	if current == store.FileChangeOriginCommandObserved || next == store.FileChangeOriginCommandObserved {
		return store.FileChangeOriginCommandObserved
	}
	return store.FileChangeOriginStructured
}

func changedFileKeys(before, after map[fileKey]fileSnapshot) []fileKey {
	keys := make(map[fileKey]struct{}, len(before)+len(after))
	for key, file := range before {
		if next, ok := after[key]; !ok || file.digest != next.digest {
			keys[key] = struct{}{}
		}
	}
	for key, file := range after {
		if previous, ok := before[key]; !ok || file.digest != previous.digest {
			keys[key] = struct{}{}
		}
	}
	out := make([]fileKey, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].root != out[j].root {
			return out[i].root < out[j].root
		}
		return out[i].path < out[j].path
	})
	return out
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

func normalizeTargets(roots, targets []string) []string {
	normalized := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, raw := range targets {
		raw = strings.TrimSpace(raw)
		if raw == "" || !filepath.IsAbs(raw) {
			continue
		}
		root, target, _, err := projectpath.Resolve(roots, raw, true, true)
		if err != nil {
			continue
		}
		if filepath.Clean(target) == filepath.Clean(root) {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		normalized = append(normalized, target)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if len(normalized[i]) != len(normalized[j]) {
			return len(normalized[i]) < len(normalized[j])
		}
		return normalized[i] < normalized[j]
	})
	out := make([]string, 0, len(normalized))
	for _, target := range normalized {
		nested := false
		for _, parent := range out {
			if projectpath.Inside(target, parent) {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, target)
		}
	}
	return out
}

func snapshotScope(roots, targets []string) (map[fileKey]fileSnapshot, error) {
	if len(targets) == 0 {
		return map[fileKey]fileSnapshot{}, nil
	}
	out := make(map[fileKey]fileSnapshot)
	remainingContentBytes := int64(maxSnapshotTotalContentBytes)
	for _, target := range targets {
		root := containingRoot(roots, target)
		if root == "" {
			continue
		}
		if err := snapshotPath(out, root, target, &remainingContentBytes); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func containingRoot(roots []string, path string) string {
	for _, root := range roots {
		if projectpath.Inside(path, root) {
			return root
		}
	}
	return ""
}

func snapshotPath(out map[fileKey]fileSnapshot, root, target string, remainingContentBytes *int64) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		relative, err := filepath.Rel(root, target)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
		if file, ok := readSnapshotFile(target, remainingContentBytes); ok {
			out[fileKey{root: root, path: filepath.ToSlash(relative)}] = file
		}
		return nil
	}
	return filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == target && errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if path == target {
				return walkErr
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil
		}
		file, ok := readSnapshotFile(path, remainingContentBytes)
		if ok {
			out[fileKey{root: root, path: filepath.ToSlash(relative)}] = file
		}
		return nil
	})
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
