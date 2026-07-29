package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const turnFileChangeSummaryColumns = `id,session_id,turn_id,root_path,path,original_path,kind,origin,additions,deletions,binary,too_large,old_size,new_size,created_at`

func insertTurnFileChangesTx(ctx context.Context, tx *sql.Tx, turn *store.Turn, changes []store.TurnFileChangeInput, now time.Time) error {
	for _, change := range changes {
		rootPath := strings.TrimSpace(change.RootPath)
		path := strings.TrimSpace(change.Path)
		if rootPath == "" || path == "" || !validFileChangeKind(change.Kind) {
			continue
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO turn_file_changes(
			id,session_id,turn_id,root_path,path,original_path,kind,origin,additions,deletions,binary,too_large,old_size,new_size,old_content,new_content,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			store.NewID("change"), turn.SessionID, turn.ID, rootPath, path, strings.TrimSpace(change.OriginalPath), change.Kind,
			store.NormalizeFileChangeOrigin(change.Origin),
			change.Additions, change.Deletions, boolInt(change.Binary), boolInt(change.TooLarge), change.OldSize, change.NewSize,
			change.OldContent, change.NewContent, unixMS(now),
		)
		if err != nil {
			return err
		}
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
	return out, rows.Err()
}

func (s *Store) GetTurnFileChange(ctx context.Context, sessionID, turnID, changeID string) (*store.TurnFileChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRowContext(ctx, `SELECT `+turnFileChangeSummaryColumns+`,old_content,new_content FROM turn_file_changes WHERE session_id=? AND turn_id=? AND id=?`, sessionID, turnID, changeID)
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
	var binary, tooLarge int
	var createdAt int64
	err := scanner.Scan(
		&change.ID, &change.SessionID, &change.TurnID, &change.RootPath, &change.Path, &change.OriginalPath, &kind,
		&origin,
		&change.Additions, &change.Deletions, &binary, &tooLarge, &change.OldSize, &change.NewSize, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	change.Kind = store.FileChangeKind(kind)
	change.Origin = store.NormalizeFileChangeOrigin(store.FileChangeOrigin(origin))
	change.Binary = binary != 0
	change.TooLarge = tooLarge != 0
	change.CreatedAt = timeFromMS(createdAt)
	return &change, nil
}

func scanTurnFileChangeDetail(scanner rowScanner) (*store.TurnFileChange, error) {
	var change store.TurnFileChange
	var kind, origin string
	var binary, tooLarge int
	var createdAt int64
	err := scanner.Scan(
		&change.ID, &change.SessionID, &change.TurnID, &change.RootPath, &change.Path, &change.OriginalPath, &kind,
		&origin,
		&change.Additions, &change.Deletions, &binary, &tooLarge, &change.OldSize, &change.NewSize, &createdAt,
		&change.OldContent, &change.NewContent,
	)
	if err != nil {
		return nil, err
	}
	change.Kind = store.FileChangeKind(kind)
	change.Origin = store.NormalizeFileChangeOrigin(store.FileChangeOrigin(origin))
	change.Binary = binary != 0
	change.TooLarge = tooLarge != 0
	change.CreatedAt = timeFromMS(createdAt)
	return &change, nil
}
