package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

const sessionDirectoriesSchema = `CREATE TABLE IF NOT EXISTS session_directory_grants (
 session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
 root_dirs TEXT NOT NULL DEFAULT '[]'
);`

func (s *Store) GetSessionDirectories(ctx context.Context, id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(g.root_dirs,'[]') FROM sessions s LEFT JOIN session_directory_grants g ON g.session_id=s.id WHERE s.id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	if err := json.Unmarshal([]byte(raw), &dirs); err != nil {
		return nil, err
	}
	return store.NormalizeProjectDirs(dirs), nil
}

func setSessionDirectoriesTx(ctx context.Context, tx *sql.Tx, id string, dirs []string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO session_directory_grants(session_id,root_dirs) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET root_dirs=excluded.root_dirs`, id, encodeStringSlice(store.NormalizeProjectDirs(dirs)))
	return err
}

// Earlier development builds provisioned a project for the assistant. Preserve
// its directories as session consent, detach it and remove only unused metadata.
func migrateSessionDirectoriesV61(tx *sql.Tx) error {
	if _, err := tx.Exec(sessionDirectoriesSchema); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT s.id,s.project_id,p.root_dirs,coalesce(g.root_dirs,'[]') FROM assistant_sessions a JOIN sessions s ON s.id=a.session_id JOIN projects p ON p.id=s.project_id LEFT JOIN session_directory_grants g ON g.session_id=s.id`)
	if err != nil {
		return err
	}
	type binding struct {
		sessionID, projectID string
		dirs                 []string
	}
	var bindings []binding
	for rows.Next() {
		var b binding
		var projectJSON, sessionJSON string
		if err := rows.Scan(&b.sessionID, &b.projectID, &projectJSON, &sessionJSON); err != nil {
			rows.Close()
			return err
		}
		var projectDirs, sessionDirs []string
		if err := json.Unmarshal([]byte(projectJSON), &projectDirs); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(sessionJSON), &sessionDirs); err != nil {
			rows.Close()
			return err
		}
		b.dirs = store.NormalizeProjectDirs(append(sessionDirs, projectDirs...))
		bindings = append(bindings, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if err := setSessionDirectoriesTx(context.Background(), tx, b.sessionID, b.dirs); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE sessions SET project_id='' WHERE id=?`, b.sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM projects WHERE id=? AND NOT EXISTS(SELECT 1 FROM sessions WHERE project_id=projects.id)`, b.projectID); err != nil {
			return err
		}
	}
	return nil
}
