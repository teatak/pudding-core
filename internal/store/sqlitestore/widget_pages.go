package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
)

const pageColumns = "id,item_id,scope,revision_hash,target_id,version,data,interaction"

func scanWidgetPage(row *sql.Row) (*store.WidgetPage, error) {
	p := &store.WidgetPage{}
	var data, interaction string
	err := row.Scan(&p.ID, &p.ItemID, &p.Scope, &p.RevisionHash, &p.TargetID, &p.Version, &data, &interaction)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Data = json.RawMessage(data)
	if interaction != "" {
		p.Interaction = json.RawMessage(interaction)
	}
	return p, nil
}
func (s *Store) OpenWidgetPage(ctx context.Context, itemID, scope, revision, target string) (*store.WidgetPage, error) {
	if len(scope) == 0 || len(scope) > 200 || len(target) == 0 || len(target) > 100 || len(revision) != 64 {
		return nil, store.ErrInvalidWidgetData
	}
	var page *store.WidgetPage
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var found string
		if err := tx.QueryRowContext(ctx, `SELECT r.hash FROM studio_item_revisions r JOIN studio_items i ON i.id=r.item_id WHERE i.id=? AND i.kind='widget' AND i.deleted=0 AND i.archived_at=0 AND r.hash=?`, itemID, revision).Scan(&found); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrNotFound
			}
			return err
		}
		if scope != "library" {
			var archived int64
			if err := tx.QueryRowContext(ctx, `SELECT archived_at FROM sessions WHERE id=?`, scope).Scan(&archived); err != nil {
				return store.ErrNotFound
			}
			if archived != 0 {
				return store.ErrNotFound
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO widget_pages(id,item_id,scope,revision_hash,target_id,version,data) VALUES(?,?,?,?,?,0,'{}') ON CONFLICT(item_id,scope,revision_hash) DO UPDATE SET target_id=excluded.target_id`, store.NewID("page"), itemID, scope, revision, target)
		if err != nil {
			return err
		}
		page, err = scanWidgetPage(tx.QueryRowContext(ctx, `SELECT `+pageColumns+` FROM widget_pages WHERE target_id=?`, target))
		return err
	})
	return page, err
}
func (s *Store) GetWidgetPageByTarget(ctx context.Context, target string) (*store.WidgetPage, error) {
	return scanWidgetPage(s.db.QueryRowContext(ctx, `SELECT `+pageColumns+` FROM widget_pages WHERE target_id=?`, target))
}
func (s *Store) WriteWidgetPage(ctx context.Context, id, target string, expected int64, data json.RawMessage) (*store.WidgetData, error) {
	var object map[string]json.RawMessage
	if expected < 0 || len(data) > contracts.Widget().MaxStorageBytes || json.Unmarshal(data, &object) != nil || object == nil {
		return nil, store.ErrInvalidWidgetData
	}
	var result *store.WidgetData
	err := s.tx(ctx, func(tx *sql.Tx) error {
		p, err := scanWidgetPage(tx.QueryRowContext(ctx, `SELECT `+pageColumns+` FROM widget_pages WHERE id=? AND target_id=?`, id, target))
		if err != nil {
			return err
		}
		if p.Version != expected {
			return store.ErrWidgetDataConflict
		}
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM studio_items WHERE id=? AND deleted=0 AND archived_at=0`, p.ItemID).Scan(&exists); err != nil {
			return store.ErrNotFound
		}
		// Each revision owns its own page snapshot; previews may save page state.
		_, err = tx.ExecContext(ctx, `UPDATE widget_pages SET version=version+1,data=? WHERE id=?`, string(data), id)
		result = &store.WidgetData{Version: expected + 1, Data: data}
		return err
	})
	return result, err
}
func (s *Store) SetWidgetPageInteraction(ctx context.Context, target string, definition json.RawMessage) error {
	result, err := s.db.ExecContext(ctx, `UPDATE widget_pages SET interaction=? WHERE target_id=?`, string(definition), target)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return store.ErrNotFound
	}
	return err
}
func (s *Store) CloseWidgetPages(ctx context.Context, itemID, scope string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM widget_pages WHERE item_id=? AND scope=?`, itemID, scope)
	return err
}
