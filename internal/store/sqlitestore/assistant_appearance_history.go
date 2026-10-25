package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

const assistantAppearanceHistorySchema = `CREATE TABLE IF NOT EXISTS assistant_appearance_history (
    assistant_id TEXT PRIMARY KEY REFERENCES assistants(id) ON DELETE CASCADE,
    history TEXT NOT NULL,
    edit_group TEXT NOT NULL,
    edit_base TEXT NOT NULL
);`

func migrateAssistantAppearanceHistoryV57(tx *sql.Tx) error {
	_, err := tx.Exec(assistantAppearanceHistorySchema + assistantAppearanceFavoritesSchema)
	return err
}

const assistantAppearanceHistoryLimit = 6

func appearanceHistoryFrom(base []store.AssistantAppearance, current store.AssistantAppearance) []store.AssistantAppearance {
	history := make([]store.AssistantAppearance, 0, assistantAppearanceHistoryLimit)
	for _, candidate := range base {
		if candidate.Equal(current) {
			continue
		}
		duplicate := false
		for _, saved := range history {
			if candidate.Equal(saved) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			history = append(history, candidate)
		}
		if len(history) == assistantAppearanceHistoryLimit {
			break
		}
	}
	return history
}

func saveAssistantAppearanceHistory(ctx context.Context, tx *sql.Tx, previous *store.Assistant, in store.SaveAssistantInput) error {
	var group string
	err := tx.QueryRowContext(ctx, `SELECT edit_group FROM assistant_appearance_history WHERE assistant_id=?`, previous.ID).Scan(&group)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if in.AppearanceEditGroup != "" && group == in.AppearanceEditGroup {
		return nil
	}
	base := append([]store.AssistantAppearance{previous.Appearance}, previous.AppearanceHistory...)
	history := previous.AppearanceHistory
	if in.AppearanceEditGroup == "" {
		// Explicit saves and one-click outfits are complete operations.
		history = appearanceHistoryFrom(base, in.Appearance)
		base = []store.AssistantAppearance{}
	}
	historyJSON, err := json.Marshal(history)
	if err != nil {
		return err
	}
	baseJSON, err := json.Marshal(base)
	if err != nil {
		return err
	}
	// Autosaves stage the opening checkpoint but never change the visible history.
	_, err = tx.ExecContext(ctx, `INSERT INTO assistant_appearance_history(assistant_id,history,edit_group,edit_base) VALUES(?,?,?,?)
ON CONFLICT(assistant_id) DO UPDATE SET history=excluded.history,edit_group=excluded.edit_group,edit_base=excluded.edit_base`, previous.ID, string(historyJSON), in.AppearanceEditGroup, string(baseJSON))
	return err
}

func (s *Store) FinishAssistantAppearanceEdit(ctx context.Context, group string) (*store.AssistantSnapshot, error) {
	var out *store.AssistantSnapshot
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		out, err = readAssistant(ctx, tx)
		if err != nil || out.Assistant == nil {
			return err
		}
		var savedGroup, encodedBase string
		err = tx.QueryRowContext(ctx, `SELECT edit_group,edit_base FROM assistant_appearance_history WHERE assistant_id=?`, out.Assistant.ID).Scan(&savedGroup, &encodedBase)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// Closing an unchanged dialog, a replay, or an older editor is a no-op.
		if savedGroup != group {
			return nil
		}
		var base []store.AssistantAppearance
		if err := json.Unmarshal([]byte(encodedBase), &base); err != nil {
			return err
		}
		if len(base) == 0 {
			return nil
		}
		history := base[1:]
		if !base[0].Equal(out.Assistant.Appearance) {
			history = appearanceHistoryFrom(base, out.Assistant.Appearance)
		}
		historyJSON, err := json.Marshal(history)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE assistant_appearance_history SET history=?,edit_base='[]' WHERE assistant_id=?`, string(historyJSON), out.Assistant.ID); err != nil {
			return err
		}
		out, err = readAssistant(ctx, tx)
		return err
	})
	return out, err
}
