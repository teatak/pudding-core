package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
)

const sessionPluginConnectionsSchema = `CREATE TABLE IF NOT EXISTS session_plugin_connections (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    plugin_id TEXT NOT NULL,
    connection_id TEXT NOT NULL,
    identity TEXT NOT NULL,
    PRIMARY KEY (session_id, plugin_id)
);`

func (s *Store) GetSessionPluginConnection(ctx context.Context, sessionID, pluginID string) (*store.SessionPluginConnection, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	var out store.SessionPluginConnection
	err := s.db.QueryRowContext(ctx, `SELECT connection_id, identity FROM session_plugin_connections WHERE session_id=? AND plugin_id=?`, sessionID, store.CurrentPluginID(pluginID)).Scan(&out.ConnectionID, &out.Identity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &out, err
}

func (s *Store) SetSessionPluginConnection(ctx context.Context, sessionID, pluginID, connectionID, identity string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, sessionID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	} else if err != nil {
		return err
	}
	if connectionID == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM session_plugin_connections WHERE session_id=? AND plugin_id=?`, sessionID, store.CurrentPluginID(pluginID))
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO session_plugin_connections(session_id,plugin_id,connection_id,identity) VALUES(?,?,?,?) ON CONFLICT(session_id,plugin_id) DO UPDATE SET connection_id=excluded.connection_id,identity=excluded.identity`, sessionID, store.CurrentPluginID(pluginID), connectionID, identity)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
