package sqlitestore

import (
	"context"
	"database/sql"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Store) SetStudioItemArchived(ctx context.Context, id string, expected int64, archived bool) (*store.StudioItem, error) {
	var item *store.StudioItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0`, id))
		if err != nil {
			return err
		}
		// Retrying an archive never extends retention or changes its revision.
		if (item.ArchivedAt != nil) == archived {
			return nil
		}
		if item.Revision != expected {
			return store.ErrStudioItemConflict
		}
		var timestamp int64
		item.ArchivedAt = nil
		if archived {
			now := time.Now().UTC().Truncate(time.Millisecond)
			item.ArchivedAt = &now
			timestamp = now.UnixMilli()
		}
		_, err = tx.ExecContext(ctx, `UPDATE studio_items SET archived_at=?,revision=revision+1 WHERE id=?`, timestamp, id)
		item.Revision++
		return err
	})
	return item, err
}

// A tombstone closes access before filesystem cleanup. Failed cleanup remains
// retryable after restart, and a tombstoned item cannot be restored meanwhile.
func (s *Store) MarkStudioItemDeleted(ctx context.Context, id string, expected int64) (*store.StudioItem, error) {
	var item *store.StudioItem
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		item, err = scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=?`, id))
		if err != nil {
			return err
		}
		if item.Deleted {
			return nil
		}
		if item.Revision != expected {
			return store.ErrStudioItemConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE studio_items SET deleted=1,revision=revision+1 WHERE id=?`, id)
		item.Deleted = true
		item.Revision++
		return err
	})
	return item, err
}

func (s *Store) PurgeDeletedStudioItem(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM studio_items WHERE id=? AND deleted=1`, id)
		return err
	})
}

func (s *Store) ListStudioItemsForCleanup(ctx context.Context, cutoff time.Time) ([]*store.StudioItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listStudioItems(ctx, `deleted=1 OR (archived_at>0 AND archived_at<=?) ORDER BY archived_at,id`, unixMS(cutoff))
}
