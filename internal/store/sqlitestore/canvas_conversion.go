package sqlitestore

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/teatak/pudding-core/internal/canvaslegacy"
	"github.com/teatak/pudding-core/internal/store"
)

// Packages are installed atomically before their hashes are referenced by SQL.
// A rolled-back transaction can safely reuse the immutable packages on restart.
func convertLegacyCanvases(tx *sql.Tx, home string) error {
	rows, err := tx.Query(`SELECT canvas_id,hash,content_json FROM canvas_revisions WHERE content_json<>'' ORDER BY canvas_id,created_at,hash`)
	if err != nil {
		return err
	}
	type revision struct{ id, old, content string }
	var revisions []revision
	for rows.Next() {
		var r revision
		if err = rows.Scan(&r.id, &r.old, &r.content); err != nil {
			rows.Close()
			return err
		}
		revisions = append(revisions, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	resolveImage := canvaslegacy.Images(home)
	for _, r := range revisions {
		var content store.CanvasContent
		if err = json.Unmarshal([]byte(r.content), &content); err != nil {
			return err
		}
		p, err := canvaslegacy.Convert(content, resolveImage)
		if err != nil {
			return fmt.Errorf("convert canvas %s: %w", r.id, err)
		}
		hash, err := canvaslegacy.WritePackage(home, r.id, p)
		if err != nil {
			return err
		}
		// Carry the existing selection forward, but never fabricate a build receipt.
		// The source runtime builds the converted package normally when it is opened.
		if _, err = tx.Exec(`UPDATE canvas_revisions SET hash=?,content_json='' WHERE canvas_id=? AND hash=?;
   UPDATE canvas_revisions SET parent_revision=? WHERE canvas_id=? AND parent_revision=?;
   UPDATE canvas_saves SET hash=? WHERE canvas_id=? AND hash=?;
   UPDATE canvas_resources SET head_revision=CASE WHEN head_revision=? THEN ? ELSE head_revision END,
    active_revision=CASE WHEN active_revision=? THEN ? ELSE active_revision END WHERE id=?`,
			hash, r.id, r.old, hash, r.id, r.old, hash, r.id, r.old, r.old, hash, r.old, hash, r.id); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`ALTER TABLE canvas_revisions DROP COLUMN content_json; ALTER TABLE canvas_mounts DROP COLUMN window_json;`)
	return err
}
