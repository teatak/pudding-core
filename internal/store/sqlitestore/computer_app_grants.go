package sqlitestore

import (
	"context"
	"database/sql"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) ListComputerAppGrants(ctx context.Context, sessionID string) ([]store.ComputerAppGrant, error) {
	grants := []store.ComputerAppGrant{}
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionAnyTx(ctx, tx, sessionID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT app_id, created_at FROM computer_app_grants WHERE session_id=? ORDER BY created_at, app_id`, sessionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var grant store.ComputerAppGrant
			var createdAt int64
			if err := rows.Scan(&grant.AppID, &createdAt); err != nil {
				return err
			}
			grant.CreatedAt = timeFromMS(createdAt)
			grants = append(grants, grant)
		}
		return rows.Err()
	})
	return grants, err
}

// Revocation only removes consent for this session and app. Calls already
// admitted by the engine may finish; subsequent calls read this same table.
func (s *Store) RevokeComputerAppGrant(ctx context.Context, sessionID, appID string) error {
	sessionID, appID = strings.TrimSpace(sessionID), strings.TrimSpace(appID)
	if sessionID == "" || appID == "" {
		return store.ErrInvalidComputerAppGrant
	}
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionAnyTx(ctx, tx, sessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM computer_app_grants WHERE session_id=? AND app_id=?`, sessionID, appID)
		return err
	})
}
