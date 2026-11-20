package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const turnFileChangeSummaryColumns = `id,session_id,turn_id,root_path,path,original_path,kind,origin,additions,deletions,binary,too_large,old_size,new_size,snapshot_version,old_digest,new_digest,old_mode,new_mode,old_type,new_type,old_binary,new_binary,created_at`

func insertTurnFileChangesTx(ctx context.Context, tx *sql.Tx, turn *store.Turn, changes []store.TurnFileChangeInput, now time.Time) error {
	inserted := false
	for _, change := range changes {
		rootPath := change.RootPath
		path := change.Path
		if strings.TrimSpace(rootPath) == "" || strings.TrimSpace(path) == "" || !validFileChangeKind(change.Kind) {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO turn_file_changes(
			id,session_id,turn_id,root_path,path,original_path,kind,origin,additions,deletions,binary,too_large,old_size,new_size,old_content,new_content,
			snapshot_version,old_digest,new_digest,old_mode,new_mode,old_type,new_type,old_binary,new_binary,old_data,new_data,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			store.NewID("change"), turn.SessionID, turn.ID, rootPath, path, change.OriginalPath, change.Kind,
			store.NormalizeFileChangeOrigin(change.Origin),
			change.Additions, change.Deletions, boolInt(change.Binary), boolInt(change.TooLarge), change.OldSize, change.NewSize,
			change.OldContent, change.NewContent, change.SnapshotVersion, change.OldDigest, change.NewDigest,
			change.OldMode, change.NewMode, change.OldType, change.NewType, boolInt(change.OldBinary), boolInt(change.NewBinary),
			append([]byte{}, change.OldData...), append([]byte{}, change.NewData...), unixMS(now),
		)
		if err != nil {
			return err
		}
		inserted = true
	}
	if inserted {
		_, err := tx.ExecContext(ctx, `INSERT INTO turn_file_change_states(session_id,turn_id,state,updated_at) VALUES(?,?,?,?)`,
			turn.SessionID, turn.ID, store.TurnFileChangesApplied, unixMS(now))
		return err
	}
	return nil
}

func validFileChangeKind(kind store.FileChangeKind) bool {
	switch kind {
	case store.FileChangeAdded, store.FileChangeModified, store.FileChangeDeleted, store.FileChangeRenamed:
		return true
	default:
		return false
	}
}

func turnFileChangesForTurnTx(ctx context.Context, tx *sql.Tx, sessionID, turnID string) ([]*store.TurnFileChange, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+turnFileChangeSummaryColumns+` FROM turn_file_changes WHERE session_id=? AND turn_id=? ORDER BY root_path ASC, path ASC, id ASC`, sessionID, turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.TurnFileChange, 0)
	for rows.Next() {
		change, err := scanTurnFileChangeSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, change)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	store.MarkUnsafeTurnFileChangeLayouts(out)
	return out, nil
}

