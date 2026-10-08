package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func migrateWidgetOrigins(tx *sql.Tx) error {
	_, err := tx.Exec(`ALTER TABLE studio_items ADD COLUMN origin TEXT NOT NULL DEFAULT 'null';
CREATE UNIQUE INDEX studio_items_package ON studio_items(json_extract(origin, '$.registryURL'), json_extract(origin, '$.packageID')) WHERE deleted=0 AND json_extract(origin, '$.copy')=0;`)
	return err
}

// expected==0 installs (or returns the canonical install); a positive revision stages an upgrade.
// Source, provenance and item creation commit together. Activation remains an explicit build/activate step.
func (s *Store) InstallWidgetPackage(ctx context.Context, w *store.StudioItem, expected int64) (*store.StudioItem, error) {
	id := w.ID
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if w.Origin == nil || w.Origin.Copy || w.Kind != store.StudioItemKindWidget {
			return errors.New("invalid package item")
		}
		current, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=?`, id))
		if expected == 0 {
			current, err = scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE deleted=0 AND json_extract(origin,'$.registryURL')=? AND json_extract(origin,'$.packageID')=? AND json_extract(origin,'$.copy')=0`, w.Origin.RegistryURL, w.Origin.PackageID))
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if current != nil && (current.Deleted || current.ArchivedAt != nil) {
			return store.ErrStudioItemConflict
		}
		if expected == 0 && current != nil {
			if current.Origin == nil || current.Origin.RegistryURL != w.Origin.RegistryURL || current.Origin.PackageID != w.Origin.PackageID {
				return store.ErrStudioItemConflict
			}
			id = current.ID
			return nil
		}
		base := ""
		if expected > 0 {
			if current == nil || current.Revision != expected || current.Kind != store.StudioItemKindWidget || current.Origin == nil || current.Origin.Copy || current.Origin.RegistryURL != w.Origin.RegistryURL || current.Origin.PackageID != w.Origin.PackageID || current.HeadRevision != current.Origin.SourceHash {
				return store.ErrStudioItemConflict
			}
			if current.Origin.Version == w.Origin.Version {
				if current.Origin.PackageHash != w.Origin.PackageHash {
					return store.ErrStudioItemConflict
				}
				return nil
			}
			base = current.HeadRevision
		} else {
			if err := createStudioItemTx(ctx, tx, w); err != nil {
				return err
			}
		}
		r := &store.StudioItemRevision{ItemID: id, Hash: w.Origin.SourceHash, ClientRequestID: fmt.Sprintf("package:%s:%d", w.Origin.PackageHash, expected), CreatedAt: w.UpdatedAt}
		if err := saveStudioItemRevisionTx(ctx, tx, r, base); err != nil {
			return err
		}
		origin, err := json.Marshal(w.Origin)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE studio_items SET origin=?, icon=? WHERE id=?`, string(origin), w.Icon, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetStudioItem(ctx, id)
}

// ForkWidgetForEditing snapshots package content, bindings and durable data in
// one transaction. The downloaded original retains its independent update path.
func (s *Store) ForkWidgetForEditing(ctx context.Context, sourceID, id, name string, expected int64) (*store.StudioItem, error) {
	err := s.tx(ctx, func(tx *sql.Tx) error {
		source, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND deleted=0 AND archived_at=0`, sourceID))
		if err != nil {
			return err
		}
		if source.Kind != store.StudioItemKindWidget || source.Origin == nil || source.Origin.Copy {
			return store.ErrStudioItemConflict
		}
		existing, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=?`, id))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if existing != nil {
			if existing.Deleted || existing.ArchivedAt != nil || existing.Origin == nil || !existing.Origin.Copy || existing.Origin.RegistryURL != source.Origin.RegistryURL || existing.Origin.PackageID != source.Origin.PackageID {
				return store.ErrStudioItemConflict
			}
			return nil
		}
		if source.Revision != expected {
			return store.ErrStudioItemConflict
		}
		now := time.Now().UTC()
		copy := *source
		origin := *source.Origin
		origin.Copy = true
		copy.Origin = &origin
		copy.ID = id
		copy.Name = name
		copy.CreatedAt = now
		copy.UpdatedAt = now
		if err := createStudioItemTx(ctx, tx, &copy); err != nil {
			return err
		}
		originJSON, err := json.Marshal(copy.Origin)
		if err != nil {
			return err
		}
		bindings, err := json.Marshal(source.Bindings)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE studio_items SET origin=?, head_revision=?, active_revision=?, bindings=? WHERE id=?`, string(originJSON), source.HeadRevision, source.ActiveRevision, string(bindings), id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt) SELECT ?,hash,'','fork:'||hash,?,build_receipt FROM studio_item_revisions WHERE item_id=? AND hash IN (?,?)`, id, unixMS(now), sourceID, source.HeadRevision, source.ActiveRevision); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO widget_data(item_id,version,data) SELECT ?,1,data FROM widget_data WHERE item_id=?`, id, sourceID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetStudioItem(ctx, id)
}
