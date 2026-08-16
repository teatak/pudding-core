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
		rootPath := strings.TrimSpace(change.RootPath)
		path := strings.TrimSpace(change.Path)
		if rootPath == "" || path == "" || !validFileChangeKind(change.Kind) {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO turn_file_changes(
			id,session_id,turn_id,root_path,path,original_path,kind,origin,additions,deletions,binary,too_large,old_size,new_size,old_content,new_content,
			snapshot_version,old_digest,new_digest,old_mode,new_mode,old_type,new_type,old_binary,new_binary,old_data,new_data,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			store.NewID("change"), turn.SessionID, turn.ID, rootPath, path, strings.TrimSpace(change.OriginalPath), change.Kind,
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
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRowContext(ctx, `SELECT `+turnFileChangeSummaryColumns+`,old_content,new_content,old_data,new_data FROM turn_file_changes WHERE session_id=? AND turn_id=? AND id=?`, sessionID, turnID, changeID)
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

func turnFileChangeStateForTurnTx(ctx context.Context, tx *sql.Tx, sessionID, turnID string) (store.TurnFileChangeState, error) {
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM turn_file_change_states WHERE session_id=? AND turn_id=?`, sessionID, turnID).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return store.TurnFileChangeState(state), nil
}

func (s *Store) UpdateTurnFileChangeState(ctx context.Context, sessionID, turnID string, expected, next store.TurnFileChangeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE turn_file_change_states SET state=?,updated_at=? WHERE session_id=? AND turn_id=? AND state=?`,
		next, unixMS(time.Now()), sessionID, turnID, expected)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return store.ErrTurnFileChangeConflict
	}
	return nil
}