func (s *Store) GetTurnFileChange(ctx context.Context, sessionID, turnID, changeID string) (*store.TurnFileChange, error) {
	defer s.readLock()()
	row := s.readDB.QueryRowContext(ctx, `SELECT `+turnFileChangeSummaryColumns+`,old_content,new_content,old_data,new_data FROM turn_file_changes WHERE session_id=? AND turn_id=? AND id=?`, sessionID, turnID, changeID)
	change, err := scanTurnFileChangeDetail(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return change, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTurnFileChangeSummary(scanner rowScanner) (*store.TurnFileChange, error) {
	var change store.TurnFileChange
	var kind, origin string
	var binary, tooLarge, oldBinary, newBinary int
	var createdAt int64
	err := scanner.Scan(
		&change.ID, &change.SessionID, &change.TurnID, &change.RootPath, &change.Path, &change.OriginalPath, &kind,
		&origin,
		&change.Additions, &change.Deletions, &binary, &tooLarge, &change.OldSize, &change.NewSize,
		&change.SnapshotVersion, &change.OldDigest, &change.NewDigest, &change.OldMode, &change.NewMode,
		&change.OldType, &change.NewType, &oldBinary, &newBinary, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	change.Kind = store.FileChangeKind(kind)
	change.Origin = store.NormalizeFileChangeOrigin(store.FileChangeOrigin(origin))
	change.Binary = binary != 0
	change.TooLarge = tooLarge != 0
	change.OldBinary = oldBinary != 0
	change.NewBinary = newBinary != 0
	change.Reversible = turnFileChangeReversible(&change)
	change.CreatedAt = timeFromMS(createdAt)
	return &change, nil
}

func scanTurnFileChangeDetail(scanner rowScanner) (*store.TurnFileChange, error) {
	var change store.TurnFileChange
	var kind, origin string
	var binary, tooLarge, oldBinary, newBinary int
	var createdAt int64
	err := scanner.Scan(
		&change.ID, &change.SessionID, &change.TurnID, &change.RootPath, &change.Path, &change.OriginalPath, &kind,
		&origin,
		&change.Additions, &change.Deletions, &binary, &tooLarge, &change.OldSize, &change.NewSize,
		&change.SnapshotVersion, &change.OldDigest, &change.NewDigest, &change.OldMode, &change.NewMode,
		&change.OldType, &change.NewType, &oldBinary, &newBinary, &createdAt, &change.OldContent, &change.NewContent,
		&change.OldData, &change.NewData,
	)
	if err != nil {
		return nil, err
	}
	change.Kind = store.FileChangeKind(kind)
	change.Origin = store.NormalizeFileChangeOrigin(store.FileChangeOrigin(origin))
	change.Binary = binary != 0
	change.TooLarge = tooLarge != 0
	change.OldBinary = oldBinary != 0
	change.NewBinary = newBinary != 0
	change.Reversible = turnFileChangeReversible(&change)
	change.CreatedAt = timeFromMS(createdAt)
	return &change, nil
}

func turnFileChangeReversible(change *store.TurnFileChange) bool {
	if change == nil || change.SnapshotVersion != 1 || change.TooLarge {
		return false
	}
	valid := func(kind, digest string) bool { return kind == "" || (kind == "file" && digest != "") }
	return valid(change.OldType, change.OldDigest) && valid(change.NewType, change.NewDigest)
}

// updated_at also serves as a strictly increasing revision for replay state.
// Existing databases already have this value; no snapshot migration is needed.
func turnFileChangeStateForTurnTx(ctx context.Context, tx *sql.Tx, sessionID, turnID string) (store.TurnFileChangeState, int64, error) {
	var state string
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT state,updated_at FROM turn_file_change_states WHERE session_id=? AND turn_id=?`, sessionID, turnID).Scan(&state, &revision); errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	} else if err != nil {
		return "", 0, err
	}
	return store.TurnFileChangeState(state), revision, nil
}

type turnFileChangeStateSnapshot struct {
	state    store.TurnFileChangeState
	revision int64
}

// turnFileChangesForTurnsTx 按 turn_id 批量读取文件变更摘要，避免分页逐条查询。
func turnFileChangesForTurnsTx(ctx context.Context, tx *sql.Tx, sessionID string, turnIDs []string) (map[string][]*store.TurnFileChange, error) {
	out := make(map[string][]*store.TurnFileChange, len(turnIDs))
	if len(turnIDs) == 0 {
		return out, nil
	}
	err := forEachSQLInBatch(turnIDs, []any{sessionID}, func(placeholders string, args []any) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+turnFileChangeSummaryColumns+` FROM turn_file_changes WHERE session_id=? AND turn_id IN (`+placeholders+`) ORDER BY root_path ASC, path ASC, id ASC`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			change, err := scanTurnFileChangeSummary(rows)
			if err != nil {
				return err
			}
			out[change.TurnID] = append(out[change.TurnID], change)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return rows.Close()
	})
	if err != nil {
		return nil, err
	}
	for turnID, changes := range out {
		store.MarkUnsafeTurnFileChangeLayouts(changes)
		out[turnID] = changes
	}
	return out, nil
}

// turnFileChangeStatesForTurnsTx 按 turn_id 批量读取文件变更状态与版本号。
func turnFileChangeStatesForTurnsTx(ctx context.Context, tx *sql.Tx, sessionID string, turnIDs []string) (map[string]turnFileChangeStateSnapshot, error) {
	out := make(map[string]turnFileChangeStateSnapshot, len(turnIDs))
	if len(turnIDs) == 0 {
		return out, nil
	}
	err := forEachSQLInBatch(turnIDs, []any{sessionID}, func(placeholders string, args []any) error {
		rows, err := tx.QueryContext(ctx, `SELECT turn_id,state,updated_at FROM turn_file_change_states WHERE session_id=? AND turn_id IN (`+placeholders+`)`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var turnID, state string
			var revision int64
			if err := rows.Scan(&turnID, &state, &revision); err != nil {
				return err
			}
			out[turnID] = turnFileChangeStateSnapshot{state: store.TurnFileChangeState(state), revision: revision}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return rows.Close()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) UpdateTurnFileChangeState(ctx context.Context, sessionID, turnID string, expected, next store.TurnFileChangeState, expectedRevision int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var revision int64
	err := s.db.QueryRowContext(ctx, `UPDATE turn_file_change_states SET state=?,updated_at=MAX(updated_at+1,?) WHERE session_id=? AND turn_id=? AND state=? AND updated_at=? RETURNING updated_at`,
		next, unixMS(time.Now()), sessionID, turnID, expected, expectedRevision).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, store.ErrTurnFileChangeConflict
	}
	return revision, err
}
