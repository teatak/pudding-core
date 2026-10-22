package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

// Receipts deliberately outlive project deletion. An old retry may report a
// missing result, but must never recreate a removed or already merged project.
const projectRequestsSchema = `CREATE TABLE IF NOT EXISTS project_requests (
 request_key TEXT PRIMARY KEY,
 request_hash TEXT NOT NULL,
 project_id TEXT NOT NULL
)`

func projectRequest(kind string, input any, keys []string) (string, string, error) {
	if len(keys) == 0 {
		return "", "", nil
	}
	if len(keys) != 1 || !nativeCreationKey.MatchString(keys[0]) {
		return "", "", store.ErrInvalidProject
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", "", err
	}
	return kind + ":" + keys[0], store.ContentHash(string(raw)), nil
}

func replayProjectRequest(ctx context.Context, tx *sql.Tx, key, hash string) (*store.Project, error) {
	if key == "" {
		return nil, nil
	}
	var saved, id string
	err := tx.QueryRowContext(ctx, `SELECT request_hash,project_id FROM project_requests WHERE request_key=?`, key).Scan(&saved, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if saved != hash {
		return nil, store.ErrProjectRequestConflict
	}
	// Return current metadata, never replay the old name, roots or timestamps.
	return getProjectTx(ctx, tx, id)
}

func saveProjectRequest(ctx context.Context, tx *sql.Tx, key, hash, id string) error {
	if key == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO project_requests(request_key,request_hash,project_id) VALUES(?,?,?)`, key, hash, id)
	return err
}
