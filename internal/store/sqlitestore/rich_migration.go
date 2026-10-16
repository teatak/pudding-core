package sqlitestore

import (
	"database/sql"
	"fmt"
	"strings"
)

func migrateRichDocuments(tx *sql.Tx) error {
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
	old := "'doc','table','widget'"
	if !strings.Contains(ddl, old) {
		return fmt.Errorf("unexpected Studio kind constraint")
	}
	if !strings.Contains(ddl, "'rich-document'") {
		ddl = strings.Replace(ddl, old, old+",'rich-document'", 1)
	}
	if _, err = tx.Exec(ddl); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO studio_items_rich SELECT * FROM studio_items; DROP TABLE studio_items; ALTER TABLE studio_items_rich RENAME TO studio_items;`); err != nil {
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
