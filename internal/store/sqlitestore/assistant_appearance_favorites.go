package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const assistantAppearanceFavoritesSchema = `CREATE TABLE IF NOT EXISTS assistant_appearance_favorites (
    id TEXT PRIMARY KEY,
    assistant_id TEXT NOT NULL REFERENCES assistants(id) ON DELETE CASCADE,
    appearance TEXT NOT NULL,
    created_at INTEGER NOT NULL
);`

func readAssistantAppearanceFavorites(ctx context.Context, tx *sql.Tx, assistant *store.Assistant) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,appearance FROM assistant_appearance_favorites WHERE assistant_id=? ORDER BY created_at DESC,id`, assistant.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var favorite store.AssistantAppearanceFavorite
		var appearance string
		if err := rows.Scan(&favorite.ID, &appearance); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(appearance), &favorite.Appearance); err != nil {
			return err
		}
		assistant.AppearanceFavorites = append(assistant.AppearanceFavorites, favorite)
	}
	return rows.Err()
}

func (s *Store) SaveAssistantAppearanceFavorite(ctx context.Context, appearance store.AssistantAppearance) (*store.AssistantSnapshot, error) {
	data, err := json.Marshal(appearance)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	id := hex.EncodeToString(hash[:])
	var out *store.AssistantSnapshot
	err = s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, err := readAssistant(ctx, tx)
		if err != nil {
			return err
		}
		if current.Assistant == nil {
			return store.ErrNotFound
		}
		// The content hash makes repeated saves idempotent, including lost responses.
		_, err = tx.ExecContext(ctx, `INSERT INTO assistant_appearance_favorites(id,assistant_id,appearance,created_at) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING`, id, current.Assistant.ID, string(data), time.Now().UTC().UnixMilli())
		if err != nil {
			return err
		}
		out, err = readAssistant(ctx, tx)
		return err
	})
	return out, err
}

func (s *Store) DeleteAssistantAppearanceFavorite(ctx context.Context, id string) (*store.AssistantSnapshot, error) {
	var out *store.AssistantSnapshot
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM assistant_appearance_favorites WHERE assistant_id=? AND id=?`, store.PersonalAssistantID, id); err != nil {
			return err
		}
		var err error
		out, err = readAssistant(ctx, tx)
		return err
	})
	return out, err
}
