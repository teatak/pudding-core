package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) PutSessionAppGrant(ctx context.Context, grant *store.SessionAppGrant) (*store.SessionAppGrant, error) {
	if err := store.NormalizeSessionAppGrant(grant); err != nil {
		return nil, err
	}
	var out *store.SessionAppGrant
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, grant.SessionID); err != nil {
			return err
		}
		now := time.Now()
		var created int64
		err := tx.QueryRowContext(ctx,
			`SELECT created_at FROM session_app_grants WHERE session_id=? AND app_id=? AND connection_id=?`,
			grant.SessionID, grant.AppID, grant.ConnectionID,
		).Scan(&created)
		switch {
		case err == nil:
		case errors.Is(err, sql.ErrNoRows):
			created = unixMS(now)
		default:
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO session_app_grants(session_id,app_id,connection_id,allowed_endpoints,permissions,constraints,created_at,updated_at)
			 VALUES(?,?,?,?,?,?,?,?)
			 ON CONFLICT(session_id,app_id,connection_id) DO UPDATE SET
			   allowed_endpoints=excluded.allowed_endpoints,
			   permissions=excluded.permissions,
			   constraints=excluded.constraints,
			   updated_at=excluded.updated_at`,
			grant.SessionID, grant.AppID, grant.ConnectionID,
			encodeStringSlice(grant.AllowedEndpoints), encodeStringSlice(grant.Permissions), string(grant.Constraints),
			created, unixMS(now),
		)
		if err != nil {
			return err
		}
		out = cloneSessionAppGrant(grant)
		out.CreatedAt = timeFromMS(created)
		out.UpdatedAt = now
		return nil
	})
	return out, err
}

func (s *Store) ListSessionAppGrants(ctx context.Context, sessionID string) ([]*store.SessionAppGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(sessionID) == "" {
		return nil, store.ErrNotFound
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, sessionID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id,app_id,connection_id,allowed_endpoints,permissions,constraints,created_at,updated_at
		 FROM session_app_grants WHERE session_id=? ORDER BY app_id, connection_id`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.SessionAppGrant, 0)
	for rows.Next() {
		grant, err := scanSessionAppGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSessionAppGrant(ctx context.Context, sessionID, appID, connectionID string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM session_app_grants WHERE session_id=? AND app_id=? AND connection_id=?`,
			sessionID, appID, connectionID,
		)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}

func scanSessionAppGrant(row messageScanner) (*store.SessionAppGrant, error) {
	var grant store.SessionAppGrant
	var endpoints, permissions string
	var constraints string
	var created, updated int64
	if err := row.Scan(&grant.SessionID, &grant.AppID, &grant.ConnectionID, &endpoints, &permissions, &constraints, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	grant.AllowedEndpoints = decodeStringSlice(endpoints)
	grant.Permissions = decodeStringSlice(permissions)
	grant.Constraints = []byte(constraints)
	grant.CreatedAt = timeFromMS(created)
	grant.UpdatedAt = timeFromMS(updated)
	if err := store.NormalizeSessionAppGrant(&grant); err != nil {
		return nil, err
	}
	grant.CreatedAt = timeFromMS(created)
	grant.UpdatedAt = timeFromMS(updated)
	return &grant, nil
}

func cloneSessionAppGrant(grant *store.SessionAppGrant) *store.SessionAppGrant {
	if grant == nil {
		return nil
	}
	cp := *grant
	cp.AllowedEndpoints = append([]string(nil), grant.AllowedEndpoints...)
	cp.Permissions = append([]string(nil), grant.Permissions...)
	cp.Constraints = append([]byte(nil), grant.Constraints...)
	return &cp
}
