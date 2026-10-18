package sqlitestore

import (
	"database/sql"
	"encoding/json"

	"github.com/teatak/pudding-core/internal/store"
)

// Convert only working Page snapshots. Immutable revisions, request hashes and
// active leases retain their bytes/IDs. The normal migration backup precedes us.
func migratePageDocuments(tx *sql.Tx) error {
	for _, table := range []string{"studio_item_content", "studio_item_revisions"} {
		if err := addTableColumnIfMissing(tx, table, "content_format", "TEXT NOT NULL DEFAULT 'plain'"); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT c.item_id,c.body FROM studio_item_content c JOIN studio_items i ON i.id=c.item_id WHERE i.kind IN ('page','note','doc') AND c.content_format='plain'`)
	if err != nil {
		return err
	}
	type entry struct{ id, body string }
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.body); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err = store.ValidatePage(e.body); err != nil {
			return err
		}
		raw, err := json.Marshal(store.LegacyPageDocument(e.id, e.body))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE studio_item_content SET body=?,content_format=? WHERE item_id=?`, string(raw), store.PageDocumentFormat, e.id); err != nil {
			return err
		}
	}
	return nil
}
