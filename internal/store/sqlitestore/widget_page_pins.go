package sqlitestore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

func migrateWidgetPagePins(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE widget_page_pins (
 item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
 scope TEXT NOT NULL,
 revision_hash TEXT NOT NULL,
 PRIMARY KEY(item_id,scope)
 );
 INSERT INTO widget_page_pins(item_id,scope,revision_hash)
 SELECT p.item_id,p.scope,p.revision_hash FROM widget_pages p
 JOIN studio_items i ON i.id=p.item_id AND i.active_revision=p.revision_hash;`)
	return err
}

// Resolve once per content page. A new default never changes an existing pin.
// An explicit revision switches only this scope; historical snapshots remain.
func (s *Store) SelectWidgetPage(ctx context.Context, itemID, scope, revision string) (string, error) {
	if scope == "" || len(scope) > 200 {
		return "", store.ErrInvalidWidgetData
	}
	var selected string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		w, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND kind='widget' AND deleted=0 AND archived_at=0`, itemID))
		if err != nil {
			return err
		}
		if scope != "library" {
			var n int
			if err = tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=? AND archived_at=0`, scope).Scan(&n); err != nil {
				return store.ErrNotFound
			}
		}
		err = tx.QueryRowContext(ctx, `SELECT revision_hash FROM widget_page_pins WHERE item_id=? AND scope=?`, itemID, scope).Scan(&selected)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision == "" && selected != "" {
			return nil
		}
		next := w.ActiveRevision
		if next == "" {
			selected = w.HeadRevision
			return nil
		}
		if revision != "" && revision != next {
			return store.ErrStudioItemConflict
		}
		if selected != "" && selected != next {
			// Retire other revisions; an already-rendered preview of the chosen
			// revision becomes the content page without replacing its guest.
			if _, err = tx.ExecContext(ctx, `UPDATE widget_pages SET target_id=id||? WHERE item_id=? AND scope=? AND revision_hash<>?`, store.NewID("retired"), itemID, scope, next); err != nil {
				return err
			}
		}
		selected = next
		_, err = tx.ExecContext(ctx, `INSERT INTO widget_page_pins(item_id,scope,revision_hash) VALUES(?,?,?) ON CONFLICT(item_id,scope) DO UPDATE SET revision_hash=excluded.revision_hash`, itemID, scope, next)
		return err
	})
	return selected, err
}

// A page's target is host supplied. Historical previews do not gain write or
// interaction authority merely because their revision exists on disk.
func widgetRevisionAllowed(ctx context.Context, tx *sql.Tx, itemID, revision, target string) error {
	var found int
	var err error
	if target == "" {
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM studio_items WHERE id=? AND active_revision=? AND active_revision<>'' AND deleted=0 AND archived_at=0`, itemID, revision).Scan(&found)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM widget_pages p JOIN widget_page_pins pin ON pin.item_id=p.item_id AND pin.scope=p.scope AND pin.revision_hash=p.revision_hash JOIN studio_items i ON i.id=p.item_id JOIN studio_item_revisions r ON r.item_id=p.item_id AND r.hash=p.revision_hash WHERE p.target_id=? AND p.item_id=? AND p.revision_hash=? AND i.deleted=0 AND i.archived_at=0 AND r.build_receipt<>''`, target, itemID, revision).Scan(&found)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrStudioItemConflict
	}
	return err
}
func (s *Store) AuthorizeWidgetPage(ctx context.Context, itemID, revision, target string) error {
	if target == "" {
		return store.ErrStudioItemConflict
	}
	return s.tx(ctx, func(tx *sql.Tx) error { return widgetRevisionAllowed(ctx, tx, itemID, revision, target) })
}
