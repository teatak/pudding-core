package sqlitestore

import (
	"database/sql"
	"fmt"
	"strings"
)

func migrateDocuments(tx *sql.Tx) error {
	var ddl string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='studio_items'`).Scan(&ddl); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE type='index' AND tbl_name='studio_items' AND sql IS NOT NULL`)
	if err != nil {
		return err
	}
	var indexes []string
	for rows.Next() {
		var query string
		if err = rows.Scan(&query); err != nil {
			rows.Close()
			return err
		}
		indexes = append(indexes, query)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	ddl = strings.Replace(ddl, "studio_items", "studio_items_rich", 1)
	replaced := false
	for _, old := range []string{"'doc','table','widget','rich-document'", "'doc','table','widget'", "'note','table','widget','document'"} {
		if strings.Contains(ddl, old) {
			ddl = strings.Replace(ddl, old, "'note','table','widget','document'", 1)
			replaced = true
			break
		}
	}
	if !replaced {
		return fmt.Errorf("unexpected Studio kind constraint")
	}
	if _, err = tx.Exec(ddl); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO studio_items_rich SELECT id,CASE kind WHEN 'doc' THEN 'note' WHEN 'rich-document' THEN 'document' ELSE kind END,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at,archived_at,origin FROM studio_items; DROP TABLE studio_items; ALTER TABLE studio_items_rich RENAME TO studio_items;`); err != nil {
		return err
	}
	for _, query := range indexes {
		if _, err = tx.Exec(query); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS studio_rich_blobs (item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,hash TEXT NOT NULL,mime TEXT NOT NULL,data BLOB NOT NULL,PRIMARY KEY(item_id,hash));`); err != nil {
		return err
	}
	check, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer check.Close()
	if check.Next() {
		return fmt.Errorf("foreign key violation after rich document migration")
	}
	return check.Err()
}
