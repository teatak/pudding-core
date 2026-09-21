package sqlitestore

import (
	"context"
	"database/sql"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) CreateChildSession(ctx context.Context, parentSessionID string, child *store.Session) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		parent, err := getSessionTx(ctx, tx, parentSessionID)
		if err != nil {
			return err
		}
		owner, err := parentSessionIDTx(ctx, tx, parentSessionID)
		if err != nil {
			return err
		}
		if owner != "" {
			return store.ErrInvalidSessionRelation
		}
		if err := store.PrepareChildSession(parent, child); err != nil {
			return err
		}
		if err := createSessionTx(ctx, tx, child); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO session_children(child_session_id,parent_session_id) VALUES(?,?)`, child.ID, parentSessionID)
		return err
	})
}

func (s *Store) ParentSessionID(ctx context.Context, sessionID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var parentID string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(c.parent_session_id,'') FROM sessions s
		LEFT JOIN session_children c ON c.child_session_id=s.id WHERE s.id=?`, sessionID).Scan(&parentID)
	if err == sql.ErrNoRows {
		return "", store.ErrNotFound
	}
	return parentID, err
}

func parentSessionIDTx(ctx context.Context, tx *sql.Tx, sessionID string) (string, error) {
	var parentID string
	err := tx.QueryRowContext(ctx, `SELECT parent_session_id FROM session_children WHERE child_session_id=?`, sessionID).Scan(&parentID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return parentID, err
}

// ListChildSessions includes archived children for group lifecycle operations.
func (s *Store) ListChildSessions(ctx context.Context, parentSessionID string) ([]*store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)`, parentSessionID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, store.ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionSelectColumnsAliasS+` FROM sessions s
		JOIN session_children c ON c.child_session_id=s.id WHERE c.parent_session_id=?
		ORDER BY s.created_at,s.id`, parentSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.Session, 0)
	for rows.Next() {
		child, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, rows.Err()
}
