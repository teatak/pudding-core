package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) CreateSessionOnce(ctx context.Context, requestID string, session *store.Session) (*store.Session, error) {
	if requestID == "" {
		return nil, store.ErrInvalidSession
	}
	if err := store.NormalizeSessionProviderModel(session); err != nil {
		return nil, err
	}
	// The existing primary key holds the durable creation identity. Its separate
	// namespace avoids collisions with server-generated IDs; no mapping is needed.
	session.ID = fmt.Sprintf("sess_request_%x", sha256.Sum256([]byte(requestID)))
	var out *store.Session
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		out, err = scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionSelectColumnsAliasS+` FROM sessions s WHERE s.id=?`, session.ID))
		if err == nil {
			if out.ArchivedAt != nil {
				return store.ErrNotFound
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := createSessionTx(ctx, tx, session); err != nil {
			return err
		}
		out, err = getSessionTx(ctx, tx, session.ID)
		return err
	})
	return out, err
}
