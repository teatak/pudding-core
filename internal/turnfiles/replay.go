package turnfiles

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/projectpath"
	"github.com/teatak/pudding-core/internal/store"
)

var (
	ErrReplayConflict      = errors.New("turnfiles: files changed after the turn")
	ErrReplayNotReversible = errors.New("turnfiles: turn file changes are not reversible")
	ErrReplayUnauthorized  = errors.New("turnfiles: project roots no longer authorize the changes")
	ErrReplayInvalidState  = errors.New("turnfiles: action does not match the current state")
)

type ReplayDirection string

const (
	ReplayUndo ReplayDirection = "undo"
	ReplayRedo ReplayDirection = "redo"
)

type Replayer struct {
	store store.Store
	mu    sync.Mutex
}

func NewReplayer(s store.Store) *Replayer {
	return &Replayer{store: s}
}

func (r *Replayer) Apply(ctx context.Context, sessionID, turnID string, direction ReplayDirection, roots []string) (store.TurnFileChangeState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := r.store.RunningTurn(ctx, sessionID); err == nil {
		return "", store.ErrTurnRunning
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	turn, err := r.store.GetConversationTurn(ctx, sessionID, turnID)
	if err != nil {
		return "", err
	}
	expectedState, nextState := store.TurnFileChangesApplied, store.TurnFileChangesUndone
	if direction == ReplayRedo {
		expectedState, nextState = store.TurnFileChangesUndone, store.TurnFileChangesApplied
	} else if direction != ReplayUndo {
		return "", ErrReplayInvalidState
	}
	if turn.FileChangeState != expectedState || len(turn.FileChanges) == 0 {
		return "", ErrReplayInvalidState
	}
	details := make([]*store.TurnFileChange, 0, len(turn.FileChanges))
	for _, summary := range turn.FileChanges {
		if summary == nil || !summary.Reversible {
			return "", ErrReplayNotReversible
		}
		detail, err := r.store.GetTurnFileChange(ctx, sessionID, turnID, summary.ID)
		if err != nil {
			return "", err
		}
		if !detail.Reversible {
			return "", ErrReplayNotReversible
		}
		details = append(details, detail)
	}
	actions, err := buildReplayActions(details, direction, roots)
	if err != nil {
		return "", err
	}
	tx, err := prepareReplayTransaction(actions)
	if err != nil {
		return "", err
	}
	if err := tx.install(); err != nil {
		tx.cleanup()
		return "", err
	}
	if err := r.store.UpdateTurnFileChangeState(ctx, sessionID, turnID, expectedState, nextState); err != nil {
		rollbackErr := tx.rollback()
		tx.cleanup()
		if rollbackErr != nil {
			return "", fmt.Errorf("%w; rollback: %v", err, rollbackErr)
		}
		return "", err
	}
	tx.commit()
	return nextState, nil
}

type replaySnapshot struct {
	exists bool
	digest string
	mode   os.FileMode
	data   []byte
}

type replayAction struct {
	path     string
	expected replaySnapshot
	desired  replaySnapshot
}

func buildReplayActions(changes []*store.TurnFileChange, direction ReplayDirection, roots []string) ([]replayAction, error) {
	authorized, err := authorizedReplayRoots(roots)
	if err != nil {
		return nil, err
	}
	actions := make(map[string]replayAction)
	add := func(root, rel string, expected, desired replaySnapshot) error {
		historicalRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
		if err != nil {
			return ErrReplayUnauthorized
		}
		resolvedRoot := authorized[filepath.Clean(historicalRoot)]
		if resolvedRoot == "" {
			return ErrReplayUnauthorized
		}
		target, err := replayTarget(resolvedRoot, rel)
		if err != nil {
			return err
		}
		action := replayAction{path: target, expected: expected, desired: desired}
		if previous, ok := actions[target]; ok {
			if !sameReplayAction(previous, action) {
				return ErrReplayNotReversible
			}
			return nil
		}
		actions[target] = action
		return nil
	}
	for _, change := range changes {
		oldSnapshot, newSnapshot, err := replaySnapshots(change)
		if err != nil {
			return nil, err
		}
		before, after := oldSnapshot, newSnapshot
		if direction == ReplayUndo {
			before, after = newSnapshot, oldSnapshot
		}
		switch change.Kind {
		case store.FileChangeAdded, store.FileChangeModified, store.FileChangeDeleted:
			if err := add(change.RootPath, change.Path, before, after); err != nil {
				return nil, err
			}
		case store.FileChangeRenamed:
			if strings.TrimSpace(change.OriginalPath) == "" {
				return nil, ErrReplayNotReversible
			}
			if direction == ReplayUndo {
				if err := add(change.RootPath, change.Path, newSnapshot, replaySnapshot{}); err != nil {
					return nil, err
				}
				if err := add(change.RootPath, change.OriginalPath, replaySnapshot{}, oldSnapshot); err != nil {
					return nil, err
				}
			} else {
				if err := add(change.RootPath, change.OriginalPath, oldSnapshot, replaySnapshot{}); err != nil {
					return nil, err
				}
				if err := add(change.RootPath, change.Path, replaySnapshot{}, newSnapshot); err != nil {
					return nil, err
				}
			}
		default:
			return nil, ErrReplayNotReversible
		}
	}
	out := make([]replayAction, 0, len(actions))
	for _, action := range actions {
		out = append(out, action)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

func authorizedReplayRoots(roots []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, root := range projectpath.NormalizeRoots(roots) {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			continue
		}
		resolved = filepath.Clean(resolved)
		out[resolved] = resolved
	}
	if len(out) == 0 {
		return nil, ErrReplayUnauthorized
	}
	return out, nil
}

func replayTarget(root, rel string) (string, error) {
	rel = filepath.Clean(filepath.FromSlash(strings.TrimSpace(rel)))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrReplayUnauthorized
	}
	lexical := filepath.Join(root, rel)
	_, resolved, _, err := projectpath.Resolve([]string{root}, lexical, false, true)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(lexical) {
		return "", ErrReplayUnauthorized
	}
	return lexical, nil
}

func replaySnapshots(change *store.TurnFileChange) (replaySnapshot, replaySnapshot, error) {
	if change == nil || !change.Reversible {
		return replaySnapshot{}, replaySnapshot{}, ErrReplayNotReversible
	}
	decode := func(typeName, digest string, mode uint32, binary bool, content string, data []byte) (replaySnapshot, error) {
		if typeName == "" {
			return replaySnapshot{}, nil
		}
		if typeName != "file" || digest == "" {
			return replaySnapshot{}, ErrReplayNotReversible
		}
		bytes := []byte(content)
		if binary {
			bytes = append([]byte(nil), data...)
		}
		if sum := fmt.Sprintf("%x", sha256.Sum256(bytes)); sum != digest {
			return replaySnapshot{}, ErrReplayNotReversible
		}
		return replaySnapshot{exists: true, digest: digest, mode: os.FileMode(mode).Perm(), data: bytes}, nil
	}
	oldSnapshot, err := decode(change.OldType, change.OldDigest, change.OldMode, change.OldBinary, change.OldContent, change.OldData)
	if err != nil {
		return replaySnapshot{}, replaySnapshot{}, err
	}
	newSnapshot, err := decode(change.NewType, change.NewDigest, change.NewMode, change.NewBinary, change.NewContent, change.NewData)
	return oldSnapshot, newSnapshot, err
}

func sameReplayAction(left, right replayAction) bool {
	return sameReplaySnapshot(left.expected, right.expected) && sameReplaySnapshot(left.desired, right.desired)
}

func sameReplaySnapshot(left, right replaySnapshot) bool {
	return left.exists == right.exists && left.digest == right.digest && left.mode == right.mode
}

type replayItem struct {
	action     replayAction
	tempPath   string
	backupPath string
	installed  bool
}

type replayTransaction struct {
	items       []replayItem
	createdDirs []string
}

func prepareReplayTransaction(actions []replayAction) (*replayTransaction, error) {
	if len(actions) == 0 {
		return nil, ErrReplayNotReversible
	}
	tx := &replayTransaction{items: make([]replayItem, len(actions))}
	for i, action := range actions {
		tx.items[i].action = action
		if err := validateReplayState(action); err != nil {
			tx.cleanup()
			return nil, err
		}
		if !action.desired.exists {
			continue
		}
		created, err := ensureReplayParent(filepath.Dir(action.path))
		if err != nil {
			tx.cleanup()
			return nil, err
		}
		tx.createdDirs = append(tx.createdDirs, created...)
		temp, err := os.CreateTemp(filepath.Dir(action.path), ".pudding-turn-replay-*")
		if err != nil {
			tx.cleanup()
			return nil, err
		}
		tx.items[i].tempPath = temp.Name()
		_, writeErr := temp.Write(action.desired.data)
		if writeErr == nil {
			writeErr = temp.Sync()
		}
		if writeErr == nil {
			writeErr = temp.Chmod(action.desired.mode)
		}
		if closeErr := temp.Close(); writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			tx.cleanup()
			return nil, writeErr
		}
	}
	for _, action := range actions {
		if err := validateReplayState(action); err != nil {
			tx.cleanup()
			return nil, err
		}
	}
	return tx, nil
}

