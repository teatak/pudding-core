package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) CreateAgentInvite(ctx context.Context, b store.ExternalAgentBinding, hash string, expires int64) error {
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := checkAgentHandoff(ctx, tx, b.SessionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM external_agent_bindings WHERE invite_hash IS NOT NULL AND (session_id=? OR expires_at<=?)`, b.SessionID, time.Now().UnixMilli()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO external_agent_bindings(id,session_id,runtime_id,invite_hash,expires_at) VALUES(?,?,?,?,?)`, b.ID, b.SessionID, b.RuntimeID, hash, expires)
		return err
	})
}

const externalAgentColumns = "id,session_id,runtime_id,agent_name,agent_title,agent_icon"

func scanAgent(row interface{ Scan(...any) error }) (*store.ExternalAgentBinding, error) {
	var b store.ExternalAgentBinding
	err := row.Scan(&b.ID, &b.SessionID, &b.RuntimeID, &b.Name, &b.Title, &b.IconPNGBase64)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	// Older bindings stored client-supplied URLs here. Only expose raw PNGs
	// written by the desktop launcher icon flow.
	if !strings.HasPrefix(b.IconPNGBase64, "iVBORw0KGgo") {
		b.IconPNGBase64 = ""
	}
	return &b, err
}

// Only a handoff can displace the owner of a running turn.
func checkAgentHandoff(ctx context.Context, tx *sql.Tx, sid string) error {
	var busy bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM external_agent_bindings WHERE session_id=? AND credential_hash IS NOT NULL)
        AND EXISTS(SELECT 1 FROM turns WHERE session_id=? AND status=?)`, sid, sid, store.TurnRunning).Scan(&busy)
	if err != nil {
		return err
	}
	if busy {
		return store.ErrTurnRunning
	}
	return nil
}

func (s *Store) RedeemAgentInvite(ctx context.Context, invite, credential string, identity store.ExternalAgentIdentity, now int64) (*store.ExternalAgentBinding, error) {
	var binding *store.ExternalAgentBinding
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		pending, err := scanAgent(tx.QueryRowContext(ctx, `SELECT `+externalAgentColumns+` FROM external_agent_bindings WHERE invite_hash=? AND expires_at>? AND session_id IN (SELECT id FROM sessions WHERE archived_at=0)`, invite, now))
		if err != nil {
			return err
		}
		if err := checkAgentHandoff(ctx, tx, pending.SessionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE external_agent_bindings SET invite_hash=NULL,credential_hash=?,agent_name=?,agent_title=?,agent_icon=? WHERE id=?`, credential, identity.Name, identity.Title, identity.IconPNGBase64, pending.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM external_agent_bindings WHERE session_id=? AND id<>?`, pending.SessionID, pending.ID); err != nil {
			return err
		}
		pending.ExternalAgentIdentity = identity
		binding = pending
		return nil
	})
	if err != nil {
		return nil, err
	}
	return binding, nil
}

func (s *Store) AgentBinding(ctx context.Context, credential string) (*store.ExternalAgentBinding, error) {
	return scanAgent(s.readDB.QueryRowContext(ctx, `SELECT `+externalAgentColumns+` FROM external_agent_bindings WHERE credential_hash=?`, credential))
}

func (s *Store) ListAgentBindings(ctx context.Context, sid string) ([]store.ExternalAgentBinding, error) {
	rows, err := s.readDB.QueryContext(ctx, `SELECT `+externalAgentColumns+` FROM external_agent_bindings WHERE session_id=? AND credential_hash IS NOT NULL ORDER BY rowid`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.ExternalAgentBinding{}
	for rows.Next() {
		b, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Store) RemoveAgentBinding(ctx context.Context, sid, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM external_agent_bindings WHERE session_id=? AND id=?`, sid, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return store.ErrNotFound
	}
	return err
}
