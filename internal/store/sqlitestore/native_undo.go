package sqlitestore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

// Reconstruct a change only from immutable canonical versions of this item.
// Preserved drafts and versions on another item cannot be undo sources.
func nativeUndoSourceTx(ctx context.Context, tx *sql.Tx, itemID, head, revision string) (string, string, error) {
	var before, after string
	var author, parent string
	err := tx.QueryRowContext(ctx, `SELECT content,author_kind,parent_revision FROM studio_item_revisions WHERE item_id=? AND hash=?`, itemID, revision).Scan(&after, &author, &parent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", store.ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if author != "session" || parent == "" {
		return "", "", &store.InvalidDocument{Message: "undo requires an AI edit with a parent version"}
	}
	var present int
	err = tx.QueryRowContext(ctx, `WITH RECURSIVE ancestry(hash,parent_revision) AS (
 SELECT hash,parent_revision FROM studio_item_revisions WHERE item_id=? AND hash=?
 UNION ALL SELECT r.hash,r.parent_revision FROM studio_item_revisions r JOIN ancestry a ON r.hash=a.parent_revision WHERE r.item_id=?
 ) SELECT count(*) FROM ancestry WHERE hash=?`, itemID, head, itemID, revision).Scan(&present)
	if err != nil {
		return "", "", err
	}
	if present == 0 {
		return "", "", store.ErrUndoConflict
	}
	err = tx.QueryRowContext(ctx, `SELECT content FROM studio_item_revisions WHERE item_id=? AND hash=?`, itemID, parent).Scan(&before)
	return before, after, err
}