func validateReplayState(action replayAction) error {
	info, err := os.Lstat(action.path)
	if !action.expected.exists {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return ErrReplayConflict
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != action.expected.mode {
		return ErrReplayConflict
	}
	file, err := os.Open(action.path)
	if err != nil {
		return ErrReplayConflict
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || fmt.Sprintf("%x", hash.Sum(nil)) != action.expected.digest {
		return ErrReplayConflict
	}
	return nil
}

func ensureReplayParent(parent string) ([]string, error) {
	missing := make([]string, 0)
	current := filepath.Clean(parent)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, ErrReplayUnauthorized
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, current)
		next := filepath.Dir(current)
		if next == current {
			return nil, ErrReplayUnauthorized
		}
		current = next
	}
	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil {
			return created, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}

func (tx *replayTransaction) install() error {
	for _, item := range tx.items {
		if err := validateReplayState(item.action); err != nil {
			_ = tx.rollback()
			return err
		}
	}
	for i := range tx.items {
		item := &tx.items[i]
		if item.action.expected.exists {
			backup, err := reserveReplayBackup(filepath.Dir(item.action.path))
			if err != nil {
				_ = tx.rollback()
				return err
			}
			if err := os.Rename(item.action.path, backup); err != nil {
				_ = tx.rollback()
				return err
			}
			item.backupPath = backup
		}
		if item.action.desired.exists {
			if !item.action.expected.exists {
				if _, err := os.Lstat(item.action.path); !errors.Is(err, os.ErrNotExist) {
					_ = tx.rollback()
					return ErrReplayConflict
				}
			}
			if err := os.Rename(item.tempPath, item.action.path); err != nil {
				_ = tx.rollback()
				return err
			}
			item.tempPath = ""
		}
		item.installed = true
	}
	return nil
}

func reserveReplayBackup(dir string) (string, error) {
	file, err := os.CreateTemp(dir, ".pudding-turn-backup-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func (tx *replayTransaction) rollback() error {
	var first error
	for i := len(tx.items) - 1; i >= 0; i-- {
		item := &tx.items[i]
		if !item.installed && item.backupPath == "" {
			continue
		}
		if item.action.desired.exists {
			if err := os.Remove(item.action.path); err != nil && !errors.Is(err, os.ErrNotExist) && first == nil {
				first = err
			}
		}
		if item.backupPath != "" {
			if err := os.Rename(item.backupPath, item.action.path); err != nil && first == nil {
				first = err
			} else if err == nil {
				item.backupPath = ""
			}
		}
		item.installed = false
	}
	return first
}

func (tx *replayTransaction) commit() {
	for i := range tx.items {
		if tx.items[i].backupPath != "" {
			_ = os.Remove(tx.items[i].backupPath)
			tx.items[i].backupPath = ""
		}
	}
	tx.cleanup()
}

func (tx *replayTransaction) cleanup() {
	for i := range tx.items {
		if tx.items[i].tempPath != "" {
			_ = os.Remove(tx.items[i].tempPath)
			tx.items[i].tempPath = ""
		}
	}
	for i := len(tx.createdDirs) - 1; i >= 0; i-- {
		_ = os.Remove(tx.createdDirs[i])
	}
}
